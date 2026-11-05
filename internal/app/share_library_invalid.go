package app

import (
	"context"
	"errors"
	"os"
	"path"
	"slices"
	"strings"
)

type librarySourceCleanupKey struct{}
type librarySourceInventoryKey struct{}

func sourceLibraryMediaGone(ctx context.Context, name string) bool {
	if sourceLibraryCleanup(ctx) {
		return true
	}
	present, ok := ctx.Value(librarySourceInventoryKey{}).(map[string]bool)
	if !ok || present[name] {
		return false
	}
	for prefix := range present {
		if strings.HasSuffix(prefix, "/") && strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}

func sourceLibraryCleanup(ctx context.Context) bool {
	value, _ := ctx.Value(librarySourceCleanupKey{}).(bool)
	return value
}

func (a *App) cleanInvalidQuarkLibrary(ctx context.Context, card quarkLibraryCard) (int, error) {
	root, err := openStrmReplaceRoot(quarkLibraryRoot(card))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer root.Close()
	if err := a.ensureQuarkLibraryOwnership(ctx, card, root); err != nil {
		return 0, err
	}
	return a.reconcileQuarkLibrary(context.WithValue(ctx, librarySourceCleanupKey{}, true), card, root, nil, false)
}
func (a *App) cleanInvalidMobileLibrary(ctx context.Context, card quarkLibraryCard) (int, error) {
	root, err := openStrmReplaceRoot(quarkLibraryRoot(card))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer root.Close()
	if err := a.ensureMobileLibraryOwner(ctx, card, root); err != nil {
		return 0, err
	}
	return a.reconcileMobileLibrary(context.WithValue(ctx, librarySourceCleanupKey{}, true), card, root, nil)
}

// Capture all companion candidates before touching the media files. Modified
// registered files and companions still used by a surviving STRM are retained.
func mobileLibraryCompanions(ctx context.Context, root *os.Root, card quarkLibraryCard, files map[string]quarkLibraryFile, wanted map[string]bool) ([]quarkLibraryDiskEntry, error) {
	entries, err := scanQuarkLibraryDisk(ctx, root)
	if err != nil {
		return nil, err
	}
	known := map[string]quarkLibraryFile{}
	stale, live := []string{}, []string{}
	for _, f := range files {
		if f.CardID != card.ID {
			continue
		}
		known[f.Relative] = f
		if !f.Media {
			continue
		}
		digest, e := quarkLibraryDigest(root, f.Relative)
		if !wanted[f.Relative] && (card.CascadeCleanup != "off" || sourceLibraryMediaGone(ctx, f.Relative)) && (os.IsNotExist(e) || e == nil && digest == f.Digest) {
			stale = append(stale, f.Relative)
		} else {
			live = append(live, f.Relative)
		}
	}
	for _, e := range entries {
		if e.info.IsDir() {
			continue
		}
		if _, ok := known[e.name]; !ok && isShareSTRM(e.name) {
			live = append(live, e.name)
		}
	}
	planned := []quarkLibraryDiskEntry{}
	for _, entry := range entries {
		if entry.info.IsDir() || !shareLibraryCascadeType(card, entry.name) || wanted[entry.name] {
			continue
		}
		same := func(media string) bool { return shareLibraryCascadeCompanion(card, entry.name, media) }
		group := func(media string) bool {
			return path.Dir(entry.name) == path.Dir(media) || stringsHasDirectory(media, path.Dir(entry.name))
		}
		candidate := slices.ContainsFunc(stale, same) && !slices.ContainsFunc(live, same)
		if !candidate && strmGroupSidecar.MatchString(path.Base(entry.name)) && len(stale) > 0 && !slices.ContainsFunc(live, group) && slices.ContainsFunc(stale, group) {
			candidate = true
		}
		if !candidate {
			continue
		}
		entry.digest, err = quarkLibraryDigest(root, entry.name)
		if err != nil {
			return nil, err
		}
		if registered, ok := known[entry.name]; ok && registered.Digest != entry.digest {
			continue
		}
		planned = append(planned, entry)
	}
	return planned, nil
}
func stringsHasDirectory(file, dir string) bool {
	return dir == "." || len(file) > len(dir) && file[:len(dir)] == dir && file[len(dir)] == '/'
}

func (a *App) removeMobileLibraryCompanions(ctx context.Context, card quarkLibraryCard, root *os.Root, plan []quarkLibraryDiskEntry) (int, error) {
	removed := 0
	for _, entry := range plan {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if err := generationWorkCheck(ctx); err != nil {
			return removed, err
		}
		if err := a.mobileLibraryCheck(card, nil); err != nil {
			return removed, err
		}
		if err := a.checkMobileLibraryRoot(card, root); err != nil {
			return removed, err
		}
		parent, err := quarkLibraryParent(root, entry.name)
		if err != nil {
			return removed, err
		}
		name := path.Base(entry.name)
		current, e := parent.Lstat(name)
		digest, de := quarkLibraryDigest(parent, name)
		if e != nil || de != nil || !os.SameFile(entry.info, current) || digest != entry.digest {
			parent.Close()
			return removed, errors.New("伴随文件清理前发生变化，已停止")
		}
		err = parent.Remove(name)
		parent.Close()
		if err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// A complete share-directory read can invalidate one file without expiring the
// entire share. Keep every other media file and any companions they still use.
func (a *App) cleanMissingLibraryFile(ctx context.Context, provider string, card quarkLibraryCard, missing quarkLibraryFile) {
	lock := &a.quarkLibraryMu
	if provider == "mobile" {
		lock = &a.mobileLibraryMu
	}
	if !lock.TryLock() {
		return
	}
	defer lock.Unlock()
	st := a.store.snapshotWithLogLimit(0)
	files := st.QuarkLibraryRuntime.Files
	if provider == "mobile" {
		files = st.MobileLibraryRuntime.Files
	}
	if current, ok := files[missing.Key]; !ok || current != missing {
		return
	}
	wanted := map[string]bool{}
	live := []string{}
	for _, f := range files {
		if f.CardID == card.ID && f.Key != missing.Key {
			wanted[f.Relative] = true
			if f.Media {
				live = append(live, f.Relative)
			}
		}
	}
	for name := range wanted {
		if shareLibraryCascadeCompanion(card, name, missing.Relative) && !slices.ContainsFunc(live, func(media string) bool { return shareLibraryCascadeCompanion(card, name, media) }) {
			delete(wanted, name)
		}
	}
	root, err := openStrmReplaceRoot(quarkLibraryRoot(card))
	if err != nil {
		return
	}
	defer root.Close()
	ctx = context.WithValue(ctx, librarySourceCleanupKey{}, true)
	if provider == "mobile" {
		if a.ensureMobileLibraryOwner(ctx, card, root) == nil {
			_, err = a.reconcileMobileLibrary(ctx, card, root, wanted)
		}
	} else {
		if a.ensureQuarkLibraryOwnership(ctx, card, root) == nil {
			_, err = a.reconcileQuarkLibrary(ctx, card, root, wanted, false)
		}
	}
	if err != nil {
		a.store.event("warn", "virtual", "失效分享文件清理待重试："+card.Name+"；"+err.Error())
	}
}
