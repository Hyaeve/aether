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
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

func TestBackupCloudUploadTransferBodyControls(t *testing.T) {
	for _, action := range []string{"pause", "delete"} {
		t.Run(action, func(t *testing.T) {
			a, _, sourceDir, _ := backupFixture(t)
			remote := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(t.TempDir()), LockSystem: webdav.NewMemLS()})
			defer remote.Close()
			target := Storage{ID: "target", Name: "Cloud", Type: "webdav", Enabled: true, Config: map[string]string{"address": remote.URL}}
			if err := a.store.update(func(st *State) error { st.Storages[1] = target; return nil }); err != nil {
				t.Fatal(err)
			}
			source := a.store.snapshot().Storages[0]
			data := bytes.Repeat([]byte("x"), 96*1024)
			if err := os.WriteFile(filepath.Join(sourceDir, "body.bin"), data, 0600); err != nil {
				t.Fatal(err)
			}
			first, proceed, boundary, readAgain, canceled := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			oldClient := apiClient
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "PUT" {
					return http.DefaultTransport.RoundTrip(r)
				}
				buf := make([]byte, 32*1024)
				if _, err := io.ReadFull(r.Body, buf); err != nil {
					return nil, err
				}
				close(first)
				select {
				case <-r.Context().Done():
					close(canceled)
					return nil, r.Context().Err()
				case <-proceed:
				}
				close(boundary)
				if _, err := r.Body.Read(buf); err != nil {
					return nil, err
				}
				close(readAgain)
				// Stop before publication: this test isolates request-body controls.
				return nil, errors.New("fixture upload complete")
			})}
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				done <- a.copyBackupFile(ctx, source, target, File{ID: "/body.bin", Name: "body.bin", Size: int64(len(data))}, "/", "body.bin", "skip")
				close(finished)
			}()
			defer func() {
				cancel()
				select {
				case <-finished:
				case <-time.After(3 * time.Second):
					t.Error("backup upload did not stop")
				}
				apiClient = oldClient
			}()
			awaitBackupTransferSignal(t, first, "first upload body chunk")
			a.transfers.mu.Lock()
			transferID := ""
			for key, item := range a.transfers.items {
				if item.Kind == "upload" && item.Name == "body.bin" {
					transferID = key
				}
			}
			a.transfers.mu.Unlock()
			if transferID == "" {
				t.Fatal("missing backup upload transfer")
			}
			backupTransferAction(t, a, transferID, action)
			if action == "delete" {
				awaitBackupTransferSignal(t, canceled, "upstream request cancellation")
			} else {
				close(proceed)
				awaitBackupTransferSignal(t, boundary, "next body read boundary")
				select {
				case <-readAgain:
					t.Fatal("paused backup upload read another body chunk")
				case <-done:
					t.Fatal("paused backup upload exited instead of waiting")
				case <-time.After(150 * time.Millisecond):
				}
				backupTransferAction(t, a, transferID, "resume")
				awaitBackupTransferSignal(t, readAgain, "resumed body read")
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("interrupted upstream upload unexpectedly published")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("backup transfer remained blocked")
			}
			a.resources.mu.Lock()
			active := a.resources.active["upload"] + a.resources.active["preprocess"]
			a.resources.mu.Unlock()
			if active != 0 {
				t.Fatal("backup transfer leaked resource slots", active)
			}
		})
	}
}

func awaitBackupTransferSignal(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for " + name)
	}
}

func backupTransferAction(t *testing.T, a *App, id, action string) {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"id": id, "action": action})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.transferAction(w, httptest.NewRequest("POST", "/api/transfers/action", bytes.NewReader(raw)))
	if w.Code != http.StatusOK {
		t.Fatal("transfer action failed", w.Code, w.Body.String())
	}
}
