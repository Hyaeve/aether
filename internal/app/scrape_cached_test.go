package app

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestScrapeCachedIndexAndTaskInvalidation(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	writeTest(t, filepath.Join(dir, "Movie", "a.strm"), "https://example.test/video")
	if err := a.store.update(func(st *State) error {
		st.Tasks = append(st.Tasks, Task{ID: "cached", Kind: "strm", Target: dir})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	read := func(cached string) []scrapeWork {
		t.Helper()
		w := request(t, h, "GET", "/api/strm-scrape/items?taskId=cached&group=true&cached="+cached, nil, cookie)
		var items []scrapeWork
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &items) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return items
	}
	if len(read("true")) != 1 {
		t.Fatal("initial discovery failed")
	}
	first := a.loadScrapeIndex("cached", dir).ScannedAt
	writeTest(t, filepath.Join(dir, "Other", "b.strm"), "https://example.test/other")
	if len(read("true")) != 1 || !a.loadScrapeIndex("cached", dir).ScannedAt.Equal(first) {
		t.Fatal("cached visit scanned or rewrote the index")
	}
	if len(read("false")) != 2 {
		t.Fatal("explicit refresh failed")
	}
	writeTest(t, filepath.Join(dir, "Third", "c.strm"), "https://example.test/third")
	if err := a.store.update(func(st *State) error { st.Tasks[0].LastRun = time.Now().Add(time.Second); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(read("true")) != 3 {
		t.Fatal("generation did not invalidate cache")
	}
	index := a.loadScrapeIndex("cached", dir)
	index.ScannedAt = time.Now().Add(-6 * time.Minute)
	if err := a.saveScrapeIndex("cached", index); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(dir, "Fourth", "d.strm"), "https://example.test/fourth")
	if len(read("true")) != 4 {
		t.Fatal("expired cache was used")
	}
	if err := a.store.update(func(st *State) error {
		st.Tasks[0].LastRun = time.Now().Add(-time.Minute)
		st.Tasks[0].Status = "running"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	read("true")
	writeTest(t, filepath.Join(dir, "Fifth", "e.strm"), "https://example.test/fifth")
	if err := a.store.update(func(st *State) error { st.Tasks[0].Status = "success"; return nil }); err != nil {
		t.Fatal(err)
	}
	if len(read("true")) != 5 {
		t.Fatal("completion after an in-progress read did not invalidate cache")
	}
}
