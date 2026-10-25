package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func telegramShareName(items []shareEntry) string {
	names := []string{}
	for _, item := range items {
		if len(names) == 2 {
			break
		}
		names = append(names, item.Name)
	}
	name := strings.Join(names, "、")
	if len([]rune(name)) > 120 {
		name = string([]rune(name)[:119]) + "…"
	}
	return name
}

func (a *App) telegramResourceName(ctx context.Context, job telegramTransfer, target telegramTarget) string {
	if a.telegramExecutionValid(job, target, "") != nil {
		return ""
	}
	if job.Kind == "ed2k" {
		if info, err := parseED2KText(job.Link); err == nil {
			return telegramShareName([]shareEntry{{Name: info.Name}})
		}
		return ""
	}
	s, err := a.store.storage(target.StorageID)
	provider, code, pass, parseErr := parseShareLink(job.Link, "")
	if err != nil || parseErr != nil || provider != s.Type {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, items, err := a.readShareSession(ctx, &s, code, pass, "root", "")
	if err != nil {
		return ""
	}
	return telegramShareName(items)
}

func (a *App) finishTelegram(key, status, message string, processed int) {
	err := a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			job := &st.TelegramRuntime.Jobs[i]
			if job.ID == key {
				job.Status, job.Link, job.Processed = status, "", processed
				if job.GroupID == job.ID {
					for j := range st.TelegramRuntime.Jobs {
						member := &st.TelegramRuntime.Jobs[j]
						if member.ID != job.ID && telegramSameGroup(*job, *member) && member.Status == "waiting" {
							member.Status, member.Link = status, ""
						}
					}
				}
			}
		}
		return nil
	})
	level := "info"
	if status == "failed" || err != nil {
		level = "error"
	}
	if err != nil {
		message = "TG 转存结果保存失败，请检查网盘目标目录；勿直接重复提交"
	}
	a.store.event(level, "notifications", fmt.Sprintf("[TG:%s] %s", key, message))
}

func (a *App) telegramWorker() {
	defer a.wg.Done()
	for a.ctx.Err() == nil {
		batches := map[string]bool{}
		for _, job := range a.store.snapshotWithLogLimit(0).TelegramRuntime.Jobs {
			if job.BatchID != "" && job.Status == "waiting" && job.GroupID == job.ID {
				batches[job.BatchID] = true
			}
		}
		for batch := range batches {
			if err := a.advanceTelegramBatch(a.ctx, batch); err != nil {
				a.store.event("warn", "notifications", "TG 下一批交互未确认："+err.Error())
			}
		}
		st := a.store.snapshotWithLogLimit(0)
		var next *telegramTransfer
		for _, job := range st.TelegramRuntime.Jobs {
			if job.Status == "queued" {
				copy := job
				next = &copy
				break
			}
		}
		if next != nil {
			a.executeTelegramTransfer(a.ctx, *next)
			continue
		}
		if !telegramPause(a.ctx, time.Second) {
			return
		}
	}
}

func (a *App) telegramExecutionValid(job telegramTransfer, target telegramTarget, stamp string) error {
	st := a.store.snapshotWithLogLimit(0)
	if !telegramTargetCurrent(st, job, target) {
		return errors.New("TG 授权或转存目录配置已变化，已停止")
	}
	s, err := a.store.storage(target.StorageID)
	if err != nil || s.Type != target.Provider || s.Health != nil && s.Health.Stamp == storageRevision(s) && s.Health.State != "active" {
		return errors.New("转存存储不可用，已停止")
	}
	if stamp != "" {
		hash := shareConfig(s)
		if hex.EncodeToString(hash[:]) != stamp {
			return errors.New("转存账号凭据已变化，已停止")
		}
	}
	return nil
}

func (a *App) executeTelegramTransfer(ctx context.Context, job telegramTransfer) {
	if job.Kind == "library" {
		a.executeTelegramLibrary(ctx, job)
		return
	}
	if job.Selected < 0 || job.Selected >= len(job.Targets) {
		a.finishTelegram(job.ID, "failed", "TG 转存目录无效", 0)
		return
	}
	target := job.Targets[job.Selected]
	cfg := a.store.snapshotWithLogLimit(0).Notifications.Telegram
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	processed, skipped := 0, 0
	name := "分享资源"
	s, err := a.store.storage(target.StorageID)
	storageName := s.Name
	stamp := shareConfig(s)
	stampText := hex.EncodeToString(stamp[:])
	finish := func(status, result string) {
		message := fmt.Sprintf("%s：%s · %s · %s；已提交%d项，跳过%d项", result, storageName, target.Path, name, processed, skipped)
		a.finishTelegram(job.ID, status, message, processed)
		if current := a.store.snapshotWithLogLimit(0).Notifications.Telegram; telegramDigest(current) == job.Auth && telegramAllowed(current, job.ChatID, job.UserID) {
			if _, e := a.telegramSend(ctx, cfg, job.ChatID, message, nil); e != nil && ctx.Err() == nil {
				a.store.event("warn", "notifications", "TG 转存反馈消息未确认，请查看运行日志")
			}
		}
	}
	if err != nil {
		finish("failed", "转存失败：存储池已停用或移除")
		return
	}
	if err = a.telegramExecutionValid(job, target, stampText); err != nil {
		finish("failed", err.Error())
		return
	}
	claimed := false
	err = a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			current := &st.TelegramRuntime.Jobs[i]
			if current.ID == job.ID && current.Status == "queued" && telegramTargetCurrent(*st, *current, target) {
				current.Status, current.StorageStamp = "running", stampText
				claimed = true
				return nil
			}
		}
		return errors.New("TG 任务已处理或配置已变化")
	})
	if err != nil || !claimed {
		return
	}
	action := telegramAction(job.Kind)
	a.store.event("info", "notifications", fmt.Sprintf("[TG:%s] 开始%s：%s · %s", job.ID, action, s.Name, target.Path))
	if job.Kind == "ed2k" {
		info, e := parseED2KText(job.Link)
		if e != nil || s.Type != "115" {
			finish("failed", "ED2K 链接或115离线目录无效")
			return
		}
		name = info.Name
		_, _ = a.telegramSend(ctx, cfg, job.ChatID, "准备"+action+"："+name+"\n目标："+s.Name+" · "+target.Path, nil)
		processed, err = a.submitTelegramED2K(ctx, job, target, s, stampText, submit115URIs)
		if err != nil {
			finish("failed", err.Error())
			return
		}
		finish("submitted", "ED2K 离线已提交，请查看115云下载状态")
		return
	}
	provider, code, pass, err := parseShareLink(job.Link, "")
	if err != nil || provider != s.Type {
		finish("failed", "分享链接无效或类型不一致")
		return
	}
	session := s
	previewCtx, previewCancel := context.WithTimeout(ctx, 2*time.Minute)
	token, items, err := a.readShareSession(previewCtx, &session, code, pass, "root", "")
	var batches []shareBatch
	if err == nil {
		if resolved := telegramShareName(items); resolved != "" {
			name = resolved
		}
		_, _ = a.telegramSend(ctx, cfg, job.ChatID, "准备"+action+"："+name+"\n目标："+s.Name+" · "+target.Path, nil)
		batches, err = a.planShareSession(previewCtx, &session, code, pass, token, items)
	}
	previewCancel()
	if err != nil {
		finish("failed", "读取分享失败："+err.Error())
		return
	}
	preview := sharePreview{Code: code, Pass: pass, Token: token}
	parent := target.Parent
	if parent == "/" || parent == "" {
		parent = rootOf(s)
	}
	for index, batch := range batches {
		if err := a.telegramExecutionValid(job, target, stampText); err != nil {
			finish("failed", err.Error()+"；请核对已提交结果，勿直接重试")
			return
		}
		if err := ctx.Err(); err != nil {
			finish("interrupted", "转存已中断；请核对网盘结果，勿直接重试")
			return
		}
		// Persist the next batch intent before directory creation or transfer submission.
		err = a.store.update(func(st *State) error {
			for i := range st.TelegramRuntime.Jobs {
				if st.TelegramRuntime.Jobs[i].ID == job.ID {
					st.TelegramRuntime.Jobs[i].Batches = index + 1
					st.TelegramRuntime.Jobs[i].Name = name
					return nil
				}
			}
			return errors.New("任务回执不存在")
		})
		if err != nil {
			finish("failed", "批次回执保存失败，已停止")
			return
		}
		task, ignored, submitted, err := a.submitPlannedShareBatch(ctx, &session, preview, parent, batch, func() error { return a.telegramExecutionValid(job, target, stampText) })
		if err != nil {
			finish("failed", err.Error())
			return
		}
		processed += submitted
		skipped += ignored
		if s.Type == "quark" && task != "" {
			complete := false
			for poll := 0; poll < 120; poll++ {
				if !telegramPause(ctx, 2*time.Second) {
					break
				}
				if e := a.telegramExecutionValid(job, target, stampText); e != nil {
					err = e
					break
				}
				var result struct {
					Data struct {
						Status int `json:"status"`
					} `json:"data"`
				}
				err = shareSessionRequest(ctx, &session, "GET", "https://drive.quark.cn/1/clouddrive/task?pr=ucpro&fr=pc&retry_index=0&task_id="+url.QueryEscape(task), nil, &result)
				if err != nil || result.Data.Status == 3 {
					break
				}
				if result.Data.Status == 2 {
					complete = true
					break
				}
			}
			if !complete {
				finish("failed", "夸克转存未确认完成；已停止后续批次，请检查目标目录，勿直接重复提交")
				return
			}
		}
		a.store.event("info", "notifications", fmt.Sprintf("[TG:%s] %s · %s · %s：批次%d/%d已提交%d项，跳过%d项", job.ID, s.Name, target.Path, name, index+1, len(batches), submitted, ignored))
		a.cache.clear()
	}
	if s.Type == "quark" || processed == 0 {
		finish("completed", "转存完成")
	} else {
		finish("submitted", "转存已提交，请核对云盘结果")
	}
}

func (a *App) submitTelegramED2K(ctx context.Context, job telegramTransfer, target telegramTarget, s Storage, stamp string, submit func(context.Context, Storage, string, []string) []offlineResult) (int, error) {
	info, err := parseED2KText(job.Link)
	if err != nil || job.Kind != "ed2k" || s.Type != "115" || len(job.Link) > 8192 {
		return 0, errors.New("ED2K 链接或115离线目录无效")
	}
	if err := a.telegramExecutionValid(job, target, stamp); err != nil {
		return 0, err
	}
	parent := target.Parent
	if parent == "/" || parent == "" {
		parent = rootOf(s)
	}
	if _, err := a.rawList(ctx, s, parent); err != nil {
		return 0, errors.New("115离线目录不可访问，未提交任务")
	}
	// Persist once before submission; an uncertain native result must never be replayed.
	err = a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			current := &st.TelegramRuntime.Jobs[i]
			if current.ID == job.ID && current.Status == "running" && current.Batches == 0 && telegramTargetCurrent(*st, *current, target) {
				current.Batches, current.Name = 1, info.Name
				return nil
			}
		}
		return errors.New("离线任务已处理或目录配置已变化，未重复提交")
	})
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, errors.New("ED2K 离线已中断，未提交任务")
	}
	if err := a.telegramExecutionValid(job, target, stamp); err != nil {
		return 0, err
	}
	results := submit(ctx, s, parent, []string{info.URI()})
	if len(results) != 1 || !results[0].Success {
		return 0, errors.New("ED2K 离线未确认，请检查115云下载记录及账号权益；勿直接重复提交")
	}
	a.store.event("info", "notifications", fmt.Sprintf("[TG:%s] ED2K 离线已提交：%s · %s · %s", job.ID, s.Name, target.Path, info.Name))
	return 1, nil
}
