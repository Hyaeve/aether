package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type virtual115Defaults struct {
	StorageID string `json:"storageId"`
	Directory string `json:"directory"`
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
	if err := a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			j := &st.TelegramRuntime.Jobs[i]
			if j.ID == job.ID && j.Status == "queued" && j.Link == job.Link {
				j.Status = "running"
				claimed = true
				return nil
			}
		}
		return errors.New("任务已处理")
	}); err != nil || !claimed {
		return
	}
	count, skipped := 0, 0
	name := "115分享"
	finish := func(status, message string) { a.telegramComplete(ctx, job, status, name, count, skipped, message) }
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !telegramLibraryValid(a.store.snapshotWithLogLimit(0), job) {
			return errors.New("TG授权或115链库设置已改变")
		}
		return nil
	}
	st := a.store.snapshotWithLogLimit(0)
	if err := check(); err != nil {
		finish("failed", err.Error())
		return
	}
	cfg := st.Virtual115
	if cfg == nil || cfg.Directory == "" {
		finish("failed", "请先在115链库保存默认解析存储池和生成目录")
		return
	}
	s, err := a.store.storage(cfg.StorageID)
	if err != nil || s.Type != "115" || !s.Enabled {
		finish("failed", "默认115存储不可用")
		return
	}
	provider, code, pass, err := parseShareLink(job.Link, "")
	if err != nil || provider != "115" {
		finish("failed", "请输入115分享链接")
		return
	}
	_, items, err := a.readShare(ctx, s, code, pass)
	if err != nil {
		finish("failed", "读取115分享失败："+err.Error())
		return
	}
	if title := telegramShareName(items); title != "" {
		name = title
	}
	batches, err := a.planShare(ctx, s, code, pass, items)
	if err != nil {
		finish("failed", err.Error())
		return
	}
	dirName := name
	if !validShareDirectory(dirName) || dirName == "" {
		dirName = "115分享-" + quarkLibraryStamp(code)[:8]
	}
	card := quarkLibraryCard{Directory: cfg.Directory, DirectoryName: dirName}
	card.DirectoryName, err = telegramLibraryDirectory(st, card, time.Now())
	if err != nil {
		finish("failed", err.Error())
		return
	}
	target := quarkLibraryRoot(card)
	for _, protected := range []string{a.store.dir, a.dataDir} {
		if protected != "" && (backupPathsOverlap(target, protected) || backupPathsOverlap(protected, target)) {
			finish("failed", "生成目录不能覆盖配置或运行数据")
			return
		}
	}
	release, err := a.reserveTaskFileNodes(ctx, []taskFileNode{{storage: Storage{Type: "local", Config: map[string]string{"root": card.Directory}}, id: card.DirectoryName}})
	if err != nil {
		finish("failed", err.Error())
		return
	}
	defer release()
	if err := check(); err != nil {
		finish("failed", err.Error())
		return
	}
	parent, err := openStrmReplaceRoot(card.Directory)
	if err != nil {
		finish("failed", err.Error())
		return
	}
	defer parent.Close()
	root, err := virtualOutputRoot(parent, card.DirectoryName)
	if err != nil {
		finish("failed", err.Error())
		return
	}
	defer root.Close()
	identity, err := root.Stat(".")
	if err != nil {
		finish("failed", err.Error())
		return
	}
	names := virtual115Names{Version: 1, Files: []virtual115NamedFile{}}
	for _, batch := range batches {
		for _, item := range batch.Items {
			if !taskMedia(Task{}, item.Name) {
				continue
			}
			if err := check(); err != nil {
				finish("failed", err.Error())
				return
			}
			fresh, e := openStrmReplaceRoot(target)
			if e != nil {
				finish("failed", e.Error())
				return
			}
			info, e := fresh.Stat(".")
			fresh.Close()
			if e != nil || !os.SameFile(identity, info) {
				finish("failed", "生成目录身份已改变")
				return
			}
			content, e := virtualShareURL(a.currentSTRMSettings().Address, code, pass, item, a.currentSTRMSettings().Content == "encoded")
			if e != nil {
				finish("failed", e.Error())
				return
			}
			key := code + "_" + pass + "_" + item.ID + strings.ToLower(path.Ext(item.Name))
			if e := a.virtualGrant(key, true, batch.Source); e != nil {
				finish("failed", e.Error())
				return
			}
			names.Files = append(names.Files, virtual115NamedFile{Key: key, Parent: batch.Source, Name: item.Name, Size: item.Size, SHA1: item.SHA1})
			relative := path.Join(batch.Directory, item.Name+shareSTRMSuffix)
			if strings.HasPrefix(relative, card.DirectoryName+"/") {
				relative = strings.TrimPrefix(relative, card.DirectoryName+"/")
			}
			if info, e := root.Lstat(relative); e == nil {
				if !info.Mode().IsRegular() {
					finish("failed", "输出目标不是普通文件")
					return
				}
				skipped++
				continue
			} else if !os.IsNotExist(e) {
				finish("failed", e.Error())
				return
			}
			if err := writeCASOutput(root, relative, []byte(content+"\n"), true, true); err != nil {
				finish("failed", err.Error())
				return
			}
			count++
		}
	}
	if err := check(); err != nil {
		finish("failed", err.Error())
		return
	}
	if err := a.saveVirtual115Names(code, pass, names); err != nil {
		finish("failed", err.Error())
		return
	}
	finish("completed", "115分享STRM生成完成")
}
