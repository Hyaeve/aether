package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestToolsMigrationAndRestore(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.update(func(st *State) error {
		st.Plugins = map[string]PluginConfig{"tmdb": {APIKey: "private-key"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.initTools(filepath.Join(data, "tools")); err != nil {
		t.Fatal(err)
	}
	if s.state.ToolsRevision == "" {
		t.Fatal("not migrated")
	}
	file, err := os.ReadFile(filepath.Join(data, "tools", s.state.ToolsRevision+".enc"))
	if err != nil || len(file) == 0 {
		t.Fatal(err)
	}
	if bytes.Contains(file, []byte("private-key")) {
		t.Fatal("plaintext credential persisted")
	}
	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.state.Plugins) != 0 {
		t.Fatal("plugin retained in main state")
	}
	if err := reopened.initTools(filepath.Join(data, "tools")); err != nil {
		t.Fatal(err)
	}
	if reopened.plugin("tmdb").APIKey != "private-key" {
		t.Fatal("secret lost")
	}
	previous := reopened.state.ToolsRevision
	if err := reopened.update(func(st *State) error {
		st.Plugins["tmdb"] = PluginConfig{APIKey: "new-key"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := reopened.update(func(st *State) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(data, "tools", previous+".enc")); err != nil {
		t.Fatal("previous committed snapshot removed by unrelated save", err)
	}
}

func TestHealthDetectsRecovery(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	a.checkStorageHealth(context.Background(), s)
	if a.store.snapshot().Storages[0].Status != "connected" {
		t.Fatal("not healthy")
	}
	root := s.Config["root"]
	if err := os.Rename(root, root+"-offline"); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(root+"-offline", root)
	a.checkStorageHealth(context.Background(), s)
	if a.store.snapshot().Storages[0].Status != "error" {
		t.Fatal("failure not detected")
	}
	if err := os.Rename(root+"-offline", root); err != nil {
		t.Fatal(err)
	}
	a.checkStorageHealth(context.Background(), s)
	if a.store.snapshot().Storages[0].Status != "connected" {
		t.Fatal("recovery not detected")
	}
}

func TestRenameRegexRules(t *testing.T) {
	got, err := applyRenameRules("Show.E01.mkv", []renameRule{{Kind: "replace", FindType: "regex", Find: `E(\d+)`, Replace: `Episode-$1`}})
	if err != nil || got != "Show.Episode-01.mkv" {
		t.Fatal(got, err)
	}
	if _, err := applyRenameRules("a", []renameRule{{Kind: "replace", FindType: "regex", Find: "["}}); err == nil {
		t.Fatal("invalid regex accepted")
	}
}

func TestDAVMetadataPermissions(t *testing.T) {
	file := File{Name: "file"}
	if davFileInfo(context.Background(), file).Mode()&0222 == 0 {
		t.Fatal("administrator advertised read-only")
	}
	ctx := context.WithValue(context.Background(), davGrantsKey{}, []DAVGrant{})
	if davFileInfo(ctx, file).Mode()&0222 != 0 {
		t.Fatal("restricted user writable")
	}
}
