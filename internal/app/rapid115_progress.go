package app

import (
	"context"
	"io"
)

type rapid115ReportKey struct{}
type rapid115ReadStageKey struct{}
type rapid115ReadStage struct {
	phase string
	total int64
}
type rapid115Live struct {
	Phase       string
	Done, Total int64
}

func rapid115Report(ctx context.Context, phase string, done, total int64) {
	if report, ok := ctx.Value(rapid115ReportKey{}).(func(string, int64, int64)); ok {
		report(phase, done, total)
	}
}

type rapid115ProgressReader struct {
	io.Reader
	ctx         context.Context
	phase       string
	done, total int64
}

func (r *rapid115ProgressReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.done += int64(n)
	rapid115Report(r.ctx, r.phase, min(r.done, r.total), r.total)
	return n, err
}

func rapid115ReadProgress(ctx context.Context, reader io.Reader, phase string, offset, total int64) io.Reader {
	if ctx.Value(rapid115ReportKey{}) == nil {
		return reader
	}
	rapid115Report(ctx, phase, offset, total)
	return &rapid115ProgressReader{Reader: reader, ctx: ctx, phase: phase, done: offset, total: total}
}

func (a *App) rapid115RecordProgress(record Rapid115Record) Rapid115Record {
	a.rapid115Mu.Lock()
	live, ok := a.rapid115Progress[record.ID]
	a.rapid115Mu.Unlock()
	if ok {
		record.Phase, record.Done, record.Total = live.Phase, live.Done, live.Total
	}
	if record.Status == "success" || record.Status == "uploaded" {
		record.Phase, record.Done, record.Total = record.Message, record.Size, record.Size
	}
	return record
}
