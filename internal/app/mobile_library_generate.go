package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"time"
)

type mobileLibraryPlan struct {
	Source   mobileLibrarySource
	Relative string
	Media    bool
}

func (a *App) planMobileLibrary(ctx context.Context, storage Storage, card quarkLibraryCard, cfg mobileLibrarySettings) ([]mobileLibraryPlan, mobileLibraryShare, error) {
	share, err := a.mobileLibraryShareInfo(ctx, storage, card.Secret)
	if err != nil {
		return nil, share, err
	}
	media, metadata := card.MediaExtensions, card.MetadataExtensions
	if media == "" {
		media = cfg.MediaExtensions
	}
	if metadata == "" {
		metadata = cfg.MetadataExtensions
	}
	plans := []mobileLibraryPlan{}
	visited, paths := map[string]bool{}, map[string]bool{}
	count := 0
	var walk func(string, string, int) error
	walk = func(parent, directory string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 32 || visited[parent] {
			return errors.New("移动分享目录超过32层或包含循环，未同步清理")
		}
		visited[parent] = true
		items, err := a.mobileLibraryList(ctx, storage, share, parent, false)
		if err != nil {
			return err
		}
		count += len(items)
		if count > mobileShareScanLimit {
			return errors.New("移动分享总条目超过扫描上限，未同步清理")
		}
		// The card's directory replaces a sole shared root folder.
		if depth == 0 && len(items) == 1 && items[0].Directory {
			return walk(items[0].Path, "", depth+1)
		}
		for _, item := range items {
			if quarkLibraryExcluded(card, directory, item.Name) {
				continue
			}
			relative := path.Join(directory, item.Name)
			if !quarkLibraryRelative(relative) || paths[relative] {
				return errors.New("移动分享含不安全或同名路径，未同步清理")
			}
			paths[relative] = true
			if item.Directory {
				if err := walk(item.Path, relative, depth+1); err != nil {
					return err
				}
				continue
			}
			isMedia := taskMedia(Task{MediaExtensions: media}, item.Name)
			if !isMedia && !excludedType(item.Name, metadata) {
				continue
			}
			if isMedia {
				relative += shareSTRMSuffix
			}
			if len(plans) >= 20000 {
				return errors.New("移动链库生成记录超过20000项，未执行同步清理")
			}
			plans = append(plans, mobileLibraryPlan{item, relative, isMedia})
		}
		return nil
	}
	err = walk("root", "", 0)
	return plans, share, err
}

func (a *App) ensureMobileLibraryOwner(ctx context.Context, card quarkLibraryCard, root *os.Root) error {
	st := a.store.snapshotWithLogLimit(0)
	record := st.MobileLibraryRuntime.Roots[card.ID]
	same := record.Directory == quarkLibraryRoot(card) && record.Token != ""
	if same {
		if err := verifyQuarkLibraryOwner(root, record); err == nil {
			return nil
		}
	}
	if _, err := root.Lstat(quarkLibraryMarker); !os.IsNotExist(err) {
		return errors.New("移动链库生成目录归属标记不符，未覆盖或清理")
	}
	// Missing markers may be reconstructed only from a complete unchanged manifest.
	entries, err := scanQuarkLibraryDisk(ctx, root)
	if err != nil {
		return err
	}
	known := map[string]quarkLibraryFile{}
	for _, f := range st.MobileLibraryRuntime.Files {
		if f.CardID == card.ID && f.Digest != "" {
			known[f.Relative] = f
		}
	}
	for _, entry := range entries {
		if entry.info.IsDir() {
			continue
		}
		f, ok := known[entry.name]
		digest, err := quarkLibraryDigest(root, entry.name)
		if !ok || err != nil || digest != f.Digest {
			return errors.New("移动链库生成目录含未登记或已修改文件，未接管目录")
		}
	}
	if !same {
		record = quarkLibraryOwnership{Directory: quarkLibraryRoot(card), Token: id()}
	}
	if err := a.mobileLibraryCheck(card, nil); err != nil {
		return err
	}
	if err := a.store.update(func(st *State) error {
		if current, ok := mobileLibraryCardAt(*st, card.ID); !ok || current != card {
			return errors.New("移动链库配置已改变，未登记目录归属")
		}
		initMobileLibrary(st)
		st.MobileLibraryRuntime.Roots[card.ID] = record
		return nil
	}); err != nil {
		return err
	}
	temp := ".aether-mobile-owner-" + id()
	f, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	_, err = io.WriteString(f, record.Token)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = a.mobileLibraryCheck(card, nil)
	}
	if err == nil {
		err = root.Link(temp, quarkLibraryMarker)
	}
	return err
}

func (a *App) checkMobileLibraryRoot(card quarkLibraryCard, root *os.Root) error {
	a.store.mu.RLock()
	record := a.store.state.MobileLibraryRuntime.Roots[card.ID]
	a.store.mu.RUnlock()
	if record.Directory != quarkLibraryRoot(card) {
		return errors.New("移动链库生成目录归属已改变")
	}
	live, err := openStrmReplaceRoot(record.Directory)
	if err != nil {
		return errors.New("移动链库生成目录已被移动或替换")
	}
	defer live.Close()
	before, err := root.Stat(".")
	current, currentErr := live.Stat(".")
	if err != nil || currentErr != nil || !os.SameFile(before, current) {
		return errors.New("移动链库生成目录身份已改变")
	}
	if err := verifyQuarkLibraryOwner(live, record); err != nil {
		return err
	}
	return verifyQuarkLibraryOwner(root, record)
}

func (a *App) publishMobileLibrary(ctx context.Context, card quarkLibraryCard, storage Storage, root *os.Root, grant quarkLibraryFile, content []byte, old *quarkLibraryFile) error {
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.mobileLibraryCheck(card, &storage); err != nil {
			return err
		}
		return a.checkMobileLibraryRoot(card, root)
	}
	if err := check(); err != nil {
		return err
	}
	sum := sha256.Sum256(content)
	grant.Digest, grant.Stage, grant.Published = hex.EncodeToString(sum[:]), "publish", false
	if err := a.store.update(func(st *State) error {
		initMobileLibrary(st)
		if _, exists := st.MobileLibraryRuntime.Files[grant.Key]; !exists && len(st.MobileLibraryRuntime.Files) >= 20000 {
			return errors.New("移动链库登记总量达到20000项")
		}
		st.MobileLibraryRuntime.Files[grant.Key] = grant
		return nil
	}); err != nil {
		return err
	}
	child, err := virtualOutputRoot(root, path.Dir(grant.Relative))
	if err != nil {
		return err
	}
	defer child.Close()
	temp := ".aether-mobile-" + id()
	f, err := child.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer child.Remove(temp)
	_, err = io.Copy(f, bytes.NewReader(content))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = check()
	}
	if err == nil {
		if _, statErr := child.Lstat(path.Base(grant.Relative)); os.IsNotExist(statErr) {
			err = child.Link(temp, path.Base(grant.Relative))
		} else if statErr != nil {
			err = statErr
		} else {
			digest, e := quarkLibraryDigest(root, grant.Relative)
			if old == nil || e != nil || digest != old.Digest {
				err = errors.New("移动链库输出已被修改，未覆盖")
			} else {
				err = child.Rename(temp, path.Base(grant.Relative))
			}
		}
	}
	if err != nil {
		_ = a.store.update(func(st *State) error {
			if old != nil {
				st.MobileLibraryRuntime.Files[old.Key] = *old
			} else {
				delete(st.MobileLibraryRuntime.Files, grant.Key)
			}
			return nil
		})
		return err
	}
	if err := check(); err != nil {
		return err
	}
	grant.Published, grant.Stage = true, ""
	return a.store.update(func(st *State) error { st.MobileLibraryRuntime.Files[grant.Key] = grant; return nil })
}

func (a *App) reconcileMobileLibrary(ctx context.Context, card quarkLibraryCard, root *os.Root, wanted map[string]bool) (int, error) {
	st := a.store.snapshotWithLogLimit(0)
	record := st.MobileLibraryRuntime.Roots[card.ID]
	if record.Directory != quarkLibraryRoot(card) {
		return 0, errors.New("移动生成目录归属已改变，未清理")
	}
	if err := verifyQuarkLibraryOwner(root, record); err != nil {
		return 0, err
	}
	removed := 0
	for key, file := range st.MobileLibraryRuntime.Files {
		if file.CardID != card.ID || wanted[file.Relative] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if err := a.mobileLibraryCheck(card, nil); err != nil {
			return removed, err
		}
		file.Published = false
		if err := a.store.update(func(st *State) error { st.MobileLibraryRuntime.Files[key] = file; return nil }); err != nil {
			return removed, err
		}
		parent, err := quarkLibraryParent(root, file.Relative)
		if os.IsNotExist(err) {
			_ = a.store.update(func(st *State) error { delete(st.MobileLibraryRuntime.Files, key); return nil })
			continue
		}
		if err != nil {
			return removed, err
		}
		name := path.Base(file.Relative)
		digest, err := quarkLibraryDigest(parent, name)
		if os.IsNotExist(err) {
			parent.Close()
			_ = a.store.update(func(st *State) error { delete(st.MobileLibraryRuntime.Files, key); return nil })
			continue
		}
		if err != nil || digest != file.Digest {
			parent.Close()
			continue
		}
		if err = a.checkMobileLibraryRoot(card, root); err == nil {
			err = parent.Remove(name)
		}
		parent.Close()
		if err != nil {
			return removed, err
		}
		if err = a.store.update(func(st *State) error { delete(st.MobileLibraryRuntime.Files, key); return nil }); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func (a *App) runMobileLibrary(ctx context.Context, card quarkLibraryCard) (result quarkLibraryResult, runErr error) {
	if !card.Enabled || card.Directory == "" {
		return result, errors.New("请设置移动链库生成位置并启用链接")
	}
	st := a.store.snapshotWithLogLimit(0)
	cfg := effectiveMobileLibrary(st)
	result = quarkLibraryResult{Status: "running", Message: "正在读取移动分享", LastRun: time.Now(), NextRun: quarkLibraryNext(card, time.Now())}
	if err := a.store.update(func(st *State) error {
		initMobileLibrary(st)
		st.MobileLibraryRuntime.Results[card.ID] = result
		return nil
	}); err != nil {
		return result, err
	}
	defer func() {
		if runErr != nil {
			result.Status, result.Message = "failed", mobileLibraryFailure(runErr)
		} else {
			result.Status, result.Message = "completed", fmt.Sprintf("新增STRM %d 条，元数据 %d 个，跳过 %d 个，清理 %d 个", result.Generated, result.Metadata, result.Skipped, result.Removed)
		}
		err := a.store.update(func(st *State) error {
			if current, ok := mobileLibraryCardAt(*st, card.ID); !ok || current != card {
				return errors.New("移动链库配置已改变，未覆盖新结果")
			}
			st.MobileLibraryRuntime.Results[card.ID] = result
			return nil
		})
		runErr = errors.Join(runErr, err)
		level := "info"
		if runErr != nil {
			level = "warn"
		}
		a.store.event(level, "virtual", "移动链库「"+card.Name+"」："+result.Message)
	}()
	storage, err := a.mobileLibraryStorage(st)
	if err != nil {
		return result, err
	}
	plans, share, err := a.planMobileLibrary(ctx, storage, card, cfg)
	if err != nil {
		return result, err
	}
	if err := a.mobileLibraryCheck(card, &storage); err != nil {
		return result, err
	}
	parent, err := openStrmReplaceRoot(card.Directory)
	if err != nil {
		return result, err
	}
	defer parent.Close()
	root, err := virtualOutputRoot(parent, card.DirectoryName)
	if err != nil {
		return result, err
	}
	defer root.Close()
	if err := a.ensureMobileLibraryOwner(ctx, card, root); err != nil {
		return result, err
	}
	known := map[string]quarkLibraryFile{}
	for _, f := range st.MobileLibraryRuntime.Files {
		if f.CardID == card.ID {
			known[f.Relative] = f
		}
	}
	wanted := map[string]bool{}
	for _, plan := range plans {
		if wanted[plan.Relative] {
			return result, errors.New("移动分享输出路径重复，未执行清理")
		}
		wanted[plan.Relative] = true
	}
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := a.mobileLibraryCheck(card, &storage); err != nil {
			return result, err
		}
		file := plan.Source
		grant := quarkLibraryFile{Key: id(), CardID: card.ID, ShareStamp: quarkLibraryStamp(card.Secret), SourceID: file.Path,
			SourceParent: file.Parent, Name: file.Name, Size: file.Size, Relative: plan.Relative, Media: plan.Media}
		var previous *quarkLibraryFile
		if f, ok := known[plan.Relative]; ok {
			previous, grant.Key = &f, f.Key
		}
		digest, digestErr := quarkLibraryDigest(root, plan.Relative)
		if digestErr == nil {
			if previous == nil || digest != previous.Digest {
				result.Skipped++
				continue
			}
			current := previous.ShareStamp == grant.ShareStamp && previous.SourceID == grant.SourceID && previous.SourceParent == grant.SourceParent && previous.Size == grant.Size
			if plan.Media {
				sum := sha256.Sum256([]byte(mobileLibraryURL(card, plan.Relative, a.currentSTRMSettings().Address, a.currentSTRMSettings().Content == "encoded")))
				current = current && previous.Digest == hex.EncodeToString(sum[:])
			}
			if current {
				if !previous.Published {
					previous.Published, previous.Stage = true, ""
					if err := a.store.update(func(st *State) error { st.MobileLibraryRuntime.Files[previous.Key] = *previous; return nil }); err != nil {
						return result, err
					}
				}
				result.Skipped++
				continue
			}
		} else if !os.IsNotExist(digestErr) {
			return result, digestErr
		}
		var content []byte
		if plan.Media {
			content = []byte(mobileLibraryURL(card, plan.Relative, a.currentSTRMSettings().Address, a.currentSTRMSettings().Content == "encoded"))
		} else {
			if file.Size < 0 || file.Size > 64<<20 {
				return result, errors.New("移动分享元数据大小未知或超过64MiB，未下载")
			}
			download, err := a.mobileLibraryDownload(ctx, storage, share, file)
			if err != nil {
				return result, err
			}
			reader := &davFile{ctx: ctx, info: davInfo{File{Name: file.Name, Size: file.Size}}, download: download}
			content, err = io.ReadAll(io.LimitReader(reader, 64<<20+1))
			reader.Close()
			if err != nil || int64(len(content)) != file.Size {
				return result, errors.New("移动分享元数据下载不完整，未发布")
			}
		}
		if err := a.publishMobileLibrary(ctx, card, storage, root, grant, content, previous); err != nil {
			return result, err
		}
		if plan.Media {
			result.Generated++
		} else {
			result.Metadata++
		}
	}
	result.Removed, err = a.reconcileMobileLibrary(ctx, card, root, wanted)
	return result, err
}
