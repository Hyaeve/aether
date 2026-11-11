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

// Fetch the name before locking the message so newer actions can proceed.
func (a *App) editTelegramPrompt(ctx context.Context, cfg telegramConfig, expected telegramTransfer) error {
	if expected.Status == "confirming" {
		var err error
		expected, err = a.prepareTelegramConfirmation(ctx, expected)
		if err != nil {
			return err
		}
	}
	key := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", expected.ChatID, expected.MessageID)))
	guard := &a.telegramPromptMu[int(key[0])%len(a.telegramPromptMu)]
	guard.Lock()
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

func (a *App) prepareTelegramConfirmation(ctx context.Context, expected telegramTransfer) (telegramTransfer, error) {
	if ctx.Err() != nil {
		return telegramTransfer{}, ctx.Err()
	}
	job, err := currentTelegramPrompt(a.store.snapshotWithLogLimit(0), expected)
	if err != nil || job.Name != "" || job.Kind == "ali-rapid" {
		return job, err
	}
	name := a.telegramResourceName(ctx, job, job.Targets[job.Selected])
	if err := ctx.Err(); err != nil {
		return telegramTransfer{}, err
	}
	if name == "" {
		return currentTelegramPrompt(a.store.snapshotWithLogLimit(0), expected)
	}
	err = a.store.update(func(st *State) error {
		current, err := currentTelegramPrompt(*st, expected)
		if err != nil {
			return err
		}
		if current.Name != "" {
			job = current
			return nil
		}
		for i := range st.TelegramRuntime.Jobs {
			if st.TelegramRuntime.Jobs[i].ID == current.ID {
				st.TelegramRuntime.Jobs[i].Name = name
				job = st.TelegramRuntime.Jobs[i]
				return nil
			}
		}
		return errors.New("交互记录不存在")
	})
	return job, err
}
