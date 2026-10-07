package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMountNamesWithoutOpaqueIDs(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{
			{ID: "opaque-a", Name: "Quark", Type: "quark", Enabled: true},
			{ID: "opaque-b", Name: "Quark", Type: "quark", Enabled: true},
			{ID: "opaque-c", Name: "Quark (2)", Type: "quark", Enabled: true},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	fs := mountFS{app: a}
	names := fs.storageNames()
	if names["opaque-a"] != "Quark" || names["opaque-b"] != "Quark (3)" || names["opaque-c"] != "Quark (2)" {
		t.Fatal(names)
	}
	for id, name := range names {
		got, _, _, err := fs.selectPath("/" + name)
		if err != nil || got.ID != id {
			t.Fatalf("%s: %v %s", name, err, got.ID)
		}
	}
}

func TestCacheCountersAndInvalidatedWrite(t *testing.T) {
	c := NewCache()
	cfg := Settings{CacheEnabled: true, CacheMaxItems: 1, CacheMemoryMB: 1}
	c.put("first", []File{{Name: "one"}}, 30, cfg)
	if _, ok := c.get("first"); !ok {
		t.Fatal("missing cache")
	}
	c.put("second", nil, 30, cfg)
	c.get("first")
	c.put("expired", nil, -1, cfg)
	c.get("expired")
	st := c.stats()
	if st["hits"] != uint64(1) || st["misses"] != uint64(2) || st["evictions"] != uint64(2) || st["expired"] != uint64(1) {
		t.Fatal(st)
	}
	revision := c.revision()
	c.clear()
	c.putGeneration("late", nil, 30, cfg, &revision)
	if c.stats()["entries"] != 0 {
		t.Fatal("old request resurrected cache")
	}
}

func TestDirectoryCacheCoalescesAndSkipsThrottle(t *testing.T) {
	a := testApp(t)
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":200,"data":{"content":[{"name":"movie.mp4"}],"total":1}}`))
	}))
	defer upstream.Close()
	s := Storage{ID: "olist", Type: "openlist", Config: map[string]string{"address": upstream.URL}}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			files, err := a.listFiles(context.Background(), s, "/", 0, false)
			if err != nil || len(files) != 1 {
				t.Errorf("list: %v %v", files, err)
			}
		}()
	}
	<-entered
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate requests", calls.Load())
	}
	a.gateMu.Lock()
	a.gates[s.ID] = time.Now().Add(time.Hour)
	a.gateMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := a.listFiles(ctx, s, "/", 0, false); err != nil {
		t.Fatal("cache hit waited for API gate", err)
	}
}

func TestSTRMRetainsChosenDirectory(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "B", "sub", "movie.mp4"), "video")
	n, err := a.executeTask(context.Background(), Task{Kind: "strm", Source: "/B", Target: "C", Mode: "full"}, s)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err = os.Stat(filepath.Join(a.outputDir, "C", "B", "sub", "movie.mp4.strm")); err != nil {
		t.Fatal(err)
	}
}

func TestOpenListSTRMPaths(t *testing.T) {
	s := Storage{Config: map[string]string{"address": "https://olist.example/base/", "root": "/media"}}
	for _, encoded := range []bool{false, true} {
		link, err := openlistSTRM(s, "/电影/片 名?#%.mkv", encoded)
		if err != nil || !strings.HasPrefix(link, "https://olist.example/base/d/media/") || !strings.Contains(link, "%3F%23%25.mkv") {
			t.Fatal(link, err)
		}
		if encoded && (strings.Contains(link, "电影") || strings.Contains(link, " ")) {
			t.Fatal("unencoded path", link)
		}
		if !encoded && !strings.Contains(link, "电影/片 名") {
			t.Fatal("unreadable path", link)
		}
	}
	if _, err := openlistSTRM(s, "/bad\nfile.mkv", false); err == nil {
		t.Fatal("newline accepted")
	}
}
