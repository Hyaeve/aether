package app

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func thumbnailFixture(t *testing.T) (*thumbnailService, Storage, *http.Cookie, string) {
	t.Helper()
	a := testApp(t)
	s := addLocal(t, a)
	if err := os.WriteFile(filepath.Join(s.Config["root"], "frame.mp4"), []byte("test video"), 0600); err != nil {
		t.Fatal(err)
	}
	tc, err := a.newThumbnailService()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tc.Close() })
	cookie := request(t, a.Handler(t.TempDir()), "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	endpoint := "/api/files/thumbnail?" + url.Values{"storage": {s.ID}, "parent": {"/"}, "id": {"/frame.mp4"}}.Encode()
	return tc, s, cookie, endpoint
}

func thumbnailPNG(t *testing.T, width int) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, width, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < width; x++ {
			im.Set(x, y, color.RGBA{30, 90, 150, 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func thumbnailRequest(tc *thumbnailService, method, endpoint string, cookie *http.Cookie, body io.Reader, revision string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, endpoint, body)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	r.Header.Set("Content-Type", "image/png")
	r.Header.Set("X-Thumbnail-Revision", revision)
	w := httptest.NewRecorder()
	tc.ServeHTTP(w, r)
	return w
}

func thumbnailUpload(t *testing.T, tc *thumbnailService, cookie *http.Cookie, endpoint string) []byte {
	t.Helper()
	miss := thumbnailRequest(tc, "GET", endpoint, cookie, nil, "")
	if miss.Code != 404 || len(miss.Header().Get("X-Thumbnail-Revision")) != 64 {
		t.Fatal("expected revision miss", miss.Code, miss.Body.String())
	}
	upload := thumbnailRequest(tc, "POST", endpoint, cookie, bytes.NewReader(thumbnailPNG(t, 80)), miss.Header().Get("X-Thumbnail-Revision"))
	if upload.Code != 200 || upload.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatal("upload", upload.Code, upload.Body.String())
	}
	return upload.Body.Bytes()
}

func TestThumbnailHitTouchExpiryAndRestart(t *testing.T) {
	tc, _, cookie, endpoint := thumbnailFixture(t)
	now := time.Now()
	tc.now = func() time.Time { return now }
	data := thumbnailUpload(t, tc, cookie, endpoint)
	now = now.Add(71 * time.Hour)
	got := thumbnailRequest(tc, "GET", endpoint, cookie, nil, "")
	if got.Code != 200 || !bytes.Equal(data, got.Body.Bytes()) {
		t.Fatal("hit failed", got.Code)
	}
	if got.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("browser cached private image")
	}
	now = now.Add(71 * time.Hour)
	if err := tc.cleanup(); err != nil {
		t.Fatal(err)
	}
	if tc.entries != 1 {
		t.Fatal("touch did not preserve thumbnail")
	}
	reopened, err := tc.app.newThumbnailService()
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopened.now = func() time.Time { return now }
	if hit := thumbnailRequest(reopened, "GET", endpoint, cookie, nil, ""); hit.Code != 200 {
		t.Fatal("not persisted", hit.Code)
	}
	now = now.Add(thumbnailTTL)
	if err := reopened.cleanup(); err != nil {
		t.Fatal(err)
	}
	if reopened.entries != 0 || reopened.bytes != 0 {
		t.Fatal("not cleaned")
	}
	if miss := thumbnailRequest(reopened, "GET", endpoint, cookie, nil, ""); miss.Code != 404 {
		t.Fatal("expired returned")
	}
}

func TestThumbnailInvalidInputAuthenticationAndPrivacy(t *testing.T) {
	tc, s, cookie, endpoint := thumbnailFixture(t)
	if w := thumbnailRequest(tc, "GET", endpoint, nil, nil, ""); w.Code != 401 {
		t.Fatal("not protected")
	}
	for _, query := range []string{"storage=" + s.ID + "&id=../secret.jpg", "storage=" + s.ID + "&id=http://example.test/a.jpg", "storage=" + s.ID + "&id=/frame.mp4&url=https://example.test", "storage=" + s.ID + "&storage=another&id=/frame.mp4", "storage=" + s.ID + "&parent=/../&id=/frame.mp4", "storage=" + s.ID + "&id=/unknown.jpg"} {
		if w := thumbnailRequest(tc, "GET", "/api/files/thumbnail?"+query, cookie, nil, ""); w.Code != 404 {
			t.Fatal("unsafe query accepted", query, w.Code)
		}
	}
	miss := thumbnailRequest(tc, "GET", endpoint, cookie, nil, "")
	revision := miss.Header().Get("X-Thumbnail-Revision")
	for _, data := range [][]byte{[]byte("<svg></svg>"), thumbnailPNG(t, 513), bytes.Repeat([]byte("a"), thumbnailUploadLimit+1)} {
		w := thumbnailRequest(tc, "POST", endpoint, cookie, bytes.NewReader(data), revision)
		if w.Code != 400 && w.Code != 413 {
			t.Fatal("invalid image accepted", w.Code)
		}
	}
	r := httptest.NewRequest("POST", endpoint, bytes.NewReader(thumbnailPNG(t, 80)))
	r.AddCookie(cookie)
	r.Header.Set("Origin", "https://evil.test")
	r.Header.Set("Content-Type", "image/png")
	w := httptest.NewRecorder()
	tc.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross origin accepted")
	}
	r = httptest.NewRequest("GET", endpoint, nil)
	r.AddCookie(cookie)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	tc.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross site accepted")
	}
	thumbnailUpload(t, tc, cookie, endpoint)
	entries, err := os.ReadDir(filepath.Join(tc.app.dataDir, "thumbnails"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".thumb") || strings.Contains(entries[0].Name(), s.ID) {
		t.Fatal("private cache filename", entries)
	}
	if _, err := thumbnailFilename("../../secret"); err == nil {
		t.Fatal("invalid cache path")
	}
	if err := tc.app.store.update(func(st *State) error { st.Storages[0].Config["credential"] = "secret-CK"; return nil }); err != nil {
		t.Fatal(err)
	}
	changed := thumbnailRequest(tc, "GET", endpoint, cookie, nil, "")
	if changed.Code != 404 || changed.Header().Get("X-Thumbnail-Revision") == revision || strings.Contains(changed.Body.String(), "secret-CK") {
		t.Fatal("revision privacy failed")
	}
	if w := thumbnailRequest(tc, "POST", endpoint, cookie, bytes.NewReader(thumbnailPNG(t, 80)), revision); w.Code != 409 {
		t.Fatal("old upload accepted")
	}
	if err := tc.app.store.update(func(st *State) error { st.Storages[0].Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if w := thumbnailRequest(tc, "GET", endpoint, cookie, nil, ""); w.Code != 404 {
		t.Fatal("disabled cache served")
	}
}

type thumbnailDelayedBody struct {
	*bytes.Reader
	started, release chan struct{}
	first            bool
}

func (b *thumbnailDelayedBody) Read(p []byte) (int, error) {
	if !b.first {
		b.first = true
		close(b.started)
		<-b.release
	}
	return b.Reader.Read(p)
}

func TestThumbnailLateCredentialAndFileRevision(t *testing.T) {
	for _, mode := range []string{"credentials", "file"} {
		t.Run(mode, func(t *testing.T) {
			tc, s, cookie, endpoint := thumbnailFixture(t)
			miss := thumbnailRequest(tc, "GET", endpoint, cookie, nil, "")
			body := &thumbnailDelayedBody{Reader: bytes.NewReader(thumbnailPNG(t, 80)), started: make(chan struct{}), release: make(chan struct{})}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- thumbnailRequest(tc, "POST", endpoint, cookie, body, miss.Header().Get("X-Thumbnail-Revision"))
			}()
			<-body.started
			if mode == "credentials" {
				if err := tc.app.store.update(func(st *State) error { st.Storages[0].Config["token"] = "rotated"; return nil }); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(filepath.Join(s.Config["root"], "frame.mp4"), []byte("changed source size"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			close(body.release)
			if w := <-done; w.Code != 409 {
				t.Fatal("late upload accepted", w.Code)
			}
			if tc.entries != 0 {
				t.Fatal("late upload persisted")
			}
		})
	}
}

func TestThumbnailCachedGETCoalescesUpstreamDirectory(t *testing.T) {
	tc, _, cookie, _ := thumbnailFixture(t)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fs/list" {
			t.Errorf("unexpected upstream path %s", r.URL.Path)
		}
		calls.Add(1)
		jsonResponse(w, 200, map[string]any{"code": 200, "data": map[string]any{"total": 1, "content": []map[string]any{{"name": "frame.mp4", "size": 10}}}})
	}))
	defer upstream.Close()
	s := Storage{ID: "cloud", Name: "Cloud", Type: "openlist", Enabled: true, Config: map[string]string{"address": upstream.URL, "token": "secret"}}
	if err := tc.app.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/files/thumbnail?storage=cloud&parent=/&id=/frame.mp4"
	miss := thumbnailRequest(tc, "GET", endpoint, cookie, nil, "")
	if miss.Code != 404 || miss.Header().Get("X-Thumbnail-Revision") == "" {
		t.Fatal(miss.Code, miss.Body.String())
	}
	f := File{ID: "/frame.mp4", Name: "frame.mp4", Size: 10}
	data, err := normalizeThumbnail(thumbnailPNG(t, 80))
	if err != nil {
		t.Fatal(err)
	}
	tc.mu.Lock()
	err = tc.writeLocked(tc.key(s, "/", f), data)
	tc.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if w := thumbnailRequest(tc, "GET", endpoint, cookie, nil, ""); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls.Load() != 1 {
		t.Fatal("cached GET repeatedly listed upstream", calls.Load())
	}
}

type thumbnailSlowWriter struct {
	header           http.Header
	started, release chan struct{}
}

func (w *thumbnailSlowWriter) Header() http.Header { return w.header }
func (w *thumbnailSlowWriter) WriteHeader(int)     {}
func (w *thumbnailSlowWriter) Write(p []byte) (int, error) {
	close(w.started)
	<-w.release
	return len(p), nil
}

func TestThumbnailSlowResponseDoesNotBlockConfig(t *testing.T) {
	tc, _, cookie, endpoint := thumbnailFixture(t)
	thumbnailUpload(t, tc, cookie, endpoint)
	w := &thumbnailSlowWriter{header: make(http.Header), started: make(chan struct{}), release: make(chan struct{})}
	r := httptest.NewRequest("GET", endpoint, nil)
	r.AddCookie(cookie)
	done := make(chan struct{})
	go func() { tc.ServeHTTP(w, r); close(done) }()
	<-w.started
	updated := make(chan error, 1)
	go func() { updated <- tc.app.store.update(func(st *State) error { st.Settings.LogDays = 14; return nil }) }()
	select {
	case err := <-updated:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(2 * time.Second):
		t.Error("slow network holds config lock")
	}
	close(w.release)
	<-done
}

func TestThumbnailCleanupAndBounds(t *testing.T) {
	tc, _, _, _ := thumbnailFixture(t)
	key := strings.Repeat("a", 64)
	data, err := normalizeThumbnail(thumbnailPNG(t, 80))
	if err != nil {
		t.Fatal(err)
	}
	tc.mu.Lock()
	tc.bytes = thumbnailDiskLimit
	err = tc.writeLocked(key, data)
	tc.mu.Unlock()
	if err == nil {
		t.Fatal("disk bound missing")
	}
	tc.mu.Lock()
	tc.bytes = 0
	tc.entries = thumbnailEntryLimit
	err = tc.writeLocked(key, data)
	tc.mu.Unlock()
	if err == nil {
		t.Fatal("entry bound missing")
	}
	tc.mu.Lock()
	tc.entries = 0
	err = tc.writeLocked(key, data)
	tc.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	name, _ := thumbnailFilename(key)
	if err := tc.root.WriteFile(name, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := tc.cleanup(); err != nil {
		t.Fatal(err)
	}
	if tc.entries != 0 {
		t.Fatal("corrupt cache not cleaned")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tc.Run(ctx)
}

func TestThumbnailServiceDoesNotRetainDirectoryHandles(t *testing.T) {
	a := testApp(t)
	tc, err := a.newThumbnailService()
	if err != nil {
		t.Fatal(err)
	}
	// Removing the empty directory while the service lives fails on Windows if
	// construction retained an os.Root handle.
	if err := os.Remove(filepath.Join(a.dataDir, "thumbnails")); err != nil {
		t.Fatal("retained directory handle", err)
	}
	if err := tc.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestThumbnailConfinedCacheSymlinks(t *testing.T) {
	tc, _, _, _ := thumbnailFixture(t)
	out := filepath.Join(t.TempDir(), "outside.thumb")
	if err := os.WriteFile(out, []byte("do not overwrite"), 0600); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 64)
	name, _ := thumbnailFilename(key)
	if err := tc.root.Symlink(out, name); err != nil {
		t.Skip("symlink permission unavailable")
	}
	tc.mu.Lock()
	_, readErr := tc.readLocked(key)
	writeErr := tc.writeLocked(key, []byte("bad"))
	tc.mu.Unlock()
	if readErr == nil || writeErr == nil {
		t.Fatal("cache symlink accepted")
	}
	if err := tc.cleanup(); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(out)
	if string(content) != "do not overwrite" {
		t.Fatal("escaped cache root")
	}
}

func TestThumbnailRealSlowBodyDeadlineReleasesSlot(t *testing.T) {
	tc, _, cookie, endpoint := thumbnailFixture(t)
	tc.requestTimeout = 150 * time.Millisecond
	tc.slots = make(chan struct{}, 1)
	miss := thumbnailRequest(tc, "GET", endpoint, cookie, nil, "")
	done := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { tc.ServeHTTP(w, r); done <- struct{}{} }))
	defer server.Close()
	connection, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = fmt.Fprintf(connection, "POST %s HTTP/1.1\r\nHost: %s\r\nCookie: %s=%s\r\nContent-Type: image/png\r\nX-Thumbnail-Revision: %s\r\nContent-Length: 100\r\nConnection: close\r\n\r\nx", endpoint, strings.TrimPrefix(server.URL, "http://"), cookie.Name, cookie.Value, miss.Header().Get("X-Thumbnail-Revision"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("body read did not honor deadline")
	}
	if len(tc.slots) != 0 {
		t.Fatal("slow upload retained slot")
	}
	response, err := io.ReadAll(connection)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(response, []byte("408 Request Timeout")) {
		t.Fatal("deadline response", string(response))
	}
	if w := thumbnailRequest(tc, "GET", endpoint, cookie, nil, ""); w.Code != 404 {
		t.Fatal("subsequent request cannot acquire slot", w.Code)
	}
}

func TestThumbnailConcurrentPOSTSharesFreshMetadataAndRejectsLaterRevision(t *testing.T) {
	tc, _, cookie, _ := thumbnailFixture(t)
	var calls, size atomic.Int32
	size.Store(10)
	started, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := calls.Add(1)
		if count == 2 {
			close(started)
			<-release
		}
		jsonResponse(w, 200, map[string]any{"code": 200, "data": map[string]any{"total": 1, "content": []map[string]any{{"name": "frame.mp4", "size": size.Load()}}}})
	}))
	defer upstream.Close()
	s := Storage{ID: "cloud", Name: "Cloud", Type: "openlist", Enabled: true, Config: map[string]string{"address": upstream.URL}}
	if err := tc.app.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/files/thumbnail?storage=cloud&parent=/&id=/frame.mp4"
	miss := thumbnailRequest(tc, "GET", endpoint, cookie, nil, "")
	if miss.Header().Get("X-Thumbnail-Revision") == "" {
		t.Fatal("missing revision")
	}
	data := thumbnailPNG(t, 80)
	results := make(chan int, 3)
	var launch sync.WaitGroup
	launch.Add(3)
	for range 3 {
		go func() {
			launch.Done()
			launch.Wait()
			results <- thumbnailRequest(tc, "POST", endpoint, cookie, bytes.NewReader(data), miss.Header().Get("X-Thumbnail-Revision")).Code
		}()
	}
	<-started
	time.Sleep(100 * time.Millisecond)
	close(release)
	for range 3 {
		if code := <-results; code != 200 {
			t.Fatal("upload failed", code)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("concurrent uploads did not share validation", calls.Load())
	}
	size.Store(11)
	if w := thumbnailRequest(tc, "POST", endpoint, cookie, bytes.NewReader(data), miss.Header().Get("X-Thumbnail-Revision")); w.Code != 409 {
		t.Fatal("completed metadata reused for later upload", w.Code)
	}
	if calls.Load() != 3 {
		t.Fatal("later upload not freshly validated", calls.Load())
	}
}

func TestThumbnailGETTouchesOnlyHeaderAndDoesNotHoldConfigDuringDiskWait(t *testing.T) {
	tc, s, cookie, endpoint := thumbnailFixture(t)
	thumbnailUpload(t, tc, cookie, endpoint)
	files, err := tc.app.rawList(context.Background(), s, "/")
	if err != nil {
		t.Fatal(err)
	}
	name, _ := thumbnailFilename(tc.key(s, "/", files[0]))
	filename := filepath.Join(tc.app.dataDir, "thumbnails", name)
	before, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	tc.now = func() time.Time { return time.Now().Add(time.Hour) }
	tc.mu.Lock()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- thumbnailRequest(tc, "GET", endpoint, cookie, nil, "") }()
	updated := make(chan error, 1)
	go func() { updated <- tc.app.store.update(func(st *State) error { st.Settings.LogDays = 14; return nil }) }()
	select {
	case err := <-updated:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(2 * time.Second):
		t.Error("disk wait holds config lock")
	}
	tc.mu.Unlock()
	if w := <-done; w.Code != 200 {
		t.Fatal("read failed", w.Code)
	}
	after, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	next, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(info, next) {
		t.Fatal("GET replaced JPEG file")
	}
	if !bytes.Equal(before[:8], after[:8]) || !bytes.Equal(before[16:], after[16:]) || bytes.Equal(before[8:16], after[8:16]) {
		t.Fatal("GET did not restrict writes to timestamp header")
	}
	tc.now = func() time.Time { return time.Now().Add(74 * time.Hour) }
	if w := thumbnailRequest(tc, "GET", endpoint, cookie, nil, ""); w.Code != 404 {
		t.Fatal("expired GET returned image", w.Code)
	}
	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatal("expired GET retained disk file", err)
	}
}

func TestThumbnailSourceProxiesRedirectStorageRangeAndHEAD(t *testing.T) {
	tc, _, cookie, _ := thumbnailFixture(t)
	var reads atomic.Int32
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("storage credentials forwarded to media host")
		}
		http.ServeContent(w, r, "frame.mp4", time.Time{}, strings.NewReader("0123456789"))
	}))
	defer media.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/fs/list":
			jsonResponse(w, 200, map[string]any{"code": 200, "data": map[string]any{"total": 1, "content": []map[string]any{{"name": "frame.mp4", "size": 10}}}})
		case "/api/fs/get":
			jsonResponse(w, 200, map[string]any{"code": 200, "data": map[string]any{"raw_url": media.URL}})
		default:
			t.Errorf("unexpected upstream path %s", r.URL.Path)
		}
	}))
	defer upstream.Close()
	s := Storage{ID: "cloud", Name: "Cloud", Type: "openlist", Enabled: true, Config: map[string]string{"address": upstream.URL, "token": "private-token", "downloadMode": "redirect"}}
	if err := tc.app.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/files/thumbnail-source?storage=cloud&parent=/&id=/frame.mp4"
	for _, test := range []struct {
		method, rangeHeader, body string
		status                    int
	}{{"GET", "", "0123456789", 200}, {"GET", "bytes=3-6", "3456", 206}, {"HEAD", "", "", 200}} {
		r := httptest.NewRequest(test.method, endpoint, nil)
		r.AddCookie(cookie)
		r.Header.Set("Range", test.rangeHeader)
		w := httptest.NewRecorder()
		tc.ServeSource(w, r)
		if w.Code != test.status || w.Body.String() != test.body || w.Header().Get("Location") != "" {
			t.Fatal("source did not proxy", test, w.Code, w.Body.String(), w.Header())
		}
		if test.rangeHeader != "" && w.Header().Get("Content-Range") != "bytes 3-6/10" {
			t.Fatal("range response", w.Header())
		}
	}
	if reads.Load() < 2 {
		t.Fatal("server did not read upstream")
	}
	entries, err := os.ReadDir(filepath.Join(tc.app.dataDir, "thumbnails"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("source endpoint persisted original media")
	}
}

func TestThumbnailSourceSecurityAndCancellation(t *testing.T) {
	tc, s, cookie, endpoint := thumbnailFixture(t)
	endpoint = strings.Replace(endpoint, "/thumbnail?", "/thumbnail-source?", 1)
	for _, test := range []struct {
		method, endpoint string
		cookie           *http.Cookie
		want             int
	}{
		{"GET", endpoint, nil, 401}, {"POST", endpoint, cookie, 405},
		{"GET", "/api/files/thumbnail-source?storage=" + s.ID + "&id=/../secret.mp4", cookie, 404},
		{"GET", endpoint + "&url=http://evil.test", cookie, 404},
		{"GET", "/api/files/thumbnail-source?storage=" + s.ID + "&id=/absent.mp4", cookie, 404},
	} {
		r := httptest.NewRequest(test.method, test.endpoint, nil)
		if test.cookie != nil {
			r.AddCookie(test.cookie)
		}
		w := httptest.NewRecorder()
		tc.ServeSource(w, r)
		if w.Code != test.want {
			t.Fatal(test, w.Code)
		}
	}
	r := httptest.NewRequest("GET", endpoint, nil)
	r.AddCookie(cookie)
	r.Header.Set("Origin", "https://evil.test")
	w := httptest.NewRecorder()
	tc.ServeSource(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin source accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = httptest.NewRequest("GET", endpoint, nil).WithContext(ctx)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	tc.ServeSource(w, r)
	if w.Code == 200 {
		t.Fatal("cancelled source served")
	}
	if err := tc.app.store.update(func(st *State) error { st.Storages[0].Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", endpoint, nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	tc.ServeSource(w, r)
	if w.Code != 404 {
		t.Fatal("disabled source served")
	}
}
