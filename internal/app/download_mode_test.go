package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStorageDownloadPolicy(t *testing.T) {
	for _, kind := range []string{"115", "mobile", "tianyi", "openlist", "webdav", "quark", "local"} {
		s := Storage{Type: kind, Config: map[string]string{}}
		if storageRedirect(s) != (kind != "local" && kind != "quark") {
			t.Fatal(kind)
		}
		s.Config["downloadMode"] = "proxy"
		if storageRedirect(s) {
			t.Fatal("proxy ignored", kind)
		}
	}
}

func TestOpenListUARefreshAndStreamModes(t *testing.T) {
	a := testApp(t)
	receivedUA := ""
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("OpenList credential leaked")
		}
		w.Write([]byte("media"))
	}))
	defer media.Close()
	refresh := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/fs/list":
			var body struct {
				Refresh bool `json:"refresh"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			refresh = body.Refresh
			jsonResponse(w, 200, map[string]any{"code": 200, "data": map[string]any{"content": []any{}}})
		case "/api/fs/get":
			receivedUA = r.UserAgent()
			jsonResponse(w, 200, map[string]any{"code": 200, "data": map[string]string{"raw_url": media.URL}})
		}
	}))
	defer upstream.Close()
	s := Storage{ID: "ol", Name: "ol", Type: "openlist", Enabled: true, Config: map[string]string{"address": upstream.URL, "token": "secret", "refreshList": "true", "passUA": "true"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	if _, err := a.rawList(context.Background(), s, "/"); err != nil || !refresh {
		t.Fatal(err, refresh)
	}
	for _, mode := range []string{"redirect", "proxy"} {
		s.Config["downloadMode"] = mode
		a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
		r := httptest.NewRequest("GET", a.streamURL(s.ID, "/movie", ""), nil)
		r.Header.Set("User-Agent", "MyBrowser")
		w := httptest.NewRecorder()
		a.Handler(t.TempDir()).ServeHTTP(w, r)
		if receivedUA != "MyBrowser" {
			t.Fatal(receivedUA)
		}
		if mode == "redirect" && (w.Code != 302 || w.Header().Get("Location") != media.URL) {
			t.Fatal(w.Code)
		}
		if mode == "proxy" && (w.Code != 200 || w.Body.String() != "media") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	s.Config["passUA"] = "false"
	if _, err := a.downloadWithUA(context.Background(), s, "/movie", "", "DoNotForward"); err != nil || receivedUA == "DoNotForward" {
		t.Fatal(err, receivedUA)
	}
}
