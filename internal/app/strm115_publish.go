package app

import (
	"context"
	"os"
	"sync"
)

type strm115Output struct {
	file File
	name string
}

type strmBatchEnsureKey struct{}
type strmBatchEnsure struct {
	h    *strmTaskSync
	once sync.Once
	err  error
}

func ensureSTRMOutput(ctx context.Context, h *strmTaskSync, root *os.Root) error {
	if batch, ok := ctx.Value(strmBatchEnsureKey{}).(*strmBatchEnsure); ok && batch.h == h {
		batch.once.Do(func() { batch.err = h.ensure(ctx, root) })
		return batch.err
	}
	return h.ensure(ctx, root)
}

// Only local reference/STRM IO is parallel; cloud enumeration and deletion are
// unchanged. The caller feeds bounded batches and owns all progress updates.
func (a *App) publish115Batch(ctx context.Context, h *strmTaskSync, s Storage, root *os.Root, jobs []strm115Output, missing bool, progress func(bool)) error {
	if len(jobs) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mark sync.Once
	var markErr error
	original := ctx
	ctx = context.WithValue(ctx, strmLifeWorkKey{}, func() error { mark.Do(func() { markErr = recordSTRMLifeWork(original) }); return markErr })
	ctx = context.WithValue(ctx, strmBatchEnsureKey{}, &strmBatchEnsure{h: h})
	type result struct {
		changed bool
		err     error
	}
	results := make(chan result, 16)
	queue := make(chan strm115Output)
	var workers sync.WaitGroup
	for i := 0; i < min(16, len(jobs)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range queue {
				if ctx.Err() != nil {
					continue
				}
				link, err := a.publicSTRMURL(s, job.file)
				changed := false
				if err == nil {
					changed, err = publishSTRMOutput(ctx, h, root, job.name, []byte(link+"\n"), missing)
				}
				results <- result{changed, err}
				if err != nil {
					cancel()
				}
			}
		}()
	}
	go func() {
		defer close(queue)
		for _, job := range jobs {
			select {
			case queue <- job:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { workers.Wait(); close(results) }()
	var failure error
	for result := range results {
		if result.err != nil {
			if failure == nil {
				failure = result.err
			}
		} else {
			progress(result.changed)
		}
	}
	if failure != nil {
		return failure
	}
	return ctx.Err()
}
