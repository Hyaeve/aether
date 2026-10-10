package app

import (
	"context"
	"io"
	"time"

	"golang.org/x/time/rate"
)

const mount115BytesPerSecond = 50 * 1000 * 1000
const mountReadChunk = 256 << 10

type mountReadPool struct {
	gate chan struct{}
	refs int
}

// Slots cover bounded upstream requests, never the lifetime of an idle file handle.
func (a *App) acquireMountRead(ctx context.Context, storage string) (func(), error) {
	a.mountReadMu.Lock()
	if a.mountReadPools == nil {
		a.mountReadPools = map[string]*mountReadPool{}
	}
	p := a.mountReadPools[storage]
	if p == nil {
		p = &mountReadPool{gate: make(chan struct{}, 2)}
		a.mountReadPools[storage] = p
	}
	p.refs++
	a.mountReadMu.Unlock()
	unref := func() {
		a.mountReadMu.Lock()
		p.refs--
		if p.refs == 0 {
			delete(a.mountReadPools, storage)
		}
		a.mountReadMu.Unlock()
	}
	select {
	case p.gate <- struct{}{}:
		return func() { <-p.gate; unref() }, nil
	case <-ctx.Done():
		unref()
		return nil, ctx.Err()
	}
}

func (a *App) readMount115(ctx context.Context, storage string, f *davFile, b []byte, off int64) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.ctx, cancel)
	defer func() { stop(); cancel() }()
	release, err := a.acquireMountRead(ctx, storage)
	if err != nil {
		return 0, err
	}
	defer release()
	oldContext := f.ctx
	if f.body != nil {
		f.body.Close()
		f.body = nil
	}
	f.ctx = ctx
	f.bounded, f.rangeEnd = true, off+int64(len(b))-1
	defer func() {
		if f.body != nil {
			f.body.Close()
			f.body = nil
		}
		f.ctx, f.bounded = oldContext, false
	}()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	return readMountLimited(ctx, f, b, mount115BytesPerSecond)
}

func readMountLimited(ctx context.Context, reader io.Reader, b []byte, bytesPerSecond int) (int, error) {
	limiter := rate.NewLimiter(rate.Limit(bytesPerSecond), mountReadChunk)
	limiter.AllowN(time.Now(), mountReadChunk)
	n := 0
	for n < len(b) {
		size := min(mountReadChunk, len(b)-n)
		if err := limiter.WaitN(ctx, size); err != nil {
			return n, err
		}
		count, err := io.ReadFull(reader, b[n:n+size])
		n += count
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
