package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStorageHealthBackoffAndCredentialReset(t *testing.T) {
	s := Storage{Type: "quark", Config: map[string]string{"cookie": "first"}}
	now := time.Now()
	var h *storageHealthState
	for i, want := range []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 30 * time.Minute, 24 * time.Hour} {
		h = nextStorageHealth(s, h, authStorageError(errors.New("auth"), false), now)
		if h.Attempts != i+1 || h.NextCheck.Sub(now) != want {
			t.Fatal(i, h)
		}
	}
	if h.State != "failed" || storageAuthBlocked(Storage{Type: s.Type, Config: s.Config, Health: h}, now) == nil {
		t.Fatal(h)
	}
	h = nextStorageHealth(s, h, errors.New("network"), now)
	if h.Attempts != 5 || h.State != "failed" || h.NextCheck.Sub(now) != 24*time.Hour {
		t.Fatal("network erased terminal state", h)
	}
	h = nextStorageHealth(s, h, nil, now)
	if h.Attempts != 0 || h.State != "active" || h.NextCheck.Sub(now) != 70*time.Minute {
		t.Fatal(h)
	}
	h = nextStorageHealth(s, h, authStorageError(errors.New("expired"), true), now)
	if h.State != "expired" || h.NextCheck.Sub(now) != 24*time.Hour {
		t.Fatal(h)
	}
	s.Config = map[string]string{"cookie": "second"}
	if storageAuthBlocked(Storage{Type: s.Type, Config: s.Config, Health: h}, now) != nil {
		t.Fatal("new credential blocked by old state")
	}
	h = nextStorageHealth(s, h, errors.New("network"), now)
	if h.Attempts != 0 || h.NextCheck.Sub(now) != time.Minute {
		t.Fatal(h)
	}
}

func TestStorageHealthSingleFlightAndUsagePersistence(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Name: "Q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "secret"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(a.store.dir, "storage", "storage.json")
	before, _ := os.ReadFile(configPath)
	old := apiClient
	defer func() { apiClient = old }()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var once sync.Once
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		body := `{"code":0,"status":200,"data":{"list":[]}}`
		if strings.Contains(r.URL.Path, "/member") {
			body = `{"code":0,"status":200,"data":{"total_capacity":1000,"use_capacity":250}}`
		} else {
			if r.URL.Query().Get("_size") != "1" {
				t.Error("probe enumerated whole directory")
			}
			once.Do(func() { close(started) })
			<-release
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	done := make(chan error, 2)
	go func() { done <- a.checkStorageHealth(a.ctx, s) }()
	<-started
	go func() { done <- a.checkStorageHealth(a.ctx, s) }()
	time.Sleep(30 * time.Millisecond)
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("duplicate probe/space requests", calls.Load())
	}
	current, _ := a.store.storage(s.ID)
	if current.Health == nil || current.Health.State != "active" || current.Usage == nil || current.Usage.Total != 1000 {
		t.Fatal(current)
	}
	after, _ := os.ReadFile(configPath)
	if !bytes.Equal(before, after) {
		t.Fatal("巡检修改config")
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	restored, _ := reopened.storage(s.ID)
	if restored.Usage == nil || restored.Usage.Used != 250 || restored.Health == nil || !restored.Health.NextCheck.Equal(current.Health.NextCheck) {
		t.Fatal(restored)
	}
	if storageHealthDue(restored, time.Now()) || !storageHealthDue(restored, restored.Health.NextCheck) {
		t.Fatal("restart discarded deadline")
	}
	restored.Enabled = false
	if storageHealthDue(restored, time.Now().Add(48*time.Hour)) {
		t.Fatal("disabled storage due")
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "GET", "/api/storages/q/usage", nil, cookie)
	if w.Code != 200 || calls.Load() != 2 {
		t.Fatal("saved usage fetched upstream", w.Code, calls.Load())
	}
	w = request(t, h, "GET", "/api/state", nil, cookie)
	if strings.Contains(w.Body.String(), storageRevision(s)) || !strings.Contains(w.Body.String(), `"total":1000`) {
		t.Fatal("state lost cached usage or leaked credential fingerprint")
	}
	if err = a.store.update(func(st *State) error { st.Storages[0].Config["cookie"] = "changed"; return nil }); err != nil {
		t.Fatal(err)
	}
	w = request(t, h, "GET", "/api/state", nil, cookie)
	if strings.Contains(w.Body.String(), `"total":1000`) {
		t.Fatal("old account usage survived credential change")
	}
}

func TestStorageHealthPassiveFailureCoalescesAndBlocksSource(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "source", Type: "local", Enabled: true, Config: map[string]string{"root": t.TempDir()}}
	b := Storage{ID: "binding", Type: "115", Enabled: true, Config: map[string]string{"cookie": "bad"}}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{s, b}
		st.Tasks = []Task{{ID: "task", Kind: "ed2k", StorageID: s.ID, ED2KBindingID: b.ID}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := a.recordStorageHealth(b, authStorageError(errors.New("retryable"), false)); err != nil {
			t.Fatal(err)
		}
	}
	current, _ := a.store.storage(b.ID)
	if current.Health.Attempts != 1 || current.Health.State != "cooldown" {
		t.Fatal("parallel business failures exhausted retries", current.Health)
	}
	if err := a.recordStorageHealth(s, errors.New("offline")); err != nil {
		t.Fatal(err)
	}
	if err := a.startTask("task"); err == nil {
		t.Fatal("unhealthy source allowed task start")
	}
	if err := a.recordStorageHealth(b, nil); err != nil {
		t.Fatal(err)
	}
	current, _ = a.store.storage(b.ID)
	if storageAuthBlocked(current, time.Now()) != nil || current.Status != "connected" {
		t.Fatal(current)
	}
}

func TestStorageHealthRejectsStaleProbe(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "first"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	old := apiClient
	defer func() { apiClient = old }()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-release
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":0,"status":200,"data":{"list":[]}}`)), Request: r}, nil
	})}
	done := make(chan error, 1)
	go func() { done <- a.checkStorageHealth(a.ctx, s) }()
	<-started
	if err := a.store.update(func(st *State) error { st.Storages[0].Config["cookie"] = "second"; return nil }); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "配置已更改") {
		t.Fatal(err)
	}
	current, _ := a.store.storage(s.ID)
	if current.Health != nil || current.Usage != nil || calls.Load() != 1 {
		t.Fatal("stale probe saved or queried quota", current)
	}
}

func TestTianyiHealthNetworkDoesNotRefreshToken(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "ty", Type: "tianyi", Enabled: true, Config: map[string]string{"authMode": "token", "accessToken": "old", "refreshToken": "refresh"}}
	old := apiClient
	defer func() { apiClient = old }()
	var calls atomic.Int32
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Path != "/getSessionForPC.action" {
			t.Error("network failure attempted refresh", r.URL.Path)
		}
		return nil, errors.New("offline")
	})}
	_, err := a.tianyiTokenSession(a.ctx, s, tianyiClient())
	var auth *storageAuthError
	if err == nil || errors.As(err, &auth) || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
}

func TestStorageHealthMobileProbeAndQuotaTogether(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "m", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:13900000000:token")), "userDomainId": "domain"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	old := apiClient
	defer func() { apiClient = old }()
	var calls atomic.Int32
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		body := ""
		switch r.URL.Path {
		case "/user/route/qryRoutePolicy":
			body = `{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"https://personal.yun.139.com"}]}}`
		case "/file/list":
			var payload struct {
				PageInfo struct {
					PageSize int `json:"pageSize"`
				} `json:"pageInfo"`
			}
			if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.PageInfo.PageSize != 1 {
				t.Error("probe paginated all files")
			}
			body = `{"success":true,"data":{"items":[]}}`
		case "/user/disk/quota/detail":
			body = `{"success":true,"data":{"diskSize":1024,"freeDiskSize":256}}`
		default:
			t.Error(r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	if err := a.checkStorageHealth(a.ctx, s); err != nil {
		t.Fatal(err)
	}
	current, _ := a.store.storage(s.ID)
	if calls.Load() != 3 || current.Health.State != "active" || current.Usage.Total != 1024*(1<<20) || current.Usage.Used != 768*(1<<20) {
		t.Fatal(current, calls.Load())
	}
	if a.cache.stats()["entries"] != 0 {
		t.Fatal("probe populated directory cache")
	}
}

func TestStorageHealthTianyiFilenamesAreNotAuthErrors(t *testing.T) {
	var list struct {
		Files []struct {
			Name string `json:"name"`
		} `json:"files"`
	}
	if err := tianyiDecode([]byte(`{"files":[{"name":"InvalidSessionKey.txt"}]}`), &list); err != nil || len(list.Files) != 1 {
		t.Fatal(err, list)
	}
	for _, raw := range []string{`{"errorCode":"InvalidAccessToken"}`, `<error><code>InvalidSessionKey</code></error>`} {
		var auth *storageAuthError
		if err := tianyiDecode([]byte(raw), &list); !errors.As(err, &auth) {
			t.Fatal(raw, err)
		}
	}
}

func TestStorageHealthBackupGuardsSourceAndTarget(t *testing.T) {
	for _, id := range []string{"source", "target"} {
		t.Run(id, func(t *testing.T) {
			a, rule, _, _ := backupFixture(t)
			s, _ := a.store.storage(id)
			if err := a.recordStorageHealth(s, errors.New("offline")); err != nil {
				t.Fatal(err)
			}
			if err := a.startBackup(rule.ID); err == nil {
				t.Fatal("backup started with unhealthy storage")
			}
			if len(a.backupRuns) != 0 {
				t.Fatal("blocked backup started IO")
			}
		})
	}
}

func TestStorageHealthCancelledLeaderPropagatesToWaiter(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "secret"}, Status: "connected"}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	old := apiClient
	defer func() { apiClient = old }()
	started := make(chan struct{})
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithCancel(a.ctx)
	leader, waiter := make(chan error, 1), make(chan error, 1)
	go func() { leader <- a.checkStorageHealth(ctx, s) }()
	<-started
	go func() { waiter <- a.checkStorageHealth(a.ctx, s) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	for _, ch := range []chan error{leader, waiter} {
		select {
		case err := <-ch:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("cancelled probe did not release waiter")
		}
	}
	current, _ := a.store.storage(s.ID)
	if current.Health != nil {
		t.Fatal("cancellation recorded as unhealthy")
	}
}

func TestMobileQuotaUnitsAndMalformedFallback(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "m", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:13900000000:token")), "userDomainId": "domain-123"}}
	old := apiClient
	defer func() { apiClient = old }()
	response := `{"success":true,"data":{"diskSize":1000,"freeDiskSize":750}}`
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/user/disk/quota/detail" || body["userDomainId"] != "domain-123" || r.Header.Get("Authorization") == "" {
			t.Error(r.URL, body)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response)), Request: r}, nil
	})}
	v := a.readStorageUsage(a.ctx, s)
	if v.Total != 1000*(1<<20) || v.Used != 250*(1<<20) || v.Username != "13900000000" {
		t.Fatal(v)
	}
	for _, bad := range []string{`{"success":true,"data":{"diskSize":1000,"freeDiskSize":1001}}`, `{"success":true,"data":{"diskSize":9223372036854775807,"freeDiskSize":0}}`, `{"success":false}`, `{"success":true,"data":{}}`} {
		response = bad
		v = a.readStorageUsage(a.ctx, s)
		if v.Total != 0 || v.Username == "" {
			t.Fatal(v)
		}
	}
}

func TestQuotaFailureKeepsLastSuccess(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "same"}}
	s.Usage = &storageUsageState{storageUsage: storageUsage{Total: 1000, Used: 400}, Stamp: storageRevision(s), UpdatedAt: time.Now().Add(-time.Hour)}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	old := apiClient
	defer func() { apiClient = old }()
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	v, err := a.cachedStorageUsage(context.Background(), s)
	if err != nil || v.Total != 1000 || v.Used != 400 {
		t.Fatal(v, err)
	}
	current, _ := a.store.storage(s.ID)
	if !current.Usage.UpdatedAt.Equal(s.Usage.UpdatedAt) || current.Health != nil {
		t.Fatal("quota failure changed health or success timestamp", current)
	}
}

func TestTianyiHealthProactiveRenewal(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "ty", Type: "tianyi", Enabled: true, Config: map[string]string{"authMode": "token", "accessToken": "old", "refreshToken": "refresh"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	a.tianyiSessions[s.ID] = tianyiSession{Key: "old-session", Secret: "secret", Credentials: tianyiCredentials(s), Expires: time.Now().Add(10 * time.Minute)}
	old := apiClient
	defer func() { apiClient = old }()
	var refreshes, lists atomic.Int32
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		body := `{}`
		switch r.URL.Path {
		case "/api/oauth2/refreshToken.do":
			refreshes.Add(1)
			body = `{"accessToken":"new","refreshToken":"rotated"}`
		case "/getSessionForPC.action":
			if r.URL.Query().Get("accessToken") != "new" {
				t.Error("old token used")
			}
			body = `{"sessionKey":"new-session","sessionSecret":"secret"}`
		case "/listFiles.action":
			lists.Add(1)
			if r.URL.Query().Get("pageSize") != "1" {
				t.Error("not a lightweight probe")
			}
			body = `{"fileListAO":{"fileList":[],"folderList":[]}}`
		case "/portal/getUserSizeInfo.action":
			body = `{"cloudCapacityInfo":{"totalSize":1000,"usedSize":100}}`
		default:
			t.Error(r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	if err := a.checkStorageHealth(a.ctx, s); err != nil {
		t.Fatal(err)
	}
	current, _ := a.store.storage(s.ID)
	if refreshes.Load() != 1 || lists.Load() != 1 || current.Health == nil || current.Health.Stamp != storageRevision(current) || current.Usage == nil || current.Usage.Total != 1000 {
		t.Fatal(current, refreshes.Load(), lists.Load())
	}
	if _, err := a.tianyiSessionFor(a.ctx, current); err != nil || refreshes.Load() != 1 {
		t.Fatal("rotated session not reused", err)
	}
}
