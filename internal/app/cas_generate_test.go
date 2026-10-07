package app

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalCASGeneration(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	bindings := addCASBindings(t, a)
	data := []byte("local video content")
	if err := os.WriteFile(filepath.Join(s.Config["root"], "Test.MP4"), data, 0600); err != nil {
		t.Fatal(err)
	}
	task := Task{Name: "Local CAS", Kind: "cas", StorageID: s.ID, CASBindingID: bindings[0].ID, Source: "/", SourceLabel: "根目录", Mode: "incremental", Target: "generated"}
	if err := a.validateTask(&task); err != nil {
		t.Fatal(err)
	}
	if task.CASOperation != "generate" {
		t.Fatal(task.CASOperation)
	}
	count, err := a.executeTask(context.Background(), task, s)
	if err != nil || count < 1 {
		t.Fatalf("%d %v", count, err)
	}
	output := filepath.Join(a.outputDir, "generated", "Test.MP4.cas")
	encoded, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	info, err := decodeCAS(encoded, "Test.MP4.cas")
	if err != nil {
		t.Fatal(err)
	}
	sha := sha256.Sum256(data)
	mdOriginal := md5.Sum(data)
	if info.SHA256 != hex.EncodeToString(sha[:]) || info.MD5 != hex.EncodeToString(mdOriginal[:]) || info.Provider != "mobile" || info.Size != int64(len(data)) || info.PlaybackURL == "" {
		t.Fatalf("%+v", info)
	}
	if count, err := a.executeTask(context.Background(), task, s); count != 0 || err != nil {
		t.Fatalf("incremental: %d %v", count, err)
	}
	if err := os.WriteFile(filepath.Join(s.Config["root"], "Test.MP4"), []byte("updated"), 0600); err != nil {
		t.Fatal(err)
	}
	task.Mode = "full"
	if _, err := a.executeTask(context.Background(), task, s); err != nil {
		t.Fatal(err)
	}
	updated, _ := os.ReadFile(output)
	if string(updated) == string(encoded) {
		t.Fatal("full did not replace CAS")
	}
	task.CASBindingID = bindings[1].ID
	if _, err := a.executeTask(context.Background(), task, s); err != nil {
		t.Fatal(err)
	}
	tianyiOutput, _ := os.ReadFile(output)
	tianyiInfo, err := decodeCAS(tianyiOutput, "Test.MP4.cas")
	md := md5.Sum([]byte("updated"))
	shaUpdated := sha256.Sum256([]byte("updated"))
	if err != nil || tianyiInfo.Provider != "tianyi" || tianyiInfo.SHA256 != hex.EncodeToString(shaUpdated[:]) || tianyiInfo.MD5 != hex.EncodeToString(md[:]) {
		t.Fatalf("%+v %v", tianyiInfo, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.generateCASInfo(ctx, s, File{ID: "/Test.MP4", Name: "Test.MP4"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := a.generateCASInfo(context.Background(), s, File{ID: "../escape.mp4", Name: "escape.mp4"}); err == nil {
		t.Fatal("escape accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.Config["root"], "escape.mp4")); err == nil {
		if _, err := a.generateCASInfo(context.Background(), s, File{ID: "/escape.mp4", Name: "escape.mp4"}); err == nil {
			t.Fatal("symlink escape accepted")
		}
	} else {
		t.Log("symlink creation unavailable:", err)
	}
}

func TestCloudCASGenerationHashValidation(t *testing.T) {
	a := testApp(t)
	for _, s := range []Storage{
		{Type: "mobile", Config: map[string]string{"mode": "native"}},
		{Type: "tianyi", Config: map[string]string{"mode": "native", "username": "test", "password": "test"}},
	} {
		f := File{Name: "movie.mkv", Size: 42, SHA256: strings.Repeat("a", 64), MD5: strings.Repeat("b", 32)}
		if _, err := a.generateCASInfo(context.Background(), s, f); err != nil {
			t.Fatal(err)
		}
		f.SHA256, f.MD5 = "", ""
		if _, err := a.generateCASInfo(context.Background(), s, f); err == nil {
			t.Fatal("missing hash accepted")
		}
	}
}

func TestCASOnlyGeneratesNewTasks(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	bindings := addCASBindings(t, a)
	task := Task{ID: "old-cas", Name: "old", Kind: "cas", StorageID: s.ID, Mode: "full", CASOperation: "restore"}
	if err := a.validateTask(&task); err == nil {
		t.Fatal("restore task accepted")
	}
	if err := a.store.update(func(st *State) error { st.Tasks = append(st.Tasks, task); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.startTask(task.ID); err == nil {
		t.Fatal("old conversion started")
	}
	task.CASOperation = ""
	task.CASBindingID = bindings[0].ID
	if err := a.validateTask(&task); err != nil || task.CASOperation != "generate" {
		t.Fatalf("%+v %v", task, err)
	}
}

func addCASBindings(t *testing.T, a *App) []Storage {
	t.Helper()
	bindings := []Storage{
		{ID: "bound-mobile", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": "test"}},
		{ID: "bound-tianyi", Type: "tianyi", Enabled: true, Config: map[string]string{"username": "test", "password": "test"}},
	}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, bindings...); return nil }); err != nil {
		t.Fatal(err)
	}
	return bindings
}

func TestCASBindingValidation(t *testing.T) {
	a := testApp(t)
	local := addLocal(t, a)
	bindings := addCASBindings(t, a)
	task := Task{Name: "bound", Kind: "cas", StorageID: local.ID, Mode: "full"}
	if a.validateTask(&task) == nil {
		t.Fatal("unbound local accepted")
	}
	task.CASBindingID = bindings[0].ID
	if err := a.validateTask(&task); err != nil {
		t.Fatal(err)
	}
	task.StorageID = bindings[1].ID
	if a.validateTask(&task) == nil {
		t.Fatal("cross-provider source accepted")
	}
	task.StorageID = local.ID
	task.CASBindingID = "missing"
	if a.validateTask(&task) == nil {
		t.Fatal("missing binding accepted")
	}
}
