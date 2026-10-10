package app

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScrapeFilesReadEditAndBoundaries(t *testing.T) {
	a := testApp(t)
	base := t.TempDir()
	writeTest(t, filepath.Join(base, "Show", "one.strm"), "http://example.test/play")
	writeTest(t, filepath.Join(base, "Show", "tvshow.nfo"), "<tvshow><title>Show</title></tvshow>")
	writeTest(t, filepath.Join(base, "Show", "private.key"), "secret")
	writeTest(t, filepath.Join(base, "Show", "invalid.nfo"), string([]byte{255}))
	writeTest(t, filepath.Join(base, "Show", "huge.nfo"), strings.Repeat("a", (2<<20)+1))
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{{ID: "library", Kind: "strm", Target: base}}; return nil }); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	endpoint := func(p string) string { return "/api/strm-scrape/files?taskId=library&path=" + url.QueryEscape(p) }
	if w := request(t, h, "GET", endpoint("Show"), nil, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := request(t, h, "GET", endpoint("Show"), nil, cookie)
	var entries []map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &entries) != nil || len(entries) != 4 || strings.Contains(w.Body.String(), "private.key") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "GET", endpoint("Show/tvshow.nfo"), nil, cookie)
	var text map[string]string
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &text) != nil || !strings.Contains(text["content"], "Show") {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, p := range []string{"../outside", "/absolute", "Show/private.key", "Show/invalid.nfo", "Show/huge.nfo"} {
		if w := request(t, h, "GET", endpoint(p), nil, cookie); w.Code != 400 {
			t.Fatal(p, w.Code, w.Body.String())
		}
	}
	text["content"] = "<tvshow><title>Edited</title></tvshow>"
	w = request(t, h, "PUT", endpoint("Show/tvshow.nfo"), text, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	data, _ := os.ReadFile(filepath.Join(base, "Show", "tvshow.nfo"))
	if string(data) != text["content"] {
		t.Fatal(string(data))
	}
	if w := request(t, h, "PUT", endpoint("Show/tvshow.nfo"), text, cookie); w.Code != 409 {
		t.Fatal("stale edit accepted", w.Code)
	}
	w = request(t, h, "GET", endpoint("Show/one.strm"), nil, cookie)
	_ = json.Unmarshal(w.Body.Bytes(), &text)
	a.scrapeProgress.Running = true
	if w := request(t, h, "PUT", endpoint("Show/one.strm"), text, cookie); w.Code != 409 {
		t.Fatal("running edit accepted", w.Code)
	}
	a.scrapeProgress.Running = false
	text["content"] = strings.Repeat("a", (1<<20)+1)
	if w := request(t, h, "PUT", endpoint("Show/one.strm"), text, cookie); w.Code != 200 {
		t.Fatal("valid large text rejected", w.Code, w.Body.String())
	}
	w = request(t, h, "GET", endpoint("Show/one.strm"), nil, cookie)
	_ = json.Unmarshal(w.Body.Bytes(), &text)
	text["content"] = strings.Repeat("a", (2<<20)+1)
	if w := request(t, h, "PUT", endpoint("Show/one.strm"), text, cookie); w.Code != 400 {
		t.Fatal("oversized edit accepted", w.Code)
	}
	outside := filepath.Join(t.TempDir(), "outside.nfo")
	writeTest(t, outside, "outside")
	if os.Symlink(outside, filepath.Join(base, "link.nfo")) == nil {
		if w := request(t, h, "GET", endpoint("link.nfo"), nil, cookie); w.Code == 200 {
			t.Fatal("outside symlink readable")
		}
	}
}
