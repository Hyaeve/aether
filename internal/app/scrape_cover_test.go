package app

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScrapeLocalCoverAndReset(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	writeTest(t, filepath.Join(dir, "Show", "Season 1", "S01E01.strm"), "https://example.test/video")
	writeTest(t, filepath.Join(dir, "Show", "tvshow.nfo"), "<tvshow/>")
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Show", "cover.png"), data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.store.update(func(st *State) error {
		st.Tasks = append(st.Tasks, Task{ID: "cover", Kind: "strm", Target: dir})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	item := scrapeItem{Path: "Show/Season 1/S01E01.strm", Title: "Show", Kind: "tv", TMDB: 123, Manual: true, Status: "ok"}
	if err := a.saveScrapeIndex("cover", scrapeIndex{Root: dir, Items: []scrapeItem{item}}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "GET", "/api/strm-scrape/items?taskId=cover&group=true", nil, cookie)
	var works []scrapeWork
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &works) != nil || len(works) != 1 || !strings.HasPrefix(works[0].Poster, "/api/strm-scrape/cover?") {
		t.Fatal(w.Code, w.Body.String())
	}
	url := works[0].Poster
	if w := request(t, h, "GET", url, nil, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(t, h, "GET", url, nil, cookie); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data.Bytes()) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(t, h, "GET", "/api/strm-scrape/cover?taskId=cover&path=../secret.strm", nil, cookie); w.Code != 404 {
		t.Fatal(w.Code)
	}
	input := map[string]any{"taskId": "cover", "path": item.Path, "group": true}
	if w := request(t, h, "POST", "/api/strm-scrape/reset", input, cookie); w.Code != 400 {
		t.Fatal(w.Code)
	}
	input["confirmed"] = true
	if w := request(t, h, "POST", "/api/strm-scrape/reset", input, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	got := a.loadScrapeIndex("cover", dir).Items[0]
	if got.TMDB != 0 || got.Manual || got.Status != "pending" {
		t.Fatal(got)
	}
	for _, name := range []string{"tvshow.nfo", "cover.png", "Season 1/S01E01.strm"} {
		if _, err := os.Stat(filepath.Join(dir, "Show", name)); err != nil {
			t.Fatal(err)
		}
	}
}
