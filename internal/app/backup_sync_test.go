package app

import (
	"context"
	"encoding/json"
	"golang.org/x/net/webdav"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func syncFixture(t *testing.T) (*App, BackupRule, string, string) {
	a, r, left, right := backupFixture(t)
	r.SyncMode = "two_way"
	r.ConflictRule = "keep_both"
	r.ConflictMarker = "conflict copy"
	r.DeleteLimit = 20
	r.TargetOnly = "copy"
	return a, r, left, right
}
func syncWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func syncRead(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
func syncRun(t *testing.T, a *App, r *BackupRule) {
	t.Helper()
	r.Scanned, r.Processed, r.Total = 0, 0, 0
	r.forceFullSync = true
	if err := a.executeBackup(context.Background(), r); err != nil {
		t.Fatal(err)
	}
}

func TestBackupSyncCreatesUpdatesAndRestoresBaseline(t *testing.T) {
	a, r, left, right := syncFixture(t)
	syncWrite(t, left, "Folder/a.txt", "one")
	syncWrite(t, right, "b.txt", "two")
	syncRun(t, a, &r)
	if syncRead(t, right, "Folder/a.txt") != "one" || syncRead(t, left, "b.txt") != "two" {
		t.Fatal("missing merge")
	}
	syncWrite(t, right, "Folder/a.txt", "target update")
	syncRun(t, a, &r)
	if syncRead(t, left, "Folder/a.txt") != "target update" {
		t.Fatal("reverse update missing")
	}
	data, err := os.ReadFile(a.syncStatePath(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	var baseline syncBaseline
	if err = a.store.decodeJSONConfig("runtime/backup-sync/"+r.ID, data, &baseline); err != nil || len(baseline.Files) != 2 || len(baseline.History) == 0 {
		t.Fatal("baseline/history missing", err)
	}
	if baseline.Files[0]["Folder/a.txt"] != baseline.Files[1]["Folder/a.txt"] {
		t.Fatal("inconsistent baseline")
	}
	configs, _ := os.ReadFile(filepath.Join(a.store.dir, "transfer", "backup.json"))
	if strings.Contains(string(configs), "syncPending") || strings.Contains(string(configs), "baseline") {
		t.Fatal("runtime in config")
	}
	baseline.Files[0]["tampered"] = "bad"
	raw, _ := json.Marshal(baseline)
	if err = os.WriteFile(a.syncStatePath(r.ID), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = a.executeBackup(context.Background(), &r); err == nil {
		t.Fatal("accepted corrupt baseline")
	}
}

func TestBackupSyncBothModifiedPreservesConflictAndHistory(t *testing.T) {
	a, r, left, right := syncFixture(t)
	syncWrite(t, left, "song.flac", "original")
	syncRun(t, a, &r)
	syncWrite(t, left, "song.flac", "left version")
	syncWrite(t, right, "song.flac", "right version")
	syncRun(t, a, &r)
	for _, dir := range []string{left, right} {
		if syncRead(t, dir, "song.flac") != "left version" {
			t.Fatal("wrong winner")
		}
		files, err := filepath.Glob(filepath.Join(dir, "song (conflict copy *).flac"))
		if err != nil || len(files) != 1 {
			t.Fatal("conflict absent", files, err)
		}
		if data, _ := os.ReadFile(files[0]); string(data) != "right version" {
			t.Fatal("lost conflict content")
		}
	}
	syncRun(t, a, &r)
	files, _ := filepath.Glob(filepath.Join(right, "song (conflict copy *).flac"))
	if len(files) != 1 {
		t.Fatal("duplicate conflict")
	}
}

func TestBackupSyncDeletionDisabledAndEnabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "retain", true: "delete"}[enabled], func(t *testing.T) {
			a, r, left, right := syncFixture(t)
			r.SyncDelete = enabled
			r.DeletionRule = "delete"
			syncWrite(t, left, "a.txt", "one")
			syncRun(t, a, &r)
			if err := os.Remove(filepath.Join(left, "a.txt")); err != nil {
				t.Fatal(err)
			}
			syncRun(t, a, &r)
			_, err := os.Stat(filepath.Join(right, "a.txt"))
			if enabled && !os.IsNotExist(err) {
				t.Fatal("deletion not propagated")
			}
			if !enabled && err != nil {
				t.Fatal("unexpected removal")
			}
		})
	}
}

func TestBackupSyncDeleteModifyConflictKeepsEditedFile(t *testing.T) {
	a, r, left, right := syncFixture(t)
	r.SyncDelete = true
	r.DeletionRule = "delete"
	syncWrite(t, left, "a.txt", "original")
	syncRun(t, a, &r)
	os.Remove(filepath.Join(left, "a.txt"))
	syncWrite(t, right, "a.txt", "edited")
	syncRun(t, a, &r)
	if syncRead(t, left, "a.txt") != "edited" || syncRead(t, right, "a.txt") != "edited" {
		t.Fatal("deleted modified file")
	}
}

func TestBackupSyncDeleteProtectionExactPlan(t *testing.T) {
	a, r, left, right := syncFixture(t)
	r.SyncDelete = true
	r.DeletionRule = "delete"
	for i := 0; i < 25; i++ {
		syncWrite(t, left, string(rune('A'+i))+".txt", "data")
	}
	syncRun(t, a, &r)
	for i := 0; i < 25; i++ {
		os.Remove(filepath.Join(left, string(rune('A'+i))+".txt"))
	}
	if err := a.executeBackup(context.Background(), &r); err == nil {
		t.Fatal("mass delete accepted")
	}
	_, err := os.Stat(filepath.Join(right, "A.txt"))
	if err != nil {
		t.Fatal("deleted before confirmation")
	}
	r.deleteApproval = a.store.snapshot().BackupRules[0].SyncPending
	if r.deleteApproval == "" {
		t.Fatal("no pending plan")
	}
	syncWrite(t, right, "new.txt", "new")
	if err = a.executeBackup(context.Background(), &r); err == nil {
		t.Fatal("stale confirmation accepted")
	}
	r.deleteApproval = a.store.snapshot().BackupRules[0].SyncPending
	syncRun(t, a, &r)
	if _, err = os.Stat(filepath.Join(right, "A.txt")); !os.IsNotExist(err) {
		t.Fatal("approved deletion missing", err)
	}
	if syncRead(t, left, "new.txt") != "new" {
		t.Fatal("unrelated new item lost")
	}
}

func TestBackupSyncTargetOnlyMarkerAndOverlap(t *testing.T) {
	a, r, left, right := syncFixture(t)
	r.TargetOnly = "keep"
	r.SyncMarker = true
	syncWrite(t, right, "target.txt", "target")
	syncRun(t, a, &r)
	if _, err := os.Stat(filepath.Join(left, "target.txt")); !os.IsNotExist(err) {
		t.Fatal("copied target-only")
	}
	if _, err := os.Stat(filepath.Join(left, ".aether-sync-owner")); err != nil {
		t.Fatal("marker absent")
	}
	syncWrite(t, right, ".aether-sync-owner", "foreign")
	if err := a.executeBackup(context.Background(), &r); err == nil {
		t.Fatal("foreign ownership accepted")
	}
	r.TargetID = r.SourceID
	r.Target = "/Folder"
	os.Mkdir(filepath.Join(left, "Folder"), 0755)
	if err := validateBackup(&r, a.store.snapshot()); err == nil {
		t.Fatal("overlap accepted")
	}
}

func TestBackupSyncCancelAndMissingReplicaDoNotWrite(t *testing.T) {
	a, r, left, right := syncFixture(t)
	syncWrite(t, left, "a.txt", "safe")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.executeBackup(ctx, &r); err == nil {
		t.Fatal("cancel ignored")
	}
	os.RemoveAll(right)
	if err := a.executeBackup(context.Background(), &r); err == nil {
		t.Fatal("missing replica accepted")
	}
	if syncRead(t, left, "a.txt") != "safe" {
		t.Fatal("source modified")
	}
}

func TestBackupOneWayDeletionRule(t *testing.T) {
	a, r, left, right := backupFixture(t)
	r.DeletionRule = "trash"
	r.DeleteLimit = 20
	syncWrite(t, left, "a.txt", "source")
	syncWrite(t, right, "extra.txt", "target only")
	syncRun(t, a, &r)
	if _, err := os.Stat(filepath.Join(right, "extra.txt")); !os.IsNotExist(err) {
		t.Fatal("extra retained")
	}
	files, _ := filepath.Glob(filepath.Join(right, ".aether-trash", "backup", "*", "extra.txt"))
	if len(files) != 1 {
		t.Fatal("trash missing")
	}
	if syncRead(t, right, "a.txt") != "source" {
		t.Fatal("source absent")
	}
}

func TestBackupSyncDirectoryRenameDeleteAndFilteredContent(t *testing.T) {
	a, r, left, right := syncFixture(t)
	r.SyncDelete, r.DeletionRule = true, "delete"
	syncWrite(t, left, "Old/sub/a.txt", "data")
	if err := os.MkdirAll(filepath.Join(left, "Empty"), 0755); err != nil {
		t.Fatal(err)
	}
	syncRun(t, a, &r)
	if err := os.Rename(filepath.Join(left, "Old"), filepath.Join(left, "New")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(left, "Empty")); err != nil {
		t.Fatal(err)
	}
	syncRun(t, a, &r)
	if syncRead(t, right, "New/sub/a.txt") != "data" {
		t.Fatal("rename not propagated")
	}
	for _, name := range []string{"Old", "Empty"} {
		if _, err := os.Stat(filepath.Join(right, name)); !os.IsNotExist(err) {
			t.Fatal("old directory retained", name, err)
		}
	}
	// An excluded file never becomes a recursive deletion victim.
	r.Extensions = "txt"
	syncRun(t, a, &r)
	syncWrite(t, right, "New/sub/keep.bin", "excluded")
	if err := os.RemoveAll(filepath.Join(left, "New")); err != nil {
		t.Fatal(err)
	}
	syncRun(t, a, &r)
	if syncRead(t, right, "New/sub/keep.bin") != "excluded" {
		t.Fatal("filtered data removed")
	}
}

func TestBackupSyncMarkerPreflightAndRepeatedTargetOnly(t *testing.T) {
	a, r, left, right := syncFixture(t)
	r.TargetOnly = "keep"
	syncWrite(t, right, "Only/a.txt", "data")
	syncRun(t, a, &r)
	syncRun(t, a, &r)
	if _, err := os.Stat(filepath.Join(left, "Only")); !os.IsNotExist(err) {
		t.Fatal("target-only folder propagated", err)
	}
	r.SyncMarker = true
	syncWrite(t, right, ".aether-sync-owner", "foreign")
	if err := a.executeBackup(context.Background(), &r); err == nil {
		t.Fatal("foreign marker accepted")
	}
	if _, err := os.Stat(filepath.Join(left, ".aether-sync-owner")); !os.IsNotExist(err) {
		t.Fatal("wrote before marker preflight", err)
	}
}

func TestBackupSyncPermanentDeleteAndHistoryRetention(t *testing.T) {
	a, r, left, right := syncFixture(t)
	r.SyncDelete, r.DeletionRule = true, "delete"
	syncWrite(t, left, "a.txt", "original")
	syncRun(t, a, &r)
	syncWrite(t, right, "a.txt", "edited")
	syncRun(t, a, &r)
	data, err := os.ReadFile(a.syncStatePath(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	var baseline syncBaseline
	if err = a.store.decodeJSONConfig("runtime/backup-sync/"+r.ID, data, &baseline); err != nil {
		t.Fatal(err)
	}
	if len(baseline.History) != 1 {
		t.Fatal("missing history")
	}
	entry := baseline.History[0]
	baseline.History[0].Created = time.Now().Add(-48 * time.Hour)
	raw, err := a.store.encodeJSONConfig("runtime/backup-sync/"+r.ID, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(a.syncStatePath(r.ID), raw, 0600); err != nil {
		t.Fatal(err)
	}
	syncRun(t, a, &r)
	if syncRead(t, left, entry.Name) != "original" {
		t.Fatal("zero retention not forever")
	}
	r.HistoryDays = 1
	syncRun(t, a, &r)
	if _, err = os.Stat(filepath.Join(left, filepath.FromSlash(entry.Name))); !os.IsNotExist(err) {
		t.Fatal("expired history not removed", err)
	}
	if err = os.Remove(filepath.Join(left, "a.txt")); err != nil {
		t.Fatal(err)
	}
	syncRun(t, a, &r)
	if _, err = os.Stat(filepath.Join(right, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("not deleted", err)
	}
	for _, dir := range []string{left, right} {
		if _, err = os.Stat(filepath.Join(dir, ".aether-trash")); !os.IsNotExist(err) {
			t.Fatal("permanent delete used trash", err)
		}
	}
}

func TestBackupSyncLocalSameMetadataStillDetectsEdits(t *testing.T) {
	a, r, left, right := syncFixture(t)
	syncWrite(t, left, "a.txt", "first")
	syncRun(t, a, &r)
	info, err := os.Stat(filepath.Join(right, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	syncWrite(t, right, "a.txt", "other")
	if err = os.Chtimes(filepath.Join(right, "a.txt"), info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	r.forceFullSync = false
	if err = a.executeBackup(context.Background(), &r); err != nil {
		t.Fatal(err)
	}
	if syncRead(t, left, "a.txt") != "other" {
		t.Fatal("metadata reuse hid local edit")
	}
}

func TestBackupSyncStagedPublishRejectsLateEdit(t *testing.T) {
	a, r, _, right := syncFixture(t)
	s := a.store.snapshot().Storages[1]
	if s.ID != r.TargetID {
		t.Fatal("fixture target changed")
	}
	syncWrite(t, right, "a.txt", "late edit")
	stage := filepath.Join(t.TempDir(), "stage")
	if err := os.WriteFile(stage, []byte("replace"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.publishSyncStaged(context.Background(), s, "/", "a.txt", stage, "digest", ""); err == nil {
		t.Fatal("overwrote late arrival")
	}
	if syncRead(t, right, "a.txt") != "late edit" {
		t.Fatal("late edit lost")
	}
}

func TestBackupSyncAPIApprovalRuntimeAndRestart(t *testing.T) {
	a, r, left, right := syncFixture(t)
	r.SyncDelete, r.DeletionRule = true, "delete"
	if err := a.store.update(func(st *State) error { st.BackupRules[0] = r; return nil }); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		syncWrite(t, left, string(rune('A'+i))+".txt", "data")
	}
	syncRun(t, a, &r)
	for i := 0; i < 25; i++ {
		if err := os.Remove(filepath.Join(left, string(rune('A'+i))+".txt")); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.executeBackup(context.Background(), &r); err == nil {
		t.Fatal("guard not triggered")
	}
	before, err := os.ReadFile(filepath.Join(a.store.dir, "transfer", "backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), "syncPending") {
		t.Fatal("pending token in config")
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.initRuntime(filepath.Join(a.dataDir, "runtime")); err != nil {
		t.Fatal(err)
	}
	if reopened.snapshot().BackupRules[0].SyncPending == "" {
		t.Fatal("pending token lost on restart")
	}
	if err = a.startBackupScheduled(r.ID); err == nil {
		t.Fatal("scheduled execution bypassed approval")
	}
	h := a.Handler(t.TempDir())
	if w := request(t, h, "POST", "/api/backup-rules/backup/confirm-deletions", nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatal("unprotected approval", w.Code)
	}
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	if w := request(t, h, "POST", "/api/backup-rules/backup/confirm-deletions", nil, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for a.store.snapshot().BackupRules[0].Status == "running" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	final := a.store.snapshot().BackupRules[0]
	if final.Status != "completed" || final.SyncPending != "" {
		t.Fatal("approved run failed", final.Message)
	}
	if _, err = os.Stat(filepath.Join(right, "A.txt")); !os.IsNotExist(err) {
		t.Fatal("approved deletion missing", err)
	}
}

func TestBackupSyncWebDAVTwoWayLifecycle(t *testing.T) {
	a, r, left, right := syncFixture(t)
	remote := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(right), LockSystem: webdav.NewMemLS()})
	defer remote.Close()
	if err := a.store.update(func(st *State) error {
		st.Storages[1].Type = "webdav"
		st.Storages[1].Config = map[string]string{"address": remote.URL, "root": "/", "deleteMode": "trash"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r.SyncMarker = true
	r.SyncDelete = true
	r.DeletionRule = "delete"
	syncWrite(t, left, "Folder/a.txt", "local")
	syncWrite(t, right, "b.txt", "remote")
	syncRun(t, a, &r)
	if syncRead(t, right, "Folder/a.txt") != "local" || syncRead(t, left, "b.txt") != "remote" {
		t.Fatal("merge failed")
	}
	syncWrite(t, right, "Folder/a.txt", "updated remote")
	syncRun(t, a, &r)
	if syncRead(t, left, "Folder/a.txt") != "updated remote" {
		t.Fatal("reverse edit missing")
	}
	if err := os.Remove(filepath.Join(left, "b.txt")); err != nil {
		t.Fatal(err)
	}
	syncRun(t, a, &r)
	if _, err := os.Stat(filepath.Join(right, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("remote delete missing", err)
	}
}
