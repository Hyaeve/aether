package app

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func telegramInteractionExpired(job telegramTransfer, now time.Time) bool {
	last := job.LastActivity
	if last.IsZero() {
		last = job.Created
	}
	return !now.Before(last.Add(10 * time.Minute))
}

func telegramCleanupOwnBot(cfg telegramConfig, job telegramTransfer) bool {
	if !cfg.Enabled || cfg.Token == "" {
		return false
	}
	if job.Bot != "" {
		return job.Bot == telegramDigest(cfg.Token)
	}
	return job.Auth == telegramDigest(cfg)
}

func telegramCleanupDue(job telegramTransfer, now time.Time) bool {
	if !telegramInteractionExpired(job, now) {
		return false
	}
	return job.Status == "choosing" || job.Status == "confirming" || job.CleanupPending && (job.CleanupAttempt.IsZero() || now.Sub(job.CleanupAttempt) >= time.Minute)
}

func (a *App) telegramInteractionCleaner() {
	defer a.wg.Done()
	for a.ctx.Err() == nil {
		a.cleanupTelegramInteractions(a.ctx, time.Now())
		if !telegramPause(a.ctx, 5*time.Second) {
			return
		}
	}
}

func (a *App) cleanupTelegramInteractions(ctx context.Context, now time.Time) {
	st := a.store.snapshotWithLogLimit(0)
	due := false
	for _, job := range st.TelegramRuntime.Jobs {
		if telegramCleanupDue(job, now) {
			due = true
			break
		}
	}
	if !due {
		return
	}
	var expired []string
	var abandoned []string
	var remove []telegramTransfer
	// Claim and clear share capabilities before making an idempotent message deletion.
	err := a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			job := &st.TelegramRuntime.Jobs[i]
			if !telegramCleanupDue(*job, now) {
				continue
			}
			if job.Status == "choosing" || job.Status == "confirming" {
				job.Status, job.Link = "expired", ""
				job.CleanupPending = job.MessageID > 0
				expired = append(expired, job.ID)
				for j := range st.TelegramRuntime.Jobs {
					member := &st.TelegramRuntime.Jobs[j]
					if member.ID != job.ID && telegramSameGroup(*job, *member) && member.Status == "waiting" {
						member.Status, member.Link = "expired", ""
					}
				}
			}
			if !job.CleanupPending {
				continue
			}
			job.CleanupAttempt = now
			if now.Sub(job.Created) >= 48*time.Hour {
				job.CleanupPending = false
				abandoned = append(abandoned, job.ID)
				continue
			}
			if telegramCleanupOwnBot(st.Notifications.Telegram, *job) {
				remove = append(remove, *job)
			}
		}
		return nil
	})
	if err != nil {
		a.store.event("error", "notifications", "TG 交互超时状态保存失败，未删除消息")
		return
	}
	for _, key := range expired {
		a.store.event("info", "notifications", fmt.Sprintf("[TG:%s] 交互连续10分钟未操作，已失效，未转存", key))
	}
	for _, key := range abandoned {
		a.store.event("warn", "notifications", fmt.Sprintf("[TG:%s] 超时交互消息已超过TG的48小时删除期限，停止自动清理", key))
	}
	var group sync.WaitGroup
	gate := make(chan struct{}, 4)
	for _, job := range remove {
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			group.Wait()
			return
		}
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() { <-gate }()
			cfg := a.store.snapshotWithLogLimit(0).Notifications.Telegram
			if !telegramCleanupOwnBot(cfg, job) {
				return
			}
			requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			// deleteMessages skips missing IDs, so a lost deletion response is safe to retry.
			err := telegramRequest(requestCtx, cfg, "deleteMessages", map[string]any{"chat_id": job.ChatID, "message_ids": []int64{job.MessageID}}, nil)
			if err != nil {
				if ctx.Err() == nil {
					a.store.event("warn", "notifications", fmt.Sprintf("[TG:%s] 超时交互消息清理未确认，将稍后重试：%s", job.ID, err))
				}
				return
			}
			if err := a.store.update(func(st *State) error {
				for i := range st.TelegramRuntime.Jobs {
					current := &st.TelegramRuntime.Jobs[i]
					if current.ID == job.ID && current.MessageID == job.MessageID {
						current.CleanupPending = false
					}
				}
				return nil
			}); err != nil {
				a.store.event("warn", "notifications", fmt.Sprintf("[TG:%s] 消息已清理但回执保存失败", job.ID))
			} else {
				a.store.event("info", "notifications", fmt.Sprintf("[TG:%s] 超时交互消息已清理", job.ID))
			}
		}()
	}
	group.Wait()
}
