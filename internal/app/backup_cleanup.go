package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
)

type backupCleanupTarget struct {
	storage      Storage
	parent, name string
}
type backupCleanupEntry struct {
	entry   backupEntry
	digest  string
	targets []backupCleanupTarget
}

func (a *App) backupFileDigest(ctx context.Context, s Storage, parent, name string) (string, error) {
	f, err := (mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: parent}}).OpenFile(ctx, "/"+name, os.O_RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, &cancelReader{ctx, f})
	return hex.EncodeToString(h.Sum(nil)), err
}

func (a *App) cleanupBackup(ctx context.Context, r *BackupRule, items []backupCleanupEntry, entries []backupEntry) error {
	// Verify every published target and unchanged source before removing anything.
	for _, item := range items {
		for _, target := range item.targets {
			current, err := a.store.storage(target.storage.ID)
			if err != nil || !sameBackupStorage(current, target.storage) {
				return errors.New("目标存储配置变化，取消源文件清理")
			}
			digest, err := a.backupFileDigest(ctx, target.storage, target.parent, target.name)
			if err != nil || digest != item.digest {
				return errors.New("备份目标校验失败，源文件保持不变")
			}
		}
		digest, err := a.backupFileDigest(ctx, item.entry.source, item.entry.parent, item.entry.file.Name)
		if err != nil || digest != item.digest {
			return errors.New("源文件已变化，取消清理")
		}
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if len(a.running) != 0 {
		return errors.New("有任务正在执行，源文件清理已取消")
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := a.store.storage(item.entry.source.ID)
		if err != nil || !sameBackupStorage(current, item.entry.source) {
			return errors.New("源存储配置变化，取消清理")
		}
		digest, err := a.backupFileDigest(ctx, current, item.entry.parent, item.entry.file.Name)
		if err != nil || digest != item.digest {
			return errors.New("清理前源文件变化，保留该文件及后续文件")
		}
		fs := mountFS{app: a, config: MountConfig{StorageID: current.ID, Source: item.entry.parent}}
		if err := fs.RemoveAll(ctx, "/"+item.entry.file.Name); err != nil {
			return err
		}
		r.Deleted++
	}
	if r.CompletionRule == "delete_source_dir" {
		// os.Root.Remove is non-recursive: newly added files prevent directory removal.
		for _, item := range entries {
			if !item.file.IsDir || item.file.ID == "" {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			current, err := a.store.storage(item.source.ID)
			if err != nil || !sameBackupStorage(current, item.source) {
				return errors.New("源存储变化，取消空目录清理")
			}
			rel, err := relative(item.file.ID)
			if err != nil || rel == "." || path.Clean(rel) == "." {
				continue
			}
			root, err := os.OpenRoot(current.Config["root"])
			if err != nil {
				return err
			}
			_ = root.Remove(rel)
			root.Close()
		}
	}
	return nil
}
