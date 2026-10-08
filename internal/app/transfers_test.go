package app

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestTransferProgress(t *testing.T) {
	a := testApp(t)
	p := a.beginTransfer(context.WithValue(context.Background(), transferSourceKey{}, "FUSE"), "upload", Storage{Name: "test"}, "file", 100)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); p.add(1) }()
	}
	wg.Wait()
	p.finish(errors.New("private upstream detail"))
	p.finish(nil)
	e := a.transfers.items[p.id]
	if e.Done != 100 || e.Status != "failed" || e.Source != "FUSE" || e.Message == "private upstream detail" {
		t.Fatal(e)
	}
	if a.beginTransfer(context.WithValue(context.Background(), transferOwnedKey{}, true), "download", Storage{}, "", 0) != nil {
		t.Fatal("duplicate nested transfer")
	}
}

func TestTransferLogBound(t *testing.T) {
	a := testApp(t)
	for i := 0; i < 510; i++ {
		a.beginTransfer(context.Background(), "copy", Storage{}, "file", 0).finish(nil)
	}
	if len(a.transfers.items) != 500 {
		t.Fatal(len(a.transfers.items))
	}
}

func TestTransfersRequireAdminSession(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	if w := request(t, h, "GET", "/api/transfers", nil, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	if w := request(t, h, "GET", "/api/transfers", nil, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
}
