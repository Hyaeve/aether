package app

import (
	"context"
	"errors"
	"fmt"
)

var errSTRMSyncQueued = errors.New("全量同步已排队，等待当前生活事件轮询结束")

type queuedSTRMSync struct{ config, storage string }
type queuedSTRMSyncKey struct{}

func syncQueueStamp(t Task) string {
	return archiveStamp(strmTaskConfig(t), t.Cron, fmt.Sprint(t.Enabled))
}

// Called with runMu held, only for an actual background life-event poll.
func (a *App) queueSTRMSync(t Task) error {
	if _, ok := a.pendingSTRMSync[t.ID]; ok {
		return errors.New("该STRM任务的全量同步已在排队，请勿重复提交")
	}
	s, err := a.store.storage(t.StorageID)
	if err != nil {
		return err
	}
	if a.pendingSTRMSync == nil {
		a.pendingSTRMSync = map[string]queuedSTRMSync{}
	}
	a.pendingSTRMSync[t.ID] = queuedSTRMSync{syncQueueStamp(t), storageRevision(s)}
	return errSTRMSyncQueued
}

func (a *App) drainSTRMSync(taskID string) {
	a.runMu.Lock()
	queued, ok := a.pendingSTRMSync[taskID]
	a.runMu.Unlock()
	if !ok || a.ctx.Err() != nil {
		return
	}
	ctx := context.WithValue(a.ctx, queuedSTRMSyncKey{}, queued)
	if err := a.startTaskWithTrigger(ctx, taskID, nil, "full-sync"); err != nil {
		a.store.event("warn", "tasks", "排队的STRM全量同步未启动："+err.Error())
	}
}
