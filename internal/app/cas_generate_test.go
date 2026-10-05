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
	data := []byte("local video content")
	if err := os.WriteFile(filepath.Join(s.Config["root"], "Test.MP4"), data, 0600); err != nil {
		t.Fatal(err)
	}
	task := Task{Name: "Local CAS", Kind: "cas", StorageID: s.ID, Source: "/", SourceLabel: "根目录", Mode: "incremental", Target: "generated"}
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
	sha, md := sha256.Sum256(data), md5.Sum(data)
	if info.SHA256 != hex.EncodeToString(sha[:]) || info.MD5 != hex.EncodeToString(md[:]) || info.Size != int64(len(data)) {
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
	if err := a.validateTask(&task); err != nil || task.CASOperation != "generate" {
		t.Fatalf("%+v %v", task, err)
	}
}
