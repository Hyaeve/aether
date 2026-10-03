package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestListenAddress(t *testing.T) {
	for _, tc := range []struct {
		name, port, addr, want string
		invalid                bool
	}{
		{name: "default", want: ":15151"},
		{name: "custom port", port: "15200", want: ":15200"},
		{name: "minimum", port: "1", want: ":1"},
		{name: "maximum", port: "65535", want: ":65535"},
		{name: "zero", port: "0", invalid: true},
		{name: "negative", port: "-1", invalid: true},
		{name: "out of range", port: "65536", invalid: true},
		{name: "non numeric", port: "abc", invalid: true},
		{name: "address override", port: "15200", addr: "127.0.0.1:15159", want: "127.0.0.1:15159"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AETHER_PORT", tc.port)
			t.Setenv("AETHER_ADDR", tc.addr)
			got, err := listenAddress()
			if (err != nil) != tc.invalid || got != tc.want {
				t.Fatalf("listenAddress() = %q, %v; want %q, invalid=%v", got, err, tc.want, tc.invalid)
			}
		})
	}
}

func testApp(t *testing.T) *App {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()
	a, err := New(ctx, filepath.Join(dir, "data"), filepath.Join(dir, "output"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); a.wg.Wait() })
	return a
}

func addLocal(t *testing.T, a *App) Storage {
	t.Helper()
	root := t.TempDir()
	s := Storage{ID: id(), Name: "Local", Type: "local", Enabled: true, Config: map[string]string{"root": root}}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	return s
}

func writeTest(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestStoreEncryptionAndRecovery(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	s.Config["cookie"] = "TOP_SECRET_CREDENTIAL"
	if err := a.store.update(func(st *State) error { st.Storages[0] = s; return nil }); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(a.store.dir, "state.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("TOP_SECRET")) {
		t.Fatal("credentials stored in plaintext")
	}
	recovered, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.snapshot().Storages[0].Config["cookie"] != "TOP_SECRET_CREDENTIAL" {
		t.Fatal("recovery lost credentials")
	}
	if err := a.store.update(func(st *State) error { st.Username = "should rollback"; return os.ErrPermission }); err == nil {
		t.Fatal("expected update failure")
	}
	if a.store.snapshot().Username != "" {
		t.Fatal("failed mutation was not rolled back")
	}
}

func TestCacheLRUTTLAndSnapshot(t *testing.T) {
	c := NewCache()
	cfg := Settings{CacheEnabled: true, CacheMaxItems: 2, CacheMemoryMB: 1, CachePersist: true}
	c.put("a", []File{{Name: "one"}}, 30, cfg)
	c.put("b", []File{{Name: "two"}}, 30, cfg)
	c.get("a")
	c.put("c", []File{{Name: "three"}}, 30, cfg)
	if _, ok := c.get("b"); ok {
		t.Fatal("LRU did not evict b")
	}
	c.put("expired", []File{}, -1, cfg)
	if _, ok := c.get("expired"); ok {
		t.Fatal("expired entry returned")
	}
	c.put("big", []File{{Name: strings.Repeat("x", 2<<20)}}, 30, cfg)
	if _, ok := c.get("big"); ok {
		t.Fatal("memory limit ignored")
	}
	dir := t.TempDir()
	if err := c.persist(dir); err != nil {
		t.Fatal(err)
	}
	restored := NewCache()
	restored.restore(dir, cfg)
	if _, ok := restored.get("c"); !ok {
		t.Fatal("cache snapshot not restored")
	}
	if restored.stats()["entries"].(int) > 2 {
		t.Fatal("restored cache exceeded bounds")
	}
}

func TestLocalBoundary(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "movie.MP4"), "abcdef")
	files, err := a.rawList(context.Background(), s, "/")
	if err != nil || len(files) != 1 {
		t.Fatalf("list failed: %v %v", files, err)
	}
	for _, name := range []string{"../", "/../private", "..\\outside", "C:/Windows"} {
		if _, err := relative(name); err == nil {
			t.Fatalf("accepted traversal %s", name)
		}
	}
	outside := t.TempDir()
	err = os.Symlink(outside, filepath.Join(s.Config["root"], "escape"))
	if err == nil {
		if _, err := a.rawList(context.Background(), s, "/escape"); err == nil {
			t.Fatal("followed symlink outside root")
		}
	}
}

func TestSTRMGenerationFilteringAndIncremental(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	for _, name := range []string{"Film.MP4", "Film.mkv", "trailer.mp4", "skip.avi", "notes.txt", "Excluded/hidden.mp4", "Series/episode.mkv"} {
		writeTest(t, filepath.Join(s.Config["root"], filepath.FromSlash(name)), "media")
	}
	task := Task{Kind: "strm", StorageID: s.ID, Source: "/", Target: "movies", Mode: "full", ExcludeDirs: "excluded", ExcludeFiles: "TRAILER", ExcludeTypes: "AVI"}
	count, err := a.executeTask(context.Background(), task, s)
	if err != nil || count != 3 {
		t.Fatalf("expected 3, got %d: %v", count, err)
	}
	filename := filepath.Join(a.outputDir, "movies", "Film.MP4.strm")
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(content), "http://localhost:15151/stream/") {
		t.Fatal("stream does not reuse port")
	}
	if !strings.Contains(string(content), "?sign=") {
		t.Fatal("stream not signed")
	}
	task.Mode = "incremental"
	writeTest(t, filename, "preserved")
	count, err = a.executeTask(context.Background(), task, s)
	if err != nil || count != 0 {
		t.Fatalf("incremental rewrote existing files: %d %v", count, err)
	}
	content, _ = os.ReadFile(filename)
	if string(content) != "preserved" {
		t.Fatal("incremental changed file")
	}
	task.Target = "../escape"
	if _, err = a.executeTask(context.Background(), task, s); err == nil {
		t.Fatal("accepted output traversal")
	}
}

func TestCacheTaskDepthAndPrecedence(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "one", "two", "a.mp4"), "video")
	task := Task{Kind: "cache", StorageID: s.ID, Source: "/", Depth: 1, CacheTTL: 5}
	count, err := a.executeTask(context.Background(), task, s)
	if err != nil || count != 1 {
		t.Fatalf("depth 1 scanned %d %v", count, err)
	}
	a.cache.mu.Lock()
	entry := a.cache.items[s.ID+":/"].Value.(cacheEntry)
	a.cache.mu.Unlock()
	if remaining := time.Until(entry.Expires); remaining < 4*time.Minute || remaining > 6*time.Minute {
		t.Fatal("task TTL ignored")
	}
	task.Depth = 0
	count, err = a.executeTask(context.Background(), task, s)
	if err != nil || count != 3 {
		t.Fatalf("unlimited depth scanned %d %v", count, err)
	}
}

func TestTaskValidation(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	task := Task{Name: "Test", Kind: "strm", StorageID: s.ID, Source: "/", Target: "films", Mode: "full", APIInterval: 200, Cron: "0 2 * * *", Enabled: true}
	if err := a.validateTask(&task); err != nil {
		t.Fatal(err)
	}
	if nextRun(task, time.Now()).IsZero() {
		t.Fatal("valid cron not scheduled")
	}
	task.Cron = "every day"
	if err := a.validateTask(&task); err == nil {
		t.Fatal("invalid cron accepted")
	}
	task.Cron = ""
	task.Target = filepath.Join(t.TempDir(), "external")
	if err := a.validateTask(&task); err == nil {
		t.Fatal("external target accepted")
	}
	if !excludedType("film.MP4", "mkv;mp4") || excludedType("film.mp4", "mp") {
		t.Fatal("extension matching incorrect")
	}
}

func request(t *testing.T, handler http.Handler, method, url string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, url, bytes.NewReader(data))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func setupTest(t *testing.T, a *App, h http.Handler) *http.Cookie {
	t.Helper()
	w := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "a-secure-password-123"}, nil)
	if w.Code != 201 {
		t.Fatalf("setup %d %s", w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}

func TestAuthAndAPI(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	if w := request(t, h, "GET", "/api/state", nil, nil); w.Code != 401 {
		t.Fatal("state did not require auth")
	}
	cookie := setupTest(t, a, h)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie")
	}
	w := request(t, h, "POST", "/api/auth/setup", credentials{Username: "other", Password: "a-secure-password-123"}, nil)
	if w.Code != 409 {
		t.Fatal("setup can overwrite account")
	}
	s := Storage{Name: "Local", Type: "local", Enabled: true, Config: map[string]string{"root": t.TempDir(), "cookie": "secret"}}
	w = request(t, h, "POST", "/api/storages", s, cookie)
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/state", nil, cookie)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"cookie":"secret"`) {
		t.Fatal("state exposed secret")
	}
	r := httptest.NewRequest("POST", "http://example.com/api/cache/clear", nil)
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin mutation allowed")
	}
	request(t, h, "POST", "/api/auth/logout", nil, cookie)
	if w = request(t, h, "GET", "/api/state", nil, cookie); w.Code != 401 {
		t.Fatal("logout did not revoke session")
	}
}

func TestSignedStreamAndDAV(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	setupTest(t, a, h)
	writeTest(t, filepath.Join(s.Config["root"], "movie.mp4"), "0123456789")
	link := a.streamURL(s.ID, "/movie.mp4", "")
	r := httptest.NewRequest("GET", link, nil)
	r.Header.Set("Range", "bytes=2-5")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "2345" {
		t.Fatalf("range: %d %s", w.Code, w.Body.String())
	}
	w = request(t, h, "GET", link+"tampered", nil, nil)
	if w.Code != 403 {
		t.Fatal("tampered signature allowed")
	}
	if err := a.store.update(func(st *State) error { st.Settings.WebDAVEnabled = true; return nil }); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("PROPFIND", "/dav/", nil)
	r.Header.Set("Depth", "1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("DAV without authentication")
	}
	r = httptest.NewRequest("PROPFIND", "/dav/", nil)
	r.Header.Set("Depth", "1")
	r.SetBasicAuth("admin", "a-secure-password-123")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 207 || !strings.Contains(w.Body.String(), s.ID) {
		t.Fatalf("DAV list %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("GET", "/dav/"+s.ID+"/movie.mp4", nil)
	r.Header.Set("Range", "bytes=4-7")
	r.SetBasicAuth("admin", "a-secure-password-123")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "4567" {
		t.Fatalf("DAV range %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("DELETE", "/dav/"+s.ID+"/movie.mp4", nil)
	r.SetBasicAuth("admin", "a-secure-password-123")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 405 {
		t.Fatal("DAV allowed writes")
	}
}

func TestOpenListDriverMock(t *testing.T) {
	a := testApp(t)
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "mock-token" {
			t.Error("token missing")
		}
		switch r.URL.Path {
		case "/api/fs/list":
			jsonResponse(w, 200, map[string]any{"code": 200, "data": map[string]any{"total": 1, "content": []map[string]any{{"name": "movie.mp4", "size": 12, "is_dir": false}}}})
		case "/api/fs/get":
			jsonResponse(w, 200, map[string]any{"code": 200, "data": map[string]string{"raw_url": upstream.URL + "/file"}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	s := Storage{ID: "mock", Type: "openlist", Config: map[string]string{"address": upstream.URL, "token": "mock-token"}}
	files, err := a.rawList(context.Background(), s, "/")
	if err != nil || len(files) != 1 || files[0].ID != "/movie.mp4" {
		t.Fatalf("list: %v %v", files, err)
	}
	d, err := a.download(context.Background(), s, files[0].ID, "")
	if err != nil || d.URL != upstream.URL+"/file" {
		t.Fatalf("download: %v %v", d, err)
	}
}

func TestWebDAVClientMock(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			t.Error("wrong method")
		}
		w.WriteHeader(207)
		io.WriteString(w, `<d:multistatus xmlns:d="DAV:"><d:response><d:href>/root/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response><d:response><d:href>/root/Film%20One.mp4</d:href><d:propstat><d:prop><d:getcontentlength>42</d:getcontentlength></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`)
	}))
	defer upstream.Close()
	s := Storage{Type: "webdav", Config: map[string]string{"address": upstream.URL + "/root/"}}
	files, err := davList(context.Background(), s, "/")
	if err != nil || len(files) != 1 || files[0].Name != "Film One.mp4" {
		t.Fatalf("DAV list %v %v", files, err)
	}
}

func Test115FieldCompatibility(t *testing.T) {
	for _, tc := range []struct {
		payload  string
		dir      bool
		id, name string
	}{
		{`{"fid":"42","fn":"Movies","fc":"0"}`, true, "42", "Movies"},
		{`{"file_id":123,"file_name":"Film.mkv","file_category":1,"size_byte":"1024","pick_code":"abc"}`, false, "123", "Film.mkv"},
		{`{"cid":"8","n":"Old folder"}`, true, "8", "Old folder"},
	} {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(tc.payload), &raw); err != nil {
			t.Fatal(err)
		}
		item, err := file115(raw)
		if err != nil || item.IsDir != tc.dir || item.ID != tc.id || item.Name != tc.name {
			t.Fatalf("%s => %+v %v", tc.payload, item, err)
		}
	}
}

func TestDefaultSTRMTarget(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "default.mp4"), "video")
	task := Task{Name: "Default output", Kind: "strm", StorageID: s.ID, Source: "/", Mode: "full", APIInterval: 200}
	if err := a.validateTask(&task); err != nil {
		t.Fatal(err)
	}
	count, err := a.executeTask(context.Background(), task, s)
	if err != nil || count != 1 {
		t.Fatalf("default output: %d %v", count, err)
	}
	if _, err := os.Stat(filepath.Join(a.outputDir, "default.mp4.strm")); err != nil {
		t.Fatal(err)
	}
}

func TestConfigMigrationAndDataSeparation(t *testing.T) {
	base := t.TempDir()
	configDir, dataDir := filepath.Join(base, "config"), filepath.Join(base, "data")
	legacy, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.update(func(st *State) error {
		st.Username = "existing-owner"
		st.Password = "preserved-hash"
		st.Settings.CacheTTL = 47
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a, err := newWithDirectories(context.Background(), configDir, dataDir, filepath.Join(dataDir, "strm"))
	if err != nil {
		t.Fatal(err)
	}
	st := a.store.snapshot()
	if st.Username != "existing-owner" || st.Password != "preserved-hash" || st.Settings.CacheTTL != 47 {
		t.Fatal("migration lost configuration")
	}
	for _, name := range []string{"master.key", "state.enc"} {
		old, err := os.ReadFile(filepath.Join(dataDir, name))
		if err != nil {
			t.Fatal(err)
		}
		migrated, err := os.ReadFile(filepath.Join(configDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "master.key" && !bytes.Equal(old, migrated) {
			t.Fatal("migration changed key")
		}
	}
	if err := a.cache.persist(a.dataDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "cache.json")); !os.IsNotExist(err) {
		t.Fatal("cache stored with configuration")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "cache.json")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"organize-rules", "categories", "upgrade-policies", "ai", "recognition-rules"} {
		if info, err := os.Stat(filepath.Join(configDir, name)); err != nil || !info.IsDir() {
			t.Fatalf("missing config directory %s", name)
		}
	}
	if err := a.store.update(func(st *State) error { st.Username = "new-owner"; return nil }); err != nil {
		t.Fatal(err)
	}
	reopened, err := newWithDirectories(context.Background(), configDir, dataDir, filepath.Join(dataDir, "strm"))
	if err != nil {
		t.Fatal(err)
	}
	if reopened.store.snapshot().Username != "new-owner" {
		t.Fatal("legacy configuration overwrote new configuration")
	}
}

func TestMigrationRefusesConflictingKey(t *testing.T) {
	base := t.TempDir()
	configDir, dataDir := filepath.Join(base, "config"), filepath.Join(base, "data")
	if _, err := NewStore(dataDir); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(configDir, "master.key"), "different-key")
	if err := prepareDirectories(configDir, dataDir); err == nil {
		t.Fatal("conflicting key was accepted")
	}
	key, err := os.ReadFile(filepath.Join(configDir, "master.key"))
	if err != nil || string(key) != "different-key" {
		t.Fatal("existing key overwritten")
	}
}
