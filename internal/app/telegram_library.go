package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func telegramLibraryStamp(st State) string {
	cfg := effectiveQuarkLibrary(st)
	revisions := []string{}
	for _, sid := range cfg.StorageIDs {
		for _, s := range st.Storages {
			if sid == s.ID {
				revisions = append(revisions, sid+":"+storageRevision(s))
			}
		}
	}
	return telegramDigest(struct {
		Settings  quarkLibrarySettings
		Revisions []string
	}{cfg, revisions})
}

func telegramLibraryValid(st State, job telegramTransfer) bool {
	if telegramDigest(st.Notifications.Telegram) != job.Auth || !telegramAllowed(st.Notifications.Telegram, job.ChatID, job.UserID) {
		return false
	}
	for _, current := range st.TelegramRuntime.Jobs {
		if current.ID == job.ID && current.Kind == "library" && telegramActive(current.Status) &&
			current.Auth == job.Auth && current.ChatID == job.ChatID && current.UserID == job.UserID && current.Link == job.Link {
			return current.LibraryStamp == telegramLibraryStamp(st)
		}
	}
	return false
}

func refreshTelegramLibraryStamp(st *State, previous string) {
	next := telegramLibraryStamp(*st)
	for i := range st.TelegramRuntime.Jobs {
		job := &st.TelegramRuntime.Jobs[i]
		if job.Kind == "library" && telegramActive(job.Status) && job.LibraryStamp == previous {
			job.LibraryStamp = next
		}
	}
}

func (a *App) executeTelegramLibrary(ctx context.Context, job telegramTransfer) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	claimed := false
	err := a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			current := &st.TelegramRuntime.Jobs[i]
			if current.ID == job.ID && current.Kind == "library" && current.Status == "queued" && current.Link == job.Link {
				current.Status = "running"
				claimed = true
				return nil
			}
		}
		return errors.New("TG 链库任务已处理")
	})
	if err != nil || !claimed {
		return
	}
	finish := func(status, message string, count int) {
		message = telegramProviderName(job.Provider) + "分享STRM：" + message
		a.finishTelegram(job.ID, status, message, count)
		st := a.store.snapshotWithLogLimit(0)
		if telegramDigest(st.Notifications.Telegram) == job.Auth && telegramAllowed(st.Notifications.Telegram, job.ChatID, job.UserID) {
			if _, err := a.telegramSend(ctx, st.Notifications.Telegram, job.ChatID, message, nil); err != nil {
				a.store.event("warn", "notifications", "TG 链库结果通知未确认，请查看运行日志")
			}
		}
	}
	if !telegramLibraryValid(a.store.snapshotWithLogLimit(0), job) {
		finish("failed", "授权或链库默认设置已变化，未生成，请重新发送命令", 0)
		return
	}
	if job.Provider != "quark" {
		finish("failed", "该类型的机器人链库生成暂未接入，未转存或生成", 0)
		return
	}
	provider, code, pass, err := parseShareLink(job.Link, "")
	if err != nil || provider != "quark" {
		finish("failed", "无效的夸克分享链接", 0)
		return
	}
	for !a.quarkLibraryMu.TryLock() {
		if !telegramPause(ctx, 200*time.Millisecond) {
			finish("interrupted", "等待链库执行时已中断，未生成", 0)
			return
		}
	}
	defer a.quarkLibraryMu.Unlock()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !telegramLibraryValid(a.store.snapshotWithLogLimit(0), job) {
			return errors.New("TG 授权或链库默认设置已变化，操作停止")
		}
		return nil
	}
	if err := check(); err != nil {
		finish("failed", err.Error(), 0)
		return
	}
	st := a.store.snapshotWithLogLimit(0)
	cfg := effectiveQuarkLibrary(st)
	var card quarkLibraryCard
	for _, existing := range st.QuarkLibraryCards {
		_, existingCode, existingPass, _ := parseShareLink(existing.Secret, "")
		if existingCode == code {
			if existingPass != pass {
				finish("failed", existing.Name+" 已存在且提取码不同，请在卡片中编辑，未覆盖", 0)
				return
			}
			card = existing
			break
		}
	}
	if card.ID == "" {
		title := "夸克分享-" + quarkLibraryStamp(code)[:8]
		s, readErr := a.quarkLibraryStorage(st)
		if readErr == nil {
			readCtx, stop := context.WithTimeout(ctx, 30*time.Second)
			title, _, _, readErr = a.quarkLibraryRead(readCtx, &s, job.Link, false)
			stop()
			if readErr != nil {
				finish("failed", "读取分享失败："+readErr.Error(), 0)
				return
			}
		}
		directoryName := title
		if !validShareDirectory(directoryName) || directoryName == "" {
			directoryName = "夸克分享-" + quarkLibraryStamp(code)[:8]
		}
		card = quarkLibraryCard{ID: id(), Secret: job.Link, Name: title, ShareName: title, Directory: cfg.Directory,
			DirectoryName: directoryName, Enabled: cfg.Directory != "" && readErr == nil}
		if err := validateQuarkLibraryCard(&card); err != nil {
			finish("failed", err.Error(), 0)
			return
		}
		if err := check(); err != nil {
			finish("failed", err.Error(), 0)
			return
		}
		commit, err := a.prepareQuarkLibraryMove(ctx, quarkLibraryCard{}, card, st)
		if err != nil {
			finish("failed", err.Error(), 0)
			return
		}
		err = a.store.update(func(st *State) error {
			if !telegramLibraryValid(*st, job) {
				return errors.New("TG 授权或链库默认设置已变化，未添加")
			}
			st.QuarkLibraryCards = append(st.QuarkLibraryCards, card)
			initQuarkLibraryRuntime(st)
			message := "等待执行"
			if !card.Enabled {
				message = "未启用，请设置生成位置和可用存储后启用链接"
			}
			st.QuarkLibraryRuntime.Results[card.ID] = quarkLibraryResult{Status: "idle", Message: message}
			return validateQuarkLibrary(*st)
		})
		commit(err == nil)
		if err != nil {
			finish("failed", err.Error(), 0)
			return
		}
		a.store.event("info", "virtual", fmt.Sprintf("[TG:%s] 添加夸克分享卡片：%s，启用=%t", job.ID, card.Name, card.Enabled))
	}
	if cfg.Directory == "" {
		finish("completed", card.Name+"：默认生成位置未设置，已添加或保留卡片，本次未生成；未启用的卡片须设置生成位置后手动启用", 0)
		return
	}
	if !card.Enabled || strings.TrimSpace(card.Directory) == "" {
		finish("completed", card.Name+"：已添加或保留未启用的分享卡片；请设置生成位置和可用夸克存储后手动启用", 0)
		return
	}
	if err := check(); err != nil {
		finish("failed", err.Error(), 0)
		return
	}
	_, _ = a.telegramSend(ctx, st.Notifications.Telegram, job.ChatID, "准备生成分享STRM："+card.Name+"\n生成位置："+quarkLibraryRoot(card), nil)
	runCtx, stop := context.WithTimeout(context.WithValue(ctx, quarkLibraryGuardKey{}, check), 2*time.Minute)
	defer stop()
	result, err := a.runQuarkLibrary(runCtx, card)
	if err != nil {
		finish("failed", card.Name+"："+result.Message, result.Generated)
		return
	}
	finish("completed", card.Name+"："+result.Message, result.Generated)
}
