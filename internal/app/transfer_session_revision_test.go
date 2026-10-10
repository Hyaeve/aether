package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTransferSpeedAndCompletionMinute(t *testing.T) {
	a := testApp(t)
	p := a.beginTransfer(context.Background(), "download", Storage{}, "movie", 1000)
	p.add(500)
	list := func() []transferEntry {
		w := httptest.NewRecorder()
		a.transferList(w, httptest.NewRequest("GET", "/", nil))
		var entries []transferEntry
		if json.Unmarshal(w.Body.Bytes(), &entries) != nil {
			t.Fatal(w.Body.String())
		}
		return entries
	}
	if entries := list(); len(entries) != 1 || entries[0].Speed != 250 || entries[0].Done != 500 {
		t.Fatal(entries)
	}
	p.finish(nil)
	if entries := list(); len(entries) != 1 || entries[0].Status != "completed" || entries[0].Speed != 0 {
		t.Fatal(entries)
	}
	a.transfers.mu.Lock()
	entry := a.transfers.items[p.id]
	entry.Started = time.Now().Add(-time.Hour)
	entry.Updated = time.Now().Add(-59 * time.Second)
	a.transfers.items[p.id] = entry
	a.transfers.mu.Unlock()
	if len(list()) != 1 {
		t.Fatal("retention counted from start")
	}
	a.transfers.mu.Lock()
	entry.Updated = time.Now().Add(-time.Minute)
	a.transfers.items[p.id] = entry
	a.transfers.mu.Unlock()
	if len(list()) != 0 {
		t.Fatal("completed record did not expire")
	}
}

func TestTransferResponseActualTotal(t *testing.T) {
	a := testApp(t)
	p := a.beginTransfer(context.Background(), "download", Storage{}, "file", 0)
	w := &transferResponse{ResponseWriter: httptest.NewRecorder(), progress: p}
	w.Header().Set("Content-Length", "10")
	w.Write([]byte("12345"))
	if entry := a.transfers.items[p.id]; entry.Total != 10 || entry.Done != 5 {
		t.Fatal(entry)
	}
}

func TestFolderRefreshRejectsLateSizeOnly(t *testing.T) {
	a := testApp(t)
	cfg := a.store.snapshot().Settings
	a.cache.put("unrelated", []File{{Name: "keep"}}, 30, cfg)
	key := directorySizeKey("pool", "folder")
	generation := a.cache.revision()
	a.cache.put(key, []File{{SizeKnown: true}}, 30, cfg)
	a.cache.forgetKeys([]string{key})
	a.cache.putGeneration(key, []File{{SizeKnown: true}}, 30, cfg, &generation)
	if _, ok := a.cache.get(key); ok {
		t.Fatal("stale calculation restored")
	}
	if _, ok := a.cache.get("unrelated"); !ok {
		t.Fatal("unrelated cache removed")
	}
}
