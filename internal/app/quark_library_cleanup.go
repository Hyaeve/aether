package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"
)

const quarkLibraryMarker = ".aether-library-owner"

type quarkLibraryOwnership struct {
	Directory string `json:"directory"`
	Token     string `json:"token"`
}

func (a *App) ensureQuarkLibraryOwnership(ctx context.Context, card quarkLibraryCard, root *os.Root) error {
	st := a.store.snapshotWithLogLimit(0)
	record := st.QuarkLibraryRuntime.Roots[card.ID]
	recovering := record.Directory == quarkLibraryRoot(card) && record.Token != ""
	if recovering {
		if err := verifyQuarkLibraryOwner(root, record); err == nil {
			return nil
		} else if _, statErr := root.Lstat(quarkLibraryMarker); !os.IsNotExist(statErr) {
			return err
		}
	}
	// An existing foreign marker must never be replaced, including after import.
	if _, err := root.Lstat(quarkLibraryMarker); !os.IsNotExist(err) {
		return errors.New("生成目录已有其他归属标记，未接管或覆盖")
	}
	entries, err := scanQuarkLibraryDisk(ctx, root)
	if err != nil {
		return err
	}
	known := map[string]quarkLibraryFile{}
	media := []string{}
	present := map[string]bool{}
	for _, entry := range entries {
		present[entry.name] = !entry.info.IsDir()
	}
	for _, file := range st.QuarkLibraryRuntime.Files {
		if file.CardID == card.ID {
			known[file.Relative] = file
			if file.Media {
				media = append(media, file.Relative)
			}
		}
	}
	for i := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry := &entries[i]
		if entry.info.IsDir() {
			continue
		}
		entry.digest, err = quarkLibraryDigest(root, entry.name)
		if err != nil {
			return errors.New("生成文件无法完整核验，未恢复归属标记")
		}
		if file, ok := known[entry.name]; ok {
			if entry.digest != file.Digest {
				return errors.New("旧生成目录存在已修改文件，未接管自动清理")
			}
		} else if !slices.ContainsFunc(media, func(name string) bool {
			return (!recovering || present[name]) && (quarkLibraryCompanion(entry.name, name) || quarkLibrarySidecar(entry.name) && path.Dir(entry.name) == path.Dir(name))
		}) {
			return errors.New("生成目录已有未登记的文件，请选择独立目录，未接管或清理")
		}
	}
	// A missing marker is recoverable only from a still-identical, fully
	// inventoried directory. Never overwrite an existing marker to recover.
	identity, err := root.Stat(".")
	if err != nil {
		return err
	}
	verifyInventory := func(ignore string) error {
		if err := a.quarkLibraryCheckExecution(ctx, card); err != nil {
			return err
		}
		live, err := openStrmReplaceRoot(quarkLibraryRoot(card))
		if err != nil {
			return err
		}
		current, statErr := live.Stat(".")
		live.Close()
		if statErr != nil || !os.SameFile(identity, current) {
			return errors.New("生成目录身份已改变，未恢复归属标记")
		}
		fresh, err := scanQuarkLibraryDisk(ctx, root, ignore)
		if err != nil {
			return err
		}
		byName := map[string]quarkLibraryDiskEntry{}
		for _, entry := range fresh {
			if entry.name != ignore {
				byName[entry.name] = entry
			}
		}
		if len(byName) != len(entries) {
			return errors.New("生成目录在归属核验期间出现变动，未恢复标记")
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			current, ok := byName[entry.name]
			if !ok || !os.SameFile(entry.info, current.info) || entry.info.IsDir() != current.info.IsDir() {
				return errors.New("生成目录在归属核验期间已改变，未恢复标记")
			}
			if entry.info.IsDir() {
				continue
			}
			digest, err := quarkLibraryDigest(root, entry.name)
			if err != nil || !sameStrmFile(entry.info, current.info) || digest != entry.digest {
				return errors.New("生成文件在归属核验期间已改变，未恢复标记")
			}
		}
		return nil
	}
	if err := verifyInventory(""); err != nil {
		return err
	}
	previous := record
	if !recovering {
		record = quarkLibraryOwnership{Directory: quarkLibraryRoot(card), Token: id()}
	}
	if err := a.store.update(func(st *State) error {
		if current, ok := quarkLibraryCardAt(*st, card.ID); !ok || current != card {
			return errors.New("卡片已改变，未登记目录归属")
		}
		initQuarkLibraryRuntime(st)
		if st.QuarkLibraryRuntime.Roots[card.ID] != previous {
			return errors.New("生成目录归属记录已改变，未恢复标记")
		}
		st.QuarkLibraryRuntime.Roots[card.ID] = record
		return nil
	}); err != nil {
		return err
	}
	temp := ".aether-owner-" + id()
	f, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	_, err = io.WriteString(f, record.Token)
	if err == nil {
		err = f.Sync()
	}
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err := verifyInventory(temp); err != nil {
		return err
	}
	if a.store.snapshotWithLogLimit(0).QuarkLibraryRuntime.Roots[card.ID] != record {
		return errors.New("生成目录归属记录已改变，未发布标记")
	}
	if err := root.Link(temp, quarkLibraryMarker); err != nil {
		return errors.New("生成目录归属标记发布失败或已出现其他标记，未覆盖")
	}
	if recovering {
		a.store.event("info", "virtual", "夸克链库已核验并恢复缺失的生成目录归属标记："+card.Name)
	}
	return nil
}

func verifyQuarkLibraryOwner(root *os.Root, record quarkLibraryOwnership) error {
	digest, err := quarkLibraryDigest(root, quarkLibraryMarker)
	sum := sha256.Sum256([]byte(record.Token))
	if err != nil || record.Token == "" || digest != hex.EncodeToString(sum[:]) {
		return errors.New("生成目录归属标记缺失或已改变，未执行清理")
	}
	return nil
}

type quarkLibraryDiskEntry struct {
	name   string
	info   os.FileInfo
	digest string
}

// Complete a bounded, symlink-free inventory before authorizing any deletion.
func scanQuarkLibraryDisk(ctx context.Context, root *os.Root, ignored ...string) ([]quarkLibraryDiskEntry, error) {
	var entries []quarkLibraryDiskEntry
	var visit func(*os.Root, string, int) error
	visit = func(current *os.Root, directory string, depth int) error {
		if depth > 32 {
			return errors.New("清理目录超过32层，未删除")
		}
		f, err := current.Open(".")
		if err != nil {
			return err
		}
		defer f.Close()
		for {
			children, readErr := f.ReadDir(200)
			if readErr != nil && readErr != io.EOF {
				return readErr
			}
			for _, child := range children {
				if err := ctx.Err(); err != nil {
					return err
				}
				if directory == "" && (child.Name() == quarkLibraryMarker || slices.Contains(ignored, child.Name())) {
					continue
				}
				name := path.Join(directory, child.Name())
				info, err := current.Lstat(child.Name())
				if err != nil || !safeName(child.Name()) || info.Mode()&os.ModeSymlink != 0 {
					return errors.New("清理目录包含链接、异常名称或身份变化，未删除")
				}
				if len(entries) >= 20000 {
					return errors.New("清理目录超过20000项，未删除")
				}
				entries = append(entries, quarkLibraryDiskEntry{name: name, info: info})
				if info.IsDir() {
					next, err := strmReplaceSubroot(current, child.Name())
					if err != nil {
						return err
					}
					err = visit(next, name, depth+1)
					next.Close()
					if err != nil {
						return err
					}
				} else if !info.Mode().IsRegular() {
					return errors.New("清理目录包含非普通文件，未删除")
				}
			}
			if readErr == io.EOF {
				return nil
			}
		}
	}
	err := visit(root, "", 0)
	return entries, err
}

func quarkLibrarySidecar(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".nfo", ".jpg", ".jpeg", ".png", ".webp", ".ass", ".ssa", ".srt", ".sub", ".vtt", ".tbn":
		return true
	}
	return false
}

func quarkLibraryStem(relative string) string {
	name := strings.TrimSuffix(relative, ".strm")
	return strings.TrimSuffix(name, path.Ext(name))
}

func quarkLibraryCompanion(name, media string) bool {
	if path.Dir(name) != path.Dir(media) || !quarkLibrarySidecar(name) {
		return false
	}
	stem := strings.TrimSuffix(name, path.Ext(name))
	plain := quarkLibraryStem(media)
	full := strings.TrimSuffix(media, ".strm")
	if stem == plain || stem == full {
		return true
	}
	for _, suffix := range []string{"-poster", "-fanart", "-thumb", "-banner", "-landscape", "-clearlogo", "-disc", "-clearart"} {
		if stem == plain+suffix || stem == full+suffix {
			return true
		}
	}
	return false
}

func quarkLibraryParent(root *os.Root, relative string) (*os.Root, error) {
	current, err := root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(path.Dir(relative), "/") {
		if part == "." {
			continue
		}
		next, err := strmReplaceSubroot(current, part)
		current.Close()
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}

func (a *App) reconcileQuarkLibrary(ctx context.Context, card quarkLibraryCard, root *os.Root, wanted map[string]bool, expiry bool) (int, error) {
	st := a.store.snapshotWithLogLimit(0)
	owner := st.QuarkLibraryRuntime.Roots[card.ID]
	if owner.Directory != quarkLibraryRoot(card) {
		return 0, errors.New("生成目录归属未确认，未执行清理")
	}
	identity, err := root.Stat(".")
	if err != nil {
		return 0, err
	}
	check := func() error {
		if err := a.quarkLibraryCheckExecution(ctx, card); err != nil {
			return err
		}
		live, err := openStrmReplaceRoot(quarkLibraryRoot(card))
		if err != nil {
			return errors.New("清理期间生成目录已改变")
		}
		defer live.Close()
		now, err := live.Stat(".")
		if err != nil || !os.SameFile(identity, now) {
			return errors.New("清理期间生成目录身份已改变")
		}
		return verifyQuarkLibraryOwner(live, owner)
	}
	if err := check(); err != nil {
		return 0, err
	}
	entries, err := scanQuarkLibraryDisk(ctx, root)
	if err != nil {
		return 0, err
	}
	known := map[string]quarkLibraryFile{}
	for _, file := range st.QuarkLibraryRuntime.Files {
		if file.CardID == card.ID {
			known[file.Relative] = file
		}
	}
	remove := map[string]bool{}
	staleMedia, liveMedia := []string{}, []string{}
	diskNames := map[string]bool{}
	for i := range entries {
		entry := &entries[i]
		diskNames[entry.name] = true
		if entry.info.IsDir() {
			continue
		}
		file, tracked := known[entry.name]
		if tracked && (expiry || !wanted[entry.name]) {
			entry.digest, err = quarkLibraryDigest(root, entry.name)
			if err != nil || entry.digest != file.Digest {
				if expiry {
					return 0, errors.New("到期目录包含已修改的生成文件，卡片及目录保留，请人工核对")
				}
				a.store.event("warn", "virtual", "夸克链库保留已修改的旧文件："+card.Name+" · "+entry.name)
			} else {
				remove[entry.name] = true
				if file.Media {
					staleMedia = append(staleMedia, entry.name)
				}
			}
		}
		if strings.EqualFold(path.Ext(entry.name), ".strm") && !remove[entry.name] {
			liveMedia = append(liveMedia, entry.name)
		}
	}
	for name, file := range known {
		if file.Media && !diskNames[name] && (expiry || !wanted[name]) {
			staleMedia = append(staleMedia, name)
		}
	}
	for _, entry := range entries {
		if entry.info.IsDir() || remove[entry.name] {
			continue
		}
		if expiry {
			if !quarkLibrarySidecar(entry.name) {
				return 0, errors.New("到期目录包含未登记的文件，卡片及目录保留，请人工核对")
			}
			remove[entry.name] = true
		} else if card.CascadeCleanup != "off" && quarkLibrarySidecar(entry.name) {
			directory := path.Dir(entry.name)
			hasMedia := slices.ContainsFunc(liveMedia, func(name string) bool { return directory == "." || strings.HasPrefix(name, directory+"/") })
			if directory != "." && !hasMedia {
				remove[entry.name] = true
				continue
			}
			for _, stale := range staleMedia {
				if quarkLibraryCompanion(entry.name, stale) && !slices.ContainsFunc(liveMedia, func(name string) bool {
					return quarkLibraryCompanion(entry.name, name)
				}) {
					remove[entry.name] = true
					break
				}
			}
		}
	}
	// Capture hashes for every companion too; no cleanup starts on partial reads.
	for i := range entries {
		entry := &entries[i]
		if remove[entry.name] && entry.digest == "" {
			entry.digest, err = quarkLibraryDigest(root, entry.name)
			if err != nil {
				return 0, errors.New("清理文件无法完整核验，未删除")
			}
		}
	}
	// Revoke obsolete public grants durably before touching their disk outputs.
	// A partial local failure must not leave a deleted file's old grant enabled.
	if err := a.store.update(func(st *State) error {
		if current, ok := quarkLibraryCardAt(*st, card.ID); !ok || current != card {
			return errors.New("卡片已改变，未执行同步清理")
		}
		for key, file := range st.QuarkLibraryRuntime.Files {
			if file.CardID == card.ID && (expiry || !wanted[file.Relative] || remove[file.Relative]) {
				file.Published = false
				st.QuarkLibraryRuntime.Files[key] = file
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !remove[entry.name] {
			continue
		}
		if err := check(); err != nil {
			return removed, err
		}
		parent, err := quarkLibraryParent(root, entry.name)
		if err != nil {
			return removed, err
		}
		name := path.Base(entry.name)
		current, statErr := parent.Lstat(name)
		digest, digestErr := quarkLibraryDigest(parent, name)
		if statErr != nil || digestErr != nil || !os.SameFile(entry.info, current) || digest != entry.digest {
			parent.Close()
			return removed, errors.New("清理提交前文件身份或内容变化，已停止")
		}
		err = parent.Remove(name)
		parent.Close()
		if err != nil {
			return removed, err
		}
		removed++
		a.store.event("info", "virtual", "夸克链库同步清理："+card.Name+" · "+entry.name)
	}
	if err := a.store.update(func(st *State) error {
		for key, file := range st.QuarkLibraryRuntime.Files {
			if file.CardID == card.ID && (expiry || !wanted[file.Relative] || remove[file.Relative]) {
				delete(st.QuarkLibraryRuntime.Files, key)
			}
		}
		return nil
	}); err != nil {
		return removed, err
	}
	if card.CascadeCleanup == "off" && !expiry {
		return removed, nil
	}
	// Remove only now-empty directories, deepest first. New or unrelated files
	// make Remove fail safely instead of being traversed recursively.
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if !entry.info.IsDir() {
			continue
		}
		if err := check(); err != nil {
			return removed, err
		}
		parent, err := quarkLibraryParent(root, entry.name)
		if err != nil {
			return removed, err
		}
		current, err := parent.Lstat(path.Base(entry.name))
		if err == nil && os.SameFile(entry.info, current) {
			_ = parent.Remove(path.Base(entry.name))
		}
		parent.Close()
	}
	if expiry {
		if err := check(); err != nil {
			return removed, err
		}
		remaining, err := scanQuarkLibraryDisk(ctx, root)
		if err != nil || len(remaining) != 0 {
			return removed, errors.New("到期目录仍有文件或目录变化，卡片保留，请人工核对")
		}
		if err := root.Remove(quarkLibraryMarker); err != nil {
			return removed, err
		}
		parent, err := openStrmReplaceRoot(card.Directory)
		if err != nil {
			return removed, err
		}
		defer parent.Close()
		current, err := parent.Lstat(card.DirectoryName)
		if err != nil || !os.SameFile(identity, current) {
			return removed, errors.New("到期生成根目录身份变化，未移除")
		}
		if err := parent.Remove(card.DirectoryName); err != nil {
			return removed, errors.New("到期生成目录未能移除，卡片保留")
		}
		a.store.event("info", "virtual", fmt.Sprintf("夸克链库到期目录清理完成：%s，清理 %d 个文件", card.Name, removed))
	}
	return removed, nil
}

func (a *App) expireQuarkLibraryOutput(ctx context.Context, card quarkLibraryCard) error {
	if card.Directory == "" {
		return nil
	}
	root, err := openStrmReplaceRoot(quarkLibraryRoot(card))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return errors.New("到期生成目录不可核验，卡片保留")
	}
	defer root.Close()
	_, err = a.reconcileQuarkLibrary(ctx, card, root, nil, true)
	return err
}
