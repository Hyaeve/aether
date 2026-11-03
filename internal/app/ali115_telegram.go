package app

import (
	"context"
	"errors"
	"time"
)

func ali115TelegramTargets(st State) []telegramTarget {
	out := []telegramTarget{}
	for _, t := range st.Ali115.Targets {
		for _, s := range st.Storages {
			if s.ID == t.StorageID && s.Enabled && s.Type == "115" {
				out = append(out, telegramTarget{ID: t.ID, Provider: "115", StorageID: t.StorageID, Parent: t.Directory, Path: t.Path})
			}
		}
	}
	return out
}

func (a *App) executeTelegramAli115(ctx context.Context, job telegramTransfer) {
	if !a.ali115Gate.TryLock() {
		telegramPause(ctx, time.Second)
		return
	}
	defer a.ali115Gate.Unlock()
	if job.Selected < 0 || job.Selected >= len(job.Targets) {
		a.finishTelegram(job.ID, "failed", "阿里云盘秒传接收目录无效", 0)
		return
	}
	target := job.Targets[job.Selected]
	err := a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			current := &st.TelegramRuntime.Jobs[i]
			if current.ID == job.ID && current.Status == "queued" && telegramTargetCurrent(*st, *current, target) {
				current.Status = "running"
				return nil
			}
		}
		return errors.New("TG秒传任务或授权已变化，未执行")
	})
	if err != nil {
		a.finishTelegram(job.ID, "failed", err.Error(), 0)
		return
	}
	guard := func() error {
		if err := a.telegramExecutionValid(job, target, ""); err != nil {
			return err
		}
		for _, r := range a.store.snapshotWithLogLimit(0).TelegramRuntime.Jobs {
			if r.ID == job.ID && r.Status == "running" {
				return nil
			}
		}
		return errors.New("TG秒传任务已结束，未继续写入")
	}
	receipt, err := a.newAli115Receipt(target.ID, job.Link, "TG Bot", job.ID)
	status, message, count := "failed", "阿里云盘秒传未开始", 0
	name := "阿里云盘分享"
	if err == nil {
		a.runAli115(ctx, receipt, guard)
		for _, r := range a.store.snapshotWithLogLimit(0).Ali115Receipts {
			if r.ID == receipt.ID {
				if r.Name != "" {
					name = r.Name
				}
				status, message, count = r.Status, "阿里云盘"+ali115Summary(r)+"；"+r.Message, r.Success+r.Skipped
			}
		}
	} else {
		message = err.Error()
	}
	a.telegramComplete(ctx, job, status, name, count, 0, message)
}
