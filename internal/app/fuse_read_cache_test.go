package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFuseReadCacheBlocksAndRestart(t *testing.T) {
	c := newFuseReadCache(t.TempDir())
	data := bytes.Repeat([]byte("abcd"), fuseBlockSize/4+20)
	var calls atomic.Int32
	fetch := func(b []byte, off int64) (int, error) { calls.Add(1); return bytes.NewReader(data).ReadAt(b, off) }
	b := make([]byte, 80)
	for i := 0; i < 2; i++ {
		n, e := c.readAt(context.Background(), "file", int64(len(data)), b, fuseBlockSize-20, fetch)
		if e != nil || n != 80 || !bytes.Equal(b, data[fuseBlockSize-20:fuseBlockSize+60]) {
			t.Fatal(n, e)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("not cached", calls.Load())
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := c.readAt(context.Background(), "parallel", int64(len(data)), make([]byte, 16), 0, fetch)
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 3 {
		t.Fatal("miss coalescing", calls.Load())
	}
	n, e := c.readAt(context.Background(), "file", int64(len(data)), b, int64(len(data)-10), fetch)
	if n != 10 || e != io.EOF {
		t.Fatal(n, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = c.readAt(ctx, "cancel", int64(len(data)), b, 0, fetch); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	restarted := newFuseReadCache(c.dir)
	if _, e = restarted.readAt(context.Background(), "file", int64(len(data)), b, 0, fetch); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 4 {
		t.Fatal("restart reused stale blocks")
	}
}

func TestFuseReadCacheEvictionCorruptionAndDiskFailure(t *testing.T) {
	c := newFuseReadCache(t.TempDir())
	c.limit = 40
	data := []byte("12345678")
	calls := 0
	fetch := func(b []byte, off int64) (int, error) { calls++; return bytes.NewReader(data).ReadAt(b, off) }
	read := func(key string) {
		t.Helper()
		b := make([]byte, 8)
		n, e := c.readAt(context.Background(), key, 8, b, 0, fetch)
		if n != 8 || e != nil || !bytes.Equal(b, data) {
			t.Fatal(n, e)
		}
	}
	read("a")
	read("b")
	read("a")
	if calls != 3 || c.bytes > c.limit || len(c.entries) != 1 {
		t.Fatal(calls, c.bytes)
	}
	for name := range c.entries {
		if e := os.WriteFile(filepath.Join(c.dir, name), bytes.Repeat([]byte("x"), 40), 0600); e != nil {
			t.Fatal(e)
		}
	}
	read("a")
	if calls != 4 {
		t.Fatal("corrupt cache hit")
	}
	c.ttl = time.Nanosecond
	for key, entry := range c.entries {
		entry.used = time.Now().Add(-time.Second)
		c.entries[key] = entry
	}
	read("a")
	if calls != 5 {
		t.Fatal("expired hit")
	}
	bad := filepath.Join(t.TempDir(), "file")
	os.WriteFile(bad, []byte("x"), 0600)
	d := newFuseReadCache(bad)
	b := make([]byte, 8)
	if n, e := d.readAt(context.Background(), "a", 8, b, 0, fetch); n != 8 || e != nil {
		t.Fatal("disk failure broke read", n, e)
	}
	_, e := c.readAt(context.Background(), "failed", 8, b, 0, func([]byte, int64) (int, error) { return 0, io.ErrUnexpectedEOF })
	if e == nil {
		t.Fatal("cached incomplete read")
	}
}

func TestScrapeExcludedTaskCannotBeUsedAsLibrary(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	task := Task{ID: "excluded", Kind: "strm", StorageID: s.ID, Target: t.TempDir(), ScrapeExcluded: true}
	a.store.update(func(st *State) error { st.Tasks = append(st.Tasks, task); return nil })
	if _, e := a.scrapeRoot(task.ID); e == nil {
		t.Fatal("excluded task accepted")
	}
	a.store.update(func(st *State) error { st.Tasks[0].ScrapeExcluded = false; return nil })
	if _, e := a.scrapeRoot(task.ID); e != nil {
		t.Fatal(e)
	}
}
