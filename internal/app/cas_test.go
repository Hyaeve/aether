package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type casTransport func(*http.Request) (*http.Response, error)

func (f casTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCASDecode(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, content, want string
		valid               bool
	}{
		{"Movie.mkv.cas", `{"name":"original.mkv","size":123,"sha256":"` + hash + `"}`, "Movie.mkv", true},
		{"Movie.CAS", `{"name":"original.MP4","size":"123","sha256":"` + hash + `"}`, "Movie.MP4", true},
		{"Movie.cas", `{"name":"../bad.mkv","size":123,"sha256":"` + hash + `"}`, "", false},
		{"Movie.cas", `{"name":"movie.mkv","size":-1,"sha256":"` + hash + `"}`, "", false},
		{"Movie.cas", `{"name":"movie.mkv","size":1,"sha1":"abcd"}`, "", false},
		{"Movie.cas", `{"name":"movie.mkv","size":1,"sha256":"bad"}`, "", false},
		{"Movie.cas", `{"name":"movie.txt","size":1,"sha256":"` + hash + `"}`, "", false},
		{"../Movie.cas", `{"name":"movie.mkv","size":1,"sha256":"` + hash + `"}`, "", false},
	} {
		info, err := decodeCAS([]byte(base64.StdEncoding.EncodeToString([]byte(tc.content))), tc.name)
		if (err == nil) != tc.valid || (tc.valid && info.Name != tc.want) {
			t.Fatalf("%s: %#v %v", tc.name, info, err)
		}
	}
	if _, err := decodeCAS([]byte("not base64!"), "Movie.cas"); err == nil {
		t.Fatal("malformed content accepted")
	}
	if _, err := decodeCAS(make([]byte, casLimit+1), "Movie.cas"); err == nil {
		t.Fatal("oversized content accepted")
	}
}

func TestCASLifecycle(t *testing.T) {
	a := testApp(t)
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("authorization leaked to media server")
		}
		http.ServeContent(w, r, "Movie.mkv", time.Time{}, strings.NewReader("media-content"))
	}))
	defer media.Close()
	s := Storage{ID: "mobile-test", Name: "Mobile", Type: "mobile", Enabled: true, Config: map[string]string{
		"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:13900000000:token")), "root": "/", "deleteMode": "trash",
	}}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	original := apiClient
	defer func() { apiClient = original }()
	var mu sync.Mutex
	restores, removed := 0, []string{}
	hash := strings.Repeat("a", 64)
	cas := base64.StdEncoding.EncodeToString([]byte(`{"provider":"139","name":"Movie.mkv","size":12345,"sha256":"` + hash + `"}`))
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		var payload map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&payload)
		}
		result := any(map[string]any{})
		raw, status := "", 200
		switch r.URL.Path {
		case "/user/route/qryRoutePolicy":
			result = map[string]any{"routePolicyList": []any{map[string]string{"modName": "personal", "httpsUrl": "https://personal.yun.139.com"}}}
		case "/file/list":
			result = map[string]any{"items": []any{
				map[string]any{"fileId": "cas-file", "name": "Movie.mkv.cas", "size": len(cas), "type": "file"},
				map[string]any{"fileId": "normal-file", "name": "Ignored.mp4", "size": 50, "type": "file"},
			}}
		case "/file/create":
			if payload["type"] == "folder" {
				if payload["name"] != "Aether" || payload["parentFileId"] != "/" {
					t.Error("restore folder must be /Aether")
				}
				result = map[string]any{"fileId": "temp-folder"}
			} else {
				restores++
				if payload["contentHash"] != hash || payload["parentFileId"] != "temp-folder" {
					t.Error("incorrect restore payload")
				}
				if !strings.Contains(r.Header.Get("X-DeviceInfo"), "|PC|") {
					t.Error("missing PC headers")
				}
				result = map[string]any{"fileId": "restored-file", "rapidUpload": true}
			}
		case "/file/getDownloadUrl":
			if payload["fileId"] == "cas-file" {
				result = map[string]string{"url": "https://cdn.example/cas"}
			} else {
				result = map[string]string{"cdnUrl": media.URL}
			}
		case "/cas":
			raw = cas
		case "/recyclebin/batchTrash":
			for _, v := range payload["fileIds"].([]any) {
				removed = append(removed, v.(string))
			}
		default:
			t.Errorf("unexpected request %s", r.URL)
			status = 500
		}
		if raw == "" {
			body, _ := json.Marshal(map[string]any{"success": true, "data": result})
			raw = string(body)
			if r.Header.Get("Authorization") == "" || r.Header.Get("Mcloud-Sign") == "" {
				t.Error("missing signed authorization")
			}
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(raw)), Header: http.Header{}, Request: r}, nil
	})}
	task := Task{Name: "CAS scan", Kind: "cas", StorageID: s.ID, Source: "/", Target: "cas", Mode: "incremental", APIInterval: 200, Cron: "0 2 * * *", Enabled: true}
	if err := a.validateTask(&task); err != nil {
		t.Fatal(err)
	}
	if task.RetentionHours != 12 {
		t.Fatal("CAS default retention must be 12h")
	}
	task.RetentionHours = 24
	if nextRun(task, time.Now()).IsZero() {
		t.Fatal("CAS cron did not schedule")
	}
	count, err := a.executeTask(context.Background(), task, s)
	if err != nil || count != 1 {
		t.Fatalf("scan: %d %v", count, err)
	}
	content, err := os.ReadFile(filepath.Join(a.outputDir, "cas", "Movie.mkv.strm"))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(strings.TrimSpace(string(content)))
	token := strings.TrimPrefix(u.Path, "/stream/")
	raw, _ := base64.RawURLEncoding.DecodeString(token)
	var claim streamClaim
	if json.Unmarshal(raw, &claim) != nil || claim.CAS == nil || claim.CAS.SHA256 != hash || claim.RetentionHours != 24 {
		t.Fatal("CAS claim missing")
	}
	if count, err := a.executeTask(context.Background(), task, s); err != nil || count != 0 {
		t.Fatalf("incremental: %d %v", count, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.executeTask(ctx, task, s); err == nil {
		t.Fatal("cancelled scan succeeded")
	}
	d, release, err := a.casDownload(context.Background(), s, claim)
	if err != nil || d.URL != media.URL {
		t.Fatalf("restore: %#v %v", d, err)
	}
	if a.store.snapshot().CASTemporary[0].RetentionHours != 24 {
		t.Fatal("retention not persisted")
	}
	_, release2, err := a.casDownload(context.Background(), s, claim)
	if err != nil || restores != 1 {
		t.Fatalf("duplicate restore: %d %v", restores, err)
	}
	age := func() {
		t.Helper()
		if err := a.store.update(func(st *State) error {
			for i := range st.CASTemporary {
				st.CASTemporary[i].LastUsed = time.Now().Add(-25 * time.Hour)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	age()
	if count, err := a.cleanupCAS(context.Background()); err != nil || count != 0 {
		t.Fatal("active file was cleaned", count, err)
	}
	release()
	release2()
	_ = a.store.update(func(st *State) error {
		st.CASTemporary[0].LastUsed = time.Now().Add(-13 * time.Hour)
		return nil
	})
	if n, err := a.cleanupCAS(context.Background()); err != nil || n != 0 {
		t.Fatal("custom retention ignored", n, err)
	}
	playback := httptest.NewRequest("GET", u.RequestURI(), nil)
	playback.Header.Set("Range", "bytes=0-4")
	response := httptest.NewRecorder()
	a.Handler(t.TempDir()).ServeHTTP(response, playback)
	if response.Code != 206 || response.Body.String() != "media" {
		t.Fatalf("CAS range playback failed: %d %s", response.Code, response.Body.String())
	}
	head := httptest.NewRequest("HEAD", u.RequestURI(), nil)
	response = httptest.NewRecorder()
	a.Handler(t.TempDir()).ServeHTTP(response, head)
	if response.Code != 200 || response.Body.Len() != 0 {
		t.Fatal("CAS HEAD playback failed", response.Code)
	}
	reopened, err := newWithDirectories(context.Background(), a.store.dir, a.dataDir, a.outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.store.snapshot().CASTemporary) != 1 {
		t.Fatal("temporary journal not recovered")
	}
	if _, done, err := reopened.casDownload(context.Background(), s, claim); err != nil {
		t.Fatal(err)
	} else {
		done()
	}
	if restores != 1 {
		t.Fatal("restart created duplicate media")
	}
	a = reopened
	age()
	count, err = a.cleanupCAS(context.Background())
	if err != nil || count != 1 || len(removed) != 1 || removed[0] != "restored-file" {
		t.Fatalf("cleanup %d %v %v", count, err, removed)
	}
	if len(a.store.snapshot().CASTemporary) != 0 {
		t.Fatal("cleanup journal retained deleted item")
	}
	h := a.Handler(t.TempDir())
	req := httptest.NewRequest("GET", u.RequestURI()+"broken", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("tampered playback accepted", w.Code)
	}
}

func TestCASStorageValidation(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	task := Task{Name: "invalid CAS", Kind: "cas", StorageID: s.ID, Mode: "incremental"}
	if err := a.validateTask(&task); err == nil {
		t.Fatal("local CAS accepted")
	}
	for _, address := range []string{"http://personal.yun.139.com", "https://yun.139.com.evil.example", "https://user:pass@personal.yun.139.com"} {
		if validMobileHost(address) {
			t.Fatal("unsafe mobile host accepted", address)
		}
	}
	parts := casParts(101 << 20)
	if len(parts) != 2 || parts[1]["partSize"] != int64(1<<20) {
		t.Fatalf("bad part descriptors %#v", parts)
	}
}

func TestCASRetentionDefaultsAndValidation(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "189", Name: "test", Type: "tianyi", Enabled: true, Config: map[string]string{"username": "test", "password": "test"}}
	_ = a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil })
	for _, hours := range []int{-1, 8761} {
		task := Task{Name: "CAS", Kind: "cas", StorageID: s.ID, Mode: "full", RetentionHours: hours}
		if a.validateTask(&task) == nil {
			t.Fatal("invalid retention accepted", hours)
		}
	}
	now := time.Now()
	for _, hours := range []int{0, 1, 12, 24, 8760} {
		entry := CASTemporary{LastUsed: now, RetentionHours: hours}
		want := hours
		if want == 0 {
			want = 12
		}
		if entry.expiresAt().Sub(now) != time.Duration(want)*time.Hour {
			t.Fatal("incorrect expiry", hours)
		}
	}
}

func TestCASFailedRestoreNotReusable(t *testing.T) {
	a := testApp(t)
	info := CASInfo{Name: "movie.mkv", Size: 123, SHA256: strings.Repeat("a", 64)}
	if err := a.store.update(func(st *State) error {
		st.CASTemporary = []CASTemporary{{Key: "failed", StorageID: "s", FileID: "f", Ready: false, LastUsed: time.Now()}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := validateCAS(info); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.acquireCAS(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.acquireCAS(ctx); err == nil {
		t.Fatal("CAS gate ignored cancellation")
	}
	<-a.casGate
	reopened, err := NewStore(a.store.dir)
	if err != nil || reopened.snapshot().CASTemporary[0].Ready {
		t.Fatal("failed restore was marked reusable after restart")
	}
}
