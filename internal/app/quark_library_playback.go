package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

func (a *App) quarkLibraryCheckStorage(s Storage) error {
	current, err := a.store.storage(s.ID)
	if err != nil || current.Type != "quark" || storageRevision(current) != storageRevision(s) || storageAuthBlocked(current, time.Now()) != nil {
		return errors.New("夸克存储配置或健康状态已改变，操作停止")
	}
	a.store.mu.RLock()
	cfg := effectiveQuarkLibrary(a.store.state)
	ids := append([]string(nil), cfg.StorageIDs...)
	a.store.mu.RUnlock()
	for _, sid := range ids {
		if sid == s.ID {
			return nil
		}
	}
	return errors.New("夸克存储已从链库绑定中移除")
}

func (a *App) quarkLibraryFolder(ctx context.Context, session Storage, check func() error, parent, name string) (string, error) {
	if err := check(); err != nil {
		return "", err
	}
	f, err := a.cloudByName(ctx, session, parent, name)
	if err != nil {
		return "", err
	}
	if f != nil {
		if !f.IsDir || f.ID == "" || f.ID == parent || f.ID == "0" {
			return "", errors.New("临时目录名称已被普通文件占用")
		}
		return f.ID, nil
	}
	if err = check(); err != nil {
		return "", err
	}
	if err = a.cloudMkdir(ctx, session, parent, name); err != nil {
		return "", errors.New("创建夸克链库临时目录未确认，请核对网盘")
	}
	f, err = a.cloudByName(ctx, session, parent, name)
	if err != nil || f == nil || !f.IsDir || f.ID == "" || f.ID == parent || f.ID == "0" {
		return "", errors.New("未确认夸克链库临时目录")
	}
	return f.ID, nil
}

func (a *App) quarkLibraryTransfer(ctx context.Context, card quarkLibraryCard, grant quarkLibraryFile, session *Storage, token string, item shareEntry) (quarkLibraryTemporary, Storage, error) {
	a.quarkTemporaryMu.Lock()
	defer a.quarkTemporaryMu.Unlock()
	var empty quarkLibraryTemporary
	storage, err := a.store.storage(session.ID)
	if err != nil || a.quarkLibraryCheckStorage(storage) != nil {
		return empty, storage, errors.New("夸克链库存储不可用")
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !card.Enabled || a.quarkLibraryCheckCard(card) != nil {
			return errors.New("分享卡片已变更或停用")
		}
		return a.quarkLibraryCheckStorage(storage)
	}
	key := quarkLibraryStamp(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", grant.Key, storage.ID, grant.ShareStamp, item.ID, item.Size))
	record, exists := a.store.snapshotWithLogLimit(0).QuarkLibraryRuntime.Temporary[key]
	save := func() error {
		return a.store.update(func(st *State) error {
			initQuarkLibraryRuntime(st)
			if _, exists := st.QuarkLibraryRuntime.Temporary[key]; !exists && len(st.QuarkLibraryRuntime.Temporary) >= 10000 {
				return errors.New("夸克链库临时记录已达10000项，请先核对清理")
			}
			st.QuarkLibraryRuntime.Temporary[key] = record
			return nil
		})
	}
	if exists && (record.Revision != storageRevision(storage) || record.Name != item.Name || record.Size != item.Size || record.Deleting) {
		return record, storage, errors.New("临时转存身份变化或清理中，请核对原临时文件，未重复提交")
	}
	if err := check(); err != nil {
		return record, storage, err
	}
	if !exists {
		base, err := a.quarkLibraryFolder(ctx, *session, check, "0", "Aether")
		if err != nil {
			return empty, storage, err
		}
		base, err = a.quarkLibraryFolder(ctx, *session, check, base, "Share")
		if err != nil {
			return empty, storage, err
		}
		// A private, random subfolder isolates this one file from user data.
		folderName := "q-" + id()
		record = quarkLibraryTemporary{Key: key, StorageID: storage.ID, Revision: storageRevision(storage), Name: item.Name,
			Size: item.Size, LastUsed: time.Now(), FolderName: folderName, Parent: base}
		if err = save(); err != nil {
			return record, storage, err
		}
		if err = check(); err != nil {
			return record, storage, err
		}
		if err = a.cloudMkdir(ctx, *session, base, folderName); err != nil {
			return record, storage, errors.New("专用临时目录创建结果未确认，不自动重建")
		}
		folder, err := a.cloudByName(ctx, *session, base, folderName)
		if err != nil || folder == nil || !folder.IsDir || folder.ID == "" || folder.ID == base || folder.ID == "0" {
			return record, storage, errors.New("未确认专用临时目录，不自动重建")
		}
		record.Folder = folder.ID
		if err = save(); err != nil {
			return record, storage, err
		}
	}
	if record.Folder == "" {
		return record, storage, errors.New("上次临时目录创建结果未确认，请核对网盘，未重复写入")
	}
	files, err := a.rawList(ctx, *session, record.Folder)
	if err != nil {
		return record, storage, err
	}
	if !record.Attempted {
		if len(files) != 0 {
			return record, storage, errors.New("专用临时目录非空，拒绝转存或覆盖")
		}
		record.Attempted = true
		if err = save(); err != nil {
			return record, storage, err
		}
		if err = check(); err != nil {
			return record, storage, err
		}
		_, code, pass, _ := parseShareLink(card.Secret, "")
		a.store.event("info", "virtual", fmt.Sprintf("夸克链库临时转存：存储池 %q，文件 %q", storage.Name, item.Name))
		task, err := submitShareSession(ctx, session, sharePreview{Code: code, Pass: pass, Token: token, SourceParent: grant.SourceParent}, record.Folder, []shareEntry{item})
		if err != nil {
			return record, storage, errors.New("夸克分享转存结果未确认，保留提交意图，不自动重放")
		}
		record.Task = task
		if err = save(); err != nil {
			return record, storage, err
		}
	}
	for {
		if err := check(); err != nil {
			return record, storage, err
		}
		if record.Task != "" && record.FileID == "" {
			var result struct {
				Data struct {
					Status int `json:"status"`
				} `json:"data"`
			}
			if err := a.waitAPI(ctx, storage.ID); err != nil {
				return record, storage, err
			}
			err = shareSessionRequest(ctx, session, "GET", "https://drive.quark.cn/1/clouddrive/task?pr=ucpro&fr=pc&retry_index=0&task_id="+url.QueryEscape(record.Task), nil, &result)
			if err != nil {
				return record, storage, err
			}
			if result.Data.Status == 3 || result.Data.Status == 4 {
				return record, storage, errors.New("夸克转存失败或暂停，请核对临时目录，不自动重放")
			}
			if result.Data.Status != 2 {
				select {
				case <-ctx.Done():
					return record, storage, ctx.Err()
				case <-time.After(time.Second):
					continue
				}
			}
		}
		files, err = a.rawList(ctx, *session, record.Folder)
		if err != nil {
			return record, storage, err
		}
		if len(files) == 1 && !files[0].IsDir && files[0].ID != "" && files[0].Name == record.Name && files[0].Size == record.Size &&
			(record.FileID == "" || record.FileID == files[0].ID) {
			record.FileID, record.LastUsed = files[0].ID, time.Now()
			if err = save(); err != nil {
				return record, storage, err
			}
			if err := a.persistQuarkCookies(storage, *session); err != nil {
				return record, storage, err
			}
			a.store.event("info", "virtual", fmt.Sprintf("夸克链库临时文件就绪：存储池 %q，文件 %q", storage.Name, item.Name))
			return record, *session, nil
		}
		return record, storage, errors.New("临时转存尚未确认或文件身份不符；不重复提交，请核对网盘")
	}
}

func (a *App) playQuarkLibrary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(405)
		return
	}
	st := a.store.snapshotWithLogLimit(0)
	grant, ok := st.QuarkLibraryRuntime.Files[r.PathValue("key")]
	card, present := quarkLibraryCardAt(st, grant.CardID)
	if !ok || !grant.Published || !grant.Media || !present || !card.Enabled || grant.ShareStamp != quarkLibraryStamp(card.Secret) || r.PathValue("name") != grant.Name {
		http.NotFound(w, r)
		return
	}
	// HEAD is read-only: link probes never authorize cloud writes.
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", fmt.Sprint(grant.Size))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(200)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	s, err := a.quarkLibraryStorage(st)
	if err != nil {
		fail(w, 503, err)
		return
	}
	_, code, pass, _ := parseShareLink(card.Secret, "")
	original := s
	token, items, err := a.readShareSession(ctx, &s, code, pass, grant.SourceParent, "")
	if err != nil {
		fail(w, 502, err)
		return
	}
	if err := a.persistQuarkCookies(original, s); err != nil {
		fail(w, 502, err)
		return
	}
	var source *shareEntry
	for _, item := range items {
		if item.ID == grant.SourceID && !item.IsDir && item.Name == grant.Name && item.Size == grant.Size {
			copy := item
			source = &copy
		}
	}
	if source == nil {
		http.NotFound(w, r)
		return
	}
	record, session, err := a.quarkLibraryTransfer(ctx, card, grant, &s, token, *source)
	if err != nil {
		a.store.event("warn", "virtual", "夸克链库播放失败："+card.Name+" · "+grant.Name+"；"+err.Error())
		fail(w, 502, err)
		return
	}
	if target := a.quarkTVTarget(ctx, session, record.FileID, r.UserAgent()); target != "" {
		a.store.event("info", "virtual", "夸克链库TV直链："+card.Name+" · "+grant.Name)
		redirectDownload(w, r, target)
		return
	}
	download, err := a.downloadWithUA(ctx, session, record.FileID, "", r.UserAgent())
	if err != nil {
		fail(w, 502, err)
		return
	}
	// ServeContent forwards Range through the shared reader without exposing
	// Cookie-protected web download links to the client.
	a.quarkTemporaryMu.Lock()
	if a.quarkPlaybackActive == nil {
		a.quarkPlaybackActive = map[string]int{}
	}
	a.quarkPlaybackActive[record.Key]++
	a.quarkTemporaryMu.Unlock()
	defer func() {
		a.quarkTemporaryMu.Lock()
		a.quarkPlaybackActive[record.Key]--
		if a.quarkPlaybackActive[record.Key] == 0 {
			delete(a.quarkPlaybackActive, record.Key)
		}
		a.quarkTemporaryMu.Unlock()
	}()
	from := a.transferDirectory(session, record.Folder)
	from.Directory = record.FolderName
	progress := a.beginTransfer(r.Context(), "download", session, grant.Name, grant.Size,
		transferLocations{From: from, To: transferClientLocation(r.Context())})
	reader := &davFile{ctx: progress.ctx, info: davInfo{File{Name: grant.Name, Size: grant.Size}}, download: download, progress: progress}
	defer reader.Close()
	a.store.event("info", "virtual", "夸克链库中继播放："+card.Name+" · "+grant.Name)
	http.ServeContent(w, r, grant.Name, time.Time{}, reader)
}

func (a *App) cleanupQuarkLibraryTemporary(now time.Time) {
	if !a.quarkTemporaryMu.TryLock() {
		return
	}
	defer a.quarkTemporaryMu.Unlock()
	st := a.store.snapshotWithLogLimit(0)
	retention := time.Duration(effectiveQuarkLibrary(st).RetentionHours) * time.Hour
	for key, record := range st.QuarkLibraryRuntime.Temporary {
		if record.LastUsed.Add(retention).After(now) || a.quarkPlaybackActive[key] > 0 || record.Folder == "" {
			continue
		}
		s, err := a.store.storage(record.StorageID)
		if err != nil || s.Type != "quark" || storageRevision(s) != record.Revision || storageAuthBlocked(s, now) != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
		files, err := a.rawList(ctx, s, record.Folder)
		if err == nil && len(files) == 0 && (record.FileID != "" || !record.Attempted) {
			err = a.store.update(func(st *State) error { delete(st.QuarkLibraryRuntime.Temporary, key); return nil })
		} else if err == nil && len(files) == 1 && record.FileID != "" && files[0].ID == record.FileID && !files[0].IsDir && files[0].Name == record.Name && files[0].Size == record.Size && !record.Deleting {
			record.Deleting = true
			err = a.store.update(func(st *State) error { st.QuarkLibraryRuntime.Temporary[key] = record; return nil })
			if err == nil {
				current, e := a.store.storage(s.ID)
				if e != nil || storageRevision(current) != record.Revision {
					err = errors.New("清理前存储身份变化")
				} else {
					s.Config["deleteMode"] = "trash"
					err = a.cloudFileAction(ctx, s, s, fileActionRequest{Source: record.Folder, IDs: []string{record.FileID}, Action: "delete"})
				}
			}
			if err == nil {
				files, err = a.rawList(ctx, s, record.Folder)
				if err == nil && len(files) == 0 {
					err = a.store.update(func(st *State) error { delete(st.QuarkLibraryRuntime.Temporary, key); return nil })
					if err == nil {
						a.store.event("info", "virtual", "夸克链库临时文件清理完成："+s.Name+" · "+record.Name)
					}
				}
			}
		}
		if err != nil {
			a.store.event("warn", "virtual", "夸克链库临时清理未确认："+s.Name+" · "+record.Name+"；不重复删除，请核对网盘")
		}
		cancel()
	}
}
