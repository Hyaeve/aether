package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScrapeWorksGroupSeasonsWithoutMixingLibraries(t *testing.T) {
	short := recognizeSTRMPath("Show.2020/Season 2/S02E03.strm")
	if short.Title != "Show" || short.Year != "2020" || short.Kind != "tv" || short.Episode != 3 {
		t.Fatal(short)
	}
	items := []scrapeItem{
		{Path: "Show/Season 1/Show.S01E01.strm", Title: "Show", Kind: "tv", Season: 1, Episode: 1, Status: "ok"},
		{Path: "Show/Season 2/Show.S02E01.strm", Title: "Localized Show", Kind: "tv", Season: 2, Episode: 1, Status: "pending"},
		{Path: "Other/Show.S01E01.strm", Title: "Show", Kind: "tv", Season: 1, Episode: 1, Status: "pending"},
		{Path: "Show/Show.2020.strm", Title: "Show", Kind: "movie", Status: "pending"},
	}
	works := scrapeWorks(items)
	if len(works) != 3 || works[0].Count != 2 || works[0].Status != "pending" {
		t.Fatal(works)
	}
	a := testApp(t)
	s := addLocal(t, a)
	task := Task{ID: "test", Kind: "strm", StorageID: s.ID, Target: t.TempDir()}
	if err := a.store.update(func(st *State) error { st.Tasks = append(st.Tasks, task); return nil }); err != nil {
		t.Fatal(err)
	}
	root, err := a.scrapeRoot(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.saveScrapeIndex(task.ID, scrapeIndex{Root: root, Items: items}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/strm-scrape/match", map[string]any{"taskId": task.ID, "path": items[0].Path, "group": true, "kind": "tv", "tmdb": 123}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	updated := a.loadScrapeIndex(task.ID, root).Items
	if updated[0].TMDB != 123 || updated[1].TMDB != 123 || updated[2].TMDB != 0 || updated[3].TMDB != 0 || updated[1].Season != 2 {
		t.Fatal(updated)
	}
	w = request(t, h, "POST", "/api/strm-scrape/reset", map[string]any{"taskId": task.ID, "scope": "../Show", "confirmed": true}, cookie)
	if w.Code != 400 {
		t.Fatal("invalid scope accepted", w.Code)
	}
	w = request(t, h, "POST", "/api/strm-scrape/reset", map[string]any{"taskId": task.ID, "path": items[0].Path, "group": true, "scope": "Show/Season 2", "confirmed": true}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	updated = a.loadScrapeIndex(task.ID, root).Items
	if updated[0].TMDB != 123 || updated[1].TMDB != 0 {
		t.Fatal("scope crossed season boundary", updated)
	}
}

func TestScrapeCandidatesUseConfiguredTMDB(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/search/tv" || r.URL.Query().Get("query") != "Show" || r.Header.Get("Authorization") != "Bearer key" {
			t.Error("invalid candidate query")
		}
		jsonResponse(w, 200, map[string]any{"results": []map[string]any{{"id": 123, "name": "Show", "first_air_date": "2026-01-01", "poster_path": "/poster.jpg"}}})
	}))
	defer up.Close()
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		if st.Plugins == nil {
			st.Plugins = map[string]PluginConfig{}
		}
		st.Plugins["tmdb"] = PluginConfig{Enabled: true, APIURL: up.URL, ImageURL: up.URL, APIKey: "key", Language: "zh-CN"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	input := map[string]string{"query": "Show", "kind": "tv"}
	if w := request(t, h, "POST", "/api/strm-scrape/candidates", input, nil); w.Code != 401 {
		t.Fatal("unprotected search")
	}
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/strm-scrape/candidates", input, cookie)
	var candidates []map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &candidates) != nil || len(candidates) != 1 || candidates[0]["poster"] != up.URL+"/t/p/w342/poster.jpg" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestIdentifySTRMLibraryDoesNotWriteMediaFiles(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		media := map[string]any{"id": 123, "title": "Film", "release_date": "2026-01-01", "poster_path": "/poster.jpg"}
		if r.URL.Path == "/3/search/movie" {
			jsonResponse(w, 200, map[string]any{"results": []any{media}})
			return
		}
		if r.URL.Path == "/3/movie/123" {
			jsonResponse(w, 200, media)
			return
		}
		t.Error("identify attempted image or episode download", r.URL.Path)
		w.WriteHeader(404)
	}))
	defer up.Close()
	a := testApp(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Film.2026.strm"), []byte("https://media.example/file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.store.update(func(st *State) error {
		st.Tasks = []Task{{ID: "identify", Kind: "strm", Target: dir}}
		st.Plugins = map[string]PluginConfig{"tmdb": {Enabled: true, APIURL: up.URL, ImageURL: up.URL, APIKey: "key"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/strm-scrape/identify", map[string]string{"taskId": "identify"}, cookie)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.scrapeMu.Lock()
		running := a.scrapeProgress.Running
		a.scrapeMu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("identify timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
	items := a.loadScrapeIndex("identify", dir).Items
	if len(items) != 1 || items[0].TMDB != 123 || items[0].Poster != up.URL+"/t/p/w342/poster.jpg" || items[0].Status != "pending" {
		t.Fatal(items)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("identify wrote media files", entries, err)
	}
}
