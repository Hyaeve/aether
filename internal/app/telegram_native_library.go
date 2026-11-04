package app

import (
	"context"
	"errors"
	"strings"
	"time"
)

func (a *App) executeTelegramCASLibrary(ctx context.Context, job telegramTransfer) {
	a.executeTelegramNativeLibrary(ctx, job, true)
}
func (a *App) executeTelegramMobileLibrary(ctx context.Context, job telegramTransfer) {
	a.executeTelegramNativeLibrary(ctx, job, false)
}
func (a *App) executeTelegramNativeLibrary(ctx context.Context, job telegramTransfer, cas bool) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	claimed := false
	if err := a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			j := &st.TelegramRuntime.Jobs[i]
			if j.ID == job.ID && j.Status == "queued" && !j.QueuePending && j.Link == job.Link {
				j.Status = "running"
				claimed = true
				return nil
			}
		}
		return errors.New("机器人任务已处理")
	}); err != nil || !claimed {
		return
	}
	finish := func(status, message string, count int) {
		a.telegramComplete(ctx, job, status, job.Name, count, 0, message)
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !telegramLibraryValid(a.store.snapshotWithLogLimit(0), job) {
			return errors.New("TG授权或链库默认设置已变化，操作停止")
		}
		return nil
	}
	if err := check(); err != nil {
		finish("failed", err.Error(), 0)
		return
	}
	provider, code, pass, err := parseShareLink(job.Link, "")
	if err != nil || provider != "mobile" {
		finish("failed", "只接受移动云盘分享链接", 0)
		return
	}
	lock := &a.mobileLibraryMu
	if cas {
		lock = &a.casLibraryMu
	}
	for !lock.TryLock() {
		if !telegramPause(ctx, 200*time.Millisecond) {
			finish("interrupted", "等待链库时已取消", 0)
			return
		}
	}
	defer lock.Unlock()
	if err := check(); err != nil {
		finish("failed", err.Error(), 0)
		return
	}
	st := a.store.snapshotWithLogLimit(0)
	cards := st.MobileLibraryCards
	directory := effectiveMobileLibrary(st).Directory
	if cas {
		cards = st.CASLibraryCards
		directory = effectiveCASLibrary(st).Directory
	}
	var card quarkLibraryCard
	for _, c := range cards {
		_, existing, oldPass, _ := parseShareLink(c.Secret, "")
		if existing == code {
			if pass != oldPass {
				finish("failed", "已存在同一分享且提取码不同，请编辑原卡片", 0)
				return
			}
			card = c
			break
		}
	}
	if card.ID == "" {
		var title string
		if cas {
			title, err = a.casLibraryTitle(ctx, job.Link)
		} else {
			storage, e := a.mobileLibraryStorage(st)
			err = e
			if err == nil {
				var share mobileLibraryShare
				share, err = a.mobileLibraryShareInfo(ctx, storage, job.Link)
				title = share.Name
			}
		}
		if err != nil {
			finish("failed", "读取分享失败："+err.Error(), 0)
			return
		}
		job.Name = title
		name := title
		if !validShareDirectory(name) || name == "" {
			name = "移动分享-" + quarkLibraryStamp(code)[:8]
		}
		card = quarkLibraryCard{ID: id(), Secret: mobileLibrarySecret(code, pass), Name: title, ShareName: title, Directory: directory, DirectoryName: name, Enabled: directory != "", CascadeCleanup: "off"}
		card.ScrapeEnabled = effectiveMobileLibrary(st).ScrapeEnabled
		days := effectiveMobileLibrary(st).UpdateDays
		if cas {
			days = effectiveCASLibrary(st).UpdateDays
			card.ScrapeEnabled = effectiveCASLibrary(st).ScrapeEnabled
		}
		botLibrarySchedule(&card, days, job.Created)
		card.DirectoryName, err = telegramLibraryDirectory(st, card)
		if err != nil {
			finish("failed", err.Error(), 0)
			return
		}
		if err := check(); err != nil {
			finish("failed", err.Error(), 0)
			return
		}
		err = a.store.update(func(st *State) error {
			if !telegramLibraryValid(*st, job) {
				return errors.New("TG配置已改变")
			}
			if err := shareLibraryNamesAvailable(*st, card); err != nil {
				return err
			}
			if cas {
				st.CASLibraryCards = append(st.CASLibraryCards, card)
				initCASLibrary(st)
				return validateCASLibrary(*st)
			}
			st.MobileLibraryCards = append(st.MobileLibraryCards, card)
			initMobileLibrary(st)
			return validateMobileLibrary(*st)
		})
		if err != nil {
			finish("failed", err.Error(), 0)
			return
		}
	}
	job.Name = card.Name
	if card.Directory == "" {
		finish("completed", "默认生成位置未设置，已添加或保留卡片，本次未生成", 0)
		return
	}
	if !card.Enabled || strings.TrimSpace(card.Directory) == "" {
		finish("completed", "已保存未启用的卡片，请在链库设置生成位置后启用", 0)
		return
	}
	if err := check(); err != nil {
		finish("failed", err.Error(), 0)
		return
	}
	if cas {
		result, err := a.runCASLibrary(ctx, card, check)
		if err != nil {
			finish("failed", err.Error(), result.Generated)
		} else {
			a.telegramComplete(ctx, job, "completed", card.Name, result.Generated+result.Metadata, result.Skipped, result.Message)
		}
		return
	}
	result, err := a.runMobileLibrary(context.WithValue(ctx, generationWorkGuardKey{}, check), card)
	if err != nil {
		finish("failed", result.Message, result.Generated)
	} else {
		a.telegramComplete(ctx, job, "completed", card.Name, result.Generated+result.Metadata, result.Skipped, result.Message)
	}
}
