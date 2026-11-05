package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// Reuse the native volume/file-index (Windows) or device/inode (Unix) identity.
// It lives in the authenticated runtime manifest, never in the media directory.
func strmRootIdentity(root *os.Root) (string, error) {
	f, err := root.Open(".")
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		return "", errors.New("无法核验STRM生成目录")
	}
	identity, err := renameLocalIdentity(f)
	if err != nil || identity == "" || identity == "0:0" || identity == "0:0:0" {
		return "", errors.New("文件系统无法提供STRM目录身份，未执行覆盖或清理")
	}
	return identity, nil
}

// Legacy markers are read only for an authenticated one-time migration. Unknown
// or replaced files are retained; absence cannot establish old ownership.
func (h *strmTaskSync) rootOwnership(root *os.Root) (bool, error) {
	identity, err := strmRootIdentity(root)
	if err != nil {
		return false, err
	}
	if h.manifest.RootIdentity != "" {
		if identity != h.manifest.RootIdentity {
			return false, errors.New("STRM生成目录身份与内部登记不符，请执行全量同步核验已有产物并恢复关联")
		}
		return true, nil
	}
	name := strmOwnerName(h.task.ID)
	if _, err := root.Lstat(name); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	token, err := strmReadOwner(root, name)
	if err != nil || token != h.manifest.Token {
		return false, errors.New("旧STRM归属文件与任务登记不符，未修改")
	}
	return true, nil
}

func (h *strmTaskSync) bindRootIdentity(ctx context.Context) error {
	identity, err := strmRootIdentity(h.root)
	if err != nil {
		return err
	}
	owned, err := h.rootOwnership(h.root)
	if err != nil || !owned && len(h.manifest.Files) > 0 && !h.task.resetGeneration {
		// Device numbers can change across mounts. A different inode, or a
		// legacy library without its marker, requires an explicit full sync.
		before, after := strings.SplitN(h.manifest.RootIdentity, ":", 2), strings.SplitN(identity, ":", 2)
		volumeChanged := len(before) == 2 && len(after) == 2 && before[0] != after[0] && before[1] == after[1]
		if !h.task.fullSyncGeneration && !volumeChanged {
			if err != nil {
				return err
			}
			return errors.New("旧STRM生成库缺少归属文件，请执行全量同步核验已有产物并恢复关联")
		}
		if err := h.recoverRootIdentity(ctx, identity); err != nil {
			return err
		}
		owned = true
	}
	if !owned && len(h.manifest.Files) > 0 && !h.task.resetGeneration {
		return errors.New("旧STRM生成库身份无法核验，请核对或确认全量重置")
	}
	if h.manifest.RootIdentity == "" {
		h.manifest.RootIdentity = identity
		if err := h.save(); err != nil {
			return err
		}
	}
	if err := h.check(ctx); err != nil {
		return err
	}
	name := strmOwnerName(h.task.ID)
	before, err := h.root.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	token, err := strmReadOwner(h.root, name)
	if err != nil || token != h.manifest.Token {
		return errors.New("旧STRM归属文件不符，保留原文件")
	}
	after, err := h.root.Lstat(name)
	if err != nil || !os.SameFile(before, after) {
		return errors.New("旧STRM归属文件已改变，保留原文件")
	}
	if err := h.check(ctx); err != nil {
		return err
	}
	return h.root.Remove(name)
}

// Recovery uses the authenticated output ledger, never just a matching path.
// Unknown files are not adopted. Per-file digest checks still govern later
// overwrites/deletions, and the open directory stays pinned throughout the run.
func (h *strmTaskSync) recoverRootIdentity(ctx context.Context, identity string) error {
	if err := h.cleanupScopes(); err != nil {
		return err
	}
	if err := h.checkRoot(ctx, identity); err != nil {
		return err
	}
	marker := strmOwnerName(h.task.ID)
	if _, err := h.root.Lstat(marker); err == nil {
		token, err := strmReadOwner(h.root, marker)
		if err != nil || token != h.manifest.Token {
			return errors.New("旧STRM归属文件不符，未恢复目录关联")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	matched := 0
	for name, expected := range h.manifest.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		digest, _, err := strmOutputDigest(h.root, name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || digest != expected {
			return errors.New("STRM生成库含无法核验或已修改的登记文件，未恢复目录关联：" + name)
		}
		matched++
	}
	if matched == 0 {
		entries, err := fs.ReadDir(h.root.FS(), ".")
		if err != nil || len(entries) != 0 || !h.task.fullSyncGeneration {
			return errors.New("STRM生成库没有可核验的登记产物，未恢复目录关联；请核对生成位置")
		}
	}
	if err := h.checkRoot(ctx, identity); err != nil {
		return err
	}
	previous := h.manifest.RootIdentity
	h.manifest.RootIdentity = identity
	if err := h.save(); err != nil {
		h.manifest.RootIdentity = previous
		return err
	}
	h.app.store.event("info", "tasks", "STRM任务「"+h.task.Name+"」：已核验登记产物并恢复生成目录关联")
	return nil
}
