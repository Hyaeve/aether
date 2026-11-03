package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSTRMInternalIdentityWithoutOutputMarkerAndAfterRestart(t *testing.T) {
	a, s, task := strmModeFixture(t)
	writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "media")
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(task.Target)
	if err != nil || len(files) != 1 || files[0].Name() != "film.strm" {
		t.Fatal("unexpected generated files", files, err)
	}
	h, err := a.beginSTRMSync(task, s, task.Target, "")
	if err != nil || h.manifest.RootIdentity == "" {
		t.Fatal("no internal directory identity", err)
	}
	store, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = store
	if err := os.Remove(filepath.Join(s.Config["root"], "film.mkv")); err != nil {
		t.Fatal(err)
	}
	a.cache.clear()
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal("restart lost cleanup ownership", err)
	}
	files, err = os.ReadDir(task.Target)
	if err != nil || len(files) != 0 {
		t.Fatal("deleted source retained output or marker", files, err)
	}
}

func TestSTRMInternalIdentityRejectsCopiedReplacementAcrossRuns(t *testing.T) {
	a, s, task := strmModeFixture(t)
	writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "media")
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(task.Target, "film.strm"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(task.Target, task.Target+"-original"); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(task.Target, "film.strm"), string(body))
	store, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = store
	os.Remove(filepath.Join(s.Config["root"], "film.mkv"))
	a.cache.clear()
	if _, err := a.executeTask(a.ctx, task, s); err == nil {
		t.Fatal("copied output established ownership")
	}
	if err := a.resetTaskOutput(task, s); err == nil {
		t.Fatal("reset accepted replaced directory")
	}
	got, err := os.ReadFile(filepath.Join(task.Target, "film.strm"))
	if err != nil || string(got) != string(body) {
		t.Fatal("replacement file modified", err)
	}
}

func TestSTRMInternalIdentityMigratesOnlyVerifiedLegacyMarker(t *testing.T) {
	for _, mode := range []string{"matching", "wrong-token", "missing"} {
		t.Run(mode, func(t *testing.T) {
			a, s, task := strmModeFixture(t)
			writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "media")
			if _, err := a.executeTask(a.ctx, task, s); err != nil {
				t.Fatal(err)
			}
			h, err := a.beginSTRMSync(task, s, task.Target, "")
			if err != nil {
				t.Fatal(err)
			}
			h.manifest.RootIdentity = ""
			if err := h.save(); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(task.Target, strmOwnerName(task.ID))
			if mode != "missing" {
				token := h.manifest.Token
				if mode == "wrong-token" {
					token = "foreign"
				}
				writeTest(t, marker, token)
			}
			before, err := os.ReadFile(filepath.Join(task.Target, "film.strm"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = a.executeTask(a.ctx, task, s)
			if mode == "matching" {
				if err != nil {
					t.Fatal("legacy migration failed", err)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("verified legacy marker retained", err)
				}
				h, err = a.beginSTRMSync(task, s, task.Target, "")
				if err != nil || h.manifest.RootIdentity == "" {
					t.Fatal("legacy migration not durable", err)
				}
			} else {
				if err == nil {
					t.Fatal("unverified legacy root accepted", mode)
				}
				if mode == "wrong-token" {
					got, e := os.ReadFile(marker)
					if e != nil || string(got) != "foreign" {
						t.Fatal("foreign marker deleted", e)
					}
				}
			}
			after, e := os.ReadFile(filepath.Join(task.Target, "film.strm"))
			if e != nil || string(before) != string(after) {
				t.Fatal("migration changed media output", e)
			}
		})
	}
}

func TestSTRMInternalIdentityResetRebindsWithoutMarker(t *testing.T) {
	a, s, task := strmModeFixture(t)
	writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "media")
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(task.Target, "private.txt"), "confirmed reset owns this library")
	if err := a.startTask(task.ID, true); err != nil {
		t.Fatal(err)
	}
	a.wg.Wait()
	for _, current := range a.store.snapshot().Tasks {
		if current.ID == task.ID && current.Status != "success" {
			t.Fatal(current.Status, current.Message)
		}
	}
	files, err := os.ReadDir(task.Target)
	if err != nil || len(files) != 1 || files[0].Name() != "film.strm" {
		t.Fatal("reset generated unexpected files", files, err)
	}
	h, err := a.beginSTRMSync(task, s, task.Target, "")
	if err != nil || h.manifest.RootIdentity == "" {
		t.Fatal("reset failed to rebind directory", err)
	}
	if strings.Contains(h.file, task.Target) {
		t.Fatal("runtime identity written into output")
	}
}

func TestSTRMInternalIdentityLegacyResetRebuildsWithoutMarker(t *testing.T) {
	a, s, task := strmModeFixture(t)
	writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "media")
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	h, err := a.beginSTRMSync(task, s, task.Target, "")
	if err != nil {
		t.Fatal(err)
	}
	h.manifest.RootIdentity = ""
	if err := h.save(); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(task.Target, strmOwnerName(task.ID)), h.manifest.Token)
	writeTest(t, filepath.Join(task.Target, "private.txt"), "confirmed legacy reset")
	if err := a.startTask(task.ID, true); err != nil {
		t.Fatal("legacy reset rejected", err)
	}
	a.wg.Wait()
	for _, current := range a.store.snapshot().Tasks {
		if current.ID == task.ID && current.Status != "success" {
			t.Fatal(current.Status, current.Message)
		}
	}
	files, err := os.ReadDir(task.Target)
	if err != nil || len(files) != 1 || files[0].Name() != "film.strm" {
		t.Fatal("legacy reset left an owner", files, err)
	}
	h, err = a.beginSTRMSync(task, s, task.Target, "")
	if err != nil || h.manifest.RootIdentity == "" {
		t.Fatal("legacy reset lost internal identity", err)
	}
}

func TestSTRMInternalIdentityMigratesEmptyLegacyLibrary(t *testing.T) {
	a, s, task := strmModeFixture(t)
	writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "media")
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(s.Config["root"], "film.mkv"))
	a.cache.clear()
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	h, err := a.beginSTRMSync(task, s, task.Target, "")
	if err != nil || len(h.manifest.Files) != 0 {
		t.Fatal(err)
	}
	h.manifest.RootIdentity = ""
	if err := h.save(); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(task.Target, strmOwnerName(task.ID)), h.manifest.Token)
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal("empty legacy library migration failed", err)
	}
	files, err := os.ReadDir(task.Target)
	if err != nil || len(files) != 0 {
		t.Fatal("empty library retained legacy owner", files, err)
	}
	h, err = a.beginSTRMSync(task, s, task.Target, "")
	if err != nil || h.manifest.RootIdentity == "" {
		t.Fatal("empty legacy identity not persisted", err)
	}
}
