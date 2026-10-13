package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rapid115Fixture(t *testing.T) (*App, Rapid115Job) {
	t.Helper()
	a := testApp(t)
	source := t.TempDir()
	s := Storage{ID: "rapid115", Type: "115", Name: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	job := Rapid115Job{Source: source, StorageID: s.ID, Target: "99", SuccessAction: "delete", FailureAction: "skip"}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{s}
		st.Rapid115 = Rapid115Settings{Manual: job}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/files" {
			t.Fatalf("unexpected endpoint %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"state":true,"cid":"99","count":0,"offset":0,"data":[]}`))}, nil
	})}
	return a, job
}
func rapid115Put(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRapid115ExecutionOutcomesAndNoReplay(t *testing.T) {
	for _, mode := range []string{"rapid", "upload", "skip", "uncertain", "modified", "settings", "cancel", "unclean"} {
		t.Run(mode, func(t *testing.T) {
			a, job := rapid115Fixture(t)
			job.Extensions = "mp4"
			if mode == "upload" {
				job.FailureAction = "upload"
			}
			if mode == "unclean" {
				job.SuccessAction = "keep"
			}
			if err := a.store.update(func(st *State) error { st.Rapid115.Manual = job; return nil }); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(job.Source, "video.mp4")
			rapid115Put(t, file, "media")
			rapid115Put(t, filepath.Join(job.Source, "ignore.txt"), "ignore")
			calls := 0
			ctx, cancel := context.WithCancel(a.ctx)
			defer cancel()
			a.rapid115Upload = func(ctx context.Context, s Storage, parent, name string, f *os.File, size int64, allow bool) (bool, string, error) {
				calls++
				if parent != "99" || name != "video.mp4" || size != 5 {
					t.Fatal("wrong upload arguments")
				}
				if _, err := rapid115Digest(ctx, f); err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "upload":
					if !allow {
						t.Fatal("upload disabled")
					}
					return false, "pick", nil
				case "skip":
					if allow {
						t.Fatal("skip allowed upload")
					}
					return false, "", nil
				case "uncertain":
					return false, "", errors.New("uncertain secret upstream")
				case "modified":
					rapid115Put(t, file, "changed media")
				case "settings":
					_ = a.store.update(func(st *State) error { st.Storages[0].Config["cookie"] = "changed"; return nil })
				case "cancel":
					cancel()
					return false, "", ctx.Err()
				}
				return true, "pick", nil
			}
			revision := rapid115Stamp(a.store.snapshot().Rapid115)
			err := a.runRapid115(ctx, job, "manual", "手动", revision)
			if mode == "cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			records := a.store.snapshot().Rapid115Records
			if len(records) != 1 {
				t.Fatal(records)
			}
			expected := "success"
			if mode == "upload" {
				expected = "uploaded"
			}
			if mode == "skip" || mode == "uncertain" || mode == "cancel" {
				expected = "failed"
			}
			if records[0].Status != expected || strings.Contains(records[0].Message, "secret") {
				t.Fatal(records)
			}
			_, statErr := os.Stat(file)
			deleted := mode == "rapid" || mode == "upload"
			if deleted != os.IsNotExist(statErr) {
				t.Fatal("unsafe cleanup", mode, statErr)
			}
			if _, err := os.Stat(filepath.Join(job.Source, "ignore.txt")); err != nil {
				t.Fatal("filtered file removed")
			}
			if mode == "skip" || mode == "uncertain" || mode == "unclean" {
				if err := a.runRapid115(a.ctx, job, "manual", "手动", revision); err != nil {
					t.Fatal(err)
				}
				if calls != 1 {
					t.Fatal("replayed unchanged file")
				}
			}
		})
	}
}

func TestRapid115SettingsAuthPersistenceAndValidation(t *testing.T) {
	a, job := rapid115Fixture(t)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	for _, route := range []string{"/api/115-rapid", "/api/115-rapid/records", "/api/115-rapid/run"} {
		method := "GET"
		if strings.HasSuffix(route, "run") {
			method = "POST"
		}
		if w := request(t, h, method, route, map[string]bool{"confirm": true}, nil); w.Code != 401 {
			t.Fatal("unprotected", route, w.Code)
		}
	}
	config := Rapid115Settings{Manual: job, Watches: []Rapid115Watch{{Rapid115Job: job, ID: "watch1", Name: "监控", Mode: "cron", Cron: "*/5 * * * *", Enabled: true}}}
	config.Manual.Extensions = ".MP4;mp4;MKV"
	res := request(t, h, "PUT", "/api/115-rapid", config, cookie)
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.store.snapshot().Rapid115.Manual.Extensions != "mp4;mkv" || len(reopened.store.snapshot().Rapid115.Watches) != 1 {
		t.Fatal("settings lost")
	}
	module := filepath.Join(a.store.dir, "tool", "115-rapid.json")
	if _, err := os.Stat(module); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"target", "source", "mode", "cron", "success", "failure", "duplicate"} {
		invalid := normalizedRapid115(config)
		switch field {
		case "target":
			invalid.Manual.Target = "../bad"
		case "source":
			invalid.Manual.Source = "relative"
		case "mode":
			invalid.Watches[0].Mode = "bad"
		case "cron":
			invalid.Watches[0].Cron = "bad"
		case "success":
			invalid.Manual.SuccessAction = "erase"
		case "failure":
			invalid.Manual.FailureAction = "retry"
		case "duplicate":
			invalid.Watches = append(invalid.Watches, invalid.Watches[0])
		}
		if res := request(t, h, "PUT", "/api/115-rapid", invalid, cookie); res.Code != 400 {
			t.Fatal("invalid accepted", field, res.Code)
		}
	}
	if res := request(t, h, "POST", "/api/115-rapid/run", map[string]bool{"confirm": false}, cookie); res.Code != 400 {
		t.Fatal("confirmation ignored")
	}
}

func TestRapid115JournalSurvivesRestartAndSeparatesConfig(t *testing.T) {
	a, job := rapid115Fixture(t)
	record := Rapid115Record{ID: "record", Time: time.Now(), Name: "movie.mp4", Status: "running"}
	fresh, err := a.rapid115Intent("intent", record)
	if err != nil || !fresh {
		t.Fatal(err)
	}
	restarted, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil {
		t.Fatal(err)
	}
	st := restarted.store.snapshot()
	if !st.Rapid115Attempts["intent"] || len(st.Rapid115Records) != 1 || st.Rapid115Records[0].Status != "failed" {
		t.Fatal("lost interrupted intent")
	}
	if fresh, err := restarted.rapid115Intent("intent", record); err != nil || fresh {
		t.Fatal("replayed intent", err)
	}
	raw, err := os.ReadFile(filepath.Join(a.store.dir, "tool", "115-rapid.json"))
	if err != nil || strings.Contains(string(raw), "record") {
		t.Fatal("runtime leaked to config", err)
	}
	if st.Rapid115.Manual.Source != job.Source {
		t.Fatal("source not persisted")
	}
}

func TestRapid115LegacyModuleUpgradeAndMissingNewModuleRejected(t *testing.T) {
	a, _ := rapid115Fixture(t)
	s := a.store
	root, err := os.ReadFile(filepath.Join(s.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = s.decodeJSONConfig("state", root, &value); err != nil {
		t.Fatal(err)
	}
	delete(value, "rapid115Version")
	raw, err := s.encodeJSONConfig("state", value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(s.dir, "state.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	module := filepath.Join(s.dir, "tool", "115-rapid.json")
	if err = os.Remove(module); err != nil {
		t.Fatal(err)
	}
	upgraded, err := NewStore(s.dir)
	if err != nil {
		t.Fatal("legacy upgrade failed", err)
	}
	if upgraded.state.Rapid115Version != 1 {
		t.Fatal("version not upgraded")
	}
	if _, err = os.Stat(module); err != nil {
		t.Fatal("module not created", err)
	}
	if err = os.Remove(module); err != nil {
		t.Fatal(err)
	}
	if _, err = NewStore(s.dir); err == nil {
		t.Fatal("missing registered module silently recreated")
	}
}

func TestRapid115NativeMonitorProcessesNewFiles(t *testing.T) {
	a, job := rapid115Fixture(t)
	ctx, cancel := context.WithCancel(a.ctx)
	a.ctx = ctx
	defer func() { cancel(); a.wg.Wait() }()
	job.SuccessAction = "keep"
	if err := a.store.update(func(st *State) error {
		st.Rapid115.Watches = []Rapid115Watch{{Rapid115Job: job, ID: "monitor", Name: "本地", Mode: "watch", Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.rapid115Upload = func(context.Context, Storage, string, string, *os.File, int64, bool) (bool, string, error) {
		return true, "pick", nil
	}
	a.wg.Add(1)
	go a.rapid115Monitor()
	time.Sleep(1200 * time.Millisecond)
	rapid115Put(t, filepath.Join(job.Source, "new.mp4"), "video")
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		st := a.store.snapshot()
		if len(st.Rapid115Records) == 1 && st.Rapid115Records[0].Status == "success" && strings.HasPrefix(st.Rapid115Records[0].Source, "监控") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("monitor did not process filesystem event", a.store.snapshot().Rapid115Records)
}

func TestRapid115SameNameRejectAndProtectedDirectories(t *testing.T) {
	a, job := rapid115Fixture(t)
	rapid115Put(t, filepath.Join(job.Source, "a.mp4"), "media")
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"state":true,"cid":"99","count":1,"offset":0,"data":[{"fid":"1","n":"a.mp4","s":"5"}]}`))}, nil
	})}
	a.rapid115Upload = func(context.Context, Storage, string, string, *os.File, int64, bool) (bool, string, error) {
		t.Fatal("collision submitted")
		return true, "", nil
	}
	if err := a.runRapid115(a.ctx, job, "manual", "手动", rapid115Stamp(a.store.snapshot().Rapid115)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(job.Source, "a.mp4")); err != nil {
		t.Fatal(err)
	}
	job.Source = filepath.Dir(a.store.dir)
	if err := a.runRapid115(a.ctx, job, "manual", "手动", "unused"); err == nil {
		t.Fatal("system directories accepted")
	}
	job.Source = filepath.Join(a.store.dir, "tool")
	if err := a.runRapid115(a.ctx, job, "manual", "手动", "unused"); err == nil {
		t.Fatal("config subdirectory accepted")
	}
}

func TestRapid115RespectsGlobalIgnoredExtensions(t *testing.T) {
	a, job := rapid115Fixture(t)
	if err := a.store.update(func(st *State) error {
		st.TransferSettings = normalizedTransferSettings(st.TransferSettings)
		st.TransferSettings.IgnoredExtensions = []string{"mp4"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(job.Source, "ignored.mp4")
	rapid115Put(t, file, "media")
	a.rapid115Upload = func(context.Context, Storage, string, string, *os.File, int64, bool) (bool, string, error) {
		t.Fatal("globally ignored file submitted")
		return false, "", nil
	}
	if err := a.runRapid115(a.ctx, job, "manual", "手动", rapid115Stamp(a.store.snapshot().Rapid115)); err != nil {
		t.Fatal(err)
	}
	if len(a.store.snapshot().Rapid115Records) != 0 {
		t.Fatal("ignored file generated a record")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
}

func TestSimulcastRetentionCleanupAndNoUncertainReplay(t *testing.T) {
	for _, mode := range []string{"success", "changed", "uncertain"} {
		t.Run(mode, func(t *testing.T) {
			p, s, _ := simulcastFixture(t, "")
			a := p.app
			expires := time.Now().Add(-time.Hour)
			item := SimulcastTemporary{StorageID: s.ID, FileID: "22", Parent: "99", Name: "copy.mp4", Size: 5, PickCode: "pick", Revision: rapid115Stamp(s.Config), Expires: expires}
			if err := a.store.update(func(st *State) error { st.SimulcastTemporary = []SimulcastTemporary{item}; return nil }); err != nil {
				t.Fatal(err)
			}
			key, client := simulcastHash("source"), simulcastHash("client")
			p.sessions[key] = map[[32]byte]*simulcastClient{client: {storageID: s.ID, file: File{ID: item.FileID}}}
			deletes := 0
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				body := `{"state":true}`
				switch r.URL.Path {
				case "/files/get_info":
					name := item.Name
					if mode == "changed" {
						name = "changed.mp4"
					}
					raw, _ := json.Marshal(map[string]any{"state": true, "data": []any{map[string]any{"fid": "22", "cid": "99", "n": name, "s": "5", "pc": "pick"}}})
					body = string(raw)
				case "/rb/delete":
					deletes++
					if mode == "uncertain" {
						return nil, fmt.Errorf("lost response")
					}
				default:
					t.Fatalf("unexpected endpoint %s", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			err := p.cleanup(a.ctx)
			if mode == "uncertain" {
				if err == nil {
					t.Fatal("uncertain hidden")
				}
				if err := p.cleanup(a.ctx); err != nil {
					t.Fatal(err)
				}
				if deletes != 1 {
					t.Fatal("uncertain deletion replayed")
				}
				if err := p.touchTemporary(s, File{ID: item.FileID}, SimulcastConfig{RetentionHours: 12}); err == nil {
					t.Fatal("uncertain cleanup was renewed")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if mode == "success" && (deletes != 1 || len(a.store.snapshot().SimulcastTemporary) != 0 || len(p.sessions) != 0) {
				t.Fatal("cleanup failed")
			}
			if mode == "changed" && deletes != 0 {
				t.Fatal("changed file deleted")
			}
		})
	}
}

func TestSimulcastRetentionImportValidation(t *testing.T) {
	for _, hours := range []int{-1, 0, 1, 12, 8760, 8761} {
		st := State{Simulcast: map[string]SimulcastConfig{"115": {RetentionHours: hours}}}
		err := validateResourceState(&st)
		if (err != nil) != (hours < 0 || hours > 8760) {
			t.Fatalf("hours=%d: %v", hours, err)
		}
	}
}
