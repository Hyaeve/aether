package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMountReadTwoSlotsAndCancellation(t *testing.T) {
	a := &App{ctx: context.Background()}
	ctx := context.Background()
	first, _ := a.acquireMountRead(ctx, "115")
	second, _ := a.acquireMountRead(ctx, "115")
	other, err := a.acquireMountRead(ctx, "other-115")
	if err != nil {
		t.Fatal(err)
	}
	other()
	waiting := make(chan error, 1)
	canceled, cancel := context.WithCancel(ctx)
	go func() {
		release, err := a.acquireMountRead(canceled, "115")
		if release != nil {
			release()
		}
		waiting <- err
	}()
	select {
	case e := <-waiting:
		t.Fatal("third slot acquired", e)
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	if !errors.Is(<-waiting, context.Canceled) {
		t.Fatal("queue not cancelable")
	}
	first()
	second()
	if len(a.mountReadPools) != 0 {
		t.Fatal("pool leaked", a.mountReadPools)
	}
	third, err := a.acquireMountRead(ctx, "115")
	if err != nil {
		t.Fatal(err)
	}
	third()
}

func TestStorageReadCopiesOnlySelectedConfig(t *testing.T) {
	s := &Store{state: State{Storages: []Storage{
		{ID: "enabled", Enabled: true, Config: map[string]string{"cookie": "original"}},
		{ID: "disabled", Enabled: false},
	}}}
	storage, err := s.storage("enabled")
	if err != nil {
		t.Fatal(err)
	}
	storage.Config["cookie"] = "changed"
	storage.Name = "changed"
	again, err := s.storage("enabled")
	if err != nil || again.Config["cookie"] != "original" || again.Name != "" {
		t.Fatal("returned config aliases store", again, err)
	}
	if _, err := s.storage("disabled"); err == nil {
		t.Fatal("disabled storage returned")
	}
	if _, err := s.storage("missing"); err == nil {
		t.Fatal("missing storage returned")
	}
}

func TestMountReadRateLimitAndCancel(t *testing.T) {
	b := bytes.Repeat([]byte("x"), 2*mountReadChunk)
	start := time.Now()
	n, err := readMountLimited(context.Background(), bytes.NewReader(b), make([]byte, len(b)), newMountReadLimiter(4*mountReadChunk))
	if err != nil || n != len(b) || time.Since(start) < 450*time.Millisecond {
		t.Fatal("missing per-stream limit", n, err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if n, err := readMountLimited(ctx, bytes.NewReader(b), make([]byte, len(b)), newMountReadLimiter(1)); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal(n, err)
	}
	if mount115BytesPerSecond != 50_000_000 {
		t.Fatal("50 MB/s is decimal bytes")
	}
}

func TestMount115BoundedProxyReadAndSeekReuse(t *testing.T) {
	content := bytes.Repeat([]byte("abcdef"), 10000)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Range") == "" {
			w.Write(content)
			return
		}
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			t.Error(r.Header)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.WriteHeader(206)
		w.Write(content[start : end+1])
	}))
	defer server.Close()
	a := &App{ctx: context.Background()}
	f := &davFile{ctx: a.ctx, info: davInfo{File{Name: "book", Size: int64(len(content))}}, download: Download{URL: server.URL}}
	for _, off := range []int64{0, 1000, 32000} {
		b := make([]byte, 1000)
		n, err := a.readMount115(context.Background(), "115", f, b, off)
		if err != nil || n != len(b) || !bytes.Equal(b, content[off:off+1000]) {
			t.Fatal(n, err)
		}
		f.stopMountStream()
		if f.body != nil || len(a.mountReadPools) != 0 || f.ctx != a.ctx || f.bounded {
			t.Fatal("closed stream held a network slot")
		}
	}
	f.offset = 0
	first := make([]byte, 1000)
	if _, err := io.ReadFull(f, first); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	if _, err := f.Seek(1000, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(f, first); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Fatal("same-position seek reopened HTTP stream")
	}
	f.Close()
}

func TestMount115RejectsWrongRangeAndReleases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-99/100")
		w.WriteHeader(206)
		w.Write(bytes.Repeat([]byte("0123456789"), 10))
	}))
	defer server.Close()
	a := &App{ctx: context.Background()}
	f := &davFile{ctx: a.ctx, info: davInfo{File{Name: "book", Size: 100}}, download: Download{URL: server.URL}}
	if _, err := a.readMount115(a.ctx, "115", f, make([]byte, 10), 20); err == nil {
		t.Fatal("wrong offset accepted")
	}
	if f.body != nil || len(a.mountReadPools) != 0 {
		t.Fatal("failure leaked slot")
	}
}

func TestMount115ActualHTTPConcurrencyAndRequestCancel(t *testing.T) {
	var active, peak atomic.Int32
	started := make(chan struct{}, 4)
	unblock := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		defer active.Add(-1)
		started <- struct{}{}
		select {
		case <-unblock:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Range", "bytes 0-99/100")
		w.WriteHeader(206)
		w.Write(bytes.Repeat([]byte("0123456789"), 10))
	}))
	defer server.Close()
	a := &App{ctx: context.Background()}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	read := func(ctx context.Context) {
		defer wg.Done()
		f := &davFile{ctx: a.ctx, info: davInfo{File{Name: "book", Size: 100}}, download: Download{URL: server.URL}}
		_, err := a.readMount115(ctx, "same-pool", f, make([]byte, 10), 0)
		f.Close()
		errs <- err
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go read(context.Background())
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("request not started")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	wg.Add(1)
	go read(ctx)
	select {
	case <-started:
		t.Fatal("more than two upstream requests")
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(unblock)
	wg.Wait()
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if peak.Load() != 2 || len(a.mountReadPools) != 0 {
		t.Fatal(peak.Load(), a.mountReadPools)
	}
	// Cancel a live HTTP request as well as a queued request.
	cancelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { started <- struct{}{}; <-r.Context().Done() }))
	defer cancelServer.Close()
	ctx, cancel = context.WithCancel(context.Background())
	f := &davFile{ctx: a.ctx, info: davInfo{File{Name: "book", Size: 100}}, download: Download{URL: cancelServer.URL}}
	done := make(chan error, 1)
	go func() { _, err := a.readMount115(ctx, "same-pool", f, make([]byte, 10), 0); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("live request not canceled")
	}
	if len(a.mountReadPools) != 0 {
		t.Fatal("canceled request leaked slot")
	}
}

func mountReadTestServer(t *testing.T, content []byte, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var start, end int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil || start < 0 || end >= len(content) || end < start {
			t.Error("invalid range", r.Header.Get("Range"))
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
		w.WriteHeader(206)
		w.Write(content[start : end+1])
	}))
}

func TestMount115ContinuousWindowPrefetchSeekAndEOF(t *testing.T) {
	content := bytes.Repeat([]byte("abcdefghijklmnop"), (mountReadWindow+fuseBlockSize)/16)
	var calls atomic.Int32
	server := mountReadTestServer(t, content, &calls)
	defer server.Close()
	a := &App{ctx: context.Background()}
	f := &davFile{ctx: a.ctx, info: davInfo{File{Name: "video", Size: int64(len(content))}}, download: Download{URL: server.URL}}
	defer f.Close()
	buf := make([]byte, fuseBlockSize)
	for off := int64(0); off < int64(len(content)); off += int64(len(buf)) {
		n, err := a.readMount115(a.ctx, "pool", f, buf, off)
		if n != len(buf) || err != nil || !bytes.Equal(buf, content[off:off+int64(n)]) {
			t.Fatal("sequential data", off, n, err)
		}
		want := int32(1)
		if off >= mountReadWindow {
			want = 2
		}
		if calls.Load() != want {
			t.Fatal("4 MiB boundary reopened HTTP", off, calls.Load(), want)
		}
	}
	if _, err := a.readMount115(a.ctx, "pool", f, buf, int64(len(content))); err != io.EOF {
		t.Fatal(err)
	}
	if _, err := a.readMount115(a.ctx, "pool", f, buf, 123); err != nil {
		t.Fatal(err)
	}
	if _, err := a.readMount115(a.ctx, "pool", f, buf, 456); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if calls.Load() != 4 || len(a.mountReadPools) != 0 || f.body != nil {
		t.Fatal("seek/release", calls.Load(), a.mountReadPools)
	}
}

func TestMount115IdleProducerReleasesAndReopens(t *testing.T) {
	content := bytes.Repeat([]byte("x"), 8*fuseBlockSize)
	var calls atomic.Int32
	server := mountReadTestServer(t, content, &calls)
	defer server.Close()
	a := &App{ctx: context.Background()}
	f := &davFile{ctx: a.ctx, info: davInfo{File{Name: "video", Size: int64(len(content))}}, download: Download{URL: server.URL}}
	defer f.Close()
	if _, err := a.readMount115(a.ctx, "pool", f, make([]byte, 64), 0); err != nil {
		t.Fatal(err)
	}
	s := f.mountStream
	select {
	case <-s.done:
	case <-time.After(mountReadIdle + time.Second):
		t.Fatal("idle stream pinned slot")
	}
	if len(a.mountReadPools) != 0 {
		t.Fatal("idle slot leaked")
	}
	if n, err := a.readMount115(a.ctx, "pool", f, make([]byte, 64), 64); n != 64 || err != nil {
		t.Fatal(n, err)
	}
	f.Close()
	if calls.Load() != 2 {
		t.Fatal("expired stream not reopened", calls.Load())
	}
}

func TestMount115DuplicateFileSharesRateBudget(t *testing.T) {
	started := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { started <- struct{}{}; <-r.Context().Done() }))
	defer server.Close()
	a := &App{ctx: context.Background()}
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f := &davFile{ctx: a.ctx, info: davInfo{File{Size: 100}}, download: Download{URL: server.URL}, mountKey: "same-file"}
			defer f.Close()
			a.readMount115(ctx, "pool", f, make([]byte, 10), 0)
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("producer not started")
		}
	}
	a.mountReadMu.Lock()
	l := a.mountReadPools["pool"].files["same-file"]
	valid := l != nil && l.refs == 2 && l.limiter.Limit() == 50_000_000
	a.mountReadMu.Unlock()
	cancel()
	wg.Wait()
	if !valid || len(a.mountReadPools) != 0 {
		t.Fatal("per-file budget/release", valid)
	}
}

func TestMount115PrefetchFailureAndShutdown(t *testing.T) {
	t.Run("short-body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Range", "bytes 0-524287/524288")
			w.WriteHeader(206)
			w.Write(bytes.Repeat([]byte("x"), mountReadChunk+17))
		}))
		defer server.Close()
		a := &App{ctx: context.Background()}
		f := &davFile{ctx: a.ctx, info: davInfo{File{Size: 2 * mountReadChunk}}, download: Download{URL: server.URL}}
		n, err := a.readMount115(a.ctx, "pool", f, make([]byte, 2*mountReadChunk), 0)
		if err != io.ErrUnexpectedEOF || n != mountReadChunk || f.mountStream != nil || len(a.mountReadPools) != 0 {
			t.Fatal("truncated response admitted/leaked", n, err)
		}
	})
	t.Run("shutdown-lazy-opener", func(t *testing.T) {
		started := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
		defer server.Close()
		appCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		a := &App{ctx: appCtx}
		f := &davFile{ctx: a.ctx, info: davInfo{File{Size: 100}}}
		f.open = func() error { f.ctx = context.Background(); f.download = Download{URL: server.URL}; return nil }
		done := make(chan error, 1)
		go func() { _, err := a.readMount115(context.Background(), "pool", f, make([]byte, 10), 0); done <- err }()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("lazy opener did not run")
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("app shutdown left HTTP alive")
		}
		if f.ctx != a.ctx || len(a.mountReadPools) != 0 {
			t.Fatal("lazy opener context/slot leaked")
		}
	})
}
