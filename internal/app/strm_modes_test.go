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

func strmModeFixture(t *testing.T) (*App, Storage, Task) {
	t.Helper()
	a := testApp(t)
	s := addLocal(t, a)
	task := Task{ID: "strm-mode", Name: "STRM模式", Kind: "strm", StorageID: s.ID, Source: "/",
		Target: t.TempDir(), Mode: "incremental_update", MediaExtensions: "mkv", MetadataExtensions: "nfo"}
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	return a, s, task
}

func TestSTRMTaskModesDefaultAndLegacyValidation(t *testing.T) {
	a, _, task := strmModeFixture(t)
	for _, tc := range []struct{ input, want string }{
		{"", "incremental_update"}, {"incremental_update", "incremental_update"}, {"incremental_missing", "incremental_missing"},
		{"full", "incremental_update"}, {"incremental", "incremental_missing"},
	} {
		task.Mode = tc.input
		if err := a.validateTask(&task); err != nil || task.Mode != tc.want {
			t.Fatalf("mode %q: %q, %v", tc.input, task.Mode, err)
		}
	}
	task.Mode = "full_sync"
	if a.validateTask(&task) == nil {
		t.Fatal("full sync must remain a separate confirmed reset")
	}
}

func TestSTRMTaskModesUpdateOnlyChangedAndMissingKeepsExisting(t *testing.T) {
	for _, mode := range []string{"incremental_update", "incremental_missing"} {
		t.Run(mode, func(t *testing.T) {
			a, s, task := strmModeFixture(t)
			task.Mode = mode
			writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "media")
			name := filepath.Join(task.Target, "film.strm")
			writeTest(t, name, "old link\n")
			before, _ := os.Stat(name)
			a.store.update(func(st *State) error { st.Tasks[0] = task; st.Settings.STRMOverwriteMode = "skip"; return nil })
			n, err := a.executeTask(a.ctx, task, s)
			body, readErr := os.ReadFile(name)
			if err != nil || readErr != nil {
				t.Fatal(err, readErr)
			}
			if mode == "incremental_missing" {
				if n != 0 || string(body) != "old link\n" {
					t.Fatal("missing mode rewrote a file", n, string(body))
				}
				return
			}
			if n != 1 || strings.TrimSpace(string(body)) != filepath.Join(s.Config["root"], "film.mkv") {
				t.Fatal("update mode did not refresh changed contents", n, string(body))
			}
			after, _ := os.Stat(name)
			n, err = a.executeTask(a.ctx, task, s)
			unchanged, _ := os.Stat(name)
			if err != nil || n != 0 || !os.SameFile(after, unchanged) || !after.ModTime().Equal(unchanged.ModTime()) {
				t.Fatal("unchanged STRM was rewritten", n, err, before.ModTime(), after.ModTime(), unchanged.ModTime())
			}
		})
	}
}

func TestSTRMTaskModesReconcileOnlyOwnedAndConfirmFreshSources(t *testing.T) {
	a, s, task := strmModeFixture(t)
	for _, name := range []string{"gone.mkv", "changed.mkv", "keep.mkv", "gone.nfo"} {
		writeTest(t, filepath.Join(s.Config["root"], name), "source")
	}
	if n, err := a.executeTask(a.ctx, task, s); err != nil || n != 4 {
		t.Fatal(n, err)
	}
	writeTest(t, filepath.Join(task.Target, "personal.strm"), "not owned")
	writeTest(t, filepath.Join(task.Target, "changed.strm"), "external edit")
	for _, name := range []string{"gone.mkv", "changed.mkv", "gone.nfo"} {
		if err := os.Remove(filepath.Join(s.Config["root"], name)); err != nil {
			t.Fatal(err)
		}
	}
	// An expired source must not be inferred from a stale cached directory listing.
	a.cache.clear()
	if _, err := a.executeTask(context.Background(), task, s); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gone.strm", "gone.nfo"} {
		if _, err := os.Stat(filepath.Join(task.Target, name)); !os.IsNotExist(err) {
			t.Fatal("unchanged owned stale output was not removed", name, err)
		}
	}
	for _, name := range []string{"personal.strm", "changed.strm", "keep.strm"} {
		if _, err := os.Stat(filepath.Join(task.Target, name)); err != nil {
			t.Fatal("unowned, modified or live output was removed", name, err)
		}
	}
}

func TestSTRMTaskModesMissingNeverUpdatesOrPrunes(t *testing.T) {
	a, s, task := strmModeFixture(t)
	for _, name := range []string{"gone.mkv", "film.nfo"} {
		writeTest(t, filepath.Join(s.Config["root"], name), "old")
	}
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	task.Mode = "incremental_missing"
	a.store.update(func(st *State) error { st.Tasks[0] = task; st.Settings.STRMOverwriteMode = "overwrite"; return nil })
	os.Remove(filepath.Join(s.Config["root"], "gone.mkv"))
	writeTest(t, filepath.Join(s.Config["root"], "film.nfo"), "updated")
	writeTest(t, filepath.Join(s.Config["root"], "new.mkv"), "new")
	a.cache.clear()
	if n, err := a.executeTask(a.ctx, task, s); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	body, err := os.ReadFile(filepath.Join(task.Target, "film.nfo"))
	if err != nil || string(body) != "old" {
		t.Fatal("missing mode changed metadata", string(body), err)
	}
	if _, err := os.Stat(filepath.Join(task.Target, "gone.strm")); err != nil {
		t.Fatal("missing mode pruned an old output", err)
	}
}

func TestSTRMTaskModesFreshRecheckRejectsFalseCacheDeletion(t *testing.T) {
	a, s, task := strmModeFixture(t)
	writeTest(t, filepath.Join(s.Config["root"], "live.mkv"), "source")
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	cfg := a.store.snapshot().Settings
	a.cache.put(s.ID+":"+rootOf(s), []File{}, 60, cfg)
	if n, err := a.executeTask(a.ctx, task, s); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if _, err := os.Stat(filepath.Join(task.Target, "live.strm")); err != nil {
		t.Fatal("cache omission was treated as source deletion", err)
	}
}

func TestSTRMTaskModesFailureAndCancellationNeverPrune(t *testing.T) {
	for _, scenario := range []string{"cancel", "metadata-read"} {
		t.Run(scenario, func(t *testing.T) {
			a, s, task := strmModeFixture(t)
			writeTest(t, filepath.Join(s.Config["root"], "keep.mkv"), "source")
			if _, err := a.executeTask(a.ctx, task, s); err != nil {
				t.Fatal(err)
			}
			os.Remove(filepath.Join(s.Config["root"], "keep.mkv"))
			ctx, cancel := context.WithCancel(a.ctx)
			defer cancel()
			if scenario == "cancel" {
				cancel()
			} else {
				a.cache.put(s.ID+":"+rootOf(s), []File{{ID: "/missing.nfo", Name: "missing.nfo", Size: 8}}, 60, a.store.snapshot().Settings)
			}
			if _, err := a.executeTask(ctx, task, s); err == nil {
				t.Fatal("expected the unsafe run to fail")
			}
			if _, err := os.Stat(filepath.Join(task.Target, "keep.strm")); err != nil {
				t.Fatal("failed run pruned existing output", err)
			}
		})
	}
}

func TestSTRMTaskModesPersistedManifestAndTamperFailClosed(t *testing.T) {
	a, s, task := strmModeFixture(t)
	writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "source")
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = store
	os.Remove(filepath.Join(s.Config["root"], "film.mkv"))
	a.cache.clear()
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal("persisted output tracking was not restored", err)
	}
	if _, err := os.Stat(filepath.Join(task.Target, "film.strm")); !os.IsNotExist(err) {
		t.Fatal("restart lost ownership", err)
	}
	h, err := a.beginSTRMSync(task, s, task.Target, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(h.file)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["auth"] = "tampered"
	raw, _ = json.Marshal(doc)
	writeTest(t, h.file, string(raw))
	writeTest(t, filepath.Join(s.Config["root"], "new.mkv"), "new")
	a.cache.clear()
	if _, err := a.executeTask(a.ctx, task, s); err == nil {
		t.Fatal("tampered ownership manifest accepted")
	}
	if _, err := os.Stat(filepath.Join(task.Target, "new.strm")); !os.IsNotExist(err) {
		t.Fatal("tamper failure still published a new output")
	}
}

func TestSTRMTaskModesMarkerAndOverlappingOutputsProtectCleanup(t *testing.T) {
	for _, scenario := range []string{"marker", "overlap"} {
		t.Run(scenario, func(t *testing.T) {
			a, s, task := strmModeFixture(t)
			writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "source")
			if _, err := a.executeTask(a.ctx, task, s); err != nil {
				t.Fatal(err)
			}
			os.Remove(filepath.Join(s.Config["root"], "film.mkv"))
			a.cache.clear()
			if scenario == "marker" {
				writeTest(t, filepath.Join(task.Target, strmOwnerName(task.ID)), "foreign owner")
			} else {
				other := task
				other.ID = "other-task"
				a.store.update(func(st *State) error { st.Tasks = append(st.Tasks, other); return nil })
			}
			if _, err := a.executeTask(a.ctx, task, s); err == nil {
				t.Fatal("unsafe cleanup continued")
			}
			if _, err := os.Stat(filepath.Join(task.Target, "film.strm")); err != nil {
				t.Fatal("unsafe cleanup removed a file", err)
			}
		})
	}
}

func TestSTRMTaskModesMetadataComparisonAndConfirmedReset(t *testing.T) {
	a, s, task := strmModeFixture(t)
	for _, name := range []string{"film.mkv", "film.nfo"} {
		writeTest(t, filepath.Join(s.Config["root"], name), "old")
	}
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(filepath.Join(task.Target, "film.nfo"))
	if n, err := a.executeTask(a.ctx, task, s); err != nil || n != 0 {
		t.Fatal("unchanged metadata was rewritten", n, err)
	}
	after, _ := os.Stat(filepath.Join(task.Target, "film.nfo"))
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged metadata was replaced")
	}
	writeTest(t, filepath.Join(s.Config["root"], "film.nfo"), "changed source")
	a.cache.clear()
	if n, err := a.executeTask(a.ctx, task, s); err != nil || n != 1 {
		t.Fatal("changed metadata was not refreshed", n, err)
	}
	task.Mode = "incremental_missing"
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	// Keep the old listing cached; reset must enumerate fresh sources.
	os.Remove(filepath.Join(s.Config["root"], "film.mkv"))
	writeTest(t, filepath.Join(s.Config["root"], "new.mkv"), "source")
	if err := a.startTask(task.ID, true); err != nil {
		t.Fatal("confirmed reset rejected its own ownership marker", err)
	}
	a.wg.Wait()
	for _, name := range []string{"new.strm", "film.nfo"} {
		if _, err := os.Stat(filepath.Join(task.Target, name)); err != nil {
			t.Fatal("reset missed a current source", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(task.Target, "film.strm")); !os.IsNotExist(err) {
		t.Fatal("reset reused an old cached directory", err)
	}
	saved := a.store.snapshot().Tasks[0]
	if saved.Mode != "incremental_missing" || saved.Status != "success" {
		t.Fatal("reset changed saved generation mode or failed", saved.Mode, saved.Status, saved.Message)
	}
	if _, err := os.Stat(filepath.Join(s.Config["root"], "new.mkv")); err != nil {
		t.Fatal("reset modified the source", err)
	}
}

func TestSTRMTaskModesAPIAndConfigImportValidation(t *testing.T) {
	a, _, task := strmModeFixture(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	task.ID, task.Mode = "", ""
	w := request(t, h, "POST", "/api/tasks", task, cookie)
	var created Task
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &created) != nil || created.Mode != "incremental_update" {
		t.Fatal("new task did not default to update", w.Code, w.Body.String())
	}
	for _, mode := range []string{"incremental_update", "incremental_missing"} {
		created.Mode = mode
		if w := request(t, h, "PUT", "/api/tasks/"+created.ID, created, cookie); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		store, err := NewStore(a.store.dir)
		if err != nil {
			t.Fatal(err)
		}
		a.store = store
		found := false
		for _, saved := range a.store.snapshot().Tasks {
			if saved.ID == created.ID && saved.Mode == mode {
				found = true
			}
		}
		if !found {
			t.Fatal("mode lost on restart", mode)
		}
	}
	st := a.store.snapshot()
	st.Tasks[0].Mode = "unknown"
	if validateResourceState(&st) == nil || validateResourceState(&st, true) == nil {
		t.Fatal("invalid imported task mode accepted")
	}
}

func TestSTRMTaskModesPinRootIdentityAndSourceConfiguration(t *testing.T) {
	for _, scenario := range []string{"root-replaced", "source-changed"} {
		t.Run(scenario, func(t *testing.T) {
			a, s, task := strmModeFixture(t)
			h, err := a.beginSTRMSync(task, s, filepath.Dir(task.Target), filepath.Base(task.Target))
			if err != nil {
				t.Fatal(err)
			}
			base, err := os.OpenRoot(filepath.Dir(task.Target))
			if err != nil {
				t.Fatal(err)
			}
			defer base.Close()
			defer func() {
				if h.root != nil {
					h.root.Close()
				}
			}()
			if err := h.ensure(a.ctx, base); err != nil {
				t.Fatal(err)
			}
			if scenario == "root-replaced" {
				if err := os.Rename(task.Target, task.Target+"-moved"); err != nil {
					t.Fatal(err)
				}
				writeTest(t, filepath.Join(task.Target, strmOwnerName(task.ID)), h.manifest.Token)
			} else {
				a.store.update(func(st *State) error { st.Tasks[0].Source = "/Other"; return nil })
			}
			if err := h.check(a.ctx); err == nil {
				t.Fatal("changed root identity or task configuration was accepted")
			}
		})
	}
}

func TestSTRMTaskModesDoNotAdoptIdenticalLegacyOutput(t *testing.T) {
	a, s, task := strmModeFixture(t)
	writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "source")
	name := filepath.Join(task.Target, "film.strm")
	writeTest(t, name, filepath.Join(s.Config["root"], "film.mkv")+"\n")
	fixed := time.Unix(1700000000, 0)
	os.Chtimes(name, fixed, fixed)
	if n, err := a.executeTask(a.ctx, task, s); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	os.Remove(filepath.Join(s.Config["root"], "film.mkv"))
	a.cache.clear()
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(name); err != nil || !info.ModTime().Equal(fixed) {
		t.Fatal("unregistered legacy output was adopted or rewritten", err)
	}
}

func TestSTRMTaskModesSiblingCASOutputDoesNotBlockCleanup(t *testing.T) {
	a, s, task := strmModeFixture(t)
	task.Source = "/A"
	other := task
	other.ID, other.Kind, other.Source, other.Mode, other.CASOperation = "cas-peer", "cas", "/B", "incremental", "generate"
	a.store.update(func(st *State) error { st.Tasks = []Task{task, other}; return nil })
	writeTest(t, filepath.Join(s.Config["root"], "A", "film.mkv"), "source")
	writeTest(t, filepath.Join(s.Config["root"], "B", "other.mkv"), "source")
	writeTest(t, filepath.Join(task.Target, "B", "keep.cas.strm"), "unrelated CAS output")
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(s.Config["root"], "A", "film.mkv")); err != nil {
		t.Fatal(err)
	}
	a.cache.clear()
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal("sibling CAS directory was treated as the whole target", err)
	}
	if _, err := os.Stat(filepath.Join(task.Target, "A", "film.strm")); !os.IsNotExist(err) {
		t.Fatal("owned stale STRM was not removed", err)
	}
	if _, err := os.Stat(filepath.Join(task.Target, "B", "keep.cas.strm")); err != nil {
		t.Fatal("sibling CAS output was modified", err)
	}
}
