package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
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

func transferEvents(a *App) []LogEntry {
	var entries []LogEntry
	for _, entry := range a.store.logSnapshot() {
		if entry.Module == "transfers" {
			entries = append(entries, entry)
		}
	}
	return entries
}

func TestTransferSystemLogLifecycleAndPersistence(t *testing.T) {
	a := testApp(t)
	ctx := context.WithValue(a.ctx, transferSourceKey{}, "FUSE")
	p := a.beginTransfer(ctx, "download", Storage{Name: "115影视"}, "/电影/a.mkv", 100)
	p.add(50)
	p.finish(nil)
	p.finish(errors.New("ignored duplicate"))
	failed := a.beginTransfer(ctx, "download", Storage{Name: "115影视"}, "/电影/b.mkv", 100)
	failed.add(25)
	failed.finish(io.ErrUnexpectedEOF)
	private := a.beginTransfer(ctx, "upload", Storage{Name: "目标"}, "book.txt", 10)
	private.finish(errors.New("https://cdn.invalid/file?sign=private-token Cookie=private-cookie"))
	canceled := a.beginTransfer(ctx, "download", Storage{Name: "115影视"}, "cancel.mkv", 100)
	canceled.finish(context.Canceled)
	entries := transferEvents(a)
	if len(entries) != 8 || entries[1].Level != "info" || entries[3].Level != "error" || entries[7].Level != "warn" {
		t.Fatal(entries)
	}
	if !strings.Contains(entries[1].Message, "50/100") || !strings.Contains(entries[3].Message, "响应提前结束") || !strings.Contains(entries[0].Message, "FUSE") || !strings.Contains(entries[0].Message, "115影视") {
		t.Fatal(entries)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Message, "private-") || strings.Contains(entry.Message, "cdn.invalid") {
			t.Fatal("sensitive upstream logged", entry)
		}
	}
	reloaded, err := NewStore(a.store.dir)
	if err == nil {
		err = reloaded.initLogs(a.store.logDir)
	}
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range reloaded.logSnapshot() {
		if entry.Module == "transfers" {
			count++
		}
	}
	if count != 8 {
		t.Fatal("transfer system logs not persisted", count)
	}
}

func TestTransferActionsLogOnlyChangesAndNoDuplicateCancel(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	p := a.beginTransfer(a.ctx, "download", Storage{Name: "pool"}, "movie.mkv", 100)
	action := func(value string, want int) {
		t.Helper()
		w := request(t, h, "POST", "/api/transfers/action", map[string]string{"id": p.id, "action": value}, cookie)
		if w.Code != want {
			t.Fatal(value, w.Code, w.Body.String())
		}
	}
	action("pause", 200)
	action("pause", 200)
	action("resume", 200)
	action("invalid", 400)
	action("delete", 200)
	p.finish(context.Canceled)
	entries := transferEvents(a)
	if len(entries) != 4 || !strings.Contains(entries[1].Message, "暂停") || !strings.Contains(entries[2].Message, "继续") || entries[3].Level != "warn" || !strings.Contains(entries[3].Message, "取消") {
		t.Fatal(entries)
	}
	failed := a.beginTransfer(a.ctx, "download", Storage{Name: "pool"}, "failed.mkv", 100)
	failed.finish(io.ErrUnexpectedEOF)
	w := request(t, h, "DELETE", "/api/transfers", nil, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	entries = transferEvents(a)
	if !strings.Contains(entries[len(entries)-1].Message, "清理失败传输记录：1 项") {
		t.Fatal(entries)
	}
}

func TestTransferListOverflowStillLogsResult(t *testing.T) {
	a := testApp(t)
	a.transfers.items = map[string]transferEntry{}
	for i := 0; i < 500; i++ {
		a.transfers.items[fmt.Sprint(i)] = transferEntry{Status: "running"}
	}
	p := a.beginTransfer(a.ctx, "download", Storage{Name: "pool"}, "overflow", 10)
	p.finish(context.DeadlineExceeded)
	entries := transferEvents(a)
	if p.log != nil || len(entries) != 2 || !strings.Contains(entries[0].Message, "上限") || entries[1].Level != "error" || !strings.Contains(entries[1].Message, "超时") {
		t.Fatal(entries)
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
