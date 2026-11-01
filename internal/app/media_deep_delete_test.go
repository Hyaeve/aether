package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aether/internal/linkcore/upstream"
)

type deepFixture struct {
	a       *App
	s       Storage
	link    MediaLink
	root    string
	names   []string
	handler http.Handler
	after   func()
	status  int
	remove  bool
	writes  int
	kind    string
	series  bool
}

func newDeepFixture(t *testing.T, kind string, names ...string) *deepFixture {
	t.Helper()
	a := testApp(t)
	s := addLocal(t, a)
	root := t.TempDir()
	f := &deepFixture{a: a, s: s, root: root, names: names, kind: kind, status: 204, remove: true}
	for _, name := range names {
		writeTest(t, filepath.Join(s.Config["root"], strings.TrimSuffix(name, ".strm")+".mkv"), "real-media")
		writeTest(t, filepath.Join(root, name), filepath.Join(s.Config["root"], strings.TrimSuffix(name, ".strm")+".mkv"))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" || r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/Delete") {
			f.writes++
			if f.after != nil {
				f.after()
			}
			if f.status >= 200 && f.status < 300 && f.remove {
				for _, name := range f.names {
					_ = os.Remove(filepath.Join(root, name))
				}
			}
			w.WriteHeader(f.status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if kind == "audiobookshelf" {
			files := []any{}
			for i, name := range f.names {
				files = append(files, map[string]any{"ino": string(rune('a' + i)), "metadata": map[string]any{"path": "/media/" + name, "filename": name, "ext": ".strm"}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "item", "libraryFiles": files, "media": map[string]any{"audioFiles": []any{}, "metadata": map[string]any{"title": "作品"}}})
			return
		}
		items := []any{}
		for i, name := range f.names {
			items = append(items, map[string]any{"Id": string(rune('a' + i)), "Name": name, "Type": "Episode", "Path": "/media/" + name, "MediaSources": []any{map[string]any{"Id": "source", "Path": "/media/" + name, "Container": "strm"}}})
		}
		if strings.HasSuffix(r.URL.Path, "PlaybackInfo") {
			_ = json.NewEncoder(w).Encode(map[string]any{"MediaSources": []any{map[string]any{"Id": "source", "Path": "/media/" + f.names[0], "Container": "strm"}}})
			return
		}
		if r.URL.Query().Get("ParentId") != "" {
			_ = json.NewEncoder(w).Encode(map[string]any{"Items": items, "TotalRecordCount": len(items)})
			return
		}
		item := map[string]any{"Id": "item", "Name": "作品", "Type": "Movie", "Path": "/media/" + f.names[0], "MediaSources": []any{map[string]any{"Id": "source", "Path": "https://upstream.test/movie.mkv", "Container": "strm"}}}
		if f.series {
			item["Type"] = "Series"
			item["Path"] = "/media/series"
		}
		if r.URL.Path == "/Items" || r.URL.Path == "/emby/Items" {
			_ = json.NewEncoder(w).Encode(map[string]any{"Items": []any{item}, "TotalRecordCount": 1})
			return
		}
		_ = json.NewEncoder(w).Encode(item)
	}))
	t.Cleanup(server.Close)
	f.link = MediaLink{ID: "deep-link", Name: "媒体服务", Type: kind, Address: server.URL, APIKey: "test", Enabled: true, MediaRoot: root, UpstreamRoot: "/media", Port: 15992, Mode: "always"}
	if err := a.store.update(func(st *State) error {
		st.Links = []MediaLink{f.link}
		st.MediaDeepDelete = mediaDeepDeleteSettings{true, []string{f.link.ID}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	provider, err := upstream.New(a.linkUpstream(f.link))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(server.URL)
	f.handler = a.withMediaDeepDelete(f.link, provider, httputil.NewSingleHostReverseProxy(u))
	return f
}
func (f *deepFixture) request(method, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("X-Emby-Token", "client-token")
	r.Header.Set("X-Emby-Authorization", `MediaBrowser UserId="u",Token="client-token"`)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func (f *deepFixture) source(name string) string {
	return filepath.Join(f.s.Config["root"], strings.TrimSuffix(name, ".strm")+".mkv")
}

func TestMediaDeepDeleteExplicitProviders(t *testing.T) {
	for _, kind := range []string{"emby", "fnos", "audiobookshelf"} {
		t.Run(kind, func(t *testing.T) {
			f := newDeepFixture(t, kind, "film.strm")
			path := "/emby/Items/item?UserId=u"
			if kind == "audiobookshelf" {
				path = "/api/items/item?hard=1"
			}
			w := f.request("DELETE", path)
			if w.Code != 204 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if _, err := os.Stat(f.source("film.strm")); !os.IsNotExist(err) {
				t.Fatal("real source not removed", err)
			}
			ledger, err := f.a.deepLedger()
			if err != nil || len(ledger.Records) != 1 || ledger.Records[0].Status != "success" || ledger.Records[0].Count != 1 {
				t.Fatalf("%+v %v", ledger, err)
			}
			trash, _ := filepath.Glob(filepath.Join(f.s.Config["root"], ".aether-trash", "*", "film.mkv"))
			if len(trash) != 1 {
				t.Fatal("storage recycle mode not preserved")
			}
		})
	}
}
func TestMediaDeepDeleteWholeSeriesAndProtectedFormats(t *testing.T) {
	f := newDeepFixture(t, "emby", "one.strm", "two.strm", "protected.cas.strm", "protected.share.strm", "protected.ed2k.strm")
	f.series = true
	writeTest(t, filepath.Join(f.s.Config["root"], "unrelated.mkv"), "keep")
	w := f.request("POST", "/Items/item/Delete?UserId=u")
	if w.Code != 204 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, name := range f.names {
		_, err := os.Stat(f.source(name))
		if ordinaryDeepSTRM(name) != os.IsNotExist(err) {
			t.Fatal("wrong source set", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.s.Config["root"], "unrelated.mkv")); err != nil {
		t.Fatal(err)
	}
	ledger, _ := f.a.deepLedger()
	if ledger.Records[0].Count != 2 {
		t.Fatal(ledger)
	}
}
func TestMediaDeepDeleteRejectsFailureStaleAndRecreated(t *testing.T) {
	for _, condition := range []string{"denied", "still-exists", "source-changed", "config-changed", "recreated", "ledger-intent", "bad-pointer", "task-busy"} {
		t.Run(condition, func(t *testing.T) {
			f := newDeepFixture(t, "emby", "film.strm")
			switch condition {
			case "denied":
				f.status = 403
			case "still-exists":
				f.remove = false
			case "source-changed":
				f.after = func() {
					writeTest(t, f.source("film.strm"), "changed-media")
					_ = os.Chtimes(f.source("film.strm"), time.Now(), time.Now().Add(time.Second))
				}
			case "config-changed":
				f.after = func() { _ = f.a.store.update(func(st *State) error { st.MediaDeepDelete.Enabled = false; return nil }) }
			case "recreated":
				f.remove = false
				f.after = func() {
					_ = os.Remove(filepath.Join(f.root, "film.strm"))
					writeTest(t, filepath.Join(f.root, "film.strm"), "changed")
				}
			case "ledger-intent":
				source, e := f.a.findDeepSource(context.Background(), f.s, "/film.mkv")
				if e != nil {
					t.Fatal(e)
				}
				if e = f.a.saveDeepLedger(mediaDeepDeleteLedger{Intents: map[string]bool{deepIntent(source): true}}); e != nil {
					t.Fatal(e)
				}
			case "bad-pointer":
				writeTest(t, filepath.Join(f.root, "film.strm"), "https://unknown.test/stream/unsafe?sign=bad")
			case "task-busy":
				task := Task{ID: "running", Name: "生成中", Kind: "strm", StorageID: f.s.ID, Source: "/", Target: t.TempDir()}
				_ = f.a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil })
				f.a.running[task.ID] = func() {}
			}
			w := f.request("DELETE", "/Items/item?UserId=u")
			if condition == "ledger-intent" || condition == "bad-pointer" || condition == "task-busy" {
				if w.Code != 409 || f.writes != 0 {
					t.Fatalf("unsafe request forwarded: %d %d %s", w.Code, f.writes, w.Body.String())
				}
			}
			if _, err := os.Stat(f.source("film.strm")); err != nil {
				t.Fatal("source removed", err)
			}
		})
	}
}
func TestMediaDeepDeleteIgnoresRefreshDatabaseOnlyAndDisabled(t *testing.T) {
	for _, condition := range []string{"scan", "abs-soft", "disabled", "excluded"} {
		t.Run(condition, func(t *testing.T) {
			kind := "emby"
			if condition == "abs-soft" {
				kind = "audiobookshelf"
			}
			name := "film.strm"
			if condition == "excluded" {
				name = "film.share.strm"
			}
			f := newDeepFixture(t, kind, name)
			if condition == "disabled" {
				_ = f.a.store.update(func(st *State) error { st.MediaDeepDelete.Enabled = false; return nil })
			}
			method, target := "DELETE", "/Items/item?UserId=u"
			if condition == "scan" {
				method, target = "POST", "/Library/Refresh"
			}
			if condition == "abs-soft" {
				target = "/api/items/item"
			}
			f.request(method, target)
			if _, err := os.Stat(f.source(name)); err != nil {
				t.Fatal("unrelated source removed", err)
			}
			ledger, err := f.a.deepLedger()
			if err != nil || len(ledger.Records) != 0 {
				t.Fatal("unexpected operation ledger", ledger, err)
			}
		})
	}
}
func TestMediaDeepDeleteSignedContentsAndNoDisguisedCAS(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	for _, claim := range []streamClaim{{Storage: s.ID, File: "/film.mkv"}, {Storage: s.ID, File: "/film.mkv", CAS: &CASInfo{Name: "film.mkv"}}, {Storage: s.ID, File: "/film.mkv", ED2K: &ED2KInfo{}}} {
		_, _, err := a.deepSourceRef(a.signedStreamURL(claim))
		if (claim.CAS != nil || claim.ED2K != nil) != (err != nil) {
			t.Fatal("wrong typed content decision", err)
		}
	}
}
func TestMediaDeepDeleteConfigConfirmationAndPersistence(t *testing.T) {
	f := newDeepFixture(t, "emby", "film.strm")
	_ = f.a.store.update(func(st *State) error { st.MediaDeepDelete.Enabled = false; return nil })
	for _, confirmed := range []bool{false, true} {
		raw, _ := json.Marshal(map[string]any{"enabled": true, "linkIds": []string{f.link.ID}, "confirmed": confirmed})
		r := httptest.NewRequest("PUT", "/api/media-deep-delete", strings.NewReader(string(raw)))
		w := httptest.NewRecorder()
		f.a.mediaDeepDeleteAPI(w, r)
		want := 400
		if confirmed {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	raw, _ := json.Marshal(f.a.store.snapshotWithLogLimit(0))
	if !strings.Contains(string(raw), `"mediaDeepDelete":{"enabled":true`) {
		t.Fatal("settings not part of encrypted config")
	}
	if _, err := f.a.deepLedger(); err != nil {
		t.Fatal(err)
	}
}

func TestMediaDeepDeleteNative115AndReferenceAuthorization(t *testing.T) {
	f := newDeepFixture(t, "emby", "film.strm")
	s := Storage{ID: "cloud115", Name: "115测试", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie, "root": "0"}}
	q := Storage{ID: "cloudquark", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "test"}}
	if err := f.a.store.update(func(st *State) error { st.Storages = append(st.Storages, s, q); return nil }); err != nil {
		t.Fatal(err)
	}
	file := File{ID: "123", Name: "电影 & 中文.mkv", PickCode: "cq391yoe47hu5dd6u"}
	for _, pool := range []Storage{s, q} {
		content, err := f.a.publicSTRMURL(pool, file)
		if err != nil {
			t.Fatal(err)
		}
		got, fid, err := f.a.deepSourceRef(content)
		if err != nil || got.ID != pool.ID || fid != file.ID {
			t.Fatalf("%s %s %v", got.ID, fid, err)
		}
		if pool.Type == "115" {
			writeTest(t, filepath.Join(f.root, "film.strm"), content)
		}
	}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	deletes := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"state":false,"errno":500}`
		switch r.URL.Path {
		case "/files":
			body = `{"state":true,"cid":"0","count":1,"offset":0,"data":[{"fid":"123","cid":"0","n":"电影 & 中文.mkv","s":9,"pc":"cq391yoe47hu5dd6u"}]}`
		case "/rb/delete":
			deletes++
			_ = r.ParseForm()
			if !strings.Contains(r.Form.Encode(), "123") {
				t.Error("wrong cloud deletion IDs", r.Form)
			}
			body = `{"state":true}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	w := f.request("DELETE", "/Items/item?UserId=u")
	if w.Code != 204 || deletes != 1 {
		ledger, _ := f.a.deepLedger()
		t.Fatalf("%d deletes=%d %s %+v", w.Code, deletes, w.Body.String(), ledger)
	}
	// The unrelated local file is not chosen just because its basename matches.
	if _, err := os.Stat(f.source("film.strm")); err != nil {
		t.Fatal("wrong account deleted")
	}
}

func TestMediaDeepDeleteRefusesPointerMutationAndRestartReplay(t *testing.T) {
	f := newDeepFixture(t, "emby", "film.strm")
	p, err := f.a.prepareDeepPlan(context.Background(), f.link, []string{"/media/film.strm"})
	if err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(f.root, "film.strm"), "different target")
	if verifyDeepPointers(p) == nil {
		t.Fatal("changed pointer accepted")
	}
	p.root.Close()
	writeTest(t, filepath.Join(f.root, "film.strm"), f.source("film.strm"))
	f.request("DELETE", "/Items/item?UserId=u")
	reopened, err := NewStore(f.a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	f.a.store = reopened
	if !reopened.snapshotWithLogLimit(0).MediaDeepDelete.Enabled {
		t.Fatal("config lost at restart")
	}
	writeTest(t, f.source("film.strm"), "restored-media")
	writeTest(t, filepath.Join(f.root, "film.strm"), f.source("film.strm"))
	w := f.request("DELETE", "/Items/item?UserId=u")
	if w.Code != 409 || f.writes != 1 {
		t.Fatal("replayed deletion", w.Code, f.writes)
	}
}

func TestMediaDeepDeleteHTTPProtection(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	if w := request(t, h, "GET", "/api/media-deep-delete", nil, nil); w.Code != 401 {
		t.Fatal("missing authentication", w.Code)
	}
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	if w := request(t, h, "GET", "/api/media-deep-delete", nil, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(t, h, "DELETE", "/api/media-deep-delete", nil, cookie); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := request(t, h, "PUT", "/api/media-deep-delete", map[string]any{"enabled": true, "linkIds": []string{"missing"}, "confirmed": true}, cookie); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestMediaDeepDeleteRequestBoundaries(t *testing.T) {
	for _, tc := range []struct {
		kind, method, path string
		match              bool
	}{
		{"emby", "DELETE", "/Emby/items/ABC", true}, {"emby", "POST", "/items/ABC/delete", true},
		{"emby", "DELETE", "/Items?Ids=a,b", true}, {"emby", "DELETE", "/Items/a/PlaybackInfo", false},
		{"emby", "POST", "/Library/Refresh", false}, {"emby", "DELETE", "/Users/u/FavoriteItems/a", false},
		{"audiobookshelf", "DELETE", "/api/items/a?hard=1", true}, {"audiobookshelf", "DELETE", "/api/items/a?hard=0", false},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		_, matched, err := deepDeleteIDs(tc.kind, r)
		if matched != tc.match || err != nil {
			t.Fatalf("%+v: %t %v", tc, matched, err)
		}
	}
	for _, name := range []string{"cas.strm", "share.strm", "ed2k.strm", "A.CAS.STRM", "a.mkv.share.strm", "a.mkv.ed2k.strm"} {
		if ordinaryDeepSTRM(name) {
			t.Fatal("special type accepted", name)
		}
	}
}

func TestMediaDeepDeleteBatchesNativeSourceParent(t *testing.T) {
	f := newDeepFixture(t, "emby", "one.strm", "two.strm")
	f.series = true
	s := Storage{ID: "native", Name: "115", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie, "root": "0"}}
	if err := f.a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	for i, name := range f.names {
		content, err := f.a.publicSTRMURL(s, File{ID: fmt.Sprint(123 + i), Name: strings.TrimSuffix(name, ".strm") + ".mkv", PickCode: fmt.Sprintf("cq391yoe47hu5dd%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		writeTest(t, filepath.Join(f.root, name), content)
	}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	reads, deletes := 0, 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"state":false,"errno":500}`
		switch r.URL.Path {
		case "/files":
			reads++
			body = `{"state":true,"cid":"0","count":2,"offset":0,"data":[{"fid":"123","cid":"0","n":"one.mkv","s":9,"pc":"cq391yoe47hu5dd0"},{"fid":"124","cid":"0","n":"two.mkv","s":9,"pc":"cq391yoe47hu5dd1"}]}`
		case "/rb/delete":
			deletes++
			_ = r.ParseForm()
			for _, fid := range []string{"123", "124"} {
				if !strings.Contains(r.Form.Encode(), fid) {
					t.Error("missing batch file", r.Form)
				}
			}
			body = `{"state":true}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	w := f.request("DELETE", "/Items/item?UserId=u")
	ledger, _ := f.a.deepLedger()
	if w.Code != 204 || deletes != 1 || reads != 4 || ledger.Records[0].Count != 2 {
		t.Fatalf("%d reads=%d deletes=%d ledger=%+v", w.Code, reads, deletes, ledger)
	}
}
