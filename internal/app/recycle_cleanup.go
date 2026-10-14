package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type recycleCleanupRule struct {
	StorageID string `json:"storageId"`
	PIN       string `json:"recyclePIN"`
	Cron      string `json:"cron"`
	Days      int    `json:"days"`
}
type recycleCleanupSettings struct {
	Enabled bool                 `json:"enabled"`
	Rules   []recycleCleanupRule `json:"rules"`
}

func validateRecycleCleanup(v recycleCleanupSettings, st State) error {
	if len(v.Rules) > 100 {
		return errors.New("清理规则过多")
	}
	seen := map[string]bool{}
	for _, rule := range v.Rules {
		if seen[rule.StorageID] {
			return errors.New("每个115存储只允许一个清理任务")
		}
		seen[rule.StorageID] = true
		found := false
		for _, s := range st.Storages {
			if s.ID == rule.StorageID && s.Type == "115" {
				found = true
			}
		}
		if !found {
			return errors.New("请选择115存储池")
		}
		if len(rule.PIN) != 4 || strings.Trim(rule.PIN, "0123456789") != "" {
			return errors.New("回收站密码须为4位数字")
		}
		if rule.Days < 1 || rule.Days > 3650 {
			return errors.New("保留天数须为1至3650，10天不是上限")
		}
		if len(rule.Cron) > 128 {
			return errors.New("Cron过长")
		}
		if _, err := cronParser.Parse(rule.Cron); err != nil {
			return errors.New("Cron表达式无效")
		}
	}
	return nil
}
func (a *App) recycleCleanupAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		value := a.store.snapshotWithLogLimit(0).RecycleCleanup
		value.Rules = append([]recycleCleanupRule(nil), value.Rules...)
		for i := range value.Rules {
			value.Rules[i].PIN = ""
		}
		jsonResponse(w, 200, value)
		return
	}
	if r.Method != http.MethodPut {
		w.WriteHeader(405)
		return
	}
	var value recycleCleanupSettings
	if !decode(w, r, &value) {
		return
	}
	err := a.store.update(func(st *State) error {
		for i := range value.Rules {
			if value.Rules[i].PIN == "" {
				for _, old := range st.RecycleCleanup.Rules {
					if old.StorageID == value.Rules[i].StorageID {
						value.Rules[i].PIN = old.PIN
					}
				}
			}
		}
		if err := validateRecycleCleanup(value, *st); err != nil {
			return err
		}
		st.RecycleCleanup = value
		return nil
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (a *App) simulcastToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		v := a.store.snapshotWithLogLimit(0).SimulcastEnabled
		jsonResponse(w, 200, map[string]bool{"enabled": v == nil || *v})
		return
	}
	var v struct {
		Enabled bool `json:"enabled"`
	}
	if r.Method != http.MethodPut || !decode(w, r, &v) {
		return
	}
	if err := a.store.update(func(st *State) error { st.SimulcastEnabled = &v.Enabled; return nil }); err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, v)
}
func (a *App) cleanup115Recycle(ctx context.Context, rule recycleCleanupRule) error {
	if err := a.acquireCAS(ctx); err != nil {
		return err
	}
	defer func() { <-a.casGate }()
	st := a.store.snapshotWithLogLimit(0)
	s, err := backupStorage(st, rule.StorageID)
	if err != nil {
		return err
	}
	if err = storageAuthBlocked(s, time.Now()); err != nil {
		return err
	}
	rows, err := a.cloudManagementList(ctx, s, "trash")
	if err != nil {
		return err
	}
	if err = validateCloudRows(rows); err != nil {
		return err
	}
	cutoff := time.Now().Add(-time.Duration(rule.Days) * 24 * time.Hour)
	ids := []string{}
	for _, row := range rows {
		if row.Time.After(time.Unix(0, 0)) && row.Time.Before(cutoff) {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	c, err := client115(ctx, s)
	if err != nil {
		return err
	}
	for start := 0; start < len(ids); start += 499 {
		current := a.store.snapshotWithLogLimit(0)
		if !current.RecycleCleanup.Enabled || rapid115Stamp(current.RecycleCleanup) != rapid115Stamp(st.RecycleCleanup) {
			return errors.New("清理配置已变化，已停止")
		}
		pool, err := a.store.storage(s.ID)
		if err != nil || !pool.Enabled || rapid115Stamp(pool.Config) != rapid115Stamp(s.Config) {
			return errors.New("存储配置已变化，已停止")
		}
		if err = a.cloudStorageCurrent(s); err != nil {
			return err
		}
		if err = c.CleanRecycleBin(rule.PIN, ids[start:min(start+499, len(ids))]...); err != nil {
			return errors.New("清理结果未确认，未自动重试")
		}
	}
	a.store.event("info", "storage", fmt.Sprintf("115回收站计划清理：%s，%d项，保留%d天", s.Name, len(ids), rule.Days))
	return nil
}
func (a *App) recycleCleanupMonitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	last := map[string]time.Time{}
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-ticker.C:
			settings := a.store.snapshotWithLogLimit(0).RecycleCleanup
			if !settings.Enabled {
				continue
			}
			for _, rule := range settings.Rules {
				cron, err := cronParser.Parse(rule.Cron)
				if err != nil {
					continue
				}
				boundary := now.Truncate(time.Minute)
				key := rule.StorageID + "\x00" + rule.Cron
				if last[key].Equal(boundary) || cron.Next(boundary.Add(-time.Minute)).After(now) {
					continue
				}
				last[key] = boundary
				ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
				err = a.cleanup115Recycle(ctx, rule)
				cancel()
				if err != nil {
					a.store.event("error", "storage", "115回收站计划清理失败："+rule.StorageID+"；"+err.Error())
				}
			}
		}
	}
}
