package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestCASMissingHashesSkipAndBatchLog(t *testing.T) {
	a := testApp(t)
	s := addCASBindings(t, a)[0]
	files := []File{}
	for i := 0; i < 53; i++ {
		files = append(files, File{ID: fmt.Sprint(i), Name: fmt.Sprintf("missing-%02d.mkv", i), Size: 42})
	}
	files = append(files, File{ID: "valid", Name: "valid.mkv", Size: 42, SHA256: strings.Repeat("a", 64)})
	cfg := a.store.snapshot().Settings
	cfg.CacheEnabled = true
	cfg.CacheMemoryMB = 128
	cfg.CacheMaxItems = 10000
	if err := a.store.update(func(st *State) error { st.Settings = cfg; return nil }); err != nil {
		t.Fatal(err)
	}
	a.cache.put(s.ID+":"+rootOf(s), files, 30, cfg)
	n, err := a.executeTask(context.Background(), Task{Name: "skip-test", Kind: "cas", CASOperation: "generate", CASBindingID: s.ID, StorageID: s.ID, Source: "/", Target: t.TempDir(), Mode: "full"}, s)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	logs := []string{}
	for _, entry := range a.store.logSnapshot() {
		if entry.Module == "tasks" && strings.Contains(entry.Message, "skip-test") {
			logs = append(logs, entry.Message)
		}
	}
	if len(logs) != 2 || !strings.Contains(logs[0], "50 个") || !strings.Contains(logs[1], "3 个") || !strings.Contains(strings.Join(logs, " "), "missing-52.mkv") {
		t.Fatal(logs)
	}
}
