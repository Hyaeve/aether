package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleMigrationEncryptedRestoreAndRollback(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.update(func(st *State) error {
		st.Storages = []Storage{{ID: "pool", Name: "Pool", Type: "115", Config: map[string]string{"cookie": "secret-cookie"}}}
		st.Tasks = []Task{{ID: "strm", Kind: "strm"}, {ID: "cas", Kind: "cas"}, {ID: "ed2k", Kind: "ed2k"}, {ID: "cache", Kind: "cache"}}
		st.Plugins = map[string]PluginConfig{"tmdb": {APIKey: "secret-token"}}
		st.Links = []MediaLink{{ID: "link"}}
		st.Mounts = []MountConfig{{ID: "mount"}}
		st.DAVUsers = []DAVUser{{ID: "dav"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.initTools(filepath.Join(data, "tools")); err != nil {
		t.Fatal(err)
	}
	a, err := newWithDirectories(context.Background(), dir, data, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st := a.store.snapshot()
	if !a.store.modular || st.ModuleVersion != 3 || st.ToolsRevision != "" {
		t.Fatal("migration missing")
	}
	for _, key := range jsonModules {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(key)+".json"))
		if err != nil {
			t.Fatal(key, err)
		}
		if bytes.Contains(b, []byte("secret-cookie")) || bytes.Contains(b, []byte("secret-token")) {
			t.Fatal("plaintext secret")
		}
	}
	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.snapshot()
	if got.Storages[0].Config["cookie"] != "secret-cookie" || got.Plugins["tmdb"].APIKey != "secret-token" || len(got.Tasks) != 4 || len(got.Links) != 1 || len(got.Mounts) != 1 || len(got.DAVUsers) != 1 {
		t.Fatal("module restore incomplete")
	}
	before, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if err := os.Rename(filepath.Join(dir, "file"), filepath.Join(dir, "file.saved")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := reopened.update(func(st *State) error { st.Storages[0].Name = "Changed"; return nil }); err == nil {
		t.Fatal("failed transaction accepted")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if !bytes.Equal(before, after) || reopened.snapshot().Storages[0].Name != "Pool" {
		t.Fatal("failed save changed committed state")
	}
	if err := os.Remove(filepath.Join(dir, "file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "file.saved"), filepath.Join(dir, "file")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(dir); err != nil {
		t.Fatal("previous commit unreadable", err)
	}
}

func TestModuleJSONRejectsTamperingAndRetainsUnchangedFiles(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.initModules(); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(s.dir, "file", "webdav.json"))
	if err := s.update(func(st *State) error { st.Settings.WebDAVEnabled = !st.Settings.WebDAVEnabled; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.update(func(st *State) error { st.Username = "same-module"; return nil }); err != nil {
		t.Fatal(err)
	}
	previous, _ := os.ReadFile(filepath.Join(s.dir, "file", "webdav.json"))
	if bytes.Equal(first, previous) {
		t.Fatal("changed module was not saved")
	}
	name := filepath.Join(s.dir, "tool", "config.json")
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	if err := os.WriteFile(name, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(s.dir); err == nil {
		t.Fatal("tampered module silently accepted")
	}
}
