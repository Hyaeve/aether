package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
)

// Cross-provider operations stage and verify bytes; native IDs never cross accounts.
func (a *App) crossStorageFileAction(ctx context.Context, source, target Storage, req fileActionRequest) (done int, resultErr error) {
	ctx = context.WithValue(ctx, transferSourceKey{}, "文件管理")
	progress := a.beginTransfer(ctx, "copy", source, fmt.Sprintf("%s · %d项", map[bool]string{true: "移动", false: "复制"}[req.Action == "move"], len(req.IDs)), 0,
		transferLocations{From: a.transferDirectory(source, req.Source), To: a.transferDirectory(target, req.Target)})
	defer func() { progress.finish(resultErr) }()
	if progress != nil {
		ctx = progress.ctx
	}
	files, err := a.rawList(ctx, source, req.Source)
	if err != nil {
		return 0, err
	}
	targetFiles, err := a.rawList(ctx, target, req.Target)
	if err != nil {
		return 0, err
	}
	selection := []File{}
	seen := map[string]bool{}
	for _, fid := range req.IDs {
		if seen[fid] {
			return 0, errors.New("重复项目")
		}
		seen[fid] = true
		found := false
		for _, f := range files {
			if f.ID == fid && fid != rootOf(source) {
				selection = append(selection, f)
				found = true
				break
			}
		}
		if !found {
			return 0, errors.New("源目录已变化，请刷新后重试")
		}
	}
	for _, f := range selection {
		for _, existing := range targetFiles {
			if strings.EqualFold(f.Name, existing.Name) {
				return 0, errors.New("目标已有同名项目，不覆盖")
			}
		}
	}
	entries := []backupEntry{}
	count, total := 0, int64(0)
	var walk func(File, string, string, int) error
	walk = func(f File, parent, rel string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 100000 || depth > 128 {
			return errors.New("选择范围超过100000项或128层")
		}
		if !safeName(f.Name) {
			return errors.New("源文件名无效")
		}
		if source.Type == "local" {
			root, err := os.OpenRoot(source.Config["root"])
			if err != nil {
				return err
			}
			name, err := relative(f.ID)
			if err == nil {
				var info os.FileInfo
				info, err = root.Lstat(name)
				if err == nil && info.Mode()&os.ModeSymlink != 0 {
					err = errors.New("不能传输符号链接")
				}
			}
			root.Close()
			if err != nil {
				return err
			}
		}
		entries = append(entries, backupEntry{source: source, file: f, parent: parent, relative: rel})
		if !f.IsDir {
			total += f.Size
			return nil
		}
		children, err := a.rawList(ctx, source, f.ID)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err = walk(child, f.ID, path.Join(rel, child.Name), depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, f := range selection {
		if err = walk(f, req.Source, f.Name, 0); err != nil {
			return 0, err
		}
	}
	progress.setTotal(total)
	cleanup := []backupCleanupEntry{}
	for _, entry := range entries {
		for _, original := range []Storage{source, target} {
			current, e := a.store.storage(original.ID)
			if e != nil || !sameBackupStorage(current, original) {
				return done, errors.New("存储配置已变化，已停止")
			}
		}
		if entry.file.IsDir {
			parent, base, e := a.uploadParent(ctx, target, req.Target, entry.relative)
			if e != nil {
				return done, e
			}
			if e = a.createDirectory(ctx, target, parent, base); e != nil {
				return done, e
			}
			continue
		}
		parent, base, e := a.uploadParent(ctx, target, req.Target, entry.relative)
		if e != nil {
			return done, e
		}
		digest := ""
		if e = a.copyBackupFile(ctx, source, target, entry.file, parent, base, "skip", &digest); e != nil {
			return done, e
		}
		actual, e := a.backupFileDigest(ctx, target, parent, base)
		if e != nil || actual != digest {
			return done, errors.New("目标内容校验失败，源文件保留")
		}
		cleanup = append(cleanup, backupCleanupEntry{entry: entry, digest: digest, targets: []backupCleanupTarget{{storage: target, parent: parent, name: base}}})
		progress.add(int(entry.file.Size))
		done++
	}
	if req.Action == "move" {
		rule := BackupRule{CompletionRule: "delete_source_dir"}
		localDirs := []backupEntry{}
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].source.Type == "local" && entries[i].file.IsDir {
				localDirs = append(localDirs, entries[i])
			}
		}
		if err = a.cleanupBackup(ctx, &rule, cleanup, localDirs); err != nil {
			return done, err
		}
		a.invalidateTransferPaths(source)
	}
	a.store.event("info", "files", fmt.Sprintf("跨存储%s：%s → %s，已校验%d个文件", req.Action, source.Name, target.Name, done))
	return len(selection), nil
}
