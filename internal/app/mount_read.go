package app

import (
	"context"
	"io"
	"os"
	"time"

	"golang.org/x/time/rate"
)

const mount115BytesPerSecond = 50 * 1000 * 1000
const mountReadChunk = 256 << 10
const mountReadWindow = 32 << 20
const mountReadIdle = 2 * time.Second

type mountReadPool struct {
	gate  chan struct{}
	refs  int
	files map[string]*mountReadFileLimit
}

type mountReadFileLimit struct {
	limiter *rate.Limiter
	refs    int
}

type mountReadPart struct {
	data []byte
	err  error
}

// A bounded producer keeps sequential reads on one HTTP response, with at most
// 2 MiB queued ahead. Idle handles cancel the producer instead of pinning a slot.
type mountReadStream struct {
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	parts     chan mountReadPart
	next, end int64
	pending   []byte
	idle      *time.Timer
}

func (f *davFile) stopMountStream() {
	if s := f.mountStream; s != nil {
		s.idle.Stop()
		s.cancel()
		<-s.done
		f.mountStream = nil
	}
}

// Slots cover upstream streams, not open file handles or cache hits.
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
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if off < 0 {
		return 0, os.ErrInvalid
	}
	if len(b) == 0 {
		return 0, nil
	}
	if off >= f.info.Size() {
		return 0, io.EOF
	}
	n := 0
	for n < len(b) && off+int64(n) < f.info.Size() {
		if err := ctx.Err(); err != nil {
			f.stopMountStream()
			return n, err
		}
		position := off + int64(n)
		s := f.mountStream
		if s != nil && (s.next != position || s.ctx.Err() != nil || position >= s.end) {
			f.stopMountStream()
			s = nil
		}
		if s == nil {
			streamCtx, cancel := context.WithCancel(a.ctx)
			s = &mountReadStream{ctx: streamCtx, cancel: cancel, done: make(chan struct{}), parts: make(chan mountReadPart, 8), next: position, end: min(f.info.Size(), position+mountReadWindow)}
			s.idle = time.AfterFunc(mountReadIdle, cancel)
			f.mountStream = s
			go a.produceMount115(storage, f, s, position)
		}
		s.idle.Stop()
		stop := context.AfterFunc(ctx, s.cancel)
		for len(s.pending) == 0 {
			select {
			case part, ok := <-s.parts:
				if !ok || part.err != nil {
					stop()
					f.stopMountStream()
					if part.err != nil {
						return n, part.err
					}
					return n, io.ErrUnexpectedEOF
				}
				s.pending = part.data
			case <-ctx.Done():
				stop()
				f.stopMountStream()
				return n, ctx.Err()
			case <-s.ctx.Done():
				stop()
				f.stopMountStream()
				return n, s.ctx.Err()
			}
		}
		count := copy(b[n:], s.pending)
		s.pending = s.pending[count:]
		s.next += int64(count)
		n += count
		stop()
		if s.next == s.end {
			f.stopMountStream()
		} else {
			s.idle.Reset(mountReadIdle)
		}
	}
	if n < len(b) {
		return n, io.EOF
	}
	return n, nil
}

func (a *App) produceMount115(storage string, f *davFile, s *mountReadStream, start int64) {
	defer close(s.done)
	defer close(s.parts)
	send := func(part mountReadPart) bool {
		select {
		case s.parts <- part:
			return true
		case <-s.ctx.Done():
			return false
		}
	}
	release, err := a.acquireMountRead(s.ctx, storage)
	if err != nil {
		send(mountReadPart{err: err})
		return
	}
	defer release()
	// Duplicate opens of the same remote file share one 50 MB/s budget.
	key := f.mountKey
	if key == "" {
		key = f.download.URL
	}
	a.mountReadMu.Lock()
	p := a.mountReadPools[storage]
	if p.files == nil {
		p.files = map[string]*mountReadFileLimit{}
	}
	l := p.files[key]
	if l == nil {
		l = &mountReadFileLimit{limiter: newMountReadLimiter(mount115BytesPerSecond)}
		p.files[key] = l
	}
	l.refs++
	a.mountReadMu.Unlock()
	defer func() {
		a.mountReadMu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(p.files, key)
		}
		a.mountReadMu.Unlock()
	}()
	oldContext := f.ctx
	if f.body != nil {
		f.body.Close()
		f.body = nil
	}
	f.ctx = s.ctx
	defer func() {
		if f.body != nil {
			f.body.Close()
			f.body = nil
		}
		f.ctx, f.bounded = oldContext, false
	}()
	// The lazy opener closes over f. Run it on the producer before setting the
	// range, and restore the stream context if transfer tracking replaced it.
	if f.open != nil {
		if err := f.open(); err != nil {
			send(mountReadPart{err: err})
			return
		}
		f.open = nil
		f.ctx = s.ctx
	}
	f.offset, f.bounded, f.rangeEnd = start, true, s.end-1
	for position := start; position < s.end; {
		buf := make([]byte, int(min(int64(mountReadChunk), s.end-position)))
		n, err := readMountLimited(s.ctx, f, buf, l.limiter)
		if err != nil {
			send(mountReadPart{err: err})
			return
		}
		if a.store != nil {
			if err := a.limitTransferBytes(s.ctx, "download", n); err != nil {
				send(mountReadPart{err: err})
				return
			}
		}
		if !send(mountReadPart{data: buf}) {
			return
		}
		position += int64(n)
	}
}

func newMountReadLimiter(bytesPerSecond int) *rate.Limiter {
	limiter := rate.NewLimiter(rate.Limit(bytesPerSecond), mountReadChunk)
	limiter.AllowN(time.Now(), mountReadChunk)
	return limiter
}

func readMountLimited(ctx context.Context, reader io.Reader, b []byte, limiter *rate.Limiter) (int, error) {
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
