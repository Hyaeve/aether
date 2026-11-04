package app

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

type virtual115Defaults struct {
	ScrapeEnabled bool   `json:"scrapeEnabled"`
	StorageID     string `json:"storageId"`
	Directory     string `json:"directory"`
}

func (a *App) virtual115SettingsAPI(w http.ResponseWriter, r *http.Request) {
	var cfg virtual115Defaults
	if !decode(w, r, &cfg) {
		return
	}
	s, err := a.store.storage(cfg.StorageID)
	if err != nil || s.Type != "115" || !s.Enabled || !filepath.IsAbs(cfg.Directory) || len(cfg.Directory) > 4096 || strings.ContainsAny(cfg.Directory, "\x00\r\n") {
		fail(w, 400, errors.New("请选择可用115存储和绝对生成目录"))
		return
	}
	root, err := openStrmReplaceRoot(cfg.Directory)
	if err != nil {
		fail(w, 400, err)
		return
	}
	root.Close()
	cfg.Directory = filepath.Clean(cfg.Directory)
	if err := a.store.update(func(st *State) error { st.Virtual115 = &cfg; return nil }); err != nil {
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 200, cfg)
}
func (a *App) executeTelegram115Library(ctx context.Context, job telegramTransfer) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	claimed := false
	err := a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			j := &st.TelegramRuntime.Jobs[i]
			if j.ID == job.ID && j.Status == "queued" && !j.QueuePending && j.Link == job.Link {
				j.Status = "running"
				claimed = true
				return nil
			}
		}
		return errors.New("任务已处理")
	})
	if err != nil || !claimed {
		return
	}
	finish := func(status, name, message string, count, skipped int) {
		a.telegramComplete(ctx, job, status, name, count, skipped, message)
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !telegramLibraryValid(a.store.snapshotWithLogLimit(0), job) {
			return errors.New("TG授权或115链库设置已改变")
		}
		return nil
	}
	if err := check(); err != nil {
		finish("failed", "115分享", err.Error(), 0, 0)
		return
	}
	for !a.library115Mu.TryLock() {
		if !telegramPause(ctx, 200*time.Millisecond) {
			finish("interrupted", "115分享", "等待执行已中断", 0, 0)
			return
		}
	}
	defer a.library115Mu.Unlock()
	st := a.store.snapshotWithLogLimit(0)
	_, code, pass, err := parseShareLink(job.Link, "")
	if err != nil {
		finish("failed", "115分享", err.Error(), 0, 0)
		return
	}
	var card quarkLibraryCard
	for _, c := range st.Library115Cards {
		_, existing, oldPass, _ := parseShareLink(c.Secret, "")
		if existing == code {
			if oldPass != pass {
				finish("failed", c.Name, "提取码与原卡片不同，请编辑原卡片", 0, 0)
				return
			}
			card = c
			break
		}
	}
	if card.ID == "" {
		cfg := st.Virtual115
		if cfg == nil || cfg.Directory == "" {
			finish("failed", "115分享", "请先保存115链库默认存储池和生成目录", 0, 0)
			return
		}
		name, e := a.library115Title(ctx, job.Link, cfg.StorageID)
		if e != nil {
			finish("failed", "115分享", e.Error(), 0, 0)
			return
		}
		dir := name
		if !validShareDirectory(dir) || dir == "" {
			dir = "115分享-" + quarkLibraryStamp(code)[:8]
		}
		card = quarkLibraryCard{ID: id(), Secret: job.Link, StorageID: cfg.StorageID, Name: name, ShareName: name, Directory: cfg.Directory, DirectoryName: dir, Enabled: true, ScrapeEnabled: cfg.ScrapeEnabled}
		card.DirectoryName, err = telegramLibraryDirectory(st, card)
		if err != nil {
			finish("failed", name, err.Error(), 0, 0)
			return
		}
		err = a.store.update(func(st *State) error {
			if !telegramLibraryValid(*st, job) {
				return errors.New("TG配置已改变")
			}
			st.Library115Cards = append(st.Library115Cards, card)
			initLibrary115(st)
			return validateLibrary115(*st)
		})
		if err != nil {
			finish("failed", name, err.Error(), 0, 0)
			return
		}
	}
	if !card.Enabled {
		finish("completed", card.Name, "卡片未启用，本次跳过", 0, 0)
		return
	}
	result, err := a.runLibrary115(context.WithValue(ctx, generationWorkGuardKey{}, check), card)
	status := "completed"
	if err != nil {
		status = "failed"
	}
	finish(status, card.Name, result.Message, result.Generated, result.Skipped)
}
