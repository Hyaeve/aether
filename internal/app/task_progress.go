package app

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
)

type taskHashProgressKey struct{}

var errTaskProgressLimit = errors.New("目录过大，继续执行并显示实际处理数")

// Report an unsupported selected name instead of silently claiming a complete
// 115 scan. Unselected files and explicitly excluded directories remain ignored.
func strm115SourceNameError(t Task, s Storage, f File, relative string) error {
	if s.Type != "115" || t.Kind != "strm" || taskSourceNameValid(t, s, f) {
		return nil
	}
	if f.IsDir {
		if excluded(relative, t.ExcludeDirs) {
			return nil
		}
	} else if excluded(f.Name, t.ExcludeFiles) || excludedType(f.Name, t.ExcludeTypes) || !taskMedia(t, f.Name) && !excludedType(f.Name, t.MetadataExtensions) {
		return nil
	}
	return fmt.Errorf("115源项目名称不适用于本地生成路径：%q；未跳过或报告全量同步成功，请调整该源项目名称", relative)
}

func (a *App) listTaskDirectory(ctx context.Context, t Task, s Storage, dir string, fresh bool) ([]File, error) {
	if t.Kind == "strm" && t.fullSyncGeneration && s.Type == "115" {
		files, err := a.list115STRMDirectory(ctx, s, dir)
		var auth *storageAuthError
		if errors.As(err, &auth) || err == nil && s.Health != nil && s.Health.Stamp == storageRevision(s) && s.Health.State != "active" {
			_ = a.recordStorageHealth(s, err)
		}
		return files, err
	}
	return a.listFiles(ctx, s, dir, t.CacheTTL, fresh)
}

// Enumerate once before doing file IO so a percentage has a real denominator.
func (a *App) scanTaskProgress(ctx context.Context, t Task, s Storage, sourceRel string, fresh bool) (map[string][]File, int, error) {
	plan := map[string][]File{}
	var bulk map[string][]File
	var checks map[string]strm115Inventory
	if bulk115STRM(t, s) {
		if t.ID != "" {
			a.taskUpdate(t.ID, func(task *Task) { task.Message = "读取115递归文件清单与目录表" })
		}
		if t.fullSyncGeneration {
			checks = map[string]strm115Inventory{}
		}
		var err error
		bulk, err = a.strm115BulkPlan(ctx, s, t.Source, checks)
		if err != nil {
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			a.store.event("info", "tasks", "STRM任务「"+t.Name+"」：批量目录清单不可用，改用逐目录核验："+err.Error())
			bulk = nil
		} else {
			a.store.event("info", "tasks", fmt.Sprintf("STRM任务「%s」：115批量目录清单已就绪，共%d个目录；后续媒体STRM在本地生成", t.Name, len(bulk)))
		}
	}
	total, entries := 0, 0
	var scan func(string, string, int) error
	scan = func(dir, rel string, depth int) error {
		if _, ok := plan[dir]; ok {
			return nil
		}
		if depth > 128 {
			return errors.New("目录嵌套超过安全上限128层")
		}
		files, found := bulk[dir]
		var err error
		if bulk == nil {
			files, err = a.listTaskDirectory(ctx, t, s, dir, fresh)
		} else if !found {
			return errors.New("115批量目录清单缺少所选目录")
		}
		if err != nil {
			return err
		}
		entries += len(files)
		if entries > 100000 {
			return errTaskProgressLimit
		}
		plan[dir] = files
		if t.Kind == "cache" {
			total++
		}
		for _, f := range files {
			if err := strm115SourceNameError(t, s, f, path.Join(rel, f.Name)); err != nil {
				return err
			}
			if !taskSourceNameValid(t, s, f) {
				continue
			}
			child := path.Join(rel, f.Name)
			if f.IsDir {
				if excluded(child, t.ExcludeDirs) || t.Kind == "cache" && t.Depth > 0 && depth+1 >= t.Depth {
					continue
				}
				if err := scan(f.ID, child, depth+1); err != nil {
					return err
				}
			} else if t.Kind != "cache" && !excluded(f.Name, t.ExcludeFiles) && !excludedType(f.Name, t.ExcludeTypes) &&
				(taskMedia(t, f.Name) || excludedType(f.Name, t.MetadataExtensions) || t.Kind == "cas" && (strings.EqualFold(path.Ext(f.Name), ".cas") || strings.HasSuffix(strings.ToLower(f.Name), ".cas.strm"))) {
				if t.Kind == "strm" && s.Type == "115" {
					if err := checkSTRMOutputPath(child); err != nil {
						return err
					}
				}
				total++
			}
		}
		return ctx.Err()
	}
	err := scan(t.Source, sourceRel, 0)
	if err == nil && bulk == nil && strings.Trim(t.ExcludeDirs, " ;\t\r\n") == "" {
		err = verify115FallbackInventory(plan, checks)
	}
	return plan, total, err
}
