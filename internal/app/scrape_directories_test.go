package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestScrapeDirectoriesListsUnindexedFoldersAndConfinesRoot(t *testing.T) {
	a := testApp(t)
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "Empty", "Child"), 0755); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(base, "film.strm"), "https://example.test/video")
	if err := a.store.update(func(st *State) error {
		st.Tasks = append(st.Tasks, Task{ID: "library", Kind: "strm", Target: base})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	for _, tc := range []struct{ path, want string }{{"", "Empty"}, {"Empty", "Empty/Child"}} {
		w := request(t, h, "GET", "/api/strm-scrape/directories?taskId=library&path="+tc.path, nil, cookie)
		var result struct {
			Root        string   `json:"root"`
			Directories []string `json:"directories"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Root != base || len(result.Directories) != 1 || result.Directories[0] != tc.want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := request(t, h, "GET", "/api/strm-scrape/directories?taskId=library&path=../outside", nil, cookie)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = request(t, h, "POST", "/api/strm-scrape/reset", map[string]any{"taskId": "library", "scopes": []string{"Empty", "../outside"}}, cookie)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
