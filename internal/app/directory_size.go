package app

import (
	"context"
	"errors"
	"net/http"
	"time"
)

func directorySizeKey(storage, id string) string { return "directory-size:" + storage + ":" + id }

func (a *App) directorySize(w http.ResponseWriter, r *http.Request) {
	var input struct{ StorageID, Parent, ID string }
	if !decode(w, r, &input) {
		return
	}
	s, err := a.store.storage(input.StorageID)
	if err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	generation := a.cache.revision()
	files, err := a.listFiles(ctx, s, input.Parent, 0, false)
	if err != nil {
		fail(w, 400, err)
		return
	}
	var found *File
	for _, f := range files {
		if f.ID == input.ID && f.IsDir {
			copy := f
			found = &copy
			break
		}
	}
	if found == nil {
		fail(w, 400, errors.New("目录不存在，请刷新后重试"))
		return
	}
	cfg := a.store.snapshotWithLogLimit(0).Settings
	if cfg.CacheEnabled {
		if cached, ok := a.cache.get(directorySizeKey(s.ID, found.ID)); ok && len(cached) == 1 && cached[0].CountsKnown {
			jsonResponse(w, 200, cached[0])
			return
		}
	}
	seen, count := map[string]bool{}, 0
	var walk func(string, int) (int64, error)
	folders, filesCount := 0, 0
	walk = func(dir string, depth int) (int64, error) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if depth > 128 || seen[dir] {
			return 0, errors.New("目录循环或层级超过 128")
		}
		seen[dir] = true
		if s.Type != "local" {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
		children, err := a.listFiles(ctx, s, dir, 0, false)
		if err != nil {
			return 0, err
		}
		var total int64
		for _, f := range children {
			count++
			if count > 100000 {
				return 0, errors.New("单次统计超过 100000 个项目")
			}
			size := f.Size
			if f.IsDir {
				folders++
				size, err = walk(f.ID, depth+1)
				if err != nil {
					return 0, err
				}
			}
			if !f.IsDir {
				filesCount++
			}
			if size < 0 || total > (1<<63-1)-size {
				return 0, errors.New("大小统计溢出")
			}
			total += size
		}
		return total, nil
	}
	found.Size, err = walk(found.ID, 0)
	if err != nil {
		fail(w, 400, err)
		return
	}
	found.SizeKnown = true
	found.FolderCount, found.FileCount = folders, filesCount
	found.CountsKnown = true
	ttl := s.CacheTTL
	if ttl <= 0 {
		ttl = cfg.CacheTTL
	}
	a.cache.putGeneration(directorySizeKey(s.ID, found.ID), []File{*found}, ttl, cfg, &generation)
	jsonResponse(w, 200, found)
}
