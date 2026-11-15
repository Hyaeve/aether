package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	telegramPromptShareLimit  = 5
	telegramShareNameTimeout  = 1500 * time.Millisecond
	telegramBatchNamesTimeout = 3 * time.Second
)

func telegramPromptShares(st State, job telegramTransfer) []telegramTransfer {
	shares := []telegramTransfer{job}
	for _, member := range st.TelegramRuntime.Jobs {
		if len(shares) == telegramPromptShareLimit || len(shares) >= job.GroupSize {
			break
		}
		if member.ID != job.ID && telegramSameGroup(job, member) {
			shares = append(shares, member)
		}
	}
	return shares
}

func telegramPromptShareNames(st State, job telegramTransfer) string {
	shares := telegramPromptShares(st, job)
	names := make([]string, 0, len(shares))
	for _, share := range shares {
		name := telegramLimit(share.Name, 512)
		if name == "" {
			name = "暂未获取"
		}
		names = append(names, name)
	}
	if job.GroupSize <= 1 {
		return "分享名称：" + names[0] + "\n"
	}
	if job.GroupSize > len(shares) {
		names = append(names, "…")
	}
	return "分享名称：\n" + strings.Join(names, "\n") + "\n"
}

func sameTelegramPrompt(current, expected telegramTransfer) bool {
	statusMatches := current.Status == expected.Status
	if expected.Status == "queued" {
		switch current.Status {
		case "running", "submitted", "completed", "success", "failed", "interrupted":
			// A background task may advance before its accepted-queue message is edited.
			statusMatches = true
		}
	}
	return current.ID == expected.ID && current.ChatID == expected.ChatID && current.UserID == expected.UserID &&
		current.MessageID == expected.MessageID && current.Auth == expected.Auth && statusMatches &&
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
			if len(current.Targets) == 0 {
				break
			}
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
		expected, err = a.prepareTelegramPrompt(ctx, expected)
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

func (a *App) prepareTelegramPrompt(ctx context.Context, expected telegramTransfer) (telegramTransfer, error) {
	if ctx.Err() != nil {
		return telegramTransfer{}, ctx.Err()
	}
	st := a.store.snapshotWithLogLimit(0)
	job, err := currentTelegramPrompt(st, expected)
	if err != nil || job.NamesLoaded || job.Kind == "ali-rapid" || job.Status == "choosing" && job.Kind == "ed2k" {
		return job, err
	}
	// The first configured destination supplies read credentials before selection.
	target := job.Targets[max(0, job.Selected)]
	shares := telegramPromptShares(st, job)
	budget := telegramShareNameTimeout
	if len(shares) > 1 {
		budget = telegramBatchNamesTimeout
	}
	lookupCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	names := make(map[string]string, telegramPromptShareLimit)
	for _, share := range shares {
		if lookupCtx.Err() != nil {
			break
		}
		if share.Name == "" {
			if name := a.telegramResourceName(lookupCtx, share, target); name != "" {
				names[share.ID] = name
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return telegramTransfer{}, err
	}
	if len(names) == 0 && job.MessageID > 0 {
		return currentTelegramPrompt(a.store.snapshotWithLogLimit(0), expected)
	}
	err = a.store.update(func(st *State) error {
		current, err := currentTelegramPrompt(*st, expected)
		if err != nil {
			return err
		}
		if current.NamesLoaded {
			job = current
			return nil
		}
		for i := range st.TelegramRuntime.Jobs {
			member := &st.TelegramRuntime.Jobs[i]
			if member.Name == "" && (member.ID == current.ID || telegramSameGroup(current, *member) && member.Status == "waiting") {
				if name := names[member.ID]; name != "" {
					member.Name = name
				}
			}
			if member.ID == current.ID {
				member.NamesLoaded = true
				job = *member
			}
		}
		return nil
	})
	return job, err
}
