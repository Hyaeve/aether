package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuiltinOfflineUnsupportedURL(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/files/offline", map[string]any{
		"storageId": s.ID, "parent": "/", "urls": []string{"ftp://127.0.0.1/file"},
	}, cookie)
	if w.Code != 400 {
		t.Fatalf("unsupported protocol: %d %s", w.Code, w.Body.String())
	}
	if a.offlineBatches.Load() != 0 {
		t.Fatal("rejected request retained batch lock")
	}
}

func TestBuiltinOfflineLocalDownload(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download.txt" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("offline local payload"))
	}))
	defer upstream.Close()
	a := testApp(t)
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	a.ctx = ctx
	defer func() { cancel(); a.wg.Wait() }()
	s := addLocal(t, a)
	if err := os.Mkdir(filepath.Join(s.Config["root"], "downloads"), 0755); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/files/offline", map[string]any{
		"storageId": s.ID, "parent": "/downloads", "urls": []string{upstream.URL + "/download.txt"},
	}, cookie)
	var results []offlineResult
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &results) != nil || len(results) != 1 || !results[0].Success {
		t.Fatalf("download submission: %d %s", w.Code, w.Body.String())
	}
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("download timed out")
	}
	data, err := os.ReadFile(filepath.Join(s.Config["root"], "downloads", "download.txt"))
	if err != nil || string(data) != "offline local payload" {
		t.Fatalf("download/upload result: %q %v", data, err)
	}
	entries, err := os.ReadDir(filepath.Join(a.dataDir, "cache", "offline"))
	if err != nil || len(entries) != 0 || a.offlineBatches.Load() != 0 {
		t.Fatalf("successful job was not cleaned up: %v %v", entries, err)
	}
}

func TestPublishOfflineDirectory(t *testing.T) {
	a := testApp(t)
	root, staging := t.TempDir(), t.TempDir()
	s := Storage{ID: "download", Type: "local", Enabled: true, Config: map[string]string{"root": root}}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(staging, "album"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "album", "track.m4a"), []byte("audio"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.publishOfflineDirectory(context.Background(), s, "/", staging); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "album", "track.m4a"))
	if err != nil || string(got) != "audio" {
		t.Fatalf("upload %q %v", got, err)
	}
	if err := a.publishOfflineDirectory(context.Background(), s, "/", staging); err == nil {
		t.Fatal("overwrote existing file")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.publishOfflineDirectory(ctx, s, "/", staging); err == nil {
		t.Fatal("ignored cancellation")
	}
	unfinished := t.TempDir()
	if err := os.WriteFile(filepath.Join(unfinished, "pending.aria2"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.publishOfflineDirectory(context.Background(), s, "/", unfinished); err == nil {
		t.Fatal("uploaded incomplete download")
	}
	if err := a.publishOfflineDirectory(context.Background(), s, "/", t.TempDir()); err == nil {
		t.Fatal("empty download accepted")
	}
}
