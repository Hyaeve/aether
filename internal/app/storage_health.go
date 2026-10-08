package app

import (
	"context"
	"encoding/json"
	"time"
)

func (a *App) checkStorageHealth(ctx context.Context, storage Storage) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, checkErr := a.listFiles(ctx, storage, rootOf(storage), 0, true)
	if a.ctx.Err() != nil {
		return
	}
	status, message := "connected", ""
	if checkErr != nil {
		status, message = "error", "健康检查失败，请检查凭据、网络及上游服务"
	}
	changed := false
	err := a.store.update(func(st *State) error {
		for i := range st.Storages {
			s := &st.Storages[i]
			before, _ := json.Marshal(storage.Config)
			after, _ := json.Marshal(s.Config)
			if s.ID == storage.ID && s.Enabled && s.Type == storage.Type && string(before) == string(after) {
				changed = s.Status != status
				s.Status, s.LastError = status, message
			}
		}
		return nil
	})
	if err == nil && changed {
		level, text := "info", "健康检查恢复正常"
		if checkErr != nil {
			level, text = "warn", message
		}
		a.store.event(level, "storage", storage.Name+"："+text)
	}
}

func (a *App) storageHealthLoop() {
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-timer.C:
		}
		for _, storage := range a.store.snapshotWithLogLimit(0).Storages {
			if !storage.Enabled {
				continue
			}
			a.checkStorageHealth(a.ctx, storage)
			select {
			case <-a.ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
		timer.Reset(time.Hour)
	}
}
