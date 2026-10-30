package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

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
