package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestTransferIgnoredIsNotFailed(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.TransferSettings.IgnoredExtensions = []string{"txt"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p := a.beginTransfer(a.ctx, "upload", Storage{Name: "local"}, "ignored.txt", 10)
	p.finish(p.wait())
	if e := a.transfers.items[p.id]; e.Status != "ignored" || e.Done != 0 {
		t.Fatal(e)
	}
}

func TestTransferLocalCopyPauseAndSafeRetry(t *testing.T) {
	a := testApp(t)
	source := addLocal(t, a)
	target := Storage{ID: "target-copy", Name: "目标", Type: "local", Enabled: true, Config: map[string]string{"root": t.TempDir()}}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, target); return nil }); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(source.Config["root"], "retry.mkv")
	if err := os.WriteFile(name, []byte("local copy bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	p := a.beginTransfer(a.ctx, "copy", source, "retry.mkv", info.Size())
	p.controlCopy()
	a.localCopyReplay(p, source, target, "retry.mkv", "retry.mkv", info)
	p.stage("preparing")
	w := request(t, h, "POST", "/api/transfers/action", map[string]string{"id": p.id, "action": "pause"}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	done := make(chan error, 1)
	go func() { done <- p.wait() }()
	select {
	case err := <-done:
		t.Fatal("paused copy continued", err)
	case <-time.After(80 * time.Millisecond):
	}
	w = request(t, h, "POST", "/api/transfers/action", map[string]string{"id": p.id, "action": "resume"}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("resume did not release copy")
	}
	p.finish(errors.New("test failure before write"))
	w = request(t, h, "POST", "/api/transfers/action", map[string]string{"id": p.id, "action": "retry"}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		a.transfers.mu.Lock()
		e := a.transfers.items[p.id]
		a.transfers.mu.Unlock()
		if !transferActive(e.Status) {
			if e.Status != "completed" || e.Done != info.Size() {
				t.Fatal(e)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("retry unfinished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	content, err := os.ReadFile(filepath.Join(target.Config["root"], "retry.mkv"))
	if err != nil || string(content) != "local copy bytes" {
		t.Fatal(string(content), err)
	}
	// A second failure cannot turn an existing target into an overwrite.
	a.transfers.mu.Lock()
	e := a.transfers.items[p.id]
	e.Status = "failed"
	a.transfers.items[p.id] = e
	a.transfers.mu.Unlock()
	w = request(t, h, "POST", "/api/transfers/action", map[string]string{"id": p.id, "action": "retry"}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for {
		a.transfers.mu.Lock()
		e = a.transfers.items[p.id]
		a.transfers.mu.Unlock()
		if !transferActive(e.Status) {
			if e.Status != "failed" {
				t.Fatal(e)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("retry conflict unfinished")
		}
		runtime.Gosched()
	}
}

func TestTransferQueueStagesAndCopyCapabilities(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	cfg := a.transferConfig()
	a.resources.active = map[string]int{"upload": cfg.MaxUploads}
	p := a.beginTransfer(a.ctx, "upload", Storage{Name: "pool"}, "queued.mkv", 100)
	done := make(chan error, 1)
	go func() { done <- p.wait() }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.transfers.mu.Lock()
		e := a.transfers.items[p.id]
		a.transfers.mu.Unlock()
		if e.Status == "queued" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(e)
		}
		time.Sleep(time.Millisecond)
	}
	a.resources.mu.Lock()
	a.resources.active["upload"] = 0
	a.resources.mu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queue never entered")
	}
	a.transfers.mu.Lock()
	e := a.transfers.items[p.id]
	a.transfers.mu.Unlock()
	if e.Status != "waiting" {
		t.Fatal(e)
	}
	p.add(1)
	p.finish(nil)
	native := a.beginTransfer(a.ctx, "copy", Storage{}, "native", 0)
	w := request(t, h, "GET", "/api/transfers", nil, cookie)
	var entries []transferEntry
	if json.Unmarshal(w.Body.Bytes(), &entries) != nil {
		t.Fatal(w.Body.String())
	}
	for _, entry := range entries {
		if entry.ID == native.id && (entry.Controls.Pause || entry.Controls.Delete || entry.Controls.Retry) {
			t.Fatal(entry)
		}
	}
	native.finish(nil)
}

func TestTransferSelectionNameUsesActualFileAndFolder(t *testing.T) {
	files := []File{{ID: "opaque-folder", Name: "电影库", IsDir: true}, {ID: "opaque-file", Name: "Arrival.mkv"}}
	if got := transferSelectionName(files, []string{"opaque-folder"}); got != "电影库" {
		t.Fatal(got)
	}
	if got := transferSelectionName(files, []string{"opaque-file"}); got != "Arrival.mkv" {
		t.Fatal(got)
	}
	if got := transferSelectionName(files, []string{"opaque-folder", "opaque-file"}); got != "电影库 等2项" {
		t.Fatal(got)
	}
}

func TestTransferClientRetryRequiresReplayableSource(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	p := a.beginTransfer(a.ctx, "upload", Storage{Name: "pool"}, "movie.mkv", 100)
	p.finish(context.DeadlineExceeded)
	w := request(t, h, "POST", "/api/transfers/action", map[string]string{"id": p.id, "action": "retry"}, cookie)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
}
