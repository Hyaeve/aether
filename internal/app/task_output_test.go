package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskOutputLocations(t *testing.T) {
	for _, kind := range []string{"strm", "cas", "ed2k"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			writeTest(t, filepath.Join(s.Config["root"], "Movies", "film.mp4"), "video")
			target := filepath.Join(t.TempDir(), "export")
			task := Task{Name: "export", Kind: kind, StorageID: s.ID, Source: "/", Target: target, Mode: "incremental", APIInterval: 200}
			if kind == "cas" {
				task.CASBindingID = addCASBindings(t, a)[0].ID
			}
			if kind == "ed2k" {
				task.ED2KBindingID = "bound-115"
				if err := a.store.update(func(st *State) error {
					st.Storages = append(st.Storages, Storage{ID: "bound-115", Type: "115", Enabled: true})
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.validateTask(&task); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatal("validation created output")
			}
			if count, err := a.executeTask(context.Background(), task, s); err != nil || count == 0 {
				t.Fatal(count, err)
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) == 0 {
				t.Fatal("missing external output", err)
			}
			if _, err := os.Stat(a.outputDir); !os.IsNotExist(err) {
				t.Fatal("default directory created for explicit output")
			}
		})
	}
	a := testApp(t)
	for _, target := range []string{"", "movies"} {
		root, rel, err := a.outputLocation(target)
		if err != nil || root != a.outputDir || (target == "" && rel != ".") {
			t.Fatal(root, rel, err)
		}
	}
	if _, _, err := a.outputLocation("../escape"); err == nil {
		t.Fatal("relative traversal accepted")
	}
}

func TestTaskExternalOutputConfinesChildren(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "Movies", "film.mp4"), "video")
	target, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(target, "Movies")); err != nil {
		t.Skip("symlink privileges unavailable")
	}
	task := Task{Kind: "strm", StorageID: s.ID, Source: "/", Target: target, Mode: "full"}
	if _, err := a.executeTask(context.Background(), task, s); err == nil {
		t.Fatal("followed output child symlink")
	}
	items, err := os.ReadDir(outside)
	if err != nil || len(items) != 0 {
		t.Fatal("escaped output boundary", err)
	}
}
