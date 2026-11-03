package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScrapeManualQueueSerialExecution(t *testing.T) {
	first, release, second := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once, releaseOnce, secondOnce sync.Once
	var active, maximum atomic.Int32
	defer releaseOnce.Do(func() { close(release) })
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		if n > maximum.Load() {
			maximum.Store(n)
		}
		switch r.URL.Path {
		case "/3/movie/123":
			once.Do(func() { close(first) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			jsonResponse(w, 200, map[string]any{"id": 123, "title": "First", "release_date": "2026-01-01"})
		case "/3/movie/124":
			secondOnce.Do(func() { close(second) })
			jsonResponse(w, 200, map[string]any{"id": 124, "title": "Second", "release_date": "2026-01-01"})
		default:
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	a := testApp(t)
	base := t.TempDir()
	for _, name := range []string{"First.strm", "Second.strm"} {
		writeTest(t, filepath.Join(base, name), "http://example.test/video")
	}
	a.store.update(func(st *State) error {
		st.Tasks = []Task{{ID: "queue", Kind: "strm", Target: base}}
		st.Plugins = map[string]PluginConfig{"tmdb": {Enabled: true, APIURL: up.URL, ImageURL: up.URL, APIKey: "key", RequestInterval: 1}}
		return nil
	})
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	if w := request(t, h, "GET", "/api/strm-scrape/items?taskId=queue", nil, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	match := func(path string, id int) *httptest.ResponseRecorder {
		return request(t, h, "POST", "/api/strm-scrape/match", map[string]any{"taskId": "queue", "path": path, "tmdb": id, "kind": "movie", "group": true, "scrape": true}, cookie)
	}
	if w := match("First.strm", 123); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	select {
	case <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("first scrape never started")
	}
	w := match("Second.strm", 124)
	var p scrapeProgress
	json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 202 || p.Queued != 1 || !p.Running {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = match("Second.strm", 124); w.Code != 409 {
		t.Fatal("duplicate queue accepted")
	}
	if w = match("missing.strm", 125); w.Code != 409 {
		t.Fatal("unknown work accepted")
	}
	select {
	case <-second:
		t.Fatal("queued work overlapped active scrape")
	default:
	}
	for _, item := range a.loadScrapeIndex("queue", base).Items {
		if item.Path == "Second.strm" && item.TMDB != 0 {
			t.Fatal("queue mutated active index")
		}
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("queue did not continue")
	}
	a.wg.Wait()
	if maximum.Load() != 1 {
		t.Fatal("concurrent TMDB writes", maximum.Load())
	}
	for _, name := range []string{"First.nfo", "Second.nfo"} {
		if _, err := os.Stat(filepath.Join(base, name)); err != nil {
			t.Fatal(name, err)
		}
	}
	if len(a.scrapeQueue) != 0 || a.scrapeProgress.Running {
		t.Fatal("queue remained running")
	}
}

func TestScrapeManualQueueStopsAndRejectsChangedRoot(t *testing.T) {
	for _, mode := range []string{"stop", "change"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			base := t.TempDir()
			writeTest(t, filepath.Join(base, "Film.strm"), "http://example.test/video")
			a.store.update(func(st *State) error { st.Tasks = []Task{{ID: "t", Kind: "strm", Target: base}}; return nil })
			a.saveScrapeIndex("t", scrapeIndex{Root: base, Items: []scrapeItem{{Path: "Film.strm", Kind: "movie"}}})
			a.scrapeProgress.Running = true
			if err := a.queueScrapeMatch(scrapeRequest{TaskID: "t", Path: "Film.strm", TMDB: 1, Kind: "movie", Scrape: true}); err != nil {
				t.Fatal(err)
			}
			if mode == "stop" {
				r := httptest.NewRequest("POST", "/", nil)
				r.SetPathValue("action", "stop")
				w := httptest.NewRecorder()
				a.strmScrape(w, r)
				if w.Code != 200 {
					t.Fatal(w.Code)
				}
			} else {
				a.store.update(func(st *State) error { st.Tasks[0].Target = t.TempDir(); return nil })
			}
			a.scrapeProgress.Running = false
			a.drainScrapeQueue()
			if len(a.scrapeQueue) != 0 || a.scrapeProgress.Running {
				t.Fatal("cancelled work executed")
			}
			for _, item := range a.loadScrapeIndex("t", base).Items {
				if item.TMDB != 0 {
					t.Fatal("cancelled work changed match")
				}
			}
		})
	}
}
