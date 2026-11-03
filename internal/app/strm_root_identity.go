package app

import (
	"context"
	"errors"
	"os"
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
			return false, errors.New("STRM生成目录身份与内部登记不符")
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
	if err != nil {
		return err
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
