package app

import (
	"context"
	"errors"
	"path"
	"strings"
)

type taskHashProgressKey struct{}

var errTaskProgressLimit = errors.New("目录过大，继续执行并显示实际处理数")

// Enumerate once before doing file IO so a percentage has a real denominator.
func (a *App) scanTaskProgress(ctx context.Context, t Task, s Storage, sourceRel string, fresh bool) (map[string][]File, int, error) {
	plan := map[string][]File{}
	total, entries := 0, 0
	var scan func(string, string, int) error
	scan = func(dir, rel string, depth int) error {
		if _, ok := plan[dir]; ok {
			return nil
		}
		if depth > 128 {
			return errors.New("目录嵌套超过安全上限128层")
		}
		files, err := a.listFiles(ctx, s, dir, t.CacheTTL, fresh)
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
			if !safeName(f.Name) {
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
				total++
			}
		}
		return ctx.Err()
	}
	err := scan(t.Source, sourceRel, 0)
	return plan, total, err
}
