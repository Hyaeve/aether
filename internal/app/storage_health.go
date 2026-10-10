package app

import (
	"context"
	"encoding/hex"
	"errors"
	"time"
)

type storageHealthState struct {
	Stamp     string    `json:"stamp"`
	State     string    `json:"state"`
	Failure   string    `json:"failure,omitempty"`
	Attempts  int       `json:"attempts"`
	CheckedAt time.Time `json:"checkedAt"`
	NextCheck time.Time `json:"nextCheck"`
}

type storageAuthError struct {
	cause error
	fatal bool
}

type forceTianyiRefreshKey struct{}

type storageHealthCheck struct {
	done chan struct{}
	err  error
}

func (e *storageAuthError) Error() string { return e.cause.Error() }
func (e *storageAuthError) Unwrap() error { return e.cause }
func authStorageError(err error, fatal bool) error {
	return &storageAuthError{cause: err, fatal: fatal}
}
func storageRevision(s Storage) string { sum := usageStamp(s); return hex.EncodeToString(sum[:]) }

func healthInterval(s Storage) time.Duration {
	if s.Type == "quark" {
		return 70 * time.Minute
	}
	// CK and opaque Authorization have no documented OAuth refresh lifetime.
	if nativeTianyi(s) {
		return 45 * time.Minute
	}
	return time.Hour
}

func nextStorageHealth(s Storage, previous *storageHealthState, err error, now time.Time) *storageHealthState {
	h := &storageHealthState{Stamp: storageRevision(s), State: "active", CheckedAt: now, NextCheck: now.Add(healthInterval(s))}
	if err == nil {
		return h
	}
	if previous != nil && previous.Stamp == h.Stamp {
		h.Attempts = previous.Attempts
	}
	var auth *storageAuthError
	if errors.As(err, &auth) {
		h.Failure = "auth"
		h.Attempts++
		if auth.fatal || h.Attempts >= 5 {
			h.State = "expired"
			if !auth.fatal {
				h.State = "failed"
			}
			h.NextCheck = now.Add(24 * time.Hour)
			return h
		}
		steps := []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 30 * time.Minute}
		h.State, h.NextCheck = "cooldown", now.Add(steps[min(h.Attempts-1, len(steps)-1)])
	} else {
		h.Failure, h.State, h.NextCheck = "network", "cooldown", now.Add(time.Minute)
		if previous != nil && previous.Stamp == h.Stamp && (previous.State == "failed" || previous.State == "expired") {
			h.State, h.NextCheck = previous.State, now.Add(24*time.Hour)
		}
	}
	return h
}

func storageAuthBlocked(s Storage, now time.Time) error {
	h := s.Health
	if h == nil || h.Stamp != storageRevision(s) || h.State == "active" {
		return nil
	}
	if h.State == "failed" || h.State == "expired" {
		return errors.New("存储凭据已失效，关联任务暂停，请更新授权或测试连接")
	}
	if now.Before(h.NextCheck) {
		return errors.New("存储处于认证或网络冷却期，请稍后重试")
	}
	return nil
}

func storageHealthDue(s Storage, now time.Time) bool {
	return s.Enabled && (s.Health == nil || s.Health.Stamp != storageRevision(s) || !now.Before(s.Health.NextCheck))
}

func (a *App) checkStorageHealth(ctx context.Context, storage Storage) (result error) {
	a.healthMu.Lock()
	if a.healthChecks == nil {
		a.healthChecks = map[string]*storageHealthCheck{}
	}
	if call := a.healthChecks[storage.ID]; call != nil {
		a.healthMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-call.done:
		}
		current, err := a.store.storage(storage.ID)
		if err != nil {
			return err
		}
		if !current.Enabled || usageStamp(current) != usageStamp(storage) {
			return errors.New("存储配置已更改，请重试")
		}
		return call.err
	}
	call := &storageHealthCheck{done: make(chan struct{})}
	a.healthChecks[storage.ID] = call
	a.healthMu.Unlock()
	defer func() {
		a.healthMu.Lock()
		call.err = result
		delete(a.healthChecks, storage.ID)
		close(call.done)
		a.healthMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	current, err := a.store.storage(storage.ID)
	if err != nil || !current.Enabled || usageStamp(current) != usageStamp(storage) {
		return errors.New("存储配置已更改")
	}
	if nativeTianyi(current) {
		a.tianyiMu.Lock()
		cached := a.tianyiSessions[current.ID]
		if !cached.Expires.IsZero() && time.Until(cached.Expires) <= 15*time.Minute {
			delete(a.tianyiSessions, current.ID)
			if current.Config["refreshToken"] != "" {
				ctx = context.WithValue(ctx, forceTianyiRefreshKey{}, true)
			}
		}
		a.tianyiMu.Unlock()
	}
	checkErr := a.probeStorage(ctx, current)
	var auth *storageAuthError
	if nativeTianyi(current) && errors.As(checkErr, &auth) && !auth.fatal && ctx.Err() == nil {
		checkErr = a.probeStorage(ctx, current)
	}
	if a.ctx.Err() != nil {
		return a.ctx.Err()
	}
	if ctx.Err() == context.Canceled {
		return ctx.Err()
	}
	if nativeTianyi(current) {
		latest, e := a.store.storage(current.ID)
		a.tianyiMu.Lock()
		cached := a.tianyiSessions[current.ID]
		a.tianyiMu.Unlock()
		if e == nil && cached.Credentials == tianyiCredentials(latest) && cached.Key != "" && cached.Credentials != tianyiCredentials(current) {
			current = latest
		}
	}
	if err := a.recordStorageHealthResult(current, checkErr, true); err != nil {
		return err
	}
	if cloudUsageType(current.Type) && checkErr == nil {
		a.usageMu.Lock()
		if entry := a.usageCache[current.ID]; entry != nil {
			select {
			case <-entry.ready:
				entry.expires = time.Time{}
			default:
			}
		}
		a.usageMu.Unlock()
		_, _ = a.cachedStorageUsage(ctx, current)
	}
	return checkErr
}

func (a *App) recordStorageHealth(s Storage, checkErr error) error {
	return a.recordStorageHealthResult(s, checkErr, false)
}

func (a *App) recordStorageHealthResult(s Storage, checkErr error, probe bool) error {
	changed := false
	matched := false
	err := a.store.update(func(st *State) error {
		for i := range st.Storages {
			current := &st.Storages[i]
			if current.ID != s.ID || !current.Enabled || usageStamp(*current) != usageStamp(s) {
				continue
			}
			matched = true
			now := time.Now()
			var auth *storageAuthError
			fatal := errors.As(checkErr, &auth) && auth.fatal
			// Passive requests share the current cooldown instead of exhausting retries.
			if !probe && checkErr != nil && !fatal && current.Health != nil && current.Health.Stamp == storageRevision(*current) && now.Before(current.Health.NextCheck) && current.Health.State != "active" {
				continue
			}
			status, message := "connected", ""
			if checkErr != nil {
				status, message = "error", "健康检查失败，连接暂不可用，将按退避计划重试"
				var auth *storageAuthError
				if errors.As(checkErr, &auth) {
					message = "认证检查失败，请检查授权；关联任务暂缓执行"
				}
			}
			changed = current.Status != status
			current.Health = nextStorageHealth(*current, current.Health, checkErr, now)
			current.Status, current.LastError = status, message
		}
		if !matched && probe {
			return errors.New("存储配置已更改，请重试")
		}
		return nil
	})
	if err == nil && changed {
		level, text := "info", "健康检查恢复正常"
		if checkErr != nil {
			level, text = "warn", "健康检查异常，已安排退避检查"
		}
		a.store.event(level, "storage", s.Name+"："+text)
	}
	return err
}

func (a *App) storageHealthLoop() {
	defer a.wg.Done()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
		}
		for _, s := range a.store.snapshotWithLogLimit(0).Storages {
			if !storageHealthDue(s, time.Now()) {
				continue
			}
			_ = a.checkStorageHealth(a.ctx, s)
			select {
			case <-a.ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
		// Inspect local deadlines without making a request on every tick.
		timer.Reset(15 * time.Second)
	}
}
