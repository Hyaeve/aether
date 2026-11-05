package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var errMobileLibraryGrant = errors.New("移动分享播放授权已改变")

func (a *App) playMobileLibrary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(405)
		return
	}
	name := r.PathValue("file")
	if !safeName(name) {
		http.NotFound(w, r)
		return
	}
	st := a.store.snapshotWithLogLimit(0)
	cfg := effectiveMobileLibrary(st)
	var card quarkLibraryCard
	var grant *quarkLibraryFile
	for _, candidate := range st.MobileLibraryCards {
		if !candidate.Enabled || r.PathValue("share") != mobileLibrarySharePath(candidate.Secret) {
			continue
		}
		for _, file := range st.MobileLibraryRuntime.Files {
			if file.CardID != candidate.ID || !file.Published || !file.Media || !isShareSTRM(file.Relative) ||
				file.ShareStamp != quarkLibraryStamp(candidate.Secret) || file.Name != name {
				continue
			}
			if grant != nil {
				fail(w, 409, errors.New("移动分享含多个同名文件，无法仅凭文件名定位，请调整分享后重新生成"))
				return
			}
			copy := file
			card, grant = candidate, &copy
		}
	}
	if grant == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		if grant.Size >= 0 {
			w.Header().Set("Content-Length", fmt.Sprint(grant.Size))
		}
		w.WriteHeader(200)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	lastFailure := "未找到已授权的原生移动存储，请检查链库绑定和账号授权"
	recordFailure := func(stage string, storage Storage, err error) {
		reason := strings.NewReplacer("\r", " ", "\n", " ").Replace(err.Error())
		if strings.Contains(reason, "://") || len([]rune(reason)) > 300 {
			reason = safeStorageError(err)
		}
		lastFailure = stage + "：" + reason
		a.store.event("warn", "virtual", "移动分享播放失败："+card.Name+" · "+name+"；存储池「"+storage.Name+"」；"+lastFailure)
	}
	for _, storage := range rotateLibraryStorages(a.mobileLibraryStorages(st), cfg.RotationHours, time.Now()) {
		check := func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := a.mobileLibraryCheck(card, &storage); err != nil {
				return err
			}
			current := a.store.snapshotWithLogLimit(0)
			before, _ := json.Marshal(cfg)
			after, _ := json.Marshal(effectiveMobileLibrary(current))
			if string(before) != string(after) {
				return errors.New("移动链库播放设置已改变，请重新播放")
			}
			file, ok := current.MobileLibraryRuntime.Files[grant.Key]
			if !ok || file != *grant || !file.Published {
				return errMobileLibraryGrant
			}
			return nil
		}
		if err := a.mobileLibraryCheck(card, &storage); err != nil {
			fail(w, 409, err)
			return
		}
		share, err := a.mobileLibraryShareInfo(ctx, storage, card.Secret)
		if err != nil {
			recordFailure("读取分享信息", storage, err)
			a.markMobileLibraryInvalid(card, err)
			continue
		}
		items, err := a.mobileLibraryList(ctx, storage, share, grant.SourceParent, false)
		if err != nil {
			recordFailure("核对分享目录", storage, err)
			continue
		}
		var source *mobileLibrarySource
		for _, file := range items {
			if !file.Directory && file.Path == grant.SourceID && file.Name == grant.Name && (grant.Size < 0 || file.Size == grant.Size) {
				copy := file
				source = &copy
			}
		}
		if source == nil {
			a.cleanMissingLibraryFile(ctx, "mobile", card, *grant)
			http.NotFound(w, r)
			return
		}
		var download Download
		if cfg.PlaybackMode == "transfer" {
			download, err = a.mobileLibraryTransferredLink(ctx, card, *grant, storage, share, *source, check)
		} else if cfg.PlaybackMode == "share" && source.StreamURL != "" {
			download = Download{URL: source.StreamURL}
		} else {
			download, err = a.mobileLibraryDownload(ctx, storage, share, *source)
		}
		if err != nil {
			stage := "获取分享下载直链"
			if cfg.PlaybackMode == "transfer" {
				stage = "临时转存并取直链"
			}
			recordFailure(stage, storage, err)
			if errors.Is(err, errMobileLibraryGrant) {
				http.NotFound(w, r)
				return
			}
			if cfg.PlaybackMode == "transfer" {
				fail(w, 502, err)
				return
			}
			continue
		}
		if err := check(); err != nil {
			if errors.Is(err, errMobileLibraryGrant) {
				http.NotFound(w, r)
				return
			}
			fail(w, 409, err)
			return
		}
		a.store.mu.RLock()
		current, exists := a.store.state.MobileLibraryRuntime.Files[grant.Key]
		a.store.mu.RUnlock()
		if !exists || current != *grant || !current.Published {
			http.NotFound(w, r)
			return
		}
		a.store.event("info", "virtual", "移动分享播放："+card.Name+" · "+name)
		if cfg.PlaybackMode != "relay" {
			if cfg.PlaybackMode == "share" && source.StreamURL != "" {
				w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			}
			redirectDownload(w, r, download.URL)
			return
		}
		if source.Size < 0 {
			fail(w, 502, errors.New("移动分享未返回文件大小，不能安全中继，请使用原文件直链"))
			return
		}
		// The lookup budget does not limit the duration of a media stream.
		reader := &davFile{ctx: r.Context(), info: davInfo{File{Name: name, Size: source.Size}}, download: download}
		defer reader.Close()
		http.ServeContent(w, r, name, time.Time{}, reader)
		return
	}
	a.store.event("warn", "virtual", "移动分享取链失败："+card.Name+" · "+name+"；"+lastFailure)
	fail(w, 502, errors.New("移动分享未取得下载直链；"+lastFailure))
}
