package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func shareLibraryRecords(st *State, provider string) (*[]quarkLibraryCard, map[string]quarkLibraryOwnership, map[string]quarkLibraryFile, map[string]quarkLibraryResult) {
	switch provider {
	case "quark":
		initQuarkLibraryRuntime(st)
		return &st.QuarkLibraryCards, st.QuarkLibraryRuntime.Roots, st.QuarkLibraryRuntime.Files, st.QuarkLibraryRuntime.Results
	case "mobile":
		initMobileLibrary(st)
		return &st.MobileLibraryCards, st.MobileLibraryRuntime.Roots, st.MobileLibraryRuntime.Files, st.MobileLibraryRuntime.Results
	case "115":
		initLibrary115(st)
		return &st.Library115Cards, st.Library115Runtime.Roots, st.Library115Runtime.Files, st.Library115Runtime.Results
	default: // All callers select one of the four library endpoints.
		initCASLibrary(st)
		return &st.CASLibraryCards, st.CASLibraryRuntime.Roots, st.CASLibraryRuntime.Files, st.CASLibraryRuntime.Results
	}
}

func shareLibraryCardUnchanged(st *State, provider string, card quarkLibraryCard) bool {
	cards, _, _, _ := shareLibraryRecords(st, provider)
	for _, current := range *cards {
		if current.ID == card.ID {
			return current == card
		}
	}
	return false
}

func libraryDeleteScopeAvailable(st State, provider string, card quarkLibraryCard) error {
	groups := map[string][]quarkLibraryCard{"quark": st.QuarkLibraryCards, "mobile": st.MobileLibraryCards, "115": st.Library115Cards, "cas": st.CASLibraryCards}
	_, ownRoots, _, _ := shareLibraryRecords(&st, provider)
	identity := ownRoots[card.ID].Identity
	for kind, cards := range groups {
		_, peerRoots, _, _ := shareLibraryRecords(&st, kind)
		for _, peer := range cards {
			if kind == provider && peer.ID == card.ID {
				continue
			}
			if quarkLibraryOutputsOverlap(card, peer) || identity != "" && peerRoots[peer.ID].Identity == identity {
				return errors.New("生成目录与其他链库重叠，未清理")
			}
		}
	}
	return nil
}

// The caller holds the provider lock. No cloud files or temporary receipts are
// removed by either action; legacy DELETE requests remain record-only.
func (a *App) shareLibraryDeleteAPI(w http.ResponseWriter, r *http.Request, provider string, card quarkLibraryCard) {
	var in struct {
		DeleteFiles bool `json:"deleteFiles"`
		Confirm     bool `json:"confirm"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	if in.DeleteFiles && !in.Confirm {
		fail(w, 400, errors.New("请确认删除记录及本地生成文件"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	removed := 0
	if in.DeleteFiles {
		var err error
		removed, err = a.purgeShareLibrary(ctx, provider, card)
		if err != nil {
			fail(w, 409, fmt.Errorf("%w；已清理%d个文件，链库记录保留", err, removed))
			return
		}
	}
	err := a.store.update(func(st *State) error {
		if !shareLibraryCardUnchanged(st, provider, card) {
			return errors.New("链库卡片已改变，未移除记录")
		}
		cards, roots, files, results := shareLibraryRecords(st, provider)
		for i, c := range *cards {
			if c.ID == card.ID {
				*cards = append((*cards)[:i], (*cards)[i+1:]...)
				break
			}
		}
		delete(roots, card.ID)
		delete(results, card.ID)
		for key, file := range files {
			if file.CardID == card.ID {
				delete(files, key)
			}
		}
		return nil
	})
	if err != nil {
		fail(w, 500, err)
		return
	}
	message := "链接记录已移除，生成文件保留"
	if in.DeleteFiles {
		message = fmt.Sprintf("链接记录及生成目录已删除，清理%d个文件", removed)
	}
	a.store.event("info", "virtual", card.Name+"："+message)
	jsonResponse(w, 200, map[string]any{"ok": true, "deletedFiles": removed, "message": message})
}

func libraryDeleteCompanion(card quarkLibraryCard, name string, media []string) bool {
	if !quarkLibrarySidecar(name) && !shareLibraryCascadeType(card, name) {
		return false
	}
	for _, source := range media {
		if strmCompanion(name, source) {
			return true
		}
		stem := strings.ToLower(strings.TrimSuffix(path.Base(name), path.Ext(name)))
		dir := path.Dir(name)
		if strmGroupSidecar.MatchString(stem) && (dir == "." || path.Dir(source) == dir || strings.HasPrefix(source, dir+"/")) {
			return true
		}
	}
	return false
}

// Preflight the complete inventory before any deletion. Unknown/modified files
// retain the whole card; an I/O failure after submission leaves it retryable.
func (a *App) purgeShareLibrary(ctx context.Context, provider string, card quarkLibraryCard) (removed int, resultErr error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if card.Directory == "" {
		return 0, nil
	}
	if !filepath.IsAbs(card.Directory) || card.DirectoryName == "" || !validShareDirectory(card.DirectoryName) {
		return 0, errors.New("生成目录无效，未清理")
	}
	for _, protected := range []string{a.store.dir, a.dataDir} {
		if protected != "" && (backupPathsOverlap(quarkLibraryRoot(card), protected) || backupPathsOverlap(protected, quarkLibraryRoot(card))) {
			return 0, errors.New("生成目录与配置或运行数据重叠，未清理")
		}
	}
	a.runMu.Lock()
	release, err := a.reserveTaskFileNodes(ctx, []taskFileNode{{storage: Storage{Type: "local", Config: map[string]string{"root": card.Directory}}, id: card.DirectoryName}})
	a.runMu.Unlock()
	if err != nil {
		return 0, err
	}
	defer release()
	st := a.store.snapshotWithLogLimit(0)
	if !shareLibraryCardUnchanged(&st, provider, card) {
		return 0, errors.New("链库卡片已改变，未清理")
	}
	if err := libraryDeleteScopeAvailable(st, provider, card); err != nil {
		return 0, err
	}
	if provider == "cas" && casLibraryUncertain(st, card) {
		return 0, errors.New("CAS临时转存结果尚未确认，请先核对生成队列回执")
	}
	_, roots, files, _ := shareLibraryRecords(&st, provider)
	owner := roots[card.ID]
	parent, err := openStrmReplaceRoot(card.Directory)
	if err != nil {
		return 0, err
	}
	defer parent.Close()
	parentID, err := strmRootIdentity(parent)
	if err != nil {
		return 0, err
	}
	info, err := parent.Lstat(card.DirectoryName)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, errors.New("生成目录不是普通目录，未清理")
	}
	root, err := strmReplaceSubroot(parent, card.DirectoryName)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := a.store.snapshotWithLogLimit(0)
		_, currentRoots, _, _ := shareLibraryRecords(&now, provider)
		if !shareLibraryCardUnchanged(&now, provider, card) || currentRoots[card.ID] != owner {
			return errors.New("卡片或目录登记已改变，停止清理")
		}
		if err := libraryDeleteScopeAvailable(now, provider, card); err != nil {
			return err
		}
		liveParent, e := openStrmReplaceRoot(card.Directory)
		if e != nil {
			return e
		}
		defer liveParent.Close()
		identity, e := strmRootIdentity(liveParent)
		current, statErr := liveParent.Lstat(card.DirectoryName)
		if e != nil || identity != parentID || statErr != nil || !os.SameFile(info, current) {
			return errors.New("生成位置或目录身份已改变，停止清理")
		}
		if owner.Directory != "" {
			if owner.Directory != quarkLibraryRoot(card) {
				return errors.New("生成目录登记位置不符，未清理")
			}
			return verifyQuarkLibraryOwner(root, owner)
		}
		if _, e := root.Lstat(quarkLibraryMarker); !os.IsNotExist(e) {
			return errors.New("生成目录包含未登记的归属标记，未清理")
		}
		return nil
	}
	if err := check(); err != nil {
		return 0, err
	}
	entries, err := scanQuarkLibraryDisk(ctx, root)
	if err != nil {
		return 0, err
	}
	known, media := map[string]quarkLibraryFile{}, []string{}
	for _, file := range files {
		if file.CardID == card.ID {
			known[file.Relative] = file
			if file.Media {
				media = append(media, file.Relative)
			}
		}
	}
	if len(entries) > 0 && owner.Directory == "" {
		return 0, errors.New("生成目录尚未登记，请先核对并执行卡片后再删除")
	}
	for i := range entries {
		entry := &entries[i]
		if entry.info.IsDir() {
			continue
		}
		file, tracked := known[entry.name]
		if !tracked && !libraryDeleteCompanion(card, entry.name, media) {
			if provider == "cas" && strings.HasSuffix(strings.ToLower(entry.name), ".cas.strm") {
				return 0, errors.New("CAS文件缺少可核验生成记录，请先执行卡片核验；无法核验的文件请移走后再删除")
			}
			return 0, errors.New("生成目录包含未登记文件，请先移走无关文件后再删除")
		}
		entry.digest, err = quarkLibraryDigest(root, entry.name)
		if err != nil || tracked && (file.Digest == "" || file.Digest != entry.digest) {
			return 0, errors.New("生成文件已修改或无法核验，未清理")
		}
	}
	// Check every entry again before revoking grants or removing the first file.
	fresh, err := scanQuarkLibraryDisk(ctx, root)
	if err != nil || len(fresh) != len(entries) {
		return 0, errors.New("生成目录清点期间发生变化，未清理")
	}
	byName := map[string]os.FileInfo{}
	for _, entry := range fresh {
		byName[entry.name] = entry.info
	}
	for _, entry := range entries {
		current := byName[entry.name]
		if current == nil || !os.SameFile(entry.info, current) {
			return 0, errors.New("生成文件身份已改变，未清理")
		}
		if !entry.info.IsDir() {
			if digest, e := quarkLibraryDigest(root, entry.name); e != nil || digest != entry.digest {
				return 0, errors.New("生成文件内容已改变，未清理")
			}
		}
	}
	if err := check(); err != nil {
		return 0, err
	}
	if err := a.store.update(func(st *State) error {
		if !shareLibraryCardUnchanged(st, provider, card) {
			return errors.New("卡片已改变，未清理")
		}
		_, currentRoots, grants, _ := shareLibraryRecords(st, provider)
		if currentRoots[card.ID] != owner {
			return errors.New("目录登记已改变，未清理")
		}
		for key, file := range grants {
			if file.CardID == card.ID {
				file.Published = false
				grants[key] = file
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if err := check(); err != nil {
			return removed, err
		}
		folder, err := quarkLibraryParent(root, entry.name)
		if err != nil {
			return removed, err
		}
		name := path.Base(entry.name)
		for dir := path.Dir(entry.name); dir != "." && err == nil; dir = path.Dir(dir) {
			current, e := root.Lstat(dir)
			if e != nil || byName[dir] == nil || !os.SameFile(byName[dir], current) {
				err = errors.New("生成文件父目录身份已改变，停止清理")
			}
		}
		if err != nil {
			folder.Close()
			return removed, err
		}
		current, err := folder.Lstat(name)
		if err == nil && !os.SameFile(entry.info, current) {
			err = errors.New("生成文件身份已改变，停止清理")
		}
		if err == nil && !entry.info.IsDir() {
			if digest, e := quarkLibraryDigest(folder, name); e != nil || digest != entry.digest {
				err = errors.New("生成文件内容已改变，停止清理")
			}
		}
		if err == nil {
			err = folder.Remove(name)
		}
		folder.Close()
		if err != nil {
			return removed, err
		}
		if !entry.info.IsDir() {
			removed++
		}
	}
	if err := check(); err != nil {
		return removed, err
	}
	if owner.Directory != "" {
		if err := removeLegacyLibraryMarker(root, owner); err != nil {
			return removed, err
		}
	}
	root.Close()
	// Empty-only removal refuses a late addition instead of traversing it.
	if err := parent.Remove(card.DirectoryName); err != nil {
		return removed, err
	}
	return removed, nil
}
