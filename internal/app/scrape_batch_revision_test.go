package app

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitRevisionScrape(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a.scrapeMu.Lock()
		running := a.scrapeProgress.Running
		a.scrapeMu.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scrape timed out")
}
func TestScrapeBatchMatchWritesAndRefreshesLocalPoster(t *testing.T) {
	image, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a9XcAAAAASUVORK5CYII=")
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/3/search/movie":
			jsonResponse(w, 200, map[string]any{"results": []any{map[string]any{"id": 123, "title": "Film", "release_date": "2026-01-01"}}})
		case "/3/movie/123":
			jsonResponse(w, 200, map[string]any{"id": 123, "title": "Film", "release_date": "2026-01-01", "poster_path": "/poster.png"})
		case "/t/p/w780/poster.png":
			w.Write(image)
		default:
			t.Errorf("unexpected URL %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	a := testApp(t)
	base := t.TempDir()
	for _, dir := range []string{"Film", "Other"} {
		os.Mkdir(filepath.Join(base, dir), 0700)
		os.WriteFile(filepath.Join(base, dir, "film.strm"), []byte("http://example.test/movie"), 0600)
	}
	os.WriteFile(filepath.Join(base, "Film", "cover.png"), image, 0600)
	a.store.update(func(st *State) error {
		st.Tasks = []Task{{ID: "t", Kind: "strm", Target: base}}
		st.Plugins = map[string]PluginConfig{"tmdb": {Enabled: true, APIURL: up.URL, ImageURL: up.URL, APIKey: "key", RequestInterval: 1}}
		return nil
	})
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	if w := request(t, h, "GET", "/api/strm-scrape/items?taskId=t", nil, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(t, h, "POST", "/api/strm-scrape/match", map[string]any{"taskId": "t", "path": "Film/film.strm", "tmdb": 123, "kind": "movie", "group": true, "scrape": true}, cookie); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitRevisionScrape(t, a)
	for _, name := range []string{"film.nfo", "film-poster.jpg"} {
		if _, err := os.Stat(filepath.Join(base, "Film", name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "Other", "film.nfo")); !os.IsNotExist(err) {
		t.Fatal("scraped unselected work")
	}
	items := a.loadScrapeIndex("t", base).Items
	root, _ := os.OpenRoot(base)
	defer root.Close()
	localScrapePosters(root, "t", items)
	cover, info := openScrapeCover(root, items[0])
	if cover == nil {
		t.Fatal("missing generated cover")
	}
	cover.Close()
	if info.Name() != "film-poster.jpg" {
		t.Fatal("old cover hid generated matched poster", info.Name())
	}
	if items[0].Status != "ok" || items[0].Poster == "" {
		t.Fatal(items)
	}
	if w := request(t, h, "POST", "/api/strm-scrape/identify", map[string]any{"taskId": "t", "paths": []string{"Other/film.strm"}, "excludedScopes": []string{"Other"}, "scrape": true, "reidentify": true}, cookie); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitRevisionScrape(t, a)
	if _, err := os.Stat(filepath.Join(base, "Other", "film.nfo")); !os.IsNotExist(err) {
		t.Fatal("excluded scope was scraped")
	}
	if w := request(t, h, "POST", "/api/strm-scrape/identify", map[string]any{"taskId": "t", "paths": []string{"missing.strm"}}, cookie); w.Code != 400 {
		t.Fatal("unknown selection accepted", w.Code)
	}
}
func TestScrapeBatchResetDeletesOnlySelectedWorkNonSTRM(t *testing.T) {
	a := testApp(t)
	base := t.TempDir()
	for _, dir := range []string{"Show/Season 1", "Show/Season 2", "Other"} {
		os.MkdirAll(filepath.Join(base, dir), 0700)
		for _, file := range []string{"S01E01.strm", "cover.jpg", "episode.nfo", "notes.txt"} {
			os.WriteFile(filepath.Join(base, dir, file), []byte("data"), 0600)
		}
	}
	items := []scrapeItem{{Path: "Show/Season 1/S01E01.strm", Kind: "tv", TMDB: 123, Manual: true}, {Path: "Show/Season 2/S01E01.strm", Kind: "tv", TMDB: 123, Manual: true}, {Path: "Other/S01E01.strm", Kind: "tv", TMDB: 456, Manual: true}}
	a.store.update(func(st *State) error { st.Tasks = []Task{{ID: "t", Kind: "strm", Target: base}}; return nil })
	a.saveScrapeIndex("t", scrapeIndex{Root: base, Items: items})
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	body := map[string]any{"taskId": "t", "paths": []string{items[0].Path}, "group": true, "deleteFiles": true}
	if w := request(t, h, "POST", "/api/strm-scrape/reset", body, cookie); w.Code != 400 {
		t.Fatal("missing confirmation accepted")
	}
	body["confirmed"] = true
	if w := request(t, h, "POST", "/api/strm-scrape/reset", body, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, dir := range []string{"Show/Season 1", "Show/Season 2"} {
		entries, err := os.ReadDir(filepath.Join(base, dir))
		if err != nil || len(entries) != 1 || entries[0].Name() != "S01E01.strm" {
			t.Fatal(entries, err)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(base, "Other")); len(entries) != 4 {
		t.Fatal("deleted another work")
	}
	updated := a.loadScrapeIndex("t", base).Items
	if updated[0].TMDB != 0 || updated[1].TMDB != 0 || updated[2].TMDB != 456 {
		t.Fatal(updated)
	}
	if _, err := resetScrapeFiles(context.Background(), base, []scrapeItem{{Path: "root.strm", Kind: "movie"}}, func(scrapeItem) bool { return true }); err == nil {
		t.Fatal("shared root accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resetScrapeFiles(ctx, base, items, func(i scrapeItem) bool { return i.Path == items[2].Path }); err == nil {
		t.Fatal("cancellation ignored")
	}
	os.WriteFile(filepath.Join(base, "Other", "new.strm"), []byte("data"), 0600)
	if _, err := resetScrapeFiles(context.Background(), base, items, func(i scrapeItem) bool { return i.Path == items[2].Path }); err == nil {
		t.Fatal("unindexed STRM accepted")
	}
	if _, err := os.Stat(filepath.Join(base, "Other", "cover.jpg")); err != nil {
		t.Fatal("preflight deleted metadata", err)
	}
}
