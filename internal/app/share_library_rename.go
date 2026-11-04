package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

func shareLibraryNameChanged(old, next quarkLibraryCard) bool {
	return old.ID != "" && old.Directory != "" && filepath.Clean(old.Directory) == filepath.Clean(next.Directory) && old.DirectoryName != next.DirectoryName
}

func updateShareLibraryRenamedRoot(roots map[string]quarkLibraryOwnership, old, next quarkLibraryCard) {
	if !shareLibraryNameChanged(old, next) {
		return
	}
	if owner := roots[old.ID]; owner.Directory == quarkLibraryRoot(old) {
		owner.Directory = quarkLibraryRoot(next)
		roots[old.ID] = owner
	}
}

// A directory-name edit keeps the entire library and its native identity.
// The caller holds the provider lock and commits configuration before releasing
// the parent handle. A failed configuration write restores the original name.
func (a *App) prepareShareLibraryRename(ctx context.Context, old, next quarkLibraryCard, owner quarkLibraryOwnership) (func(bool) error, quarkLibraryOwnership, error) {
	nop := func(bool) error { return nil }
	if err := ctx.Err(); err != nil {
		return nop, owner, err
	}
	if old.Directory == "" {
		return nop, owner, nil
	}
	if !shareLibraryNameChanged(old, next) || !validShareDirectory(next.DirectoryName) || next.DirectoryName == "" {
		return nop, owner, errors.New("目录改名只能修改同一生成位置内的目录名称")
	}
	for _, protected := range []string{a.store.dir, a.dataDir} {
		if protected != "" && (backupPathsOverlap(quarkLibraryRoot(old), protected) || backupPathsOverlap(protected, quarkLibraryRoot(old))) {
			return nop, owner, errors.New("生成目录不能覆盖配置或运行数据")
		}
	}
	parent, err := openStrmReplaceRoot(old.Directory)
	if err != nil {
		return nop, owner, err
	}
	parentIdentity, err := strmRootIdentity(parent)
	if err != nil {
		parent.Close()
		return nop, owner, err
	}
	fail := func(err error) (func(bool) error, quarkLibraryOwnership, error) {
		parent.Close()
		return nop, owner, err
	}
	before, err := parent.Lstat(old.DirectoryName)
	if os.IsNotExist(err) && owner.Directory == "" {
		parent.Close()
		return nop, owner, nil
	}
	if err != nil {
		return fail(err)
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return fail(errors.New("原生成目录不是普通目录，未改名"))
	}
	if _, err := parent.Lstat(next.DirectoryName); !os.IsNotExist(err) {
		if err == nil {
			err = errShareLibraryDirectory
		}
		return fail(err)
	}
	root, err := openStrmReplaceRoot(quarkLibraryRoot(old))
	if err != nil {
		return fail(err)
	}
	if _, markerErr := root.Lstat(quarkLibraryMarker); markerErr == nil && owner.Directory == "" {
		root.Close()
		return fail(errors.New("原生成目录包含未登记的归属标记，未改名"))
	} else if markerErr != nil && !os.IsNotExist(markerErr) {
		root.Close()
		return fail(markerErr)
	}
	identity, identityErr := strmRootIdentity(root)
	if identityErr == nil && owner.Directory != "" {
		if owner.Directory != quarkLibraryRoot(old) {
			identityErr = errors.New("原生成目录登记已改变，未改名")
		} else {
			identityErr = verifyQuarkLibraryOwner(root, owner)
		}
	}
	root.Close()
	if identityErr != nil {
		return fail(identityErr)
	}
	freshParent, err := openStrmReplaceRoot(old.Directory)
	if err != nil {
		return fail(err)
	}
	freshParentIdentity, err := strmRootIdentity(freshParent)
	freshParent.Close()
	if err != nil || freshParentIdentity != parentIdentity {
		return fail(errors.New("生成位置身份已改变，未改名"))
	}
	freshRoot, err := openStrmReplaceRoot(quarkLibraryRoot(old))
	if err != nil {
		return fail(err)
	}
	freshIdentity, err := strmRootIdentity(freshRoot)
	freshRoot.Close()
	if err != nil || freshIdentity != identity {
		return fail(errors.New("原生成目录身份已改变，未改名"))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := parent.Rename(old.DirectoryName, next.DirectoryName); err != nil {
		return fail(err)
	}
	finish := func(committed bool) error {
		defer parent.Close()
		if committed {
			return nil
		}
		root, err := openStrmReplaceRoot(quarkLibraryRoot(next))
		if err != nil {
			return errors.New("配置保存失败，新目录无法核验，请核对新旧目录")
		}
		currentIdentity, err := strmRootIdentity(root)
		root.Close()
		if err != nil || currentIdentity != identity {
			return errors.New("配置保存失败，目录身份已改变，请核对新旧目录")
		}
		if _, err := parent.Lstat(old.DirectoryName); !os.IsNotExist(err) {
			return errors.New("配置保存失败，原目录名已被占用，请核对新旧目录")
		}
		return parent.Rename(next.DirectoryName, old.DirectoryName)
	}
	owner.Directory, owner.Identity = quarkLibraryRoot(next), identity
	return finish, owner, nil
}
