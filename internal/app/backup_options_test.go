package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupMultiSourceTargetKeepsDistinctTreesAndProgress(t *testing.T) {
	a, rule, source, target := backupFixture(t)
	secondSource, secondTarget := t.TempDir(), t.TempDir()
	for _, dir := range []string{source, secondSource} {
		if err := os.Mkdir(filepath.Join(dir, "Books"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Books", "same.txt"), []byte(dir), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.store.update(func(st *State) error {
		st.Storages = append(st.Storages,
			Storage{ID: "source2", Name: "Source2", Type: "local", Enabled: true, Config: map[string]string{"root": secondSource}},
			Storage{ID: "target2", Name: "Target2", Type: "local", Enabled: true, Config: map[string]string{"root": secondTarget}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rule.Sources = []BackupLocation{{"source", "/Books", "Books"}, {"source2", "/Books", "books"}}
	rule.Targets = []BackupLocation{{"target", "/", ""}, {"target2", "/", ""}}
	if err := a.executeBackup(context.Background(), &rule); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{target, secondTarget} {
		for name, expected := range map[string]string{"Books": source, "books-2": secondSource} {
			data, err := os.ReadFile(filepath.Join(dir, name, "same.txt"))
			if err != nil || string(data) != expected {
				t.Fatal("source tree collision", name, err, string(data))
			}
		}
	}
	if rule.Scanned != 2 || rule.Copied != 4 || rule.Total != 4 || rule.Processed != 4 {
		t.Fatal(rule)
	}
	before, _ := os.ReadFile(filepath.Join(a.store.dir, "transfer", "backup.json"))
	if err := a.backupProgress(&rule, "result"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(a.store.dir, "transfer", "backup.json"))
	if string(before) != string(after) {
		t.Fatal("progress rewrote configuration")
	}
	var encoded map[string]any
	if err := json.Unmarshal(after, &encoded); err != nil {
		t.Fatal(err)
	}
}

func TestBackupEmptyRootNamesAndConfigurationRestore(t *testing.T) {
	a, rule, source, target := backupFixture(t)
	if err := os.Mkdir(filepath.Join(source, "Empty"), 0700); err != nil {
		t.Fatal(err)
	}
	rule.Sources = []BackupLocation{{"source", "/", "根目录"}, {"source", "/Empty", "Empty"}}
	rule.Targets = []BackupLocation{{"target", "/", "根目录"}}
	rule.Filters = []BackupFilter{{Type: "extension", Mode: "include", Value: "EPUB", MatchFile: true}}
	rule.ScanInterval = 600
	if err := validateBackup(&rule, a.store.snapshot()); err != nil {
		t.Fatal(err)
	}
	if err := a.store.update(func(st *State) error { st.BackupRules[0] = rule; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.executeBackup(context.Background(), &rule); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Source", "Empty"} {
		if info, err := os.Stat(filepath.Join(target, name)); err != nil || !info.IsDir() {
			t.Fatal(name, err)
		}
	}
	if err := a.backupProgress(&rule, "saved progress"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	restored := reloaded.snapshot().BackupRules[0]
	if len(restored.Sources) != 2 || len(restored.Targets) != 1 || restored.Filters[0].Value != "epub" || restored.ScanInterval != 600 || restored.NextRun.IsZero() || restored.Phase != "copy" {
		t.Fatal("configuration/runtime restore", restored)
	}
	raw, err := os.ReadFile(filepath.Join(a.store.dir, "transfer", "backup.json"))
	if err != nil || strings.Contains(string(raw), `"phase"`) || strings.Contains(string(raw), `"total"`) || strings.Contains(string(raw), `"processed"`) {
		t.Fatal("runtime leaked to rule definition", err)
	}
}

func TestBackupLaterSourceFailureWritesNothing(t *testing.T) {
	a, rule, source, target := backupFixture(t)
	if err := os.WriteFile(filepath.Join(source, "first.txt"), []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	rule.Sources = []BackupLocation{{"source", "/", "Books"}, {"source", "/missing", "Missing"}}
	rule.Targets = []BackupLocation{{"target", "/", ""}}
	if err := a.executeBackup(context.Background(), &rule); err == nil {
		t.Fatal("missing source accepted")
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatal("partial scan wrote target", entries, err)
	}
	rule.Sources[1].Path = "/"
	if err := validateBackup(&rule, a.store.snapshot()); err == nil {
		t.Fatal("duplicate source accepted")
	}
	rule.Sources[1] = BackupLocation{"target", "/", "Target"}
	if err := validateBackup(&rule, a.store.snapshot()); err == nil {
		t.Fatal("later source overlaps target")
	}
}

func TestBackupRichFilterSemantics(t *testing.T) {
	filters, err := compileBackupFilters([]BackupFilter{
		{Type: "extension", Mode: "include", Value: "mkv;mp4", MatchDir: true, MatchFile: true},
		{Type: "regex", Mode: "exclude", Value: "(?i)sample|trailer", MatchFile: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		file File
		want bool
	}{
		{File{Name: "Movies", IsDir: true}, true},
		{File{Name: "episode.MKV", Size: 5}, true},
		{File{Name: "trailer.mp4", Size: 5}, false},
		{File{Name: "book.txt", Size: 5}, false},
	} {
		if backupFilterAccept(filters, check.file, check.file.Name) != check.want {
			t.Fatal(check)
		}
	}
	filters, _ = compileBackupFilters([]BackupFilter{{Type: "size", Mode: "include", MinSize: 1, MaxSize: 2, Unit: "KB", MatchFile: true, MatchDir: true}})
	if !backupFilterAccept(filters, File{Name: "Folder", IsDir: true}, "Folder") ||
		!backupFilterAccept(filters, File{Name: "file", Size: 1024}, "file") ||
		backupFilterAccept(filters, File{Name: "file", Size: 2049}, "file") {
		t.Fatal("size unit/range")
	}
	filters, _ = compileBackupFilters([]BackupFilter{{Type: "name", Mode: "exclude", Value: "temp*;cache", MatchFile: true, MatchDir: true}})
	if backupFilterAccept(filters, File{Name: "TEMP1", IsDir: true}, "TEMP1") {
		t.Fatal("wildcard blacklist")
	}
}

func TestBackupFiltersPruneCopyButPreserveTraversalAndEmptyDirs(t *testing.T) {
	a, rule, source, target := backupFixture(t)
	for _, dir := range []string{"Books/Empty", "Temp"} {
		if err := os.MkdirAll(filepath.Join(source, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string]string{"Books/a.epub": "book", "Books/a.txt": "text", "Temp/a.epub": "ignore"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	rule.Filters = []BackupFilter{
		{Type: "extension", Mode: "include", Value: "epub", MatchFile: true},
		{Type: "name", Mode: "exclude", Value: "Temp", MatchDir: true},
	}
	if err := a.executeBackup(context.Background(), &rule); err != nil {
		t.Fatal(err)
	}
	if rule.Copied != 1 || rule.Scanned != 3 || rule.Skipped != 2 || rule.Total != 1 {
		t.Fatal(rule)
	}
	if _, err := os.Stat(filepath.Join(target, "Books", "Empty")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "Temp")); !os.IsNotExist(err) {
		t.Fatal("excluded directory created", err)
	}
	if _, err := os.Stat(filepath.Join(source, "Temp", "a.epub")); err != nil {
		t.Fatal("source deleted", err)
	}
}

func TestBackupIntervalAndFilterValidation(t *testing.T) {
	a, base, _, _ := backupFixture(t)
	now := time.Date(2026, 10, 10, 14, 5, 0, 0, time.Local)
	r := base
	r.ScanInterval = 600
	if !backupNext(r, now).Equal(now.Add(10 * time.Minute)) {
		t.Fatal("interval not scheduled")
	}
	r.Cron = "6 14 * * *"
	if !backupNext(r, now).Equal(now.Add(time.Minute)) {
		t.Fatal("earliest schedule not used")
	}
	r.Enabled = false
	if !backupNext(r, now).IsZero() {
		t.Fatal("disabled schedule")
	}
	cases := []BackupFilter{
		{Type: "regex", Mode: "exclude", Value: "[", MatchFile: true},
		{Type: "size", Mode: "include", MinSize: 1 << 62, Unit: "GB", MatchFile: true},
		{Type: "extension", Mode: "include", Value: "mp4", MatchDir: true},
		{Type: "name", Mode: "exclude", Value: "", MatchFile: true},
		{Type: "name", Mode: "exclude", Value: "*[", MatchFile: true},
	}
	for _, f := range cases {
		r = base
		r.Filters = []BackupFilter{f}
		if err := validateBackup(&r, a.store.snapshot()); err == nil {
			t.Fatal("invalid filter accepted", f)
		}
	}
	r = base
	r.ScanInterval = 1
	if err := validateBackup(&r, a.store.snapshot()); err == nil || !strings.Contains(err.Error(), "扫描间隔") {
		t.Fatal(err)
	}
}

func TestBackupConsecutiveRunsResetCounters(t *testing.T) {
	a, rule, source, _ := backupFixture(t)
	if err := os.WriteFile(filepath.Join(source, "book.txt"), []byte("book"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.startBackup(rule.ID); err != nil {
		t.Fatal(err)
	}
	a.wg.Wait()
	first := a.store.snapshot().BackupRules[0]
	if first.Copied != 1 || first.Scanned != 1 || first.Processed != 1 || first.Total != 1 {
		t.Fatal(first)
	}
	if err := a.startBackup(rule.ID); err != nil {
		t.Fatal(err)
	}
	a.wg.Wait()
	second := a.store.snapshot().BackupRules[0]
	if second.Copied != 0 || second.Skipped != 1 || second.Scanned != 1 || second.Processed != 1 || second.Total != 1 {
		t.Fatal("counts carried from previous run", second)
	}
}
