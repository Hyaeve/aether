package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func casPoolRevisionStorages() []Storage {
	return []Storage{
		{ID: "mobile-a", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": "test-a"}},
		{ID: "mobile-b", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": "test-b"}},
		{ID: "tianyi-a", Type: "tianyi", Enabled: true, Config: map[string]string{"username": "test", "password": "test"}},
		{ID: "outside-pool", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native"}},
	}
}

func casPoolRevisionConfig() CASPoolConfig {
	return normalizedCASPool(CASPoolConfig{
		RotationMinutes: 10,
		Mobile:          CASPoolGroup{StorageIDs: []string{"mobile-a", "mobile-b"}, RetentionHours: 24},
		Tianyi:          CASPoolGroup{StorageIDs: []string{"tianyi-a"}, RetentionHours: 48},
	})
}

func casPoolRevisionInfo() CASInfo {
	return CASInfo{Name: "movie.mkv", Size: 123, SHA256: strings.Repeat("a", 64), MD5: strings.Repeat("b", 32)}
}

func TestCASPoolRevisionValidation(t *testing.T) {
	defaults := normalizedCASPool(CASPoolConfig{})
	if defaults.Preferred != "mobile" || defaults.RotationMinutes != 720 || defaults.Mobile.RetentionHours != 12 || defaults.Tianyi.RetentionHours != 12 || defaults.Mobile.StorageIDs == nil || defaults.Tianyi.StorageIDs == nil || defaults.Configured == nil || *defaults.Configured {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}
	for _, tc := range []struct {
		name   string
		change func(*CASPoolConfig, *State)
		valid  bool
	}{
		{"empty pool", func(p *CASPoolConfig, _ *State) { *p = defaults }, true},
		{"both providers", func(*CASPoolConfig, *State) {}, true},
		{"minimum limits", func(p *CASPoolConfig, _ *State) {
			p.RotationMinutes, p.Mobile.RetentionHours, p.Tianyi.RetentionHours = 1, 1, 1
		}, true},
		{"maximum limits", func(p *CASPoolConfig, _ *State) {
			p.RotationMinutes, p.Mobile.RetentionHours, p.Tianyi.RetentionHours = 1440, 8760, 8760
		}, true},
		{"invalid preference", func(p *CASPoolConfig, _ *State) { p.Preferred = "quark" }, false},
		{"negative rotation", func(p *CASPoolConfig, _ *State) { p.RotationMinutes = -1 }, false},
		{"excessive rotation", func(p *CASPoolConfig, _ *State) { p.RotationMinutes = 1441 }, false},
		{"negative retention", func(p *CASPoolConfig, _ *State) { p.Mobile.RetentionHours = -1 }, false},
		{"excessive retention", func(p *CASPoolConfig, _ *State) { p.Tianyi.RetentionHours = 8761 }, false},
		{"duplicate mobile", func(p *CASPoolConfig, _ *State) { p.Mobile.StorageIDs = []string{"mobile-a", "mobile-a"} }, false},
		{"duplicate tianyi", func(p *CASPoolConfig, _ *State) { p.Tianyi.StorageIDs = []string{"tianyi-a", "tianyi-a"} }, false},
		{"unknown storage", func(p *CASPoolConfig, _ *State) { p.Mobile.StorageIDs = []string{"missing"} }, false},
		{"empty storage ID", func(p *CASPoolConfig, _ *State) { p.Mobile.StorageIDs = []string{""} }, false},
		{"wrong provider", func(p *CASPoolConfig, _ *State) { p.Mobile.StorageIDs = []string{"tianyi-a"} }, false},
		{"disabled storage", func(_ *CASPoolConfig, st *State) { st.Storages[0].Enabled = false }, false},
		{"remote mobile", func(_ *CASPoolConfig, st *State) { st.Storages[0].Config["mode"] = "openlist" }, false},
		{"non-native tianyi", func(_ *CASPoolConfig, st *State) { st.Storages[2].Config = map[string]string{} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool, state := casPoolRevisionConfig(), State{Storages: casPoolRevisionStorages()}
			tc.change(&pool, &state)
			if err := validateCASPool(pool, state); (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
	pool, state := casPoolRevisionConfig(), State{}
	pool.Mobile.StorageIDs, pool.Tianyi.StorageIDs = nil, nil
	for i := range 33 {
		s := casPoolRevisionStorages()[0]
		s.ID = strings.Repeat("x", i+1)
		state.Storages = append(state.Storages, s)
		pool.Mobile.StorageIDs = append(pool.Mobile.StorageIDs, s.ID)
		if i == 31 && validateCASPool(pool, state) != nil {
			t.Fatal("32 distinct storages rejected")
		}
	}
	if validateCASPool(pool, state) == nil {
		t.Fatal("33 distinct storages accepted")
	}
}

func TestCASPoolConfiguredMarkerAndEmptyPersistence(t *testing.T) {
	pristine := normalizedCASPool(CASPoolConfig{})
	if pristine.Configured == nil || *pristine.Configured || pristine.RotationMinutes != 720 {
		t.Fatalf("pristine config: %+v", pristine)
	}
	for _, legacy := range []CASPoolConfig{
		{Mobile: CASPoolGroup{StorageIDs: []string{}}, Tianyi: CASPoolGroup{StorageIDs: []string{}}},
		{RotationMinutes: 10},
	} {
		if p := normalizedCASPool(legacy); p.Configured == nil || !*p.Configured {
			t.Fatalf("legacy configured pool lost: %+v", p)
		}
	}
	a := testApp(t)
	initialStore, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	initial := initialStore.snapshot().CASPool
	if initial.Configured == nil || *initial.Configured || initial.RotationMinutes != 720 {
		t.Fatalf("unconfigured marker lost on restart: %+v", initial)
	}
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	w := request(t, h, "PUT", "/api/cas/pool", pristine, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("save empty: %d %s", w.Code, w.Body.String())
	}
	var saved CASPoolConfig
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || saved.Configured == nil || !*saved.Configured || len(saved.Mobile.StorageIDs) != 0 || len(saved.Tianyi.StorageIDs) != 0 {
		t.Fatalf("explicit empty response: %+v %v", saved, err)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	p := reopened.snapshot().CASPool
	if p.Configured == nil || !*p.Configured || len(p.Mobile.StorageIDs) != 0 || len(p.Tianyi.StorageIDs) != 0 {
		t.Fatalf("empty selections not persisted: %+v", p)
	}
}

func TestCASPoolRevisionSettingsPersistence(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error { st.Storages = casPoolRevisionStorages(); return nil }); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	if w := request(t, h, "GET", "/api/cas/pool", nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated pool response: %d", w.Code)
	}
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	pool := casPoolRevisionConfig()
	pool.Preferred, pool.RotationMinutes = "tianyi", 17
	if w := request(t, h, "PUT", "/api/cas/pool", pool, cookie); w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	w := request(t, h, "GET", "/api/cas/pool", nil, cookie)
	var got CASPoolConfig
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || !reflect.DeepEqual(got, pool) {
		t.Fatalf("read: %+v %v", got, err)
	}
	before, err := os.ReadFile(filepath.Join(a.store.dir, "task", "cas-pool.json"))
	if err != nil {
		t.Fatal(err)
	}
	invalid := pool
	invalid.Mobile.StorageIDs = []string{"outside-pool", "outside-pool"}
	if w := request(t, h, "PUT", "/api/cas/pool", invalid, cookie); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid pool: %d %s", w.Code, w.Body.String())
	}
	after, err := os.ReadFile(filepath.Join(a.store.dir, "task", "cas-pool.json"))
	if err != nil || !bytes.Equal(before, after) || !reflect.DeepEqual(a.store.snapshot().CASPool, pool) {
		t.Fatalf("rejected pool changed saved settings: %v", err)
	}
	if w := request(t, h, "POST", "/api/cas/pool", pool, cookie); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected method accepted: %d", w.Code)
	}
	now := time.Now()
	info := casPoolRevisionInfo()
	if err := a.store.update(func(st *State) error {
		st.CASTemporary = []CASTemporary{{Key: "restored-key", Feature: casFeatureKey(info), StorageID: "mobile-b", FileID: "restored-file", Ready: true, LastUsed: now, RetentionHours: 24}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || !reflect.DeepEqual(reopened.snapshot().CASPool, pool) {
		t.Fatalf("pool not recovered: %v", err)
	}
	var persisted CASPoolConfig
	if err := reopened.decodeJSONConfig("task/cas-pool", after, &persisted); err != nil || !reflect.DeepEqual(persisted, pool) {
		t.Fatalf("pool module: %+v %v", persisted, err)
	}
	if err := reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if got := reopened.snapshot().CASTemporary; len(got) != 1 || got[0].Feature != casFeatureKey(info) || !got[0].Ready {
		t.Fatalf("live feature not recovered: %+v", got)
	}
	restarted := &App{store: reopened}
	if s, hours, err := restarted.selectCASStorage(info, now); err != nil || s.ID != "mobile-b" || hours != 24 {
		t.Fatalf("recovered live feature did not beat preference: %s/%dh %v", s.ID, hours, err)
	}
}

func TestCASPoolRevisionSettingsWaitForRestore(t *testing.T) {
	a := testApp(t)
	if err := a.acquireCAS(context.Background()); err != nil {
		t.Fatal(err)
	}
	held := true
	defer func() {
		if held {
			<-a.casGate
		}
	}()
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		raw, _ := json.Marshal(normalizedCASPool(CASPoolConfig{}))
		w := httptest.NewRecorder()
		a.casPoolSettings(w, httptest.NewRequest("PUT", "/api/cas/pool", bytes.NewReader(raw)))
		finished <- w
	}()
	select {
	case <-finished:
		t.Fatal("settings published during restore")
	case <-time.After(30 * time.Millisecond):
	}
	<-a.casGate
	held = false
	select {
	case w := <-finished:
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("settings did not resume")
	}
	if err := a.acquireCAS(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { <-a.casGate }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, _ := json.Marshal(normalizedCASPool(CASPoolConfig{}))
	w := httptest.NewRecorder()
	a.casPoolSettings(w, httptest.NewRequest("PUT", "/api/cas/pool", bytes.NewReader(raw)).WithContext(ctx))
	if w.Code != http.StatusRequestTimeout {
		t.Fatal("cancelled settings accepted", w.Code)
	}
}

func TestCASPoolRevisionMissingModuleMigration(t *testing.T) {
	for _, version := range []int{0, 1} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			store := testApp(t).store
			statePath := filepath.Join(store.dir, "state.json")
			raw, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			var state State
			if err := store.decodeJSONConfig("state", raw, &state); err != nil {
				t.Fatal(err)
			}
			state.CASPoolVersion = version
			raw, err = store.encodeJSONConfig("state", state)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(statePath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			poolPath := filepath.Join(store.dir, "task", "cas-pool.json")
			if err := os.Remove(poolPath); err != nil {
				t.Fatal(err)
			}
			reopened, err := NewStore(store.dir)
			if version == 1 {
				if err == nil {
					t.Fatal("missing version 1 pool module accepted")
				}
				if _, err := os.Stat(poolPath); !os.IsNotExist(err) {
					t.Fatal("missing version 1 module silently recreated")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := normalizedCASPool(CASPoolConfig{})
			if !reflect.DeepEqual(reopened.snapshot().CASPool, want) {
				t.Fatalf("legacy defaults: %+v", reopened.snapshot().CASPool)
			}
			// An ordinary save must publish the new module and its version marker.
			if err := reopened.update(func(*State) error { return nil }); err != nil {
				t.Fatal(err)
			}
			raw, err = os.ReadFile(poolPath)
			if err != nil {
				t.Fatal(err)
			}
			var pool CASPoolConfig
			if err := reopened.decodeJSONConfig("task/cas-pool", raw, &pool); err != nil || !reflect.DeepEqual(pool, want) {
				t.Fatalf("upgrade module: %+v %v", pool, err)
			}
			raw, err = os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if err := reopened.decodeJSONConfig("state", raw, &state); err != nil || state.CASPoolVersion != 1 {
				t.Fatalf("upgrade marker=%d: %v", state.CASPoolVersion, err)
			}
			if _, err := NewStore(store.dir); err != nil {
				t.Fatalf("upgraded store cannot reopen: %v", err)
			}
		})
	}
}

func TestCASPoolRevisionSelection(t *testing.T) {
	a := testApp(t)
	now := time.Unix(1800000000, 0).UTC()
	for _, tc := range []struct {
		name   string
		change func(*State, *CASInfo)
		at     time.Time
		want   string
		hours  int
	}{
		{name: "first window", at: now, want: "mobile-a", hours: 24},
		{name: "before boundary", at: now.Add(10*time.Minute - time.Nanosecond), want: "mobile-a", hours: 24},
		{name: "next window", at: now.Add(10 * time.Minute), want: "mobile-b", hours: 24},
		{name: "rotation wraps", at: now.Add(20 * time.Minute), want: "mobile-a", hours: 24},
		{name: "tianyi preference", change: func(st *State, _ *CASInfo) { st.CASPool.Preferred = "tianyi" }, want: "tianyi-a", hours: 48},
		{name: "MD5 only falls back", change: func(_ *State, info *CASInfo) { info.SHA256 = "" }, want: "tianyi-a", hours: 48},
		{name: "SHA256 only falls back", change: func(st *State, info *CASInfo) { st.CASPool.Preferred, info.MD5 = "tianyi", "" }, want: "mobile-a", hours: 24},
		{name: "disabled account", change: func(st *State, _ *CASInfo) { st.Storages[0].Enabled = false }, want: "mobile-b", hours: 24},
		{name: "removed account", change: func(st *State, _ *CASInfo) { st.Storages = st.Storages[1:] }, want: "mobile-b", hours: 24},
		{name: "non-native account", change: func(st *State, _ *CASInfo) { st.Storages[0].Config["mode"] = "openlist" }, want: "mobile-b", hours: 24},
		{name: "first ordered native account is mandatory", change: func(st *State, _ *CASInfo) { st.CASPool.Mobile.StorageIDs = []string{"tianyi-a"} }, want: "mobile-a", hours: 24},
		{name: "empty preferred group selects mandatory first", change: func(st *State, _ *CASInfo) { st.CASPool.Mobile.StorageIDs = nil }, want: "mobile-a", hours: 24},
		{name: "empty pool selects mandatory first", change: func(st *State, _ *CASInfo) { st.CASPool = normalizedCASPool(CASPoolConfig{}) }, want: "mobile-a", hours: 12},
		{name: "invalid hash", change: func(_ *State, info *CASInfo) { info.SHA256 = "bad" }},
		{name: "invalid saved rotation", change: func(st *State, _ *CASInfo) { st.CASPool.RotationMinutes = -1 }},
		{name: "cooldown account", change: func(st *State, _ *CASInfo) {
			st.Storages[0].Health = &storageHealthState{Stamp: storageRevision(st.Storages[0]), State: "cooldown", NextCheck: now.Add(time.Hour)}
		}, want: "mobile-b", hours: 24},
		{name: "expired credentials", change: func(st *State, _ *CASInfo) {
			st.Storages[0].Health = &storageHealthState{Stamp: storageRevision(st.Storages[0]), State: "expired", NextCheck: now.Add(-time.Hour)}
		}, want: "mobile-b", hours: 24},
		{name: "expired cooldown is eligible", change: func(st *State, _ *CASInfo) {
			st.Storages[0].Health = &storageHealthState{Stamp: storageRevision(st.Storages[0]), State: "cooldown", NextCheck: now}
		}, want: "mobile-a", hours: 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := casPoolRevisionInfo()
			if err := a.store.update(func(st *State) error {
				st.Storages, st.CASPool, st.CASTemporary = casPoolRevisionStorages(), casPoolRevisionConfig(), nil
				if tc.change != nil {
					tc.change(st, &info)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			at := tc.at
			if at.IsZero() {
				at = now
			}
			s, hours, err := a.selectCASStorage(info, at)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("unexpected selection: %s", s.ID)
				}
			} else if err != nil || s.ID != tc.want || hours != tc.hours {
				t.Fatalf("got %s/%dh, want %s/%dh: %v", s.ID, hours, tc.want, tc.hours, err)
			}
		})
	}
}

func TestCASPoolRevisionLiveReuseBeforePreference(t *testing.T) {
	a := testApp(t)
	now := time.Unix(1800000000, 0).UTC()
	for _, tc := range []struct {
		name   string
		change func(*State, *CASInfo)
		want   string
		hours  int
	}{
		{"live account beats rotation", func(*State, *CASInfo) {}, "mobile-b", 24},
		{"live fallback beats preferred mobile", func(st *State, _ *CASInfo) { st.CASTemporary[0].StorageID = "tianyi-a" }, "tianyi-a", 48},
		{"live mobile beats preferred tianyi", func(st *State, _ *CASInfo) { st.CASPool.Preferred = "tianyi" }, "mobile-b", 24},
		{"hash case and old provider do not bind", func(_ *State, info *CASInfo) {
			info.SHA256, info.MD5, info.Provider = strings.ToUpper(info.SHA256), strings.ToUpper(info.MD5), "quark"
		}, "mobile-b", 24},
		{"unready ignored", func(st *State, _ *CASInfo) { st.CASTemporary[0].Ready = false }, "mobile-a", 24},
		{"expired ignored", func(st *State, _ *CASInfo) { st.CASTemporary[0].LastUsed = now.Add(-time.Hour) }, "mobile-a", 24},
		{"outside pool ignored", func(st *State, _ *CASInfo) { st.CASTemporary[0].StorageID = "outside-pool" }, "mobile-a", 24},
		{"unbound account ignored", func(st *State, _ *CASInfo) { st.CASPool.Mobile.StorageIDs = []string{"mobile-a"} }, "mobile-a", 24},
		{"disabled live account ignored", func(st *State, _ *CASInfo) { st.Storages[1].Enabled = false }, "mobile-a", 24},
		{"incompatible live account ignored", func(_ *State, info *CASInfo) { info.SHA256 = "" }, "tianyi-a", 48},
		{"different name ignored", func(_ *State, info *CASInfo) { info.Name = "other.mkv" }, "mobile-a", 24},
		{"different size ignored", func(_ *State, info *CASInfo) { info.Size++ }, "mobile-a", 24},
		{"different hash ignored", func(_ *State, info *CASInfo) { info.MD5 = strings.Repeat("c", 32) }, "mobile-a", 24},
		{"blocked live account ignored", func(st *State, _ *CASInfo) {
			st.Storages[1].Health = &storageHealthState{Stamp: storageRevision(st.Storages[1]), State: "cooldown", NextCheck: now.Add(time.Hour)}
		}, "mobile-a", 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := casPoolRevisionInfo()
			if err := a.store.update(func(st *State) error {
				st.Storages, st.CASPool = casPoolRevisionStorages(), casPoolRevisionConfig()
				st.CASTemporary = []CASTemporary{{Key: "existing", Feature: casFeatureKey(info), StorageID: "mobile-b", FileID: "restored", Ready: true, LastUsed: now, RetentionHours: 1}}
				tc.change(st, &info)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			s, hours, err := a.selectCASStorage(info, now)
			if err != nil || s.ID != tc.want || hours != tc.hours {
				t.Fatalf("got %s/%dh, want %s/%dh: %v", s.ID, hours, tc.want, tc.hours, err)
			}
		})
	}
}

func TestCASPoolRevisionImportTreeAndRuntimeCounters(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	info := casPoolRevisionInfo()
	info.Provider, info.PlaybackURL = "mobile", "https://old.example/stream/obsolete"
	info.SHA256, info.MD5 = strings.ToUpper(info.SHA256), strings.ToUpper(info.MD5)
	info.SliceMD5, info.SliceSize = strings.Repeat("C", 32), 10<<20
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]string{
		"films/raw.mkv.cas":          string(raw),
		"series/season/base64.cas":   base64.StdEncoding.EncodeToString(raw),
		"series/season/unpadded.cas": base64.RawStdEncoding.EncodeToString(raw),
		"music/pointer.mkv.cas.strm": a.signedStreamURL(streamClaim{Storage: "old-account", File: "old-file", TaskID: "old-task", RetentionHours: 99, CAS: &info, Redirect: true}),
		"broken/invalid.cas":         "not a CAS feature",
		"broken/invalid.cas.strm":    "https://old.example/not-a-stream",
		"ignored.txt":                "not media",
	}
	for name, content := range fixtures {
		writeTest(t, filepath.Join(s.Config["root"], filepath.FromSlash(name)), content)
	}
	retained := "iso"
	task := Task{ID: "portable-import", Name: "portable import", Kind: "cas", StorageID: s.ID, Source: "/", Target: "portable", Mode: "full", RetainedExtensions: &retained}
	if err := a.validateTask(&task); err != nil {
		t.Fatal(err)
	}
	if err := a.store.update(func(st *State) error { st.Tasks = append(st.Tasks, task); return nil }); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(a.store.dir, "task", "cas.json")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 4 {
		t.Fatalf("import: %d %v", n, err)
	}
	state := a.store.snapshot()
	if state.Tasks[0].CASGenerated != 4 || state.Tasks[0].CASFailed != 2 || len(state.CASPool.Mobile.StorageIDs) != 0 || len(state.CASPool.Tianyi.StorageIDs) != 0 || len(state.CASTemporary) != 0 {
		t.Fatalf("import runtime state: %+v", state)
	}
	for _, name := range []string{"films/raw.mkv", "series/season/base64.mkv", "series/season/unpadded.mkv", "music/pointer.mkv"} {
		content, err := os.ReadFile(filepath.Join(a.outputDir, "portable", filepath.FromSlash(name+".cas.strm")))
		if err != nil {
			t.Fatal(err)
		}
		got := generatedCASClaim(t, content)
		if got.Name != filepath.Base(name) || got.Size != info.Size || got.SHA256 != strings.ToLower(info.SHA256) || got.MD5 != strings.ToLower(info.MD5) || got.SliceMD5 != info.SliceMD5 || got.SliceSize != info.SliceSize {
			t.Fatalf("%s: %+v", name, got)
		}
	}
	outputs := 0
	if err := filepath.WalkDir(filepath.Join(a.outputDir, "portable"), func(_ string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			outputs++
		}
		return err
	}); err != nil || outputs != 4 {
		t.Fatalf("unexpected outputs: %d %v", outputs, err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("runtime counters changed task config: %v", err)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.snapshot().Tasks[0].CASGenerated != 0 || reopened.snapshot().Tasks[0].CASFailed != 0 {
		t.Fatal("runtime counters leaked into configuration")
	}
	if err := reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if got := reopened.snapshot().Tasks[0]; got.CASGenerated != 4 || got.CASFailed != 2 {
		t.Fatalf("runtime counters not recovered: %+v", got)
	}
	task.Mode = "incremental"
	if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 0 {
		t.Fatalf("incremental import: %d %v", n, err)
	}
	if got := a.store.snapshot().Tasks[0]; got.CASGenerated != 0 || got.CASFailed != 2 {
		t.Fatalf("counters accumulated across runs: %+v", got)
	}
	var runtime struct {
		Tasks map[string]struct {
			Generated int `json:"casGenerated"`
			Failed    int `json:"casFailed"`
		} `json:"tasks"`
	}
	raw, err = os.ReadFile(filepath.Join(a.store.runtimeDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.decodeJSONConfig("runtime/state", raw, &runtime); err != nil {
		t.Fatal(err)
	}
	if got, ok := runtime.Tasks[task.ID]; !ok || got.Generated != 0 || got.Failed != 2 {
		t.Fatalf("latest runtime counters: %+v", runtime.Tasks)
	}
	var config []map[string]json.RawMessage
	if err := a.store.decodeJSONConfig("task/cas", after, &config); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"casGenerated", "casFailed", "casBindingId", "retainedExtensions"} {
		if _, ok := config[0][field]; ok {
			t.Fatalf("%s stored in task config", field)
		}
	}
}

func TestCASPoolRevisionUnconfiguredPlayback(t *testing.T) {
	a := testApp(t)
	info := casPoolRevisionInfo()
	link := a.signedStreamURL(streamClaim{CAS: &info, Redirect: true})
	w := httptest.NewRecorder()
	a.Handler(t.TempDir()).ServeHTTP(w, httptest.NewRequest("GET", link, nil))
	if w.Code != http.StatusNotFound || len(a.store.snapshot().CASTemporary) != 0 {
		t.Fatalf("unconfigured playback: %d %s", w.Code, w.Body.String())
	}
}

func TestCASPoolRevisionPartialHashImport(t *testing.T) {
	for _, field := range []string{"sha256", "md5"} {
		t.Run(field, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			info := casPoolRevisionInfo()
			if field == "sha256" {
				info.MD5 = ""
			} else {
				info.SHA256 = ""
			}
			raw, _ := json.Marshal(info)
			writeTest(t, filepath.Join(s.Config["root"], "Renamed.cas"), string(raw))
			task := Task{Name: "partial", Kind: "cas", StorageID: s.ID, Source: "/", Target: "partial", Mode: "full"}
			if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 1 {
				t.Fatalf("partial import: %d %v", n, err)
			}
			output, err := os.ReadFile(filepath.Join(a.outputDir, "partial", "Renamed.mkv.cas.strm"))
			if err != nil {
				t.Fatal(err)
			}
			got := generatedCASClaim(t, output)
			if got.Name != "Renamed.mkv" || got.SHA256 != info.SHA256 || got.MD5 != info.MD5 {
				t.Fatalf("partial feature: %+v", got)
			}
		})
	}
}

func TestCASPoolRevisionImportedMediaFilters(t *testing.T) {
	for _, tc := range []struct {
		name, media, exclude string
		want                 []string
	}{
		{"media selection", "mkv", "", []string{"keep.mkv.cas.strm"}},
		{"type exclusion", "", "mp4", []string{"keep.mkv.cas.strm", "audio.mp3.cas.strm"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			for _, name := range []string{"keep.mkv", "excluded.mp4", "audio.mp3"} {
				info := casPoolRevisionInfo()
				info.Name = name
				raw, err := json.Marshal(info)
				if err != nil {
					t.Fatal(err)
				}
				writeTest(t, filepath.Join(s.Config["root"], name+".cas"), string(raw))
				writeTest(t, filepath.Join(s.Config["root"], "pointers", name+".cas.strm"), a.signedStreamURL(streamClaim{CAS: &info, Redirect: true}))
			}
			task := Task{ID: "filtered-import", Name: tc.name, Kind: "cas", StorageID: s.ID, Source: "/", Target: "filtered", Mode: "full", MediaExtensions: tc.media, ExcludeTypes: tc.exclude}
			if err := a.validateTask(&task); err != nil {
				t.Fatal(err)
			}
			if err := a.store.update(func(st *State) error { st.Tasks = append(st.Tasks, task); return nil }); err != nil {
				t.Fatal(err)
			}
			if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 2*len(tc.want) {
				t.Fatalf("filtered import: %d %v", n, err)
			}
			for _, dir := range []string{"", "pointers"} {
				entries, err := os.ReadDir(filepath.Join(a.outputDir, "filtered", dir))
				if err != nil {
					t.Fatal(err)
				}
				files := []string{}
				for _, entry := range entries {
					if !entry.IsDir() {
						files = append(files, entry.Name())
					}
				}
				if len(files) != len(tc.want) {
					t.Fatalf("%s outputs: %v", dir, files)
				}
				for _, name := range tc.want {
					if _, err := os.Stat(filepath.Join(a.outputDir, "filtered", dir, name)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if got := a.store.snapshot().Tasks[0]; got.CASGenerated != 2*len(tc.want) || got.CASFailed != 0 {
				t.Fatalf("excluded imports counted as failures: %+v", got)
			}
		})
	}
}
