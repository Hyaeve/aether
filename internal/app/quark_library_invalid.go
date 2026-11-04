package app

import (
	"context"
	"errors"
	"time"
)

// Maintenance may recheck invalidity and retire outputs, but never generates an
// archived card. Only its explicit execution or nonempty Cron can do that.
func (a *App) maintainInvalidQuarkLibrary(ctx context.Context, card quarkLibraryCard, now time.Time) {
	st := a.store.snapshotWithLogLimit(0)
	result := st.QuarkLibraryRuntime.Results[card.ID]
	if result.Status != "invalid" || result.InvalidSince.IsZero() {
		return
	}
	last := result.InvalidCheckAt
	if last.IsZero() {
		last = result.LastRun
	}
	if last.Add(time.Hour).After(now) {
		return
	}
	result.InvalidCheckAt = now
	s, err := a.quarkLibraryStorage(st)
	if err == nil {
		_, _, _, err = a.quarkLibraryRead(ctx, &s, card.Secret, false)
	}
	if err == nil {
		result.InvalidSince = time.Time{}
		result.Status, result.Message = "idle", "分享已恢复，等待执行"
	} else {
		var unavailable *quarkShareUnavailable
		if errors.As(err, &unavailable) {
			removed, e := a.cleanInvalidQuarkLibrary(ctx, card)
			result.Removed += removed
			if e != nil {
				result.Message = "分享已失效；产物清理待重试：" + e.Error()
			}
			if e == nil && !result.InvalidSince.Add(time.Duration(effectiveQuarkLibrary(st).InvalidDays)*24*time.Hour).After(now) {
				if e = a.expireQuarkLibraryOutput(ctx, card); e == nil {
					_ = a.store.update(func(st *State) error {
						current, ok := quarkLibraryCardAt(*st, card.ID)
						if !ok || current != card {
							return errors.New("夸克卡片已改变")
						}
						for i, c := range st.QuarkLibraryCards {
							if c.ID == card.ID {
								st.QuarkLibraryCards = append(st.QuarkLibraryCards[:i], st.QuarkLibraryCards[i+1:]...)
								break
							}
						}
						revokeQuarkLibraryCard(st, card.ID)
						return nil
					})
					return
				}
				result.Message = "分享已失效；到期清理待重试：" + e.Error()
			}
		}
	}
	_ = a.store.update(func(st *State) error {
		current, ok := quarkLibraryCardAt(*st, card.ID)
		if !ok || current != card {
			return errors.New("夸克卡片已改变")
		}
		st.QuarkLibraryRuntime.Results[card.ID] = result
		return nil
	})
}

func (a *App) markQuarkLibraryInvalid(card quarkLibraryCard, err error) {
	var unavailable *quarkShareUnavailable
	if !errors.As(err, &unavailable) {
		return
	}
	if !a.quarkLibraryMu.TryLock() {
		return
	}
	defer a.quarkLibraryMu.Unlock()
	st := a.store.snapshotWithLogLimit(0)
	current, ok := quarkLibraryCardAt(st, card.ID)
	if !ok || current != card {
		return
	}
	result := st.QuarkLibraryRuntime.Results[card.ID]
	if result.Status == "running" {
		return
	}
	if result.InvalidSince.IsZero() {
		result.InvalidSince = time.Now()
	}
	result.Status, result.Message = "invalid", unavailable.Error()
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	removed, e := a.cleanInvalidQuarkLibrary(ctx, card)
	result.Removed += removed
	if e != nil {
		result.Message += "；清理待重试：" + e.Error()
	}
	_ = a.store.update(func(st *State) error {
		current, ok := quarkLibraryCardAt(*st, card.ID)
		if !ok || current != card {
			return errors.New("卡片已改变")
		}
		initQuarkLibraryRuntime(st)
		st.QuarkLibraryRuntime.Results[card.ID] = result
		return nil
	})
}
