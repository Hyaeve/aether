package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeMigrationAndConfigIsolation(t *testing.T) {
	config, data := t.TempDir(), t.TempDir()
	s, err := NewStore(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.initModules(); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Truncate(time.Second)
	if err := s.update(func(st *State) error {
		st.Tasks = []Task{{ID: "task", Kind: "strm", Name: "Definition", Status: "ok", Message: "private-result", Processed: 42, LastRun: stamp}}
		st.BackupRules = []BackupRule{{ID: "backup", Name: "Backup", Status: "running", MinSize: 9007199254740993, Copied: 12}}
		st.Storages = []Storage{{ID: "pool", Name: "Pool", Status: "error", LastError: "private-error", Config: map[string]string{"cookie": "secret-ck"}}}
		st.LibraryNotices = []LibraryNotice{{ID: "notice", Name: "private-notice"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.initRuntime(filepath.Join(data, "runtime")); err != nil {
		t.Fatal(err)
	}
	for _, module := range jsonModules {
		raw, err := os.ReadFile(filepath.Join(config, filepath.FromSlash(module)+".json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{"private-result", "private-error", "private-notice", `"processed"`, `"lastRun"`, `"lastError"`, `"copied"`} {
			if bytes.Contains(raw, []byte(text)) {
				t.Fatal("runtime field in configuration", module, text)
			}
		}
	}
	name := filepath.Join(config, "task", "strm.json")
	before, _ := os.ReadFile(name)
	info, _ := os.Stat(name)
	if err := s.update(func(st *State) error { st.Tasks[0].Processed = 57; st.Tasks[0].Message = "new-result"; return nil }); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(name)
	updated, _ := os.Stat(name)
	if !bytes.Equal(before, after) || !info.ModTime().Equal(updated.ModTime()) {
		t.Fatal("progress rewrote configuration")
	}
	reopened, err := NewStore(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.initRuntime(filepath.Join(data, "runtime")); err != nil {
		t.Fatal(err)
	}
	state := reopened.snapshot()
	if state.Tasks[0].Processed != 57 || state.Tasks[0].Message != "new-result" || !state.Tasks[0].LastRun.Equal(stamp) || state.Storages[0].Config["cookie"] != "secret-ck" || state.LibraryNotices[0].ID != "notice" || state.BackupRules[0].Status != "interrupted" || state.BackupRules[0].MinSize != 9007199254740993 {
		t.Fatal("runtime migration lost values", state)
	}
	if err := os.WriteFile(filepath.Join(data, "runtime", "state.json"), []byte(`{"version":1,"data":{},"auth":""}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reopened.initRuntime(filepath.Join(data, "runtime")); err == nil {
		t.Fatal("corrupt runtime accepted")
	}
}

func TestMissingRuntimeDoesNotRestoreDeletedTaskOrOldResult(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.Tasks = []Task{{ID: "old", Kind: "strm", Status: "ok", Processed: 10}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.store.update(func(st *State) error { st.Tasks = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if len(s.snapshot().Tasks) != 0 {
		t.Fatal("deleted task restored")
	}
	if err := os.Remove(filepath.Join(a.store.runtimeDir, "state.json")); err != nil {
		t.Fatal(err)
	}
	if err := s.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRollbackWhenConfigWriteFails(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.Tasks = []Task{{ID: "task", Kind: "strm", Name: "Original", Processed: 10}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(a.store.dir, "task", "strm.json")
	good, _ := os.ReadFile(name)
	if err := os.WriteFile(name, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.store.update(func(st *State) error { st.Tasks[0].Name = "Changed"; st.Tasks[0].Processed = 20; return nil }); err == nil {
		t.Fatal("config failure accepted")
	}
	if err := os.WriteFile(name, good, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if got := s.snapshot().Tasks[0]; got.Name != "Original" || got.Processed != 10 {
		t.Fatal("runtime did not roll back", got)
	}
}
