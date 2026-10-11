package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

func TestTransferSettingsModulePersistenceMigrationAndProtection(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	for _, route := range []string{"/api/transfer/settings", "/api/backup/settings"} {
		for _, method := range []string{"GET", "PUT"} {
			if w := request(t, h, method, route, map[string]any{}, nil); w.Code != 401 {
				t.Fatal(route, method, w.Code)
			}
		}
	}
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	s := a.transferConfig()
	s.UploadDelay = 0
	s.UploadSpeed = 17
	s.IgnoredExtensions = []string{".TMP", "tmp", " .FLAC "}
	if w := request(t, h, "PUT", "/api/transfer/settings", s, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	b := a.backupResourceConfig()
	b.QueueUpper = 2
	b.MaxConcurrentScan = 2
	if w := request(t, h, "PUT", "/api/backup/settings", b, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	before, _ := os.ReadFile(filepath.Join(a.store.dir, "transfer", "settings.json"))
	if !bytes.Contains(before, []byte("uploadSpeed")) {
		t.Fatal("missing module")
	}
	if err := a.store.update(func(st *State) error { st.Tasks = append(st.Tasks, Task{ID: "run", Status: "running"}); return nil }); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(a.store.dir, "transfer", "settings.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("runtime rewrote settings")
	}
	store, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := normalizedTransferSettings(store.state.TransferSettings); got.UploadDelay != 0 || got.UploadSpeed != 17 || !reflect.DeepEqual(got.IgnoredExtensions, []string{"tmp", "flac"}) {
		t.Fatal(got)
	}
	if normalizedBackupResources(store.state.BackupResources).QueueUpper != 2 {
		t.Fatal("resource settings lost")
	}
	if err := os.Remove(filepath.Join(a.store.dir, "transfer", "resources.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(a.store.dir); err == nil {
		t.Fatal("missing initialized module silently defaulted")
	}
}

func TestTransferSettingsLimitsValidation(t *testing.T) {
	s := normalizedTransferSettings(TransferSettings{})
	if s.MaxPreprocess != 5 || s.MaxUploads != 5 || s.UploadDelay != 10 || !s.CrossCloudTemp || len(s.IgnoredExtensions) != 8 {
		t.Fatal(s)
	}
	for _, change := range []func(*TransferSettings){func(s *TransferSettings) { s.UploadSpeed = -1 }, func(s *TransferSettings) { s.MaxUploads = 0 }, func(s *TransferSettings) { s.UploadUnit = "bogus" }, func(s *TransferSettings) { s.IgnoredExtensions = []string{"../txt"} }} {
		bad := s
		change(&bad)
		if validateTransferSettings(&bad) == nil {
			t.Fatal("accepted invalid setting", bad)
		}
	}
	for _, bad := range []BackupResources{{Version: 1, QueueUpper: 2, QueueLower: 2, MaxConcurrentScan: 1}, {Version: 1, QueueUpper: 0, QueueLower: 1, MaxConcurrentScan: 1}, {Version: 1, QueueUpper: 2, MaxConcurrentScan: 0}} {
		if validateBackupResources(bad) == nil {
			t.Fatal(bad)
		}
	}
}

func TestTransferResourceConcurrencyCancellationAndLifetime(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		s := a.transferConfigWithoutLock()
		s.MaxUploads = 1
		st.TransferSettings = s
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p := a.beginTransfer(context.Background(), "upload", Storage{}, "first", 1)
	if err := p.wait(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	q := a.beginTransfer(ctx, "upload", Storage{}, "second", 1)
	done := make(chan error, 1)
	go func() { done <- q.wait() }()
	select {
	case err := <-done:
		t.Fatal("queued upload bypassed", err)
	case <-time.After(60 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel hung")
	}
	q.finish(context.Canceled)
	p.finish(nil)
	a.resources.mu.Lock()
	active := a.resources.active["upload"]
	a.resources.mu.Unlock()
	if active != 0 {
		t.Fatal("leaked", active)
	}
	r := a.beginTransfer(context.Background(), "upload", Storage{}, "third", 1)
	if err := r.wait(); err != nil {
		t.Fatal(err)
	}
	r.finish(nil)
}

func (a *App) transferConfigWithoutLock() TransferSettings {
	return normalizedTransferSettings(a.store.state.TransferSettings)
}

func TestTransferResourceSharedRateAndCancel(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		s := normalizedTransferSettings(st.TransferSettings)
		s.DownloadSpeed = 64
		st.TransferSettings = s
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := a.limitTransferBytes(context.Background(), "download", 32*1024); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- a.limitTransferBytes(context.Background(), "download", 32*1024) }()
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if time.Since(start) < 850*time.Millisecond {
		t.Fatal("aggregate rate bypassed", time.Since(start))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.limitTransferBytes(ctx, "download", 32*1024); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestBackupResourcesPausedSyncNoMutationAndRescan(t *testing.T) {
	a, r, left, right := syncFixture(t)
	syncWrite(t, left, "Folder/a.txt", "left")
	syncWrite(t, right, "b.txt", "right")
	r.SyncMarker = true
	if err := a.store.update(func(st *State) error {
		b := normalizedBackupResources(st.BackupResources)
		b.PauseBidirectional = true
		st.BackupResources = b
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err := a.executeBackup(context.Background(), &r)
	if !errors.Is(err, errBackupPaused) || r.Phase != "paused" || r.Total != 2 {
		t.Fatal(err, r)
	}
	for _, p := range []string{filepath.Join(right, "Folder"), filepath.Join(left, "b.txt"), filepath.Join(left, ".aether-sync-owner"), a.syncStatePath(r.ID)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatal("pause mutated", p, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(a.dataDir, "runtime", "backup-plans"))
	if err != nil || len(entries) != 1 {
		t.Fatal("plan missing", err)
	}
	syncWrite(t, left, "Folder/a.txt", "new source")
	if err := a.store.update(func(st *State) error { st.BackupResources.PauseBidirectional = false; return nil }); err != nil {
		t.Fatal(err)
	}
	syncRun(t, a, &r)
	if syncRead(t, right, "Folder/a.txt") != "new source" {
		t.Fatal("stale paused plan executed")
	}
}

func TestTransferIgnoredBackupAndUploadGuard(t *testing.T) {
	a, r, left, right := backupFixture(t)
	syncWrite(t, left, "a.TMP", "skip")
	syncWrite(t, left, "b.txt", "copy")
	if err := a.executeBackup(context.Background(), &r); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(right, "a.TMP")); !os.IsNotExist(err) {
		t.Fatal("ignored extension copied")
	}
	if syncRead(t, right, "b.txt") != "copy" {
		t.Fatal("allowed omitted")
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	if w := request(t, h, http.MethodPost, "/api/files/upload?name=file.tmp&storage=target", nil, cookie); w.Code != 400 || !bytes.Contains(w.Body.Bytes(), []byte("忽略")) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestTransferResourceReceiveBodyRateAndSlots(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	if err := a.store.update(func(st *State) error {
		cfg := normalizedTransferSettings(st.TransferSettings)
		cfg.MaxUploads = 1
		cfg.UploadSpeed = 128
		cfg.UploadDelay = 0
		st.TransferSettings = cfg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	data := bytes.Repeat([]byte("x"), 96*1024)
	req := httptest.NewRequest("POST", "/api/files/upload?storage="+s.ID+"&parent=/&name=test.bin", bytes.NewReader(data))
	req.AddCookie(cookie)
	start := time.Now()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || time.Since(start) < 400*time.Millisecond {
		t.Fatal(w.Code, w.Body.String(), time.Since(start))
	}
	got, err := os.ReadFile(filepath.Join(s.Config["root"], "test.bin"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("body publication", err)
	}
	a.resources.mu.Lock()
	active := a.resources.active["upload"] + a.resources.active["preprocess"]
	a.resources.mu.Unlock()
	if active != 0 {
		t.Fatal("receive lease leak", active)
	}
	release, err := a.acquireTransferResource(context.Background(), "upload")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	bad := httptest.NewRequest("POST", "/api/files/upload?storage="+s.ID+"&name=csrf.txt", strings.NewReader("bad"))
	bad.AddCookie(cookie)
	bad.Header.Set("Origin", "https://evil.invalid")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); h.ServeHTTP(w, bad); done <- w }()
	select {
	case w := <-done:
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("CSRF waited on resource slot")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = httptest.NewRequest("POST", "/api/files/upload?storage="+s.ID+"&name=cancel.txt", strings.NewReader("bad")).WithContext(ctx)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 408 {
		t.Fatal(w.Code)
	}
	if _, err := os.Stat(filepath.Join(s.Config["root"], "cancel.txt")); !os.IsNotExist(err) {
		t.Fatal("queued cancel published")
	}
}

func TestTransferResourceLocalDelayAgeAndCancellation(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		cfg := normalizedTransferSettings(st.TransferSettings)
		cfg.UploadDelay = 1
		st.TransferSettings = cfg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "pending-test")
	if err := os.WriteFile(file, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.waitLocalUpload(ctx, file); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := a.waitLocalUpload(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 300*time.Millisecond {
		t.Fatal("old file delayed")
	}
	now := time.Now()
	if err := os.Chtimes(file, now, now); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	if err := a.waitLocalUpload(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 850*time.Millisecond {
		t.Fatal("new file not delayed")
	}
}

func TestTransferResourceBackupTempReuseToggle(t *testing.T) {
	for _, reuse := range []bool{true, false} {
		t.Run(fmtBool(reuse), func(t *testing.T) {
			a, _, _, _ := backupFixture(t)
			source := Storage{ID: "source", Type: "webdav", Enabled: true, Config: map[string]string{"address": "https://backup.invalid"}}
			second := Storage{ID: "second", Type: "local", Enabled: true, Config: map[string]string{"root": t.TempDir()}}
			if err := a.store.update(func(st *State) error {
				st.Storages[0] = source
				st.Storages = append(st.Storages, second)
				cfg := normalizedTransferSettings(st.TransferSettings)
				cfg.CrossCloudTemp = reuse
				st.TransferSettings = cfg
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			first := a.store.snapshot().Storages[1]
			old := apiClient
			defer func() { apiClient = old }()
			var gets atomic.Int32
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					t.Fatal(r.Method)
				}
				gets.Add(1)
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("content")), Request: r}, nil
			})}
			cache := &backupStageResource{}
			defer cache.clear()
			ctx := context.WithValue(context.Background(), backupStageResourceKey{}, cache)
			for _, target := range []Storage{first, second} {
				var digest string
				if err := a.copyBackupFile(ctx, source, target, File{ID: "/a.txt", Name: "a.txt", Size: 7}, "/", "a.txt", "skip", &digest); err != nil {
					t.Fatal(err)
				}
				if syncRead(t, target.Config["root"], "a.txt") != "content" || digest == "" {
					t.Fatal("bad copy")
				}
			}
			want := int32(2)
			if reuse {
				want = 1
			}
			if gets.Load() != want {
				t.Fatal("reuse ignored", gets.Load(), want)
			}
		})
	}
}

func fmtBool(b bool) string {
	if b {
		return "reuse"
	}
	return "reread"
}

func TestTransferResourceActualCloudUploadRateDelayAndCancelledQueue(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		cfg := normalizedTransferSettings(st.TransferSettings)
		cfg.UploadDelay = 1
		cfg.UploadSpeed = 128
		cfg.MaxUploads = 1
		cfg.MaxPreprocess = 1
		st.TransferSettings = cfg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	remote := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()})
	defer remote.Close()
	s := Storage{Type: "webdav", Config: map[string]string{"address": remote.URL}}
	file := filepath.Join(t.TempDir(), "pending-resource")
	data := bytes.Repeat([]byte("x"), 96*1024)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := a.uploadCloud(context.Background(), s, "/", "result.bin", file); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 1300*time.Millisecond {
		t.Fatal("cloud upload bypassed delay/rate", time.Since(start))
	}
	got, err := os.ReadFile(filepath.Join(root, "result.bin"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal(err)
	}
	a.resources.mu.Lock()
	active := a.resources.active["upload"] + a.resources.active["preprocess"]
	a.resources.mu.Unlock()
	if active != 0 {
		t.Fatal("cloud lease leak", active)
	}
	release, err := a.acquireTransferResource(context.Background(), "upload")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.uploadCloud(ctx, s, "/", "cancel.bin", file) }()
	time.Sleep(40 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cloud queue cancellation hung")
	}
	if _, err := os.Stat(filepath.Join(root, "cancel.bin")); !os.IsNotExist(err) {
		t.Fatal("cancelled cloud upload published")
	}
}

func TestTransferResourceActualCloudWriteSingleSlot(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	remote := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()})
	defer remote.Close()
	s := Storage{ID: "dav", Type: "webdav", Enabled: true, Config: map[string]string{"address": remote.URL, "root": "/"}}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{s}
		cfg := normalizedTransferSettings(st.TransferSettings)
		cfg.UploadDelay = 0
		cfg.MaxUploads = 1
		cfg.MaxPreprocess = 1
		st.TransferSettings = cfg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	fs := mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: "/"}}
	f, err := fs.OpenFile(ctx, "/one.txt", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	tracked := a.trackedFile(ctx, s, "one.txt", f, os.O_WRONLY)
	if _, err := tracked.Write([]byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := tracked.Close(); err != nil {
		t.Fatal("single-slot publication deadlock", err)
	}
	if syncRead(t, root, "one.txt") != "one" {
		t.Fatal("write missing")
	}
}

func TestTransferResourceConsistentLeaseOrdering(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	if err := a.store.update(func(st *State) error {
		cfg := normalizedTransferSettings(st.TransferSettings)
		cfg.MaxUploads = 1
		cfg.MaxPreprocess = 1
		st.TransferSettings = cfg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	release, err := a.acquireTransferResource(context.Background(), "upload")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req := httptest.NewRequest("POST", "/api/files/upload?storage="+s.ID+"&name=order.txt", strings.NewReader("order")).WithContext(ctx)
	req.AddCookie(cookie)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); h.ServeHTTP(w, req); done <- w }()
	time.Sleep(60 * time.Millisecond)
	prep, err := a.acquireTransferResource(ctx, "preprocess")
	if err != nil {
		t.Fatal("queued receiver held preprocess while waiting upload", err)
	}
	prep()
	release()
	select {
	case w := <-done:
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	case <-ctx.Done():
		t.Fatal("resource-order deadlock")
	}
}

func TestTransferResourceReceiverAndCloudUploadCannotDeadlock(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	remoteRoot := t.TempDir()
	remote := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(remoteRoot), LockSystem: webdav.NewMemLS()})
	defer remote.Close()
	cloud := Storage{ID: "dav", Type: "webdav", Enabled: true, Config: map[string]string{"address": remote.URL, "root": "/"}}
	if err := a.store.update(func(st *State) error {
		cfg := normalizedTransferSettings(st.TransferSettings)
		cfg.MaxUploads, cfg.MaxPreprocess, cfg.UploadDelay = 1, 1, 0
		st.TransferSettings = cfg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cloudCtx, uploadRelease, err := a.controlCloudUpload(ctx, "cloud.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	defer uploadRelease()
	preRelease, err := a.acquireTransferResource(ctx, "preprocess")
	if err != nil {
		t.Fatal(err)
	}
	defer preRelease()
	req := httptest.NewRequest("POST", "/api/files/upload?storage="+s.ID+"&name=receiver.txt", strings.NewReader("receiver")).WithContext(ctx)
	req.AddCookie(cookie)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); h.ServeHTTP(w, req); done <- w }()
	// Both resources begin occupied. A waiting receiver must not retain the
	// newly available preprocess slot needed by an existing cloud uploader.
	time.Sleep(60 * time.Millisecond)
	preRelease()
	probe, err := a.acquireTransferResource(ctx, "preprocess")
	if err != nil {
		t.Fatal("receiver retained preprocess while waiting upload", err)
	}
	probe()
	local := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(local, []byte("cloud"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.uploadCloud(cloudCtx, cloud, "/", "cloud.txt", local); err != nil {
		t.Fatal("cloud upload and receiver deadlocked", err)
	}
	if syncRead(t, remoteRoot, "cloud.txt") != "cloud" {
		t.Fatal("cloud upload missing")
	}
	uploadRelease()
	select {
	case w := <-done:
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	case <-ctx.Done():
		t.Fatal("receiver did not finish after cloud upload released its slot")
	}
	a.resources.mu.Lock()
	activeUpload, activePrep := a.resources.active["upload"], a.resources.active["preprocess"]
	a.resources.mu.Unlock()
	if activeUpload != 0 || activePrep != 0 {
		t.Fatal("resource leases leaked", activeUpload, activePrep)
	}
}

func TestTransferSettingsRejectInitializedZeroVersions(t *testing.T) {
	for _, kind := range []string{"transfer", "backup"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			st := a.store.snapshot()
			st.TransferVersion = 1
			st.TransferSettings = normalizedTransferSettings(st.TransferSettings)
			st.BackupResources = normalizedBackupResources(st.BackupResources)
			module := "transfer/settings"
			st.TransferSettings.Version = 0
			var value any = st.TransferSettings
			if kind == "backup" {
				st.TransferSettings.Version = 1
				st.BackupResources.Version = 0
				module, value = "transfer/resources", st.BackupResources
			}
			if err := validateResourceState(&st); err == nil {
				t.Fatal("initialized resources treated as legacy defaults")
			}
			raw, err := a.store.encodeJSONConfig(module, value)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(a.store.dir, filepath.FromSlash(module)+".json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = NewStore(a.store.dir); err == nil {
				t.Fatal("zero-version resource module accepted")
			}
		})
	}
}

func TestTransferSettingsRejectInvalidPersistedAndImportedResources(t *testing.T) {
	for _, kind := range []string{"transfer", "backup"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			st := a.store.snapshot()
			st.Username, st.Password = "owner", "hashed"
			if kind == "transfer" {
				cfg := normalizedTransferSettings(st.TransferSettings)
				cfg.MaxUploads = 0
				st.TransferSettings = cfg
			} else {
				cfg := normalizedBackupResources(st.BackupResources)
				cfg.QueueUpper = -1
				st.BackupResources = cfg
			}
			if err := validateBundle(configBundle{State: st, Files: map[string]json.RawMessage{}}); err == nil {
				t.Fatal("invalid imported resources accepted")
			}
			module := "transfer/settings"
			var value any = st.TransferSettings
			if kind == "backup" {
				module = "transfer/resources"
				value = st.BackupResources
			}
			raw, err := a.store.encodeJSONConfig(module, value)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(a.store.dir, filepath.FromSlash(module)+".json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = NewStore(a.store.dir); err == nil {
				t.Fatal("invalid startup resource settings accepted")
			}
		})
	}
}

func TestTransferResourcePauseWhileQueuedRefusesPublication(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		cfg := normalizedTransferSettings(st.TransferSettings)
		cfg.MaxUploads = 1
		st.TransferSettings = cfg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	release, err := a.acquireTransferResource(context.Background(), "upload")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx := context.WithValue(context.Background(), backupSyncResourceKey{}, true)
	done := make(chan error, 1)
	go func() {
		_, free, err := a.controlCloudUpload(ctx, "test.txt", "")
		if free != nil {
			free()
		}
		done <- err
	}()
	time.Sleep(60 * time.Millisecond)
	if err := a.store.update(func(st *State) error {
		cfg := normalizedBackupResources(st.BackupResources)
		cfg.PauseBidirectional = true
		st.BackupResources = cfg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err := <-done:
		if !errors.Is(err, errBackupPaused) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued pause hung")
	}
}

func TestBackupResourcesFrozenIgnoredExtensionsRefuseChangedScan(t *testing.T) {
	a, r, left, right := syncFixture(t)
	syncWrite(t, left, "a.txt", "source")
	syncWrite(t, right, "b.txt", "target")
	changed := false
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" && !changed {
			changed = true
			err := a.store.update(func(st *State) error {
				cfg := normalizedTransferSettings(st.TransferSettings)
				cfg.IgnoredExtensions = append(cfg.IgnoredExtensions, "txt")
				st.TransferSettings = cfg
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}
		(&webdav.Handler{FileSystem: webdav.Dir(left), LockSystem: webdav.NewMemLS()}).ServeHTTP(w, req)
	}))
	defer remote.Close()
	if err := a.store.update(func(st *State) error {
		st.Storages[0].Type = "webdav"
		st.Storages[0].Config = map[string]string{"address": remote.URL, "root": "/"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err := a.executeBackup(context.Background(), &r)
	if err == nil || !strings.Contains(err.Error(), "过滤配置已变更") {
		t.Fatal("changed ignore rules advanced sync", err)
	}
	if _, err := os.Stat(a.syncStatePath(r.ID)); !os.IsNotExist(err) {
		t.Fatal("changed scan saved baseline")
	}
	if _, err := os.Stat(filepath.Join(right, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("changed scan mutated target")
	}
	if syncRead(t, left, "a.txt") != "source" || syncRead(t, right, "b.txt") != "target" {
		t.Fatal("source/target changed")
	}
}

func TestTransferResourceDeleteCancelsBlockedDAVIO(t *testing.T) {
	a := testApp(t)
	entered := make(chan struct{})
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer remote.Close()
	p := a.beginTransfer(context.Background(), "download", Storage{}, "blocked.txt", 1)
	f := &davFile{ctx: p.ctx, info: davInfo{File{Name: "blocked.txt", Size: 1}}, progress: p, download: Download{URL: remote.URL}}
	done := make(chan error, 1)
	go func() { _, err := f.Read(make([]byte, 1)); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("network read did not start")
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/transfers/action", strings.NewReader(`{"id":"`+p.id+`","action":"delete"}`))
	a.transferAction(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("network did not cancel", err)
		}
	case <-time.After(time.Second):
		t.Fatal("deleted DAV request still blocked")
	}
	f.Close()
}

func TestTransferResourceDeleteCancelsStreamBeforeUpstreamHeaders(t *testing.T) {
	a := testApp(t)
	entered, upstreamCanceled, unblock := make(chan struct{}), make(chan struct{}), make(chan struct{})
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/blocked.txt" {
			t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		close(entered)
		// Do not write headers or a body: cancellation must reach the original
		// outbound request, not only a response-body wrapper.
		select {
		case <-r.Context().Done():
			close(upstreamCanceled)
		case <-unblock:
		}
	}))
	defer remote.Close()
	defer close(unblock)
	s := Storage{ID: "blocked-dav", Name: "Blocked DAV", Type: "webdav", Enabled: true, Config: map[string]string{"address": remote.URL, "root": "/", "downloadMode": "proxy"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, a.streamURL(s.ID, "/blocked.txt", ""), nil).WithContext(ctx)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); h.ServeHTTP(w, req); done <- w }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("stream reverse proxy never requested the upstream file")
	}
	listed := request(t, h, http.MethodGet, "/api/transfers", nil, cookie)
	var entries []transferEntry
	if listed.Code != 200 || json.Unmarshal(listed.Body.Bytes(), &entries) != nil || len(entries) != 1 {
		t.Fatal("stream transfer not listed", listed.Code, listed.Body.String())
	}
	entry := entries[0]
	if entry.Kind != "download" || entry.Status != "running" || entry.Done != 0 || entry.Name != "/blocked.txt" {
		t.Fatal("unexpected pending stream transfer", entry)
	}
	deleted := request(t, h, http.MethodPost, "/api/transfers/action", map[string]string{"id": entry.ID, "action": "delete"}, cookie)
	if deleted.Code != 200 {
		t.Fatal(deleted.Code, deleted.Body.String())
	}
	select {
	case <-upstreamCanceled:
	case <-time.After(time.Second):
		t.Fatal("deleted stream still waits for upstream headers")
	}
	select {
	case w := <-done:
		if w.Code != http.StatusBadGateway {
			t.Fatal("canceled stream response", w.Code, w.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("stream handler did not return after deleting its transfer")
	}
	if err := ctx.Err(); err != nil {
		t.Fatal("test deadline, rather than transfer deletion, canceled the request", err)
	}
	listed = request(t, h, http.MethodGet, "/api/transfers", nil, cookie)
	if listed.Code != 200 || json.Unmarshal(listed.Body.Bytes(), &entries) != nil || len(entries) != 0 {
		t.Fatal("deleted stream transfer reappeared", listed.Code, listed.Body.String())
	}
	if a.downloaded.Load() != 0 {
		t.Fatal("stream counted bytes before receiving headers")
	}
}

func TestTransferResourcePublicationCompensatesHiddenOldFile(t *testing.T) {
	for _, mode := range []string{"pause", "cancel", "filter", "storage", "disabled", "occupied", "tampered", "restore-failure"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			root := t.TempDir()
			syncWrite(t, root, "old.txt", "old-content")
			local := filepath.Join(t.TempDir(), "source.txt")
			if err := os.WriteFile(local, []byte("new-content"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var hidden, restored atomic.Int32
			dav := &webdav.Handler{FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()}
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method == "MOVE" && strings.HasPrefix(req.URL.Path, "/.aether-previous-") {
					restored.Add(1)
					if mode == "restore-failure" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					if req.Header.Get("Overwrite") != "F" {
						t.Error("compensation did not retain no-overwrite protection")
					}
				}
				if req.Method != "MOVE" || req.URL.Path != "/old.txt" {
					dav.ServeHTTP(w, req)
					return
				}
				capture := httptest.NewRecorder()
				dav.ServeHTTP(capture, req)
				if capture.Code < 200 || capture.Code >= 300 {
					t.Error("fixture old rename failed", capture.Code, capture.Body.String())
				} else {
					hidden.Add(1)
					switch mode {
					case "cancel":
						cancel()
					case "filter":
						if err := a.store.update(func(st *State) error {
							cfg := normalizedTransferSettings(st.TransferSettings)
							cfg.IgnoredExtensions = append(cfg.IgnoredExtensions, "txt")
							st.TransferSettings = cfg
							return nil
						}); err != nil {
							t.Error(err)
						}
					default:
						if err := a.store.update(func(st *State) error {
							cfg := normalizedBackupResources(st.BackupResources)
							cfg.PauseBidirectional = true
							st.BackupResources = cfg
							if mode == "storage" {
								st.Storages[0].Config["username"] = "different-account"
							}
							if mode == "disabled" {
								st.Storages[0].Enabled = false
							}
							return nil
						}); err != nil {
							t.Error(err)
						}
						if mode == "occupied" {
							if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("unrelated-file"), 0600); err != nil {
								t.Error(err)
							}
						}
						if mode == "tampered" {
							files, err := os.ReadDir(root)
							if err != nil {
								t.Error(err)
							}
							for _, file := range files {
								if strings.HasPrefix(file.Name(), ".aether-previous-") {
									if err := os.WriteFile(filepath.Join(root, file.Name()), []byte("bad-content"), 0600); err != nil {
										t.Error(err)
									}
								}
							}
						}
					}
				}
				for key, values := range capture.Header() {
					w.Header()[key] = values
				}
				w.WriteHeader(capture.Code)
				_, _ = w.Write(capture.Body.Bytes())
			}))
			defer remote.Close()
			s := Storage{ID: "dav-compensation", Name: "Compensation DAV", Type: "webdav", Enabled: true, Config: map[string]string{"address": remote.URL, "root": "/"}}
			if err := a.store.update(func(st *State) error {
				st.Storages = []Storage{s}
				st.Settings.WebDAVCache = true
				st.Settings.CacheEnabled = true
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s, loadErr := a.store.storage(s.ID)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			ctx = context.WithValue(ctx, backupSyncResourceKey{}, true)
			ctx = context.WithValue(ctx, backupTransferSnapshotKey{}, a.transferConfig())
			err := a.publishCloudFile(ctx, s, "/", "old.txt", local)
			if err == nil || hidden.Load() != 1 {
				t.Fatal("did not stop after hiding old file", err, hidden.Load())
			}
			files, readErr := os.ReadDir(root)
			if readErr != nil {
				t.Fatal(readErr)
			}
			backups, uploads := 0, 0
			for _, file := range files {
				if strings.HasPrefix(file.Name(), ".aether-previous-") {
					backups++
				}
				if strings.HasPrefix(file.Name(), ".aether-upload-") {
					uploads++
				}
			}
			if uploads != 1 {
				t.Fatal("failed new upload was not retained", uploads)
			}
			if mode == "pause" || mode == "cancel" || mode == "filter" {
				if syncRead(t, root, "old.txt") != "old-content" || backups != 0 || restored.Load() != 1 {
					t.Fatal("original path was not restored", backups, restored.Load(), err)
				}
				if strings.Contains(err.Error(), "恢复旧文件失败") {
					t.Fatal("successful compensation reported failure", err)
				}
				return
			}
			if !strings.Contains(err.Error(), "恢复旧文件失败") || backups != 1 {
				t.Fatal("compensation failure was swallowed", err, backups)
			}
			logged := false
			for _, entry := range a.store.snapshot().Logs {
				if entry.Level == "error" && strings.Contains(entry.Message, "恢复旧文件失败") {
					logged = true
				}
			}
			if !logged {
				t.Fatal("compensation failure not logged")
			}
			if mode == "occupied" {
				if syncRead(t, root, "old.txt") != "unrelated-file" {
					t.Fatal("compensation overwrote a newly created file")
				}
			} else if _, statErr := os.Stat(filepath.Join(root, "old.txt")); !os.IsNotExist(statErr) {
				t.Fatal("unsafe compensation restored a wrong file", statErr)
			}
			if mode != "restore-failure" && restored.Load() != 0 {
				t.Fatal("unsafe storage or backup reached mutation", restored.Load())
			}
		})
	}
}
