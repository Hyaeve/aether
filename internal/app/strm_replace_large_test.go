package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrmReplaceScansBeyondTenThousandEntries(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10020; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("entry-%05d.txt", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeTest(t, filepath.Join(dir, "movie.strm"), "https://old/movie")
	preview := strmReplacePreview{}
	input := strmReplaceRequest{Directory: dir, Find: "old", Replace: "new", preview: &preview}
	task := strmReplaceTask{}
	if err := scanStrmReplace(context.Background(), input, &task, func() {}); err != nil || task.Scanned != 10021 || preview.Count != 1 {
		t.Fatal("large read-only scan stopped", task, preview.Count, err)
	}
	input.preview = nil
	task = strmReplaceTask{}
	if err := scanStrmReplace(context.Background(), input, &task, func() {}); err != nil || task.Scanned != 10021 || task.Changed != 1 {
		t.Fatal("large replacement stopped", task, err)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "movie.strm")); err != nil || string(raw) != "https://new/movie" {
		t.Fatal(string(raw), err)
	}
}

func TestStrmReplacePreviewBoundsEncodedResponseWithoutLosingCount(t *testing.T) {
	dir := t.TempDir()
	content := "old/" + strings.Repeat("x", 900<<10)
	for i := 0; i < 4; i++ {
		writeTest(t, filepath.Join(dir, fmt.Sprintf("large-%d.strm", i)), content)
	}
	preview := strmReplacePreview{}
	if err := scanStrmReplace(context.Background(), strmReplaceRequest{Directory: dir, Find: "old", Replace: "new", preview: &preview}, &strmReplaceTask{}, func() {}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(preview)
	if err != nil || preview.Count != 4 || len(preview.Samples) != 2 || len(raw) > (4<<20)+128 {
		t.Fatal("preview is unbounded or lost its exact count", preview.Count, len(preview.Samples), len(raw), err)
	}
	for _, sample := range preview.Samples {
		if sample.Before != content || sample.After != "new/"+strings.TrimPrefix(content, "old/") {
			t.Fatal("a preview cell was silently truncated")
		}
	}
}

func TestStrmReplacePreviewKeepsOneHundredRowsAndExactTotal(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 105; i++ {
		writeTest(t, filepath.Join(dir, fmt.Sprintf("entry-%03d.strm", i)), "old")
	}
	preview := strmReplacePreview{}
	if err := scanStrmReplace(context.Background(), strmReplaceRequest{Directory: dir, Find: "old", Replace: "new", preview: &preview}, &strmReplaceTask{}, func() {}); err != nil {
		t.Fatal(err)
	}
	if preview.Count != 105 || len(preview.Samples) != 100 {
		t.Fatal("count or preview rows lost", preview.Count, len(preview.Samples))
	}
	for _, sample := range preview.Samples {
		if sample.Before != "old" || sample.After != "new" {
			t.Fatal(sample)
		}
	}
}
