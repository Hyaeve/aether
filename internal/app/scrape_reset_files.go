package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
)

// Preflight the entire deletion set; never follow links or clean a shared library root.
func resetScrapeFiles(ctx context.Context, base string, items []scrapeItem, selected func(scrapeItem) bool) (int, error) {
	dirs := map[string]bool{}
	known := map[string]bool{}
	for _, item := range items {
		if selected(item) {
			known[item.Path] = true
			dir := scrapeWorkDir(item.Path)
			if dir == "." || genericMediaDir(path.Base(dir)) {
				return 0, errors.New("作品位于共享目录，不能删除该目录的非STRM文件，请使用独立作品目录")
			}
			dirs[dir] = true
		}
	}
	if len(dirs) == 0 {
		return 0, errors.New("未选择作品")
	}
	for _, item := range items {
		for dir := range dirs {
			if strings.HasPrefix(item.Path, dir+"/") && !selected(item) {
				return 0, errors.New("作品目录包含未选中的STRM作品，不能清理")
			}
		}
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	names := map[string]bool{}
	visited := 0
	for dir := range dirs {
		if err := fs.WalkDir(root.FS(), dir, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			visited++
			if visited > 100000 || strings.Count(name, "/") > 128 {
				return errors.New("重置范围过大")
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("作品目录包含符号链接，拒绝清理")
			}
			if !entry.IsDir() {
				if !entry.Type().IsRegular() {
					return errors.New("作品目录包含特殊文件")
				}
				if strings.EqualFold(path.Ext(name), ".strm") && !known[name] {
					return errors.New("作品目录出现未选中或未索引STRM，请刷新后重试")
				}
				if !strings.EqualFold(path.Ext(name), ".strm") {
					names[name] = true
				}
			}
			return nil
		}); err != nil {
			return 0, err
		}
	}
	removed := 0
	for name := range names {
		if err := ctx.Err(); err != nil {
			return removed, fmt.Errorf("重置中断，已删除%d个文件：%w", removed, err)
		}
		info, err := root.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			return removed, fmt.Errorf("重置目录已变化，已删除%d个文件", removed)
		}
		if err := root.Remove(name); err != nil {
			return removed, fmt.Errorf("重置失败，已删除%d个文件：%w", removed, err)
		}
		removed++
	}
	return removed, nil
}
