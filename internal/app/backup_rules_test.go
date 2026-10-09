package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func backupFixture(t *testing.T) (*App, BackupRule, string, string) {
	t.Helper()
	a := testApp(t)
	source, target := t.TempDir(), t.TempDir()
	if err := a.store.initModules(); err != nil {
		t.Fatal(err)
	}
	rule := BackupRule{ID: "backup", Name: "Books", Enabled: true, SourceID: "source", Source: "/", TargetID: "target", Target: "/", Replace: "skip"}
	err := a.store.update(func(st *State) error {
		st.Storages = []Storage{{ID: "source", Name: "Source", Enabled: true, Type: "local", Config: map[string]string{"root": source}}, {ID: "target", Name: "Target", Enabled: true, Type: "local", Config: map[string]string{"root": target}}}
		st.BackupRules = []BackupRule{rule}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, rule, source, target
}

func TestBackupCopiesStructureFilterSkipAndOverwrite(t *testing.T) {
	a, rule, source, target := backupFixture(t)
	if err := os.MkdirAll(filepath.Join(source, "Books", "Empty"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"Books/a.epub": "new book", "Books/b.txt": "ignored", "Books/secret.epub": "excluded"} {
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(target, "Books"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "Books", "a.epub"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	rule.Extensions, rule.Exclude = "epub", "secret"
	if err := validateBackup(&rule, a.store.snapshot()); err != nil {
		t.Fatal(err)
	}
	if err := a.executeBackup(context.Background(), &rule); err != nil {
		t.Fatal(err)
	}
	if rule.Copied != 0 || rule.Skipped != 3 || rule.Scanned != 3 {
		t.Fatal(rule)
	}
	data, _ := os.ReadFile(filepath.Join(target, "Books", "a.epub"))
	if string(data) != "old" {
		t.Fatal("skip overwritten")
	}
	if info, err := os.Stat(filepath.Join(target, "Books", "Empty")); err != nil || !info.IsDir() {
		t.Fatal("empty dir missing", err)
	}
	rule.Replace = "overwrite"
	rule.Scanned, rule.Skipped, rule.Copied = 0, 0, 0
	if err := a.executeBackup(context.Background(), &rule); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(target, "Books", "a.epub"))
	if string(data) != "new book" || rule.Copied != 1 {
		t.Fatal("overwrite failed", rule)
	}
	if _, err := os.Stat(filepath.Join(target, "Books", "b.txt")); !os.IsNotExist(err) {
		t.Fatal("filter ignored")
	}
	if _, err := os.Stat(filepath.Join(source, "Books", "a.epub")); err != nil {
		t.Fatal("source deleted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.executeBackup(ctx, &rule); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestBackupAPIAndEncryptedModule(t *testing.T) {
	a, rule, _, _ := backupFixture(t)
	h := a.Handler(t.TempDir())
	if request(t, h, "GET", "/api/backup-rules", nil, nil).Code != 401 {
		t.Fatal("unprotected")
	}
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	rule.ID = ""
	rule.Name = "Encrypted rule"
	rule.Cron = "0 2 * * *"
	w := request(t, h, "POST", "/api/backup-rules", rule, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var rules []BackupRule
	w = request(t, h, "GET", "/api/backup-rules", nil, cookie)
	if err := json.Unmarshal(w.Body.Bytes(), &rules); err != nil || len(rules) != 2 || rules[1].NextRun.IsZero() {
		t.Fatal(w.Body.String())
	}
	rule = rules[1]
	rule.Cron = ""
	if w = request(t, h, "PUT", "/api/backup-rules/"+rule.ID, rule, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if !a.store.snapshot().BackupRules[1].NextRun.IsZero() {
		t.Fatal("empty cron scheduled")
	}
	if w = request(t, h, "POST", "/api/backup-rules/"+rule.ID+"/run", nil, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	a.wg.Wait()
	if a.store.snapshot().BackupRules[1].Status != "completed" {
		t.Fatal(a.store.snapshot().BackupRules[1])
	}
	st := a.store.snapshot()
	file := filepath.Join(a.store.dir, "transfer", "backup."+st.Modules["transfer/backup"]+".enc")
	raw, err := os.ReadFile(file)
	if err != nil || bytes.Contains(raw, []byte("Encrypted rule")) {
		t.Fatal("not encrypted", err)
	}
	reloaded, err := NewStore(a.store.dir)
	if err != nil || len(reloaded.snapshot().BackupRules) != 2 {
		t.Fatal("restore failed", err)
	}
	if w = request(t, h, "DELETE", "/api/backup-rules/"+rule.ID, nil, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}

func TestBackupRejectsOverlapInvalidFiltersAndSchedule(t *testing.T) {
	a, rule, source, _ := backupFixture(t)
	if err := os.Mkdir(filepath.Join(source, "inside"), 0755); err != nil {
		t.Fatal(err)
	}
	st := a.store.snapshot()
	st.Storages[1].Config["root"] = source
	rule.Target = "/inside"
	if err := validateBackup(&rule, st); err == nil {
		t.Fatal("overlap accepted")
	}
	st = a.store.snapshot()
	rule.Target = "/"
	rule.Cron = "invalid"
	if err := validateBackup(&rule, st); err == nil {
		t.Fatal("bad cron")
	}
	rule.Cron = ""
	rule.Extensions = "mp4/../"
	if err := validateBackup(&rule, st); err == nil {
		t.Fatal("bad extension")
	}
	rule.Extensions = ""
	rule.MinSize = 20
	rule.MaxSize = 10
	if err := validateBackup(&rule, st); err == nil {
		t.Fatal("bad range")
	}
	rule.MinSize, rule.MaxSize = 0, 0
	rule.Enabled = false
	if !backupNext(rule, time.Now()).IsZero() {
		t.Fatal("disabled scheduled")
	}
	rule = st.BackupRules[0]
	rule.Enabled = false
	if err := a.store.update(func(st *State) error { st.BackupRules[0] = rule; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.startBackup(rule.ID); err == nil {
		t.Fatal("disabled executed")
	}
}

func TestBackupModuleLegacyMigrationAndMissingNewModuleRejected(t *testing.T) {
	a, _, _, _ := backupFixture(t)
	st := a.store.snapshot()
	// Construct an actual v0.3.9 encrypted root, without the new module version.
	st.ModuleVersion = 0
	delete(st.Modules, "transfer/backup")
	st.BackupRules = nil
	st.Storages, st.Tasks, st.Automations, st.Links, st.Mounts, st.DAVUsers = nil, nil, nil, nil, nil, nil
	st.Settings = Settings{}
	write := func() {
		plain, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		nonce := make([]byte, a.store.aead.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(a.store.dir, "state.enc"), a.store.aead.Seal(nonce, nonce, plain, nil)); err != nil {
			t.Fatal(err)
		}
	}
	write()
	reloaded, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal("old schema rejected", err)
	}
	if reloaded.snapshot().Modules["transfer/backup"] == "" || len(reloaded.snapshot().Storages) != 2 {
		t.Fatal("migration incomplete")
	}
	st.ModuleVersion = 2
	write()
	if _, err := NewStore(a.store.dir); err == nil {
		t.Fatal("missing new module silently recreated")
	}
}

func TestBackupStopAndBusyMutation(t *testing.T) {
	a, rule, _, _ := backupFixture(t)
	if err := a.store.update(func(st *State) error {
		st.Storages[0].Type = "openlist"
		st.Storages[0].Config = map[string]string{"address": "https://backup.invalid", "token": "test"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	original := apiClient
	t.Cleanup(func() { apiClient = original })
	started := make(chan struct{})
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	if w := request(t, h, "POST", "/api/backup-rules/backup/run", nil, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("backup not started")
	}
	if w := request(t, h, "PUT", "/api/backup-rules/backup", rule, cookie); w.Code != 409 {
		t.Fatal("running edit accepted", w.Code)
	}
	if w := request(t, h, "DELETE", "/api/backup-rules/backup", nil, cookie); w.Code != 409 {
		t.Fatal("running deletion accepted", w.Code)
	}
	if w := request(t, h, "POST", "/api/backup-rules/backup/stop", nil, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	a.wg.Wait()
	if a.store.snapshot().BackupRules[0].Status != "stopped" {
		t.Fatal(a.store.snapshot().BackupRules[0])
	}
}

func TestBackupShortReadDoesNotPublishOrRetainStaging(t *testing.T) {
	a, _, _, targetRoot := backupFixture(t)
	source := Storage{ID: "source", Type: "webdav", Enabled: true, Config: map[string]string{"address": "https://backup.invalid", "username": "reader", "password": "secret"}}
	if err := a.store.update(func(st *State) error { st.Storages[0] = source; return nil }); err != nil {
		t.Fatal(err)
	}
	target := a.store.snapshot().Storages[1]
	original := apiClient
	t.Cleanup(func() { apiClient = original })
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("unexpected request", r.Method)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("short")), Request: r}, nil
	})}
	err := a.copyBackupFile(context.Background(), source, target, File{ID: "/book.epub", Name: "book.epub", Size: 100}, "/", "book.epub", "skip")
	if err == nil {
		t.Fatal("short read published")
	}
	if _, err := os.Stat(filepath.Join(targetRoot, "book.epub")); !os.IsNotExist(err) {
		t.Fatal("target appeared")
	}
	files, err := os.ReadDir(filepath.Join(a.dataDir, "cache", "backup"))
	if err != nil || len(files) != 0 {
		t.Fatal("staging leaked", err)
	}
	a.transfers.mu.Lock()
	defer a.transfers.mu.Unlock()
	for _, entry := range a.transfers.items {
		if entry.Status != "failed" {
			t.Fatal("short read marked successful", entry)
		}
	}
}

func TestBackupRemoteNestedTargetStopsBeforeAnyWrite(t *testing.T) {
	a, rule, _, _ := backupFixture(t)
	if err := a.store.update(func(st *State) error {
		for i := range st.Storages {
			st.Storages[i].Type = "openlist"
			st.Storages[i].Config = map[string]string{"address": "https://backup.invalid", "token": "same-account"}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rule.Source = "/media"
	rule.Target = "/media/copies"
	original := apiClient
	t.Cleanup(func() { apiClient = original })
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/api/fs/list" {
			t.Fatal("write attempted", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":200,"data":{"content":[{"name":"copies","is_dir":true}],"total":1}}`)), Request: r}, nil
	})}
	if err := a.executeBackup(context.Background(), &rule); err == nil || !strings.Contains(err.Error(), "源目录内") {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("continued after overlap", calls)
	}
}
