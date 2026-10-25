package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskExtensions(t *testing.T) {
	got, err := normalizeExtensions(".MP4; mkv;MP4;;FLAC")
	if err != nil || got != "mp4;mkv;flac" {
		t.Fatal(got, err)
	}
	if _, err := normalizeExtensions("mp4,mkv"); err == nil {
		t.Fatal("accepted comma separator")
	}
	if !taskMedia(Task{}, "track.FLAC") || !taskMedia(Task{}, "film.MKV") || taskMedia(Task{}, "book.txt") {
		t.Fatal("incorrect default media extensions")
	}
	if taskMedia(Task{MediaExtensions: "wav"}, "film.mp4") {
		t.Fatal("ignored explicit media extensions")
	}
}

func TestFeatureTaskOutputPreservesFullMediaName(t *testing.T) {
	for _, kind := range []string{"cas", "ed2k"} {
		for _, retained := range []string{"", "iso", "mp4;mkv"} {
			for _, provider := range []string{"local", "openlist", "115"} {
				for _, name := range []string{"Series/episode.S01E01.MKV", "Music/song.flac", "disc.ISO"} {
					suffix := "." + kind + ".strm"
					got := taskOutputName(Task{Kind: kind, RetainedExtensions: &retained}, Storage{Type: provider}, name, suffix)
					if got != name+suffix {
						t.Fatalf("%s/%s retained %q: %q", kind, provider, retained, got)
					}
				}
			}
		}
	}
	empty, iso := "", "iso"
	for _, tc := range []struct {
		provider, name, want string
		retained             *string
	}{
		{"local", "film.mkv", "film.strm", nil},
		{"openlist", "film.mkv", "film.strm", nil},
		{"local", "film.mkv", "film.strm", &empty},
		{"local", "film.mkv", "film.strm", &iso},
		{"local", "disc.ISO", "disc.ISO.strm", &iso},
	} {
		if got := taskOutputName(Task{Kind: "strm", RetainedExtensions: tc.retained}, Storage{Type: tc.provider}, tc.name, ".strm"); got != tc.want {
			t.Fatalf("STRM behavior changed: %q, want %q", got, tc.want)
		}
	}
}

func TestFeatureTaskValidationRemovesRetainedExtensions(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	for _, kind := range []string{"cas", "ed2k", "strm"} {
		retained := ".ISO"
		task := Task{Name: kind, Kind: kind, StorageID: s.ID, Source: "/", Target: "generated", Mode: "incremental", RetainedExtensions: &retained}
		if err := a.validateTask(&task); err != nil {
			t.Fatal(err)
		}
		if kind == "strm" {
			if task.RetainedExtensions == nil || *task.RetainedExtensions != "iso" {
				t.Fatal("STRM setting lost", task.RetainedExtensions)
			}
		} else if task.RetainedExtensions != nil {
			t.Fatal("obsolete setting retained", kind)
		}
	}
}

func TestTaskMetadataCopy(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "film.NFO"), "<movie>metadata</movie>")
	writeTest(t, filepath.Join(s.Config["root"], "skip.txt"), "ignored")
	task := Task{Kind: "strm", Source: "/", Mode: "incremental", MetadataExtensions: "nfo"}
	n, err := a.executeTask(context.Background(), task, s)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	data, err := os.ReadFile(filepath.Join(a.outputDir, "film.NFO"))
	if err != nil || string(data) != "<movie>metadata</movie>" {
		t.Fatal(string(data), err)
	}
	n, err = a.executeTask(context.Background(), task, s)
	if err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
