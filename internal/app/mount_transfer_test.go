package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

func TestMountExistingCloudFileStaging(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "book.txt"), []byte("HELLOworld"), 0600); err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()})
	defer remote.Close()
	s := Storage{ID: "cloud", Type: "webdav", Enabled: true, Config: map[string]string{"address": remote.URL, "root": "/"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	mount := mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: "/"}}
	staged, err := mount.openCloudWrite(a.ctx, s, "/", "book.txt", os.O_TRUNC|os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	handle := staged.stagingHandle()
	defer handle.Close()
	ctx := context.WithValue(a.ctx, transferOwnedKey{}, true)
	old, err := mount.OpenFile(ctx, "/book.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if _, err := io.Copy(handle, old); err != nil {
		t.Fatal("read existing cloud file into staging", err)
	}
	if data, err := os.ReadFile(staged.Name()); err != nil || string(data) != "HELLOworld" {
		t.Fatal(string(data), err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := mount.openCloudWrite(a.ctx, s, "/", "book.txt", os.O_TRUNC|os.O_RDWR)
	if err != nil {
		t.Fatal("reopen after native handle release", err)
	}
	defer next.temp.Close()
}

func TestMountNativeStagingHandleHonorsTempBudget(t *testing.T) {
	for _, method := range []string{"random-write", "copy"} {
		t.Run(method, func(t *testing.T) {
			a, _ := tempBudgetFixture(t, 8)
			remote := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(t.TempDir()), LockSystem: webdav.NewMemLS()})
			defer remote.Close()
			s := Storage{ID: "cloud", Type: "webdav", Enabled: true, Config: map[string]string{"address": remote.URL, "root": "/"}}
			if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
				t.Fatal(err)
			}
			mount := mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: "/"}}
			staged, err := mount.openCloudWrite(a.ctx, s, "/", "book.txt", os.O_TRUNC|os.O_RDWR)
			if err != nil {
				t.Fatal(err)
			}
			handle := staged.stagingHandle()
			defer handle.Close()
			if method == "copy" {
				_, err = io.Copy(handle, strings.NewReader("123456789"))
			} else {
				if _, err := handle.Seek(8, io.SeekStart); err != nil {
					t.Fatal(err)
				}
				_, err = handle.Write([]byte("x"))
			}
			if err == nil || !strings.Contains(err.Error(), "临时文件体积达到上限") {
				t.Fatal("native staging bypassed the aggregate quota", err)
			}
			if info, err := staged.Stat(); err != nil || info.Size() != 0 {
				t.Fatal("over-budget staging mutated file", info, err)
			}
		})
	}
}

func TestMountTransferSeparatesRequestAndHandleLifetime(t *testing.T) {
	a := testApp(t)
	service, stopService := context.WithCancel(a.ctx)
	defer stopService()
	a.ctx = service
	mount := mountFS{app: a}
	progress := mount.beginTransfer("upload", Storage{Name: "Cloud"}, "book.cbz", 7)
	defer progress.finish(nil)
	request, interrupt := context.WithCancel(context.Background())
	operation, release := mount.transferContext(request, progress)
	interrupt()
	select {
	case <-operation.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("active request cancellation ignored")
	}
	release()
	if err := progress.wait(); err != nil {
		t.Fatal("an interrupted request poisoned the file handle", err)
	}
	next, nextRelease := mount.transferContext(context.Background(), progress)
	defer nextRelease()
	if next.Err() != nil || a.transfers.items[progress.id].Source != "FUSE" {
		t.Fatal("new operation lost its independent service lifetime")
	}
	stopService()
	select {
	case <-next.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("service shutdown did not cancel the file handle")
	}
}

func TestMountUploadDeletionCancelsPublicationAndRetainsStaging(t *testing.T) {
	a := testApp(t)
	remote := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(t.TempDir()), LockSystem: webdav.NewMemLS()})
	defer remote.Close()
	s := Storage{ID: "cloud", Name: "Cloud", Type: "webdav", Enabled: true, Config: map[string]string{"address": remote.URL, "root": "/"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; st.TransferSettings.UploadDelay = 0; return nil }); err != nil {
		t.Fatal(err)
	}
	mount := mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: "/"}}
	file, err := mount.openCloudWrite(a.ctx, s, "/", "book.cbz", os.O_CREATE|os.O_TRUNC|os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	defer file.File.Close()
	if _, err := file.Write([]byte("content")); err != nil {
		t.Fatal(err)
	}
	progress := mount.beginTransfer("upload", s, "book.cbz", 7)
	defer progress.finish(context.Canceled)
	if err := progress.wait(); err != nil {
		t.Fatal(err)
	}
	ctx, release := mount.transferContext(context.Background(), progress)
	defer release()
	ctx = context.WithValue(ctx, transferUploadResourceKey{}, true)
	started := make(chan struct{})
	old := apiClient
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "PUT" {
			return http.DefaultTransport.RoundTrip(r)
		}
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	finished := make(chan error, 1)
	done := make(chan struct{})
	go func() { defer close(done); finished <- file.publish(ctx) }()
	defer func() {
		progress.cancel()
		release()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("mount publication did not stop before fixture teardown")
		}
		apiClient = old
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		progress.cancel()
		<-finished
		t.Fatal("mount upload did not reach publication")
	}
	backupTransferAction(t, a, progress.id, "delete")
	select {
	case err := <-finished:
		progress.finish(err)
		if !errors.Is(err, context.Canceled) || !strings.Contains(transferFailureReason(err), "取消") {
			t.Fatal("upstream cancellation was lost", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("deleting transfer did not cancel mount publication")
	}
	if _, err := os.Stat(file.File.Name()); err != nil {
		t.Fatal("failed mount staging was removed", err)
	}
}
