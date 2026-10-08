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
