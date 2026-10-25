package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"golang.org/x/net/webdav"
)

func TestCrossStorageCopyMoveAndTransferVisibility(t *testing.T) {
	for _, mode := range []string{"copy", "move", "failed"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			sourceDir, targetDir := t.TempDir(), t.TempDir()
			os.Mkdir(filepath.Join(sourceDir, "Series"), 0700)
			os.WriteFile(filepath.Join(sourceDir, "Series", "film.mkv"), []byte("verified bytes"), 0600)
			handler := &webdav.Handler{FileSystem: webdav.Dir(targetDir), LockSystem: webdav.NewMemLS()}
			var puts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "PUT" {
					puts.Add(1)
				}
				if mode == "failed" && r.Method == "PUT" {
					w.WriteHeader(500)
					return
				}
				handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			local := Storage{ID: "local", Type: "local", Name: "本机", Enabled: true, Config: map[string]string{"root": sourceDir}}
			dav := Storage{ID: "dav", Type: "webdav", Name: "WebDAV", Enabled: true, Config: map[string]string{"address": server.URL, "root": "/"}}
			a.store.update(func(st *State) error {
				st.Storages = []Storage{local, dav}
				st.TransferSettings = normalizedTransferSettings(st.TransferSettings)
				st.TransferSettings.UploadDelay = 0
				return nil
			})
			action := mode
			if mode == "failed" {
				action = "move"
			}
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			response := request(t, h, "POST", "/api/files/action", fileActionRequest{StorageID: local.ID, TargetStorage: dav.ID, Action: action, Source: "/", Target: "/", IDs: []string{"/Series"}}, cookie)
			if puts.Load() != 1 {
				t.Fatal("test did not reach exactly one actual upload", puts.Load())
			}
			if mode == "failed" {
				if response.Code != 400 {
					t.Fatal("failed write reported success")
				}
				if _, e := os.Stat(filepath.Join(sourceDir, "Series", "film.mkv")); e != nil {
					t.Fatal("source removed after failed transfer", e)
				}
			} else {
				var result struct {
					Processed int `json:"processed"`
				}
				if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Processed != 1 {
					t.Fatal(response.Code, response.Body.String())
				}
				b, e := os.ReadFile(filepath.Join(targetDir, "Series", "film.mkv"))
				if e != nil || string(b) != "verified bytes" {
					t.Fatal("target mismatch", e)
				}
				_, e = os.Stat(filepath.Join(sourceDir, "Series", "film.mkv"))
				if (mode == "move") != os.IsNotExist(e) {
					t.Fatal("incorrect source cleanup", e)
				}
			}
			a.transfers.mu.Lock()
			defer a.transfers.mu.Unlock()
			visible := false
			for _, entry := range a.transfers.items {
				if entry.From != nil && entry.To != nil && entry.From.StorageID == "local" && entry.To.StorageID == "dav" {
					visible = true
				}
			}
			if !visible {
				t.Fatal("cross-storage operation missing from transfer center")
			}
		})
	}
}

func TestCrossStorageRemoteMoveToLocalKeepsRemoteDirectory(t *testing.T) {
	a := testApp(t)
	localRoot, remoteRoot := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(remoteRoot, "Series"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteRoot, "Series", "film.mkv"), []byte("remote verified bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(remoteRoot), LockSystem: webdav.NewMemLS()})
	defer server.Close()
	local := Storage{ID: "local", Type: "local", Name: "本机", Enabled: true, Config: map[string]string{"root": localRoot}}
	remote := Storage{ID: "dav", Type: "webdav", Name: "远程", Enabled: true, Config: map[string]string{"address": server.URL, "root": "/"}}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{local, remote}
		st.TransferSettings = normalizedTransferSettings(st.TransferSettings)
		st.TransferSettings.UploadDelay = 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	response := request(t, h, "POST", "/api/files/action", fileActionRequest{StorageID: remote.ID, TargetStorage: local.ID, Action: "move", Source: "/", Target: "/", IDs: []string{"/Series"}}, cookie)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	b, err := os.ReadFile(filepath.Join(localRoot, "Series", "film.mkv"))
	if err != nil || string(b) != "remote verified bytes" {
		t.Fatal("target verification failed", err)
	}
	if _, err := os.Stat(filepath.Join(remoteRoot, "Series", "film.mkv")); !os.IsNotExist(err) {
		t.Fatal("source file retained", err)
	}
	if _, err := os.Stat(filepath.Join(remoteRoot, "Series")); err != nil {
		t.Fatal("remote directory must remain", err)
	}
}
