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
	n, err := readMountLimited(context.Background(), bytes.NewReader(b), make([]byte, len(b)), 4*mountReadChunk)
	if err != nil || n != len(b) || time.Since(start) < 450*time.Millisecond {
		t.Fatal("missing per-stream limit", n, err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if n, err := readMountLimited(ctx, bytes.NewReader(b), make([]byte, len(b)), 1); n != 0 || !errors.Is(err, context.Canceled) {
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
		if f.body != nil || len(a.mountReadPools) != 0 || f.ctx != a.ctx || f.bounded {
			t.Fatal("idle handle held a network slot")
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
		w.Header().Set("Content-Range", "bytes 0-9/100")
		w.WriteHeader(206)
		w.Write([]byte("0123456789"))
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
		w.Header().Set("Content-Range", "bytes 0-9/100")
		w.WriteHeader(206)
		w.Write([]byte("0123456789"))
	}))
	defer server.Close()
	a := &App{ctx: context.Background()}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	read := func(ctx context.Context) {
		defer wg.Done()
		f := &davFile{ctx: a.ctx, info: davInfo{File{Name: "book", Size: 100}}, download: Download{URL: server.URL}}
		_, err := a.readMount115(ctx, "same-pool", f, make([]byte, 10), 0)
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
