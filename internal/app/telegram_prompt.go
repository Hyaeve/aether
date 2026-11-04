package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

func sameTelegramPrompt(current, expected telegramTransfer) bool {
	return current.ID == expected.ID && current.ChatID == expected.ChatID && current.UserID == expected.UserID &&
		current.MessageID == expected.MessageID && current.Auth == expected.Auth && current.Status == expected.Status &&
		current.Page == expected.Page && current.Selected == expected.Selected && current.LastActivity.Equal(expected.LastActivity)
}

func currentTelegramPrompt(st State, expected telegramTransfer) (telegramTransfer, error) {
	for _, current := range st.TelegramRuntime.Jobs {
		if !sameTelegramPrompt(current, expected) {
			continue
		}
		cfg := st.Notifications.Telegram
		if telegramDigest(cfg) != current.Auth || !telegramAllowed(cfg, current.ChatID, current.UserID) || telegramInteractionExpired(current, time.Now()) {
			break
		}
		if current.Status == "choosing" {
			for _, target := range current.Targets {
				if !telegramTargetCurrent(st, current, target) {
					return telegramTransfer{}, errors.New("目录配置已变化")
				}
			}
		} else if current.Selected < 0 || current.Selected >= len(current.Targets) || !telegramTargetCurrent(st, current, current.Targets[current.Selected]) {
			break
		}
		return current, nil
	}
	return telegramTransfer{}, errors.New("交互状态已变化，未覆盖较新的消息")
}

// Optional title edits and explicit actions serialize for the same message.
// A later callback rechecks state after acquiring the lock, so it always wins.
func (a *App) editTelegramPrompt(ctx context.Context, cfg telegramConfig, expected telegramTransfer, optional bool) error {
	key := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", expected.ChatID, expected.MessageID)))
	guard := &a.telegramPromptMu[int(key[0])%len(a.telegramPromptMu)]
	if optional {
		if !guard.TryLock() {
			return errors.New("交互消息正在更新，跳过可选名称补充")
		}
	} else {
		guard.Lock()
	}
	defer guard.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	st := a.store.snapshotWithLogLimit(0)
	current, err := currentTelegramPrompt(st, expected)
	if err != nil {
		return err
	}
	if telegramDigest(cfg) != current.Auth {
		return errors.New("TG授权已变化")
	}
	text := "已加入" + telegramAction(current.Kind) + "队列"
	keyboard := [][]map[string]string{}
	switch current.Status {
	case "choosing":
		text, keyboard = telegramDirectoryPage(st, current)
	case "confirming":
		text, keyboard = telegramConfirmation(st, current)
	case "cancelled":
		text = "已取消" + telegramAction(current.Kind)
	}
	return telegramRequest(ctx, cfg, "editMessageText", map[string]any{"chat_id": current.ChatID, "message_id": current.MessageID, "text": text, "reply_markup": map[string]any{"inline_keyboard": keyboard}}, nil)
}

func (a *App) queueTelegramTitle(job telegramTransfer) {
	if job.Status != "confirming" || job.Name != "" || job.Provider == "" || job.Kind == "ali-rapid" {
		return
	}
	select {
	case a.telegramTitles <- job:
	default:
	} // Optional metadata never blocks an interaction.
}

func (a *App) telegramTitleWorker() {
	defer a.wg.Done()
	for a.ctx.Err() == nil {
		select {
		case <-a.ctx.Done():
			return
		case job := <-a.telegramTitles:
			a.enrichTelegramPrompt(a.ctx, job)
		}
	}
}

func (a *App) enrichTelegramPrompt(ctx context.Context, expected telegramTransfer) {
	if ctx.Err() != nil {
		return
	}
	st := a.store.snapshotWithLogLimit(0)
	job, err := currentTelegramPrompt(st, expected)
	if err != nil || job.Status != "confirming" || job.Name != "" {
		return
	}
	target := job.Targets[job.Selected]
	name := a.telegramResourceName(ctx, job, target)
	if name == "" || ctx.Err() != nil {
		return
	}
	err = a.store.update(func(st *State) error {
		current, err := currentTelegramPrompt(*st, expected)
		if err != nil {
			return err
		}
		if current.Name != "" {
			return errors.New("分享名称已补充")
		}
		for i := range st.TelegramRuntime.Jobs {
			if st.TelegramRuntime.Jobs[i].ID == current.ID {
				st.TelegramRuntime.Jobs[i].Name = name
				expected = st.TelegramRuntime.Jobs[i]
				return nil
			}
		}
		return errors.New("交互记录不存在")
	})
	if err != nil {
		return
	}
	editCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	// A failed/unknown enrichment is not replayed. The first confirmation stays valid.
	_ = a.editTelegramPrompt(editCtx, st.Notifications.Telegram, expected, true)
}
