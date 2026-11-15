package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func Test115STRMFullSyncEmptyResultChecksCancellationAndConfig(t *testing.T) {
	for _, change := range []string{"cancel", "config"} {
		t.Run(change, func(t *testing.T) {
			a, s, task, _ := strmBulkFixture(t, 0, "")
			task.fullSyncGeneration = true
			h, err := a.beginSTRMSync(task, s, task.Target, "Show")
			if err != nil {
				t.Fatal(err)
			}
			h.lifeBefore = strmLifeCursor{ID: "old"}
			h.manifest.Life = h.lifeBefore
			h.sourcesBefore = map[string]strmLifeSource{"old": {Relative: "old.mkv", Parent: "10", Name: "old.mkv"}}
			h.sourcesPending = true
			h.manifest.Sources = map[string]strmLifeSource{}
			h.lifeAfterScan = &strmLifeCursor{ID: "new"}
			ctx, cancel := context.WithCancel(a.ctx)
			defer cancel()
			if change == "cancel" {
				cancel()
			} else if err := a.store.update(func(st *State) error { st.Tasks[0].ExcludeDirs = "changed"; return nil }); err != nil {
				t.Fatal(err)
			}
			if err := h.finish(ctx, true); err == nil {
				t.Fatal("empty full sync committed after cancellation or configuration change")
			}
			h, err = a.beginSTRMSync(a.store.snapshotWithLogLimit(0).Tasks[0], s, task.Target, "Show")
			if err != nil || h.manifest.Life.ID != "old" || h.manifest.Sources["old"].Name != "old.mkv" {
				t.Fatal("interrupted empty scan lost its previous cursor or source index", err)
			}
		})
	}
}

func TestSTRMFullSyncOnlyUpsertsFilteredOutputs(t *testing.T) {
	for _, mode := range []string{strmUpdate, strmMissing} {
		t.Run(mode, func(t *testing.T) {
			a, s, task := strmModeFixture(t)
			task.MediaExtensions, task.MetadataExtensions = "mkv;mp4;avi", "nfo;jpg"
			task.CascadeDelete, task.CascadeExtensions = true, "nfo;jpg"
			zero := 0
			task.DeleteThreshold = &zero
			if err := a.store.update(func(st *State) error { st.Tasks[0] = task; return nil }); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"keep.mkv", "keep.nfo", "gone.mkv", "gone.nfo", "filtered.mkv", "filtered.nfo", "typed.mp4", "media.avi", "art.jpg", "Skip/film.mkv"} {
				writeTest(t, filepath.Join(s.Config["root"], name), "original")
			}
			if _, err := a.executeTask(a.ctx, task, s); err != nil {
				t.Fatal(err)
			}
			// A previously interrupted cascade must also wait for a deleting operation.
			writeTest(t, filepath.Join(task.Target, "pending.nfo"), "pending companion")
			h, err := a.beginSTRMSync(task, s, task.Target, ".")
			if err != nil {
				t.Fatal(err)
			}
			h.manifest.PendingCascade = []string{"pending.strm"}
			if err := h.save(); err != nil {
				t.Fatal(err)
			}
			retained := map[string][]byte{}
			for _, name := range []string{"gone.strm", "gone.nfo", "filtered.strm", "filtered.nfo", "typed.strm", "media.strm", "art.jpg", "Skip/film.strm", "pending.nfo"} {
				retained[name], err = os.ReadFile(filepath.Join(task.Target, name))
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"gone.mkv", "gone.nfo"} {
				if err := os.Remove(filepath.Join(s.Config["root"], name)); err != nil {
					t.Fatal(err)
				}
			}
			writeTest(t, filepath.Join(s.Config["root"], "keep.nfo"), "updated metadata")
			for _, name := range []string{"new.mkv", "new.nfo", "legacy.mkv", "filtered-new.mkv", "typed-new.mp4", "media-new.avi", "new.jpg", "Skip/new.mkv"} {
				writeTest(t, filepath.Join(s.Config["root"], name), "new source")
			}
			writeTest(t, filepath.Join(task.Target, "legacy.strm"), "obsolete link")
			for _, name := range []string{"filtered.nfo", "art.jpg"} {
				writeTest(t, filepath.Join(s.Config["root"], name), "filtered update")
			}
			task.Mode, task.MediaExtensions, task.MetadataExtensions = mode, "mkv;mp4", "nfo"
			task.ExcludeDirs, task.ExcludeFiles, task.ExcludeTypes = "Skip", "filtered", "mp4"
			if err := a.store.update(func(st *State) error { st.Tasks[0] = task; return nil }); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			if err := a.startTaskWithTrigger(a.ctx, task.ID, done, "full-sync"); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			for name, original := range retained {
				body, err := os.ReadFile(filepath.Join(task.Target, name))
				if err != nil || string(body) != string(original) {
					t.Fatalf("full sync changed retained output %s: %q, %v", name, body, err)
				}
			}
			for name, expected := range map[string]string{"keep.nfo": "updated metadata", "new.nfo": "new source", "new.strm": filepath.Join(s.Config["root"], "new.mkv") + "\n", "legacy.strm": filepath.Join(s.Config["root"], "legacy.mkv") + "\n"} {
				body, err := os.ReadFile(filepath.Join(task.Target, name))
				if err != nil || string(body) != expected {
					t.Fatalf("full sync missed upsert %s: %q, %v", name, body, err)
				}
			}
			for _, name := range []string{"filtered-new.strm", "typed-new.strm", "media-new.strm", "new.jpg", "Skip/new.strm"} {
				if _, err := os.Stat(filepath.Join(task.Target, name)); !os.IsNotExist(err) {
					t.Fatal("full sync ignored a task filter", name, err)
				}
			}
			h, err = a.beginSTRMSync(task, s, task.Target, ".")
			if err != nil || len(h.manifest.PendingCascade) != 1 || h.manifest.Files["gone.strm"] == "" || h.manifest.Files["gone.nfo"] == "" {
				t.Fatal("full sync discarded retained ownership or pending cleanup", err)
			}
			if current := a.store.snapshotWithLogLimit(0).Tasks[0]; current.Mode != mode || current.STRMDeleteState != nil || current.Status != "success" {
				t.Fatal("full sync changed the mode or entered deletion protection", current)
			}
		})
	}
}

func Test115STRMFullSyncRetainsMissingAndCommitsLifeBaseline(t *testing.T) {
	for _, baselineAvailable := range []bool{true, false} {
		t.Run(map[bool]string{true: "baseline", false: "baseline-unavailable"}[baselineAvailable], func(t *testing.T) {
			a, s, task, _ := strmBulkFixture(t, 2, "")
			if _, err := a.executeTask(a.ctx, task, s); err != nil {
				t.Fatal(err)
			}
			oldName := filepath.Join(task.Target, "Show", "Season0001", "film.strm")
			original, err := os.ReadFile(oldName)
			if err != nil {
				t.Fatal(err)
			}
			h, err := a.beginSTRMSync(task, s, task.Target, "Show")
			if err != nil {
				t.Fatal(err)
			}
			h.manifest.Life.ID = "1"
			if err := h.save(); err != nil {
				t.Fatal(err)
			}
			task.Mode, task.STRMSource, task.STRMLifeSince = strmMissing, "life", time.Now().Add(-time.Minute)
			if err := a.store.update(func(st *State) error { st.Tasks[0] = task; return nil }); err != nil {
				t.Fatal(err)
			}
			base := apiClient.Transport
			apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					t.Error("full sync attempted a cloud write", r.Method, r.URL.Path)
				}
				if r.URL.Path == "/behavior/detail" {
					if !baselineAvailable {
						return jsonHTTPResponse(`{"state":false,"error":"unavailable"}`), nil
					}
					return jsonHTTPResponse(`{"state":true,"data":{"list":[{"id":3040163688862324737}]}}`), nil
				}
				if r.URL.Path == "/files" && r.URL.Query().Get("show_dir") == "0" {
					return jsonHTTPResponse(`{"state":true,"cid":"10","count":2,"offset":0,"data":[{"fid":"10000","cid":"20","n":"film.mkv","pc":"updatedpick123","s":42},{"fid":"10002","cid":"20","n":"new.mkv","pc":"newpick1234567","s":42}]}`), nil
				}
				return base.RoundTrip(r)
			})
			defer func() { apiClient.Transport = base }()
			done := make(chan error, 1)
			if err := a.startTaskWithTrigger(a.ctx, task.ID, done, "full-sync"); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(oldName)
			if err != nil || string(body) != string(original) {
				t.Fatal("115 full sync removed missing source output", err)
			}
			for name, pick := range map[string]string{"film.strm": "/updatedpick123/", "new.strm": "/newpick1234567/"} {
				body, err := os.ReadFile(filepath.Join(task.Target, "Show", "Season0000", name))
				if err != nil || !strings.Contains(string(body), pick) {
					t.Fatal("115 full sync missed current pickcode", name, string(body), err)
				}
			}
			h, err = a.beginSTRMSync(task, s, task.Target, "Show")
			if err != nil {
				t.Fatal(err)
			}
			wantCursor := "1"
			if baselineAvailable {
				wantCursor = "3040163688862324737"
			}
			if h.manifest.Life.ID != wantCursor || h.manifest.Sources["10000"].Name != "film.mkv" || h.manifest.Sources["10002"].Name != "new.mkv" || h.manifest.Sources["10001"].Name != "" || h.manifest.Files["Season0001/film.strm"] == "" {
				t.Fatal("full sync did not commit current source index and correct life cursor", h.manifest.Life, h.manifest.Sources)
			}
		})
	}
}
