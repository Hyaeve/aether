package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

func TestScrapeLargeLibraryAndQualityPrefix(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10001; i++ {
		writeTest(t, filepath.Join(dir, fmt.Sprintf("Series/S01E%04d.strm", i+1)), "http://localhost/media")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	items, err := scanSTRM(context.Background(), root, scrapeIndex{}, scrapeSettings{})
	if err != nil || len(items) != 10001 {
		t.Fatal(len(items), err)
	}
	name := "G 4k 感谢对战~大小姐才不玩格斗游戏~"
	item := recognizeSTRMPath("Quark/2026年七月新番/" + name + "/S01E01.strm")
	if item.Title != name {
		t.Fatal(item.Title)
	}
	if item := recognizeSTRM("Arrival.1080p.WEB-DL.strm"); item.Title != "Arrival" {
		t.Fatal(item.Title)
	}
}

func TestTaskResetBounds(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	target := t.TempDir()
	task := Task{ID: "reset", Name: "reset", Kind: "strm", StorageID: s.ID, Source: "/A", Target: target}
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(target, "A", "old.strm"), "http://localhost/old")
	writeTest(t, filepath.Join(target, "keep.txt"), "keep")
	if err := a.resetTaskOutput(task, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "A")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "keep.txt")); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(target, "A", "old.strm"), "http://localhost/old")
	writeTest(t, filepath.Join(target, "A", "original.epub"), "keep")
	if err := a.resetTaskOutput(task, s); err == nil {
		t.Fatal("removed original file")
	}
	task.Source = "/"
	task.Target = ""
	if err := a.resetTaskOutput(task, s); err == nil {
		t.Fatal("removed common output root")
	}
}

func TestTaskResetRegeneratesOnlyItsLibrary(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "A", "new.mp4"), "video")
	target := t.TempDir()
	writeTest(t, filepath.Join(target, "A", "old.strm"), "old")
	writeTest(t, filepath.Join(target, "B", "keep.strm"), "keep")
	task := Task{ID: "reset-run", Name: "reset", Kind: "strm", StorageID: s.ID, Source: "/A", Target: target, Mode: "incremental", MediaExtensions: "mp4", APIInterval: 200}
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.startTask(task.ID, true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.runMu.Lock()
		_, running := a.running[task.ID]
		a.runMu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("task did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, name := range []string{"A/new.mp4.strm", "B/keep.strm"} {
		if _, err := os.Stat(filepath.Join(target, name)); err != nil {
			t.Fatal(name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "A/old.strm")); !os.IsNotExist(err) {
		t.Fatal("stale output retained", err)
	}
}

func TestLinkTrafficCountsOnlySuccessfulBodies(t *testing.T) {
	for _, status := range []int{200, 206, 302, 403} {
		var meter trafficMeter
		writer := &transferResponse{ResponseWriter: httptest.NewRecorder(), successOnly: true, progress: &transferProgress{meter: &meter, kind: "download"}}
		writer.WriteHeader(status)
		_, err := writer.Write(make([]byte, 4096))
		if err != nil {
			t.Fatal(err)
		}
		want := int64(0)
		if status < 300 {
			want = 2048
		}
		if got := meter.rates(time.Now())["downloadRate"]; got != want {
			t.Fatal(status, got)
		}
	}
}

func TestTransferPauseResumeAndDelete(t *testing.T) {
	a := testApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := a.beginTransfer(ctx, "download", Storage{}, "test", 100)
	a.transfers.mu.Lock()
	entry := a.transfers.items[p.id]
	entry.Status = "paused"
	a.transfers.items[p.id] = entry
	a.transfers.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- p.wait() }()
	select {
	case <-done:
		t.Fatal("pause did not block")
	case <-time.After(80 * time.Millisecond):
	}
	a.transfers.mu.Lock()
	entry.Status = "running"
	a.transfers.items[p.id] = entry
	a.transfers.mu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("resume blocked")
	}
	a.transfers.mu.Lock()
	delete(a.transfers.items, p.id)
	a.transfers.mu.Unlock()
	if err := p.wait(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if w := request(t, a.Handler(t.TempDir()), http.MethodPost, "/api/transfers/action", map[string]string{"id": p.id, "action": "delete"}, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func Test115CDNTicketDoesNotLeakLoginCookie(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	origin, _ := url.Parse("https://cdn.example/file")
	bind115CDNTickets(&driver.DownloadInfo{Url: driver.FileDownloadUrl{Url: origin.String()}}, jar, []*http.Cookie{{Name: "download_ticket", Value: "ticket", Path: "/app/", Secure: true}, {Name: "UID", Value: "secret"}, {Name: "SEID", Value: "secret"}})
	cookies := jar.Cookies(origin)
	if len(cookies) != 1 || strings.Contains(cookies[0].String(), "secret") {
		t.Fatal(cookies)
	}
	other, _ := url.Parse("https://other.example/file")
	if len(jar.Cookies(other)) != 0 {
		t.Fatal("ticket leaked across CDN hosts")
	}
	insecure, _ := url.Parse("http://cdn.example/file")
	if len(jar.Cookies(insecure)) != 0 {
		t.Fatal("secure ticket leaked to HTTP")
	}
}
