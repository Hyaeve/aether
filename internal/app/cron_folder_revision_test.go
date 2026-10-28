package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestCronMinimumPolicy(t *testing.T) {
	for _, item := range []struct {
		value string
		valid bool
	}{
		{"*/14 * * * *", false}, {"*/15 * * * *", true}, {"*/16 * * * *", false},
		{"0,15,30,45 * * * *", true}, {"0,14 * * * *", false}, {"0 */3 * * *", true},
		{"59 23 * * *", true}, {"0 0 29 2 *", true}, {"bad", false},
	} {
		t.Run(item.value, func(t *testing.T) {
			_, err := cronParser.Parse(item.value)
			if (err == nil) != item.valid {
				t.Fatal(item.value, err)
			}
		})
	}
	if !nextRun(Task{Kind: "strm", Enabled: true, Cron: "*/14 * * * *"}, time.Now()).IsZero() {
		t.Fatal("short legacy task still scheduled")
	}
	a := testApp(t)
	h := a.Handler(t.TempDir())
	if w := request(t, h, "POST", "/api/cron/validate", map[string]any{"expression": "*/14 * * * *"}, nil); w.Code != 401 {
		t.Fatal("unprotected validation")
	}
	w := httptest.NewRecorder()
	a.cronPolicyAPI(w, httptest.NewRequest("POST", "/", nil))
	if w.Code != 400 {
		t.Fatal("invalid validation body accepted")
	}
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "secret"}, nil)
	cookie := setup.Result().Cookies()[0]
	for _, item := range []struct {
		expression string
		retry      bool
		valid      bool
	}{{"*/14 * * * *", false, false}, {"*/15 * * * *", false, true}, {"0 */2 * * *", true, false}, {"0 */3 * * *", true, true}} {
		response := request(t, h, "POST", "/api/cron/validate", map[string]any{"expression": item.expression, "retry": item.retry}, cookie)
		var result map[string]string
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || (result["error"] == "") != item.valid || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(response.Code, response.Body.String())
		}
	}
}

func TestFolderCleanupAgePlanAndEmptyDirectories(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	for _, days := range []int{0, 5} {
		t.Run(time.Duration(days).String(), func(t *testing.T) {
			tree := map[string][]File{
				"10": {{ID: "old", Name: "old.mkv", Created: now.AddDate(0, 0, -6)}, {ID: "new", Name: "new.mkv", Created: now.AddDate(0, 0, -1)}, {ID: "unknown", Name: "unknown.mkv"}, {ID: "20", Name: "nested", IsDir: true}},
				"20": {{ID: "child", Name: "child.mkv", Created: now.AddDate(0, 0, -7)}},
			}
			deleted := []string{}
			ops := folderCleanupOps{
				List:  func(_ context.Context, parent string) ([]File, error) { return append([]File{}, tree[parent]...), nil },
				Check: func(context.Context, string) error { return nil },
				Delete: func(_ context.Context, e folderCleanupEntry) error {
					if e.File.ID == "10" {
						t.Fatal("root deleted")
					}
					deleted = append(deleted, e.File.ID)
					for i, file := range tree[e.Parent] {
						if file.ID == e.File.ID {
							tree[e.Parent] = append(tree[e.Parent][:i], tree[e.Parent][i+1:]...)
							break
						}
					}
					return nil
				},
			}
			done, err := cleanFolderEntries(context.Background(), folderCleanupRule{Directory: "10", Days: days}, now, ops)
			expected := []string{"old", "child", "20"}
			if days == 0 {
				expected = []string{"old", "new", "unknown", "child", "20"}
			}
			if err != nil || done != len(expected) || !reflect.DeepEqual(deleted, expected) {
				t.Fatal(done, err, deleted)
			}
		})
	}
}

func TestFolderCleanupFailsClosedBeforeAndDuringWrites(t *testing.T) {
	for _, mode := range []string{"enumeration", "changed", "unknown", "cancelled", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			calls, writes := 0, 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ops := folderCleanupOps{Check: func(context.Context, string) error { return ctx.Err() }, List: func(_ context.Context, parent string) ([]File, error) {
				calls++
				if mode == "enumeration" {
					return nil, errors.New("upstream")
				}
				if mode == "cancelled" {
					cancel()
				}
				file := File{ID: "f", Name: "film.mkv", Size: 10}
				if mode == "changed" && calls > 1 {
					file.Size = 11
				}
				if mode == "duplicate" {
					return []File{file, file}, nil
				}
				return []File{file}, nil
			}, Delete: func(context.Context, folderCleanupEntry) error { writes++; return errors.New("unknown result") }}
			_, err := cleanFolderEntries(ctx, folderCleanupRule{Directory: "10"}, time.Now(), ops)
			if err == nil {
				t.Fatal("unsafe run accepted")
			}
			if mode != "unknown" && writes != 0 {
				t.Fatal("wrote before complete validation", writes)
			}
			if mode == "unknown" && writes != 1 {
				t.Fatal("ambiguous write repeated", writes)
			}
		})
	}
}

func TestRapidWatchRecordScopeAndReorderRevision(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		for i := 0; i < 35; i++ {
			watchID := "one"
			if i%2 == 0 {
				watchID = "two"
			}
			st.Rapid115Records = append(st.Rapid115Records, Rapid115Record{ID: id(), WatchID: watchID, Status: "success"})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.rapid115Records(w, httptest.NewRequest("GET", "/api/115-rapid/records?watch=one&page=999", nil))
	var page struct {
		Items                        []Rapid115Record
		Page, Pages, Total, PageSize int
	}
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || page.Total != 17 || page.Page != 2 || page.PageSize != 10 || len(page.Items) != 7 {
		t.Fatal(w.Body.String())
	}
	for _, r := range page.Items {
		if r.WatchID != "one" {
			t.Fatal("mixed monitor records")
		}
	}
	first := Rapid115Settings{Watches: []Rapid115Watch{{ID: "one"}, {ID: "two"}}}
	second := Rapid115Settings{Watches: []Rapid115Watch{{ID: "two"}, {ID: "one"}}}
	if rapid115Stamp(first) != rapid115Stamp(second) {
		t.Fatal("reordering invalidates retries")
	}
	second.Watches[0].Name = "changed"
	if rapid115Stamp(first) == rapid115Stamp(second) {
		t.Fatal("editing lost its revision")
	}
}

func TestRapidLegacyRetryRevisionMigration(t *testing.T) {
	a, job := rapid115Fixture(t)
	settings := normalizedRapid115(Rapid115Settings{Manual: job, Watches: []Rapid115Watch{
		{Rapid115Job: job, ID: "z", Name: "last", Mode: "watch", Enabled: true},
		{Rapid115Job: job, ID: "a", Name: "first", Mode: "watch", Enabled: true},
	}})
	raw, _ := json.Marshal(settings)
	sum := sha256.Sum256(raw)
	legacy := hex.EncodeToString(sum[:])
	if legacy == rapid115Stamp(settings) {
		t.Fatal("fixture did not exercise changed ordering")
	}
	next := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	if err := a.store.update(func(st *State) error {
		st.Rapid115 = settings
		st.Rapid115Retries = map[string]rapid115Retry{
			"waiting":  {WatchID: "z", Revision: legacy, RecordID: "record", Count: 2, Next: next, Digest: "digest"},
			"unknown":  {WatchID: "z", Revision: "different-configuration"},
			"inflight": {WatchID: "z", Revision: legacy},
		}
		st.Rapid115Attempts = map[string]bool{"inflight": true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil {
		t.Fatal(err)
	}
	retries := reopened.store.snapshot().Rapid115Retries
	if len(retries) != 1 || retries["waiting"].Revision != rapid115Stamp(settings) ||
		retries["waiting"].Count != 2 || !retries["waiting"].Next.Equal(next) || retries["waiting"].Digest != "digest" {
		t.Fatal("waiting retry not migrated precisely", retries)
	}
}

func TestCronImportedSchedulesUseMinimumAndLegacySchedulesRemainReadable(t *testing.T) {
	for _, kind := range []string{"task", "backup", "automation"} {
		t.Run(kind, func(t *testing.T) {
			st := State{}
			switch kind {
			case "task":
				st.Tasks = []Task{{Kind: "strm", Cron: "*/14 * * * *"}}
			case "backup":
				st.BackupRules = []BackupRule{{Cron: "*/14 * * * *"}}
			case "automation":
				st.Automations = []Automation{{Trigger: "cron", Cron: "*/14 * * * *"}}
			}
			if err := validateResourceState(&st); err == nil {
				t.Fatal("import accepted short schedule")
			}
			if err := validateResourceState(&st, true); err != nil {
				t.Fatal("legacy schedule prevented startup", err)
			}
		})
	}
}

func TestCloudSessionOpaqueRevisionTracksCredentialsNotHealth(t *testing.T) {
	a := testApp(t)
	pool := Storage{ID: "p", Name: "115", Type: "115", Enabled: true, Config: map[string]string{"cookie": "UID=123_A1;CID=private;SEID=private", "root": "0"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{pool}; return nil }); err != nil {
		t.Fatal(err)
	}
	read := func() Storage {
		t.Helper()
		w := httptest.NewRecorder()
		a.state(w, httptest.NewRequest("GET", "/", nil))
		var value struct{ Storages []Storage }
		if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil || len(value.Storages) != 1 {
			t.Fatal(w.Code, err)
		}
		s := value.Storages[0]
		if s.Config["cookie"] != "********" || s.ViewRevision == "" || s.ViewRevision == storageRevision(pool) {
			t.Fatal("private configuration or unkeyed digest exposed", s)
		}
		return s
	}
	first := read().ViewRevision
	if err := a.store.update(func(st *State) error {
		st.Storages[0].Health = &storageHealthState{State: "active", CheckedAt: time.Now()}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if read().ViewRevision != first {
		t.Fatal("periodic health polling invalidates cloud cache")
	}
	if err := a.store.update(func(st *State) error { st.Storages[0].Config["cookie"] += "x"; return nil }); err != nil {
		t.Fatal(err)
	}
	if read().ViewRevision == first {
		t.Fatal("changed private credentials retained cached rows")
	}
	if a.store.snapshot().Storages[0].ViewRevision != "" {
		t.Fatal("display revision was persisted")
	}
}

func TestQuarkLegacyShortCronNeverExecutesFromStaleRuntime(t *testing.T) {
	for _, due := range []time.Time{{}, time.Now().Add(-time.Hour)} {
		a := testApp(t)
		if err := a.store.update(func(st *State) error {
			st.QuarkLibraryCards = []quarkLibraryCard{{ID: "short", Secret: "https://pan.quark.cn/s/sample", Name: "legacy", Directory: t.TempDir(), DirectoryName: "Series", Cron: "*/14 * * * *", Enabled: true}}
			initQuarkLibraryRuntime(st)
			st.QuarkLibraryRuntime.Results["short"] = quarkLibraryResult{NextRun: due}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		a.quarkLibraryTick(time.Now())
		if !a.store.snapshot().QuarkLibraryRuntime.Results["short"].LastRun.IsZero() {
			t.Fatal("short legacy cron executed from stale next-run state")
		}
	}
}
