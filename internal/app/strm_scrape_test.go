package app

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSTRMScrapeScanMatchAndNFO(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Arrival.2016.mkv.strm"), []byte("http://media.example/video"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	cfg := scrapeSettings{WriteMode: "missing", Episodes: true, Actors: true}
	items, err := scanSTRM(context.Background(), root, scrapeIndex{}, cfg)
	if err != nil || len(items) != 1 || items[0].Title != "Arrival" || items[0].Year != "2016" {
		t.Fatal(items, err)
	}
	tv := recognizeSTRM("Show.S02E03.1080p.mkv.strm")
	if tv.Kind != "tv" || tv.Season != 2 || tv.Episode != 3 || tv.Title != "Show" {
		t.Fatal(tv)
	}
	var ambiguous atomic.Bool
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Error("missing TMDB auth")
		}
		switch r.URL.Path {
		case "/3/search/movie":
			if ambiguous.Load() {
				jsonResponse(w, 200, map[string]any{"results": []any{map[string]any{"id": 1, "title": "Arrival", "release_date": "2016-01-01"}, map[string]any{"id": 2, "title": "Arrival", "release_date": "2016-01-01"}}})
				return
			}
			jsonResponse(w, 200, map[string]any{"results": []any{map[string]any{"id": 1, "title": "Arrival", "release_date": "2016-01-01"}}})
		case "/3/movie/1":
			jsonResponse(w, 200, map[string]any{"id": 1, "title": "Arrival", "overview": "<safe> & plot", "credits": map[string]any{"cast": []any{map[string]string{"name": "Actor", "character": "Role"}}}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	p := PluginConfig{APIURL: up.URL, ImageURL: up.URL, APIKey: "key", Language: "en-US"}
	if err := scrapeOne(context.Background(), root, up.Client(), p, cfg, &items[0]); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "Arrival.2016.mkv.nfo"))
	var nfo scrapeNFO
	if err != nil || xml.Unmarshal(b, &nfo) != nil || nfo.Plot != "<safe> & plot" || len(nfo.Actors) != 1 || items[0].Status != "ok" {
		t.Fatal(string(b), nfo, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Arrival.2016.mkv.nfo"), []byte("user metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := scrapeOne(context.Background(), root, up.Client(), p, cfg, &items[0]); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "Arrival.2016.mkv.nfo"))
	if string(b) != "user metadata" {
		t.Fatal("missing-only overwrote metadata")
	}
	cfg.WriteMode = "overwrite"
	if err := scrapeOne(context.Background(), root, up.Client(), p, cfg, &items[0]); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, "Arrival.2016.mkv.nfo"))
	if string(b) == "user metadata" {
		t.Fatal("overwrite failed")
	}
	ambiguous.Store(true)
	items[0].TMDB = 0
	if err := scrapeOne(context.Background(), root, up.Client(), p, cfg, &items[0]); err != nil || items[0].Status != "doubt" {
		t.Fatal(items[0], err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scanSTRM(ctx, root, scrapeIndex{}, cfg); err == nil {
		t.Fatal("cancel ignored")
	}
	items[0].Path = "../escape.strm"
	items[0].TMDB = 1
	if err := scrapeOne(context.Background(), root, up.Client(), p, cfg, &items[0]); err == nil {
		t.Fatal("escaped root")
	}
}

func TestSTRMScrapeAPIAndPersistence(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Film.strm"), []byte("http://media.example"), 0600)
	a.store.update(func(st *State) error { st.Tasks = []Task{{ID: "task", Kind: "strm", Target: dir}}; return nil })
	h := a.Handler(t.TempDir())
	if w := request(t, h, "GET", "/api/strm-scrape/settings", nil, nil); w.Code != 401 {
		t.Fatal("unprotected")
	}
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	cfg := scrapeSettings{WriteMode: "overwrite", Episodes: true, Excluded: "skip"}
	if w := request(t, h, "PUT", "/api/strm-scrape/settings", cfg, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if a.scrapeConfig().WriteMode != "overwrite" {
		t.Fatal("settings not saved")
	}
	if w := request(t, h, "POST", "/api/strm-scrape/run", map[string]string{"taskId": "task"}, cookie); w.Code != 400 {
		t.Fatal("disabled TMDB ran")
	}
	if w := request(t, h, "POST", "/api/strm-scrape/scan", map[string]string{"taskId": "task"}, cookie); w.Code != 202 {
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
			t.Fatal("scan timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
	w := request(t, h, "GET", "/api/strm-scrape/items?taskId=task", nil, cookie)
	var items []scrapeItem
	if json.Unmarshal(w.Body.Bytes(), &items) != nil || len(items) != 1 {
		t.Fatal(w.Body.String())
	}
	w = request(t, h, "POST", "/api/strm-scrape/match", map[string]any{"taskId": "task", "path": items[0].Path, "tmdb": 123, "kind": "movie"}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request(t, h, "GET", "/api/strm-scrape/items?taskId=task", nil, cookie)
	if !strings.Contains(w.Body.String(), `"tmdb":123`) {
		t.Fatal("match not persisted")
	}
	if pluginDefaults("ai", PluginConfig{}).APIURL != "" {
		t.Fatal("AI default URL must be empty")
	}
}

func TestSTRMWorkspaceDiscoversGeneratedFiles(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "Arrival.2016.mkv"), "video")
	task := Task{ID: "discover", Kind: "strm", StorageID: s.ID, Source: "/", Target: t.TempDir(), Mode: "full"}
	if err := a.store.update(func(st *State) error { st.Tasks = append(st.Tasks, task); return nil }); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	endpoint := "/api/strm-scrape/items?taskId=discover"
	if w := request(t, h, "GET", endpoint, nil, nil); w.Code != 401 {
		t.Fatal("unprotected discovery")
	}
	// The real generator is used; no explicit scrape scan or TMDB configuration.
	if _, err := a.executeTask(context.Background(), task, s); err != nil {
		t.Fatal(err)
	}
	w := request(t, h, "GET", endpoint, nil, cookie)
	var items []scrapeItem
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &items) != nil || len(items) == 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, item := range items {
		if item.Status != "pending" || item.TMDB != 0 {
			t.Fatal(item)
		}
	}
	first := items[0]
	w = request(t, h, "POST", "/api/strm-scrape/match", map[string]any{"taskId": task.ID, "path": first.Path, "tmdb": 123, "kind": first.Kind}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	root, err := a.scrapeRoot(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "New.2026.strm"), []byte("http://media.example"), 0600); err != nil {
		t.Fatal(err)
	}
	w = request(t, h, "GET", endpoint, nil, cookie)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &items) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	found, added := false, false
	for _, item := range items {
		found = found || item.Path == first.Path && item.TMDB == 123
		added = added || item.Path == "New.2026.strm" && item.TMDB == 0
	}
	if !found || !added {
		t.Fatal("discovery lost matching or new files", items)
	}
	if err := os.Remove(filepath.Join(root, "New.2026.strm")); err != nil {
		t.Fatal(err)
	}
	w = request(t, h, "GET", endpoint, nil, cookie)
	if strings.Contains(w.Body.String(), "New.2026.strm") {
		t.Fatal("removed file remains in workspace")
	}
	if _, err := os.Stat(filepath.Join(root, strings.TrimSuffix(first.Path, ".strm")+".nfo")); !os.IsNotExist(err) {
		t.Fatal("discovery wrote metadata", err)
	}
}
