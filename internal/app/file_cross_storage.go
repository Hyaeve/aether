package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
)

type fileTransferError struct {
	phase, item, reason string
	cause               error
}

func (e *fileTransferError) Error() string {
	if e.item != "" {
		return fmt.Sprintf("%s（项目 %q）：%s", e.phase, e.item, e.reason)
	}
	return e.phase + "：" + e.reason
}
func (e *fileTransferError) Unwrap() error { return e.cause }

type fileTransferDiagnosticKey struct{}
type fileTransferProgressKey struct{}
type fileTransferCountWriter struct{ progress *transferProgress }

func (w fileTransferCountWriter) Write(p []byte) (int, error) {
	w.progress.add(len(p))
	return len(p), nil
}

// Shared backup helpers retain their existing errors outside file-manager copies.
func diagnoseFileTransfer(ctx context.Context, phase string, err error) error {
	if enabled, _ := ctx.Value(fileTransferDiagnosticKey{}).(bool); !enabled || err == nil {
		return err
	}
	var diagnosed *fileTransferError
	if errors.As(err, &diagnosed) {
		return err
	}
	return &fileTransferError{phase: phase, reason: transferFailureReason(err), cause: err}
}

func rejectFileTransfer(ctx context.Context, phase, reason string) error {
	err := errors.New(reason)
	if enabled, _ := ctx.Value(fileTransferDiagnosticKey{}).(bool); enabled {
		return &fileTransferError{phase: phase, reason: reason, cause: err}
	}
	return err
}

// Cross-provider operations stage and verify bytes; native IDs never cross accounts.
func (a *App) crossStorageFileAction(ctx context.Context, source, target Storage, req fileActionRequest) (done int, resultErr error) {
	ctx = context.WithValue(ctx, transferSourceKey{}, "文件管理")
	ctx = context.WithValue(ctx, fileTransferDiagnosticKey{}, true)
	progress := a.beginTransfer(ctx, "copy", source, fmt.Sprintf("%s · %d项", map[bool]string{true: "移动", false: "复制"}[req.Action == "move"], len(req.IDs)), 0,
		transferLocations{From: a.transferDirectory(source, req.Source), To: a.transferDirectory(target, req.Target)})
	phase := "读取来源目录"
	item := ""
	reject := func(reason string) error {
		return &fileTransferError{phase: phase, reason: reason, cause: errors.New(reason)}
	}
	defer func() {
		var diagnosed *fileTransferError
		if resultErr != nil {
			if errors.As(resultErr, &diagnosed) {
				resultErr = &fileTransferError{phase: diagnosed.phase, item: item, reason: diagnosed.reason, cause: resultErr}
			} else {
				resultErr = &fileTransferError{phase: phase, item: item, reason: transferFailureReason(resultErr), cause: resultErr}
			}
		}
		progress.finish(resultErr)
	}()
	if progress != nil {
		ctx = progress.ctx
	}
	ctx = context.WithValue(ctx, fileTransferProgressKey{}, progress)
	files, err := a.rawList(ctx, source, req.Source)
	if err != nil {
		return 0, err
	}
	phase = "读取目标目录"
	targetFiles, err := a.rawList(ctx, target, req.Target)
	if err != nil {
		return 0, err
	}
	selection := []File{}
	seen := map[string]bool{}
	phase = "核对复制选区"
	for _, fid := range req.IDs {
		if seen[fid] {
			return 0, reject("重复项目")
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
			return 0, reject("源目录已变化，请刷新后重试")
		}
	}
	phase = "检查目标同名项目"
	for _, f := range selection {
		item = f.Name
		for _, existing := range targetFiles {
			if strings.EqualFold(f.Name, existing.Name) {
				return 0, reject("目标已有同名项目，不覆盖")
			}
		}
	}
	entries := []backupEntry{}
	count, total := 0, int64(0)
	phase = "扫描来源目录"
	var walk func(File, string, string, int) error
	walk = func(f File, parent, rel string, depth int) error {
		item = rel
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 100000 || depth > 128 {
			return reject("选择范围超过100000项或128层")
		}
		if !safeName(f.Name) {
			return reject("源文件名无效")
		}
		if !f.IsDir && (f.Size < 0 || f.Size > uploadFileLimit(target)) {
			return reject("目标单文件上限 " + uploadLimitLabel(uploadFileLimit(target)))
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
					err = reject("不能传输符号链接")
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
		item = entry.relative
		phase = "核对存储配置"
		for _, original := range []Storage{source, target} {
			current, e := a.store.storage(original.ID)
			if e != nil || !sameBackupStorage(current, original) {
				return done, reject("存储配置已变化，已停止")
			}
		}
		if entry.file.IsDir {
			phase = "准备目标路径"
			parent, base, e := a.uploadParent(ctx, target, req.Target, entry.relative)
			if e != nil {
				return done, e
			}
			phase = "创建目标目录"
			if e = a.createDirectory(ctx, target, parent, base); e != nil {
				return done, e
			}
			continue
		}
		phase = "准备目标路径"
		parent, base, e := a.uploadParent(ctx, target, req.Target, entry.relative)
		if e != nil {
			return done, e
		}
		digest := ""
		phase = "暂存并写入目标文件"
		if e = a.copyBackupFile(ctx, source, target, entry.file, parent, base, "skip", &digest); e != nil {
			return done, e
		}
		phase = "校验目标内容"
		actual, e := a.backupFileDigest(ctx, target, parent, base)
		if e != nil {
			return done, e
		}
		if actual != digest {
			return done, reject("目标内容校验失败，源文件保留")
		}
		cleanup = append(cleanup, backupCleanupEntry{entry: entry, digest: digest, targets: []backupCleanupTarget{{storage: target, parent: parent, name: base}}})
		done++
	}
	if req.Action == "move" {
		phase = "清理移动源文件"
		item = ""
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
