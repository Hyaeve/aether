package app

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

func (a *App) transferTempDirectory() string {
	if dir := a.transferConfig().TempDirectory; dir != "" {
		return dir
	}
	return filepath.Join(a.dataDir, "cache", "transfer")
}

func (a *App) downloadCASInfo(ctx context.Context, source Storage, f File) (CASInfo, error) {
	release, err := a.acquireTransferResource(ctx, "feature-hash")
	if err != nil {
		return CASInfo{}, err
	}
	defer release()
	dir := a.transferTempDirectory()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return CASInfo{}, err
	}
	stage, err := a.stageBackupSource(ctx, source, f, dir)
	if err != nil {
		return CASInfo{}, err
	}
	defer stage.clear()
	local := Storage{Type: "local", Config: map[string]string{"root": filepath.Dir(stage.name)}}
	info, err := a.generateCASInfo(ctx, local, File{ID: filepath.Base(stage.name), Name: f.Name, Size: stage.size})
	if err == nil && info.SHA256 != stage.digest {
		return CASInfo{}, errors.New("临时文件在哈希计算期间变化")
	}
	return info, err
}

type casPrecomputed struct {
	info CASInfo
	err  error
}

// Two workers per directory, with a shared service-wide limit across tasks.
func (a *App) precomputeRemoteCAS(ctx context.Context, t Task, s Storage, files []File, rel, outputDir, target string) map[string]casPrecomputed {
	if t.Kind != "cas" || t.CASOperation == "restore" || s.Type == "local" || s.Type == "mobile" || s.Type == "tianyi" {
		return nil
	}
	results := map[string]casPrecomputed{}
	var mu sync.Mutex
	jobs := make(chan File)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for f := range jobs {
				info, err := a.generateCASInfo(ctx, s, f)
				mu.Lock()
				results[f.ID+"\x00"+f.Name] = casPrecomputed{info, err}
				mu.Unlock()
			}
		}()
	}
	queued := 0
	for _, f := range files {
		if f.IsDir || !safeName(f.Name) || !taskMedia(t, f.Name) || excluded(f.Name, t.ExcludeFiles) || excludedType(f.Name, t.ExcludeTypes) || strings.EqualFold(path.Ext(f.Name), ".cas") || strings.HasSuffix(strings.ToLower(f.Name), ".cas.strm") {
			continue
		}
		if t.Mode == "incremental" {
			name := filepath.Join(outputDir, filepath.FromSlash(path.Join(target, rel, f.Name+".cas.strm")))
			if _, err := os.Stat(name); err == nil {
				continue
			}
		}
		select {
		case jobs <- f:
			queued++
		case <-ctx.Done():
		}
		if ctx.Err() != nil || queued == 2 {
			break
		}
	}
	close(jobs)
	workers.Wait()
	return results
}
