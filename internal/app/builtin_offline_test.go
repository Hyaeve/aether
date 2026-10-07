package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishOfflineDirectory(t *testing.T) {
	a := testApp(t)
	root, staging := t.TempDir(), t.TempDir()
	s := Storage{ID: "download", Type: "local", Enabled: true, Config: map[string]string{"root": root}}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(staging, "album"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "album", "track.m4a"), []byte("audio"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.publishOfflineDirectory(context.Background(), s, "/", staging); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "album", "track.m4a"))
	if err != nil || string(got) != "audio" {
		t.Fatalf("upload %q %v", got, err)
	}
	if err := a.publishOfflineDirectory(context.Background(), s, "/", staging); err == nil {
		t.Fatal("overwrote existing file")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.publishOfflineDirectory(ctx, s, "/", staging); err == nil {
		t.Fatal("ignored cancellation")
	}
	unfinished := t.TempDir()
	if err := os.WriteFile(filepath.Join(unfinished, "pending.aria2"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.publishOfflineDirectory(context.Background(), s, "/", unfinished); err == nil {
		t.Fatal("uploaded incomplete download")
	}
	if err := a.publishOfflineDirectory(context.Background(), s, "/", t.TempDir()); err == nil {
		t.Fatal("empty download accepted")
	}
}
