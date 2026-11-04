package app

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type mobileLibraryTemporary struct {
	quarkLibraryTemporary
	Grant string `json:"grant"`
	Hash  string `json:"hash,omitempty"`
}

func (a *App) mobileLibraryPersonalFolder(ctx context.Context, s Storage, host, parent, name string, check func() error) (string, error) {
	if err := check(); err != nil {
		return "", err
	}
	files, err := a.mobileListAt(ctx, s, host, parent)
	if err != nil {
		return "", err
	}
	var found *File
	for _, f := range files {
		if f.Name == name {
			if found != nil || !f.IsDir || f.ID == "" || f.ID == parent {
				return "", errors.New("移动链库临时目录重名或身份异常")
			}
			copy := f
			found = &copy
		}
	}
	if found != nil {
		return found.ID, check()
	}
	return a.generationCreateFolder(ctx, s, host, parent, name)
}

// Intent precedes each cloud write. An unknown write is never submitted again,
// including after restart or selection of a different account by rotation.
func (a *App) mobileLibraryTransferredLink(ctx context.Context, card quarkLibraryCard, grant quarkLibraryFile, s Storage, share mobileLibraryShare, source mobileLibrarySource, check func() error) (Download, error) {
	resource, err := mobileLibraryDownloadResource(source)
	if err != nil {
		return Download{}, err
	}
	a.mobileTemporaryMu.Lock()
	defer a.mobileTemporaryMu.Unlock()
	ctx = context.WithValue(ctx, generationWorkGuardKey{}, check)
	if err := check(); err != nil {
		return Download{}, err
	}
	if source.Size < 0 {
		return Download{}, errors.New("移动分享文件大小未知，未执行临时转存")
	}
	key := quarkLibraryStamp(grant.Key + "\x00" + s.ID + "\x00" + grant.ShareStamp)
	st := a.store.snapshotWithLogLimit(0)
	for _, pending := range st.MobileLibraryRuntime.Temporary {
		if pending.Grant == grant.Key && (pending.Folder == "" || pending.Attempted && pending.FileID == "" && (pending.Task == "" || pending.Key != key) || pending.Deleting) {
			return Download{}, errors.New("移动链库上次临时转存或清理结果未确认，未重复写入，请核对网盘")
		}
	}
	record, exists := st.MobileLibraryRuntime.Temporary[key]
	if exists && (record.Revision != storageRevision(s) || record.Name != source.Name || record.Size != source.Size) {
		return Download{}, errors.New("移动链库临时文件身份已变化，未重新转存")
	}
	host, err := a.mobileHost(ctx, s)
	if err != nil {
		return Download{}, err
	}
	save := func() error {
		return a.store.update(func(st *State) error {
			initMobileLibrary(st)
			if len(st.MobileLibraryRuntime.Temporary) >= 10000 {
				if _, ok := st.MobileLibraryRuntime.Temporary[key]; !ok {
					return errors.New("移动链库临时回执已达10000项，请先核对清理")
				}
			}
			st.MobileLibraryRuntime.Temporary[key] = record
			return nil
		})
	}
	if !exists {
		record = mobileLibraryTemporary{quarkLibraryTemporary: quarkLibraryTemporary{Key: key, StorageID: s.ID, Revision: storageRevision(s), FolderName: "mobile-play-" + id(), Name: source.Name, Size: source.Size, LastUsed: time.Now()}, Grant: grant.Key}
		if err = save(); err != nil {
			return Download{}, err
		}
		parent, e := a.mobileLibraryPersonalFolder(ctx, s, host, "/", "Aether", check)
		if e != nil {
			return Download{}, e
		}
		parent, e = a.mobileLibraryPersonalFolder(ctx, s, host, parent, "Share", check)
		if e != nil {
			return Download{}, e
		}
		record.Parent = parent
		if err = save(); err != nil {
			return Download{}, err
		}
		record.Folder, err = a.generationCreateFolder(ctx, s, host, parent, record.FolderName)
		if err != nil {
			return Download{}, err
		}
		if err = save(); err != nil {
			return Download{}, err
		}
	}
	if err = check(); err != nil {
		return Download{}, err
	}
	parents, err := a.mobileListAt(ctx, s, host, record.Parent)
	if err != nil {
		return Download{}, err
	}
	verified := false
	for _, f := range parents {
		if f.ID == record.Folder && f.Name == record.FolderName && f.IsDir {
			verified = true
		}
	}
	if !verified {
		return Download{}, errors.New("移动链库临时目录身份未确认")
	}
	files, err := a.mobileListAt(ctx, s, host, record.Folder)
	if err != nil {
		return Download{}, err
	}
	if !record.Attempted {
		if len(files) != 0 {
			return Download{}, errors.New("移动链库临时目录非空，未转存")
		}
		record.Attempted = true
		if err = save(); err != nil {
			return Download{}, err
		}
		record.Task, err = a.generationTransfer(ctx, s, []GenerationEntry{{Mobile: &generationMobileSource{Code: share.Code, Pass: share.Pass, Token: resource}}}, record.Folder)
		if err != nil {
			return Download{}, err
		}
		if err = save(); err != nil {
			return Download{}, err
		}
	}
	if record.FileID == "" {
		if err = a.generationWaitTransfer(ctx, s, record.Task); err != nil {
			return Download{}, err
		}
		files, err = a.mobileListAt(ctx, s, host, record.Folder)
		if err != nil {
			return Download{}, err
		}
		if len(files) != 1 || files[0].IsDir || files[0].Name != source.Name || files[0].Size != source.Size || files[0].ID == record.Folder {
			return Download{}, errors.New("移动链库转存文件身份未确认，未取直链")
		}
		record.FileID, record.Hash = files[0].ID, files[0].SHA256
	}
	if len(files) != 1 || files[0].ID != record.FileID || files[0].Name != record.Name || files[0].Size != record.Size || files[0].IsDir || files[0].SHA256 != record.Hash {
		return Download{}, errors.New("移动链库已转存文件变化，未继续播放")
	}
	if err = check(); err != nil {
		return Download{}, err
	}
	record.LastUsed = time.Now()
	if err = save(); err != nil {
		return Download{}, err
	}
	download, err := a.mobileLinkAt(ctx, s, host, record.FileID)
	if err != nil {
		return Download{}, err
	}
	if err = check(); err != nil {
		return Download{}, err
	}
	return download, nil
}

func (a *App) cleanupMobileLibraryTemporary(now time.Time) {
	if !a.mobileTemporaryMu.TryLock() {
		return
	}
	defer a.mobileTemporaryMu.Unlock()
	st := a.store.snapshotWithLogLimit(0)
	ttl := time.Duration(effectiveMobileLibrary(st).RetentionHours) * time.Hour
	for key, record := range st.MobileLibraryRuntime.Temporary {
		if record.LastUsed.Add(ttl).After(now) || record.Folder == "" || record.FileID == "" || record.Deleting {
			continue
		}
		s, err := a.store.storage(record.StorageID)
		if err != nil || !nativeMobile(s) || storageRevision(s) != record.Revision || storageAuthBlocked(s, now) != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
		host, err := a.mobileHost(ctx, s)
		if err == nil {
			var parents []File
			parents, err = a.mobileListAt(ctx, s, host, record.Parent)
			found := false
			for _, f := range parents {
				if f.ID == record.Folder && f.Name == record.FolderName && f.IsDir {
					found = true
				}
			}
			if err == nil && !found {
				err = errors.New("临时目录身份变化，未清理")
			}
		}
		var files []File
		if err == nil {
			files, err = a.mobileListAt(ctx, s, host, record.Folder)
		}
		if err == nil && len(files) == 0 {
			err = a.store.update(func(st *State) error { delete(st.MobileLibraryRuntime.Temporary, key); return nil })
		} else if err == nil && len(files) == 1 && files[0].ID == record.FileID && files[0].Name == record.Name && files[0].Size == record.Size && !files[0].IsDir && files[0].SHA256 == record.Hash {
			record.Deleting = true
			err = a.store.update(func(st *State) error { st.MobileLibraryRuntime.Temporary[key] = record; return nil })
			if err == nil {
				current, e := a.store.storage(s.ID)
				if e != nil || storageRevision(current) != record.Revision {
					err = errors.New("清理前账号身份变化")
				} else {
					err = a.removeCASTemporary(ctx, s, host, record.FileID)
				}
			}
			if err == nil {
				files, err = a.mobileListAt(ctx, s, host, record.Folder)
				if err == nil && len(files) == 0 {
					err = a.store.update(func(st *State) error { delete(st.MobileLibraryRuntime.Temporary, key); return nil })
				} else if err == nil {
					err = errors.New("删除结果未确认")
				}
			}
		}
		if err != nil {
			a.store.event("warn", "virtual", fmt.Sprintf("移动链库临时清理未确认：%s · %s；未重复删除", s.Name, record.Name))
		}
		cancel()
	}
}
