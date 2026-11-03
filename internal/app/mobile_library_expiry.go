package app

import (
	"context"
	"errors"
	"os"
	"sort"
	"time"
)

type mobileLibraryUnavailable struct{}

func (*mobileLibraryUnavailable) Error() string {
	return "移动分享已失效或被取消，请更新分享链接"
}

func (a *App) markMobileLibraryInvalid(card quarkLibraryCard, err error) {
	var unavailable *mobileLibraryUnavailable
	if !errors.As(err, &unavailable) {
		return
	}
	_ = a.store.update(func(st *State) error {
		current, ok := mobileLibraryCardAt(*st, card.ID)
		if !ok || current != card {
			return nil
		}
		result := st.MobileLibraryRuntime.Results[card.ID]
		if result.Status == "running" {
			return nil
		}
		result.Status, result.Message = "invalid", unavailable.Error()
		if result.InvalidSince.IsZero() {
			result.InvalidSince = time.Now()
		}
		st.MobileLibraryRuntime.Results[card.ID] = result
		return nil
	})
}

// Expiry only removes unchanged files in this card's authenticated manifest.
// Network/auth errors never count as an invalid share or authorize deletion.
func (a *App) expireMobileLibrary(ctx context.Context, card quarkLibraryCard) error {
	root, err := openStrmReplaceRoot(quarkLibraryRoot(card))
	if os.IsNotExist(err) {
		return a.store.update(func(st *State) error {
			current, ok := mobileLibraryCardAt(*st, card.ID)
			if !ok || current != card {
				return errors.New("移动链库配置已改变，未移除记录")
			}
			revokeMobileLibraryCard(st, card.ID)
			for i, c := range st.MobileLibraryCards {
				if c == card {
					st.MobileLibraryCards = append(st.MobileLibraryCards[:i], st.MobileLibraryCards[i+1:]...)
					break
				}
			}
			return nil
		})
	}
	if err != nil {
		return err
	}
	defer root.Close()
	if err = a.checkMobileLibraryRoot(card, root); err != nil {
		return err
	}
	entries, err := scanQuarkLibraryDisk(ctx, root)
	if err != nil {
		return err
	}
	st := a.store.snapshotWithLogLimit(0)
	known := map[string]string{}
	for _, f := range st.MobileLibraryRuntime.Files {
		if f.CardID == card.ID {
			known[f.Relative] = f.Digest
		}
	}
	dirs := []string{}
	for _, e := range entries {
		if e.info.IsDir() {
			dirs = append(dirs, e.name)
			continue
		}
		if e.name == quarkLibraryMarker {
			continue
		}
		digest, readErr := quarkLibraryDigest(root, e.name)
		if readErr != nil || known[e.name] == "" || known[e.name] != digest {
			return errors.New("移动链库到期目录存在未登记或已修改文件，记录及目录保留")
		}
	}
	if _, err = a.reconcileMobileLibrary(ctx, card, root, nil); err != nil {
		return err
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, name := range dirs {
		if err = a.checkMobileLibraryRoot(card, root); err != nil {
			return err
		}
		if err = root.Remove(name); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err = a.checkMobileLibraryRoot(card, root); err != nil {
		return err
	}
	if err = a.removeEmptyMobileLibraryRoot(card, root); err != nil {
		return err
	}
	return a.store.update(func(st *State) error {
		current, ok := mobileLibraryCardAt(*st, card.ID)
		if !ok || current != card {
			return errors.New("移动链库配置变化，未移除记录")
		}
		revokeMobileLibraryCard(st, card.ID)
		for i, c := range st.MobileLibraryCards {
			if c == card {
				st.MobileLibraryCards = append(st.MobileLibraryCards[:i], st.MobileLibraryCards[i+1:]...)
				break
			}
		}
		return nil
	})
}

func (a *App) removeEmptyMobileLibraryRoot(card quarkLibraryCard, root *os.Root) error {
	entries, err := scanQuarkLibraryDisk(a.ctx, root)
	if err != nil || len(entries) != 0 {
		return errors.New("到期清理期间出现新文件，目录及记录保留")
	}
	before, err := root.Stat(".")
	if err != nil {
		return err
	}
	parent, err := openStrmReplaceRoot(card.Directory)
	if err != nil {
		return err
	}
	defer parent.Close()
	current, err := parent.Lstat(card.DirectoryName)
	if err != nil || !os.SameFile(before, current) {
		return errors.New("到期生成目录身份已变化")
	}
	st := a.store.snapshotWithLogLimit(0)
	token := st.MobileLibraryRuntime.Roots[card.ID].Token
	if err = a.checkMobileLibraryRoot(card, root); err != nil {
		return err
	}
	if err = root.Remove(quarkLibraryMarker); err != nil {
		return err
	}
	root.Close()
	if err = parent.Remove(card.DirectoryName); err == nil {
		return nil
	}
	// Remove only an empty directory. If another file appeared, restore the
	// exact marker on the same directory so the retained card remains usable.
	restored, e := strmReplaceSubroot(parent, card.DirectoryName)
	if e != nil {
		return errors.Join(err, e)
	}
	defer restored.Close()
	identity, e := restored.Stat(".")
	if e != nil || !os.SameFile(before, identity) {
		return errors.Join(err, errors.New("目录被替换，未恢复标记"))
	}
	f, e := restored.OpenFile(quarkLibraryMarker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return errors.Join(err, e)
	}
	_, e = f.WriteString(token)
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	return errors.Join(err, e)
}

func (a *App) mobileLibraryExpiryTick(ctx context.Context, st State, now time.Time) {
	cfg := effectiveMobileLibrary(st)
	for _, card := range st.MobileLibraryCards {
		r := st.MobileLibraryRuntime.Results[card.ID]
		if !card.Enabled || r.Status != "invalid" || r.InvalidSince.IsZero() || r.InvalidSince.Add(time.Duration(cfg.InvalidDays)*24*time.Hour).After(now) {
			continue
		}
		s, err := a.mobileLibraryStorage(st)
		if err != nil {
			continue
		}
		_, err = a.mobileLibraryShareInfo(ctx, s, card.Secret)
		if err == nil {
			_ = a.store.update(func(st *State) error {
				current, ok := mobileLibraryCardAt(*st, card.ID)
				if !ok || current != card {
					return nil
				}
				result := st.MobileLibraryRuntime.Results[card.ID]
				result.Status, result.Message, result.InvalidSince = "idle", "分享已恢复", time.Time{}
				st.MobileLibraryRuntime.Results[card.ID] = result
				return nil
			})
			continue
		}
		var unavailable *mobileLibraryUnavailable
		if !errors.As(err, &unavailable) {
			continue
		}
		if err = a.expireMobileLibrary(ctx, card); err != nil {
			a.store.event("warn", "virtual", "移动链库到期清理暂停："+card.Name+"；"+err.Error())
		}
	}
}
