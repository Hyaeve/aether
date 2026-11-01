package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

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
	for _, storage := range a.mobileLibraryStorages(st) {
		if err := a.mobileLibraryCheck(card, &storage); err != nil {
			fail(w, 409, err)
			return
		}
		share, err := a.mobileLibraryShareInfo(ctx, storage, card.Secret)
		if err != nil {
			continue
		}
		items, err := a.mobileLibraryList(ctx, storage, share, grant.SourceParent, false)
		if err != nil {
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
			http.NotFound(w, r)
			return
		}
		download, err := a.mobileLibraryDownload(ctx, storage, share, *source)
		if err != nil {
			continue
		}
		if err := a.mobileLibraryCheck(card, &storage); err != nil {
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
		if effectiveMobileLibrary(st).PlaybackMode != "relay" {
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
	a.store.event("warn", "virtual", "移动分享取链失败："+card.Name+" · "+name+"；请核对链接、提取码及绑定账号授权")
	fail(w, 502, errors.New("移动分享未取得下载直链，请检查分享有效性、提取码及原生账号授权"))
}
