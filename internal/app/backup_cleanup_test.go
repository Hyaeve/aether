package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupCompletionDeleteOnlyVerifiedCopies(t *testing.T) {
	for _, policy := range []string{"keep", "delete_source", "delete_source_dir"} {
		t.Run(policy, func(t *testing.T) {
			a, r, source, target := backupFixture(t)
			r.CompletionRule = policy
			os.MkdirAll(filepath.Join(source, "Books"), 0755)
			os.MkdirAll(filepath.Join(source, "Empty"), 0755)
			os.WriteFile(filepath.Join(source, "Books", "a.txt"), []byte("verified content"), 0644)
			if err := a.executeBackup(context.Background(), &r); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(target, "Books", "a.txt"))
			if err != nil || string(data) != "verified content" {
				t.Fatal(err)
			}
			_, err = os.Stat(filepath.Join(source, "Books", "a.txt"))
			if policy == "keep" && err != nil || policy != "keep" && !os.IsNotExist(err) {
				t.Fatal("incorrect cleanup", err)
			}
			_, err = os.Stat(filepath.Join(source, "Empty"))
			if policy == "delete_source_dir" && !os.IsNotExist(err) {
				t.Fatal("empty dir retained", err)
			}
			if _, err = os.Stat(source); err != nil {
				t.Fatal("source root removed")
			}
		})
	}
}

func TestBackupCompletionSkippedAndFailedTargetsKeepSource(t *testing.T) {
	a, r, source, target := backupFixture(t)
	r.CompletionRule = "delete_source"
	os.WriteFile(filepath.Join(source, "a.txt"), []byte("source"), 0644)
	os.WriteFile(filepath.Join(target, "a.txt"), []byte("different"), 0644)
	if err := a.executeBackup(context.Background(), &r); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(source, "a.txt")); err != nil {
		t.Fatal("skipped source lost")
	}
	os.Mkdir(filepath.Join(target, "b.txt"), 0755)
	os.WriteFile(filepath.Join(source, "b.txt"), []byte("b"), 0644)
	if a.executeBackup(context.Background(), &r) == nil {
		t.Fatal("conflict accepted")
	}
	if _, err := os.Stat(filepath.Join(source, "a.txt")); err != nil {
		t.Fatal("partial failure deleted source")
	}
}

func TestBackupCleanupRejectsChangedTargetOrSource(t *testing.T) {
	a, r, source, target := backupFixture(t)
	os.WriteFile(filepath.Join(source, "a.txt"), []byte("source"), 0644)
	os.WriteFile(filepath.Join(target, "a.txt"), []byte("changed target"), 0644)
	st := a.store.snapshot()
	sum := sha256.Sum256([]byte("source"))
	items := []backupCleanupEntry{{backupEntry{st.Storages[0], File{Name: "a.txt", ID: "/a.txt", Size: 6}, "a.txt", "/"}, hex.EncodeToString(sum[:]), []backupCleanupTarget{{st.Storages[1], "/", "a.txt"}}}}
	if a.cleanupBackup(context.Background(), &r, items, nil) == nil {
		t.Fatal("changed target accepted")
	}
	os.WriteFile(filepath.Join(target, "a.txt"), []byte("source"), 0644)
	os.WriteFile(filepath.Join(source, "a.txt"), []byte("changed source"), 0644)
	if a.cleanupBackup(context.Background(), &r, items, nil) == nil {
		t.Fatal("changed source accepted")
	}
	if _, err := os.Stat(filepath.Join(source, "a.txt")); err != nil {
		t.Fatal("source lost")
	}
}

func TestBackupMonitorFingerprintAndValidation(t *testing.T) {
	a, r, source, _ := backupFixture(t)
	r.MonitorEnabled = true
	st := a.store.snapshot()
	before, err := backupFingerprint(context.Background(), r, st)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(source, "a.txt"), []byte("new"), 0644)
	after, err := backupFingerprint(context.Background(), r, st)
	if err != nil || before == after {
		t.Fatal("no change detected", err)
	}
	os.MkdirAll(filepath.Join(source, ".aether-trash"), 0700)
	os.WriteFile(filepath.Join(source, ".aether-trash", "ignored"), []byte("x"), 0600)
	ignored, err := backupFingerprint(context.Background(), r, st)
	if err != nil || ignored != after {
		t.Fatal("internal files triggered monitor", err)
	}
	st.Storages[0].Type = "115"
	if validateBackupOptions(&r, st) == nil {
		t.Fatal("remote filesystem monitoring accepted")
	}
}
