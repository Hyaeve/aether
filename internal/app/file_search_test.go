package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestFileSearchScopeTrailAndLinks(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	for _, name := range []string{"Match.txt", "A/MatchFolder/MatchChild.txt", "A/MatchFolder/plain.txt", "Other/MatchOutside.txt"} {
		writeTest(t, filepath.Join(s.Config["root"], name), "data")
	}
	for _, tc := range []struct {
		dir   string
		count int
	}{{"/", 4}, {"/A", 2}, {"/A/MatchFolder", 1}} {
		results, err := a.searchFiles(context.Background(), s, tc.dir, "match")
		if err != nil || len(results) != tc.count {
			t.Fatalf("%s: %v %v", tc.dir, results, err)
		}
		for _, f := range results {
			if !f.IsDir && f.URL == "" {
				t.Fatal("missing stream URL")
			}
			if f.ID == "/A/MatchFolder/MatchChild.txt" && tc.dir == "/" {
				if f.Parent != "/A/MatchFolder" || len(f.Trail) != 2 || f.Trail[0].Name != "A" || f.Trail[1].ID != "/A" {
					t.Fatal("invalid parent/trail", f)
				}
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.searchFiles(ctx, s, "/", "match"); err == nil {
		t.Fatal("cancel ignored")
	}
	if _, err := a.searchFiles(context.Background(), s, "/../outside", "match"); err == nil {
		t.Fatal("traversal accepted")
	}
	outside := t.TempDir()
	writeTest(t, filepath.Join(outside, "MatchSecret.txt"), "secret")
	if os.Symlink(outside, filepath.Join(s.Config["root"], "escape")) == nil {
		results, err := a.searchFiles(context.Background(), s, "/", "match")
		if err != nil || len(results) != 4 {
			t.Fatal("symlink traversed", results, err)
		}
	}
}

func TestFileSearchAuthenticatedEndpoint(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "中文MATCH.txt"), "data")
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "secret"}, nil).Result().Cookies()[0]
	address := "/api/files/search?" + url.Values{"storage": {s.ID}, "path": {"/"}, "q": {" match "}}.Encode()
	if w := request(t, h, "GET", address, nil, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := request(t, h, "GET", address, nil, cookie)
	var results []fileSearchResult
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &results) != nil || len(results) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(t, h, "GET", "/api/files/search?storage="+s.ID+"&q=", nil, cookie); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := request(t, h, "POST", address, nil, cookie); w.Code != 405 && w.Code != 404 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("GET", address, nil)
	r.AddCookie(cookie)
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	r = r.WithContext(ctx)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatal("cancel returned success")
	}
}

func TestFileSearchOpaqueIDsCycleAndResultLimit(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "search-cloud", Type: "115", Enabled: true, Config: map[string]string{}}
	cfg := a.store.snapshotWithLogLimit(0).Settings
	a.cache.put(s.ID+":0", []File{{ID: "opaque-a", Name: "作品", IsDir: true}}, 30, cfg)
	a.cache.put(s.ID+":opaque-a", []File{{ID: "opaque-b", Name: "match", IsDir: true}, {ID: "file-id", Name: "MATCH.txt", PickCode: "pick-code"}}, 30, cfg)
	a.cache.put(s.ID+":opaque-b", []File{}, 30, cfg)
	results, err := a.searchFiles(context.Background(), s, "/", "match")
	if err != nil || len(results) != 2 || results[0].Parent != "opaque-a" || results[0].Trail[0].ID != "0" || results[0].Trail[0].Name != "作品" || results[1].URL == "" {
		t.Fatal(results, err)
	}
	a.cache.put(s.ID+":opaque-b", []File{{ID: "opaque-a", Name: "cycle", IsDir: true}}, 30, cfg)
	if _, err := a.searchFiles(context.Background(), s, "/", "match"); err == nil {
		t.Fatal("cycle ignored")
	}
	entries := make([]File, 10001)
	for i := range entries {
		entries[i] = File{ID: "item", Name: "match.txt"}
	}
	a.cache.put(s.ID+":0", entries, 30, cfg)
	if _, err := a.searchFiles(context.Background(), s, "/", "match"); err == nil {
		t.Fatal("result limit ignored")
	}
}
