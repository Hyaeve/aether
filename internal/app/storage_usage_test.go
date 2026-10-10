package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

func TestCloudStorageUsageAndFallback(t *testing.T) {
	oldClient, oldTransport := apiClient, http.DefaultTransport
	defer func() { apiClient, http.DefaultTransport = oldClient, oldTransport }()
	var calls atomic.Int32
	transport := casTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		raw := `{"state":true,"data":{"space_info":{"all_total":{"size":"1000"},"all_use":{"size":"250"}}}}`
		switch r.URL.Host {
		case "drive.quark.cn":
			raw = `{"code":0,"status":200,"data":{"total_capacity":2000,"use_capacity":500}}`
		case "api.cloud.189.cn":
			raw = `{"res_code":0,"cloudCapacityInfo":{"totalSize":3000,"usedSize":750}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})
	apiClient, http.DefaultTransport = &http.Client{Transport: transport}, transport
	for _, kind := range []string{"115", "quark", "tianyi", "mobile"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: kind, Type: kind, Enabled: true, Config: map[string]string{"cookie": "UID=1_A1;CID=test;SEID=secret", "username": "account", "password": "secret", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:mobile-account:token"))}}
			if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
				t.Fatal(err)
			}
			if kind == "tianyi" {
				a.tianyiSessions[s.ID] = tianyiSession{Key: "key", Secret: "secret", Credentials: tianyiCredentials(s), Expires: time.Now().Add(time.Hour)}
			}
			value, err := a.cachedStorageUsage(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "mobile" {
				if value.Username != "mobile-account" || value.Total != 0 {
					t.Fatal(value)
				}
			} else if value.Total == 0 || value.Used*4 != value.Total {
				t.Fatal(value)
			}
			count := calls.Load()
			if _, err = a.cachedStorageUsage(context.Background(), s); err != nil || calls.Load() != count {
				t.Fatal("quota was not cached", err)
			}
			h := a.Handler(t.TempDir())
			if request(t, h, "GET", "/api/storages/"+s.ID+"/usage", nil, nil).Code != 401 {
				t.Fatal("unauthenticated quota exposed")
			}
			cookie := setupTest(t, a, h)
			w := request(t, h, "GET", "/api/storages/"+s.ID+"/usage", nil, cookie)
			if w.Code != 200 || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "token") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

func TestStorageUsageFailureNicknameAndCredentialInvalidation(t *testing.T) {
	old := apiClient
	defer func() { apiClient = old }()
	var calls atomic.Int32
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		raw := `{"code":500,"status":500,"data":{"total_capacity":1000,"use_capacity":100}}`
		if r.URL.Host == "pan.quark.cn" {
			raw = `{"success":true,"data":{"nickname":"真实昵称"}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "first"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	value, err := a.cachedStorageUsage(context.Background(), s)
	if err != nil || value.Total != 0 || value.Username != "真实昵称" {
		t.Fatal(value, err)
	}
	count := calls.Load()
	s.Config = map[string]string{"cookie": "second"}
	if _, err = a.cachedStorageUsage(context.Background(), s); err != nil || calls.Load() == count {
		t.Fatal("old credentials reused", err)
	}
}

func TestStorageUsageConcurrentRequestsAndCanceledLookup(t *testing.T) {
	old := apiClient
	defer func() { apiClient = old }()
	started, release := make(chan struct{}), make(chan struct{})
	var first sync.Once
	var calls atomic.Int32
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		first.Do(func() { close(started) })
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":0,"status":200,"data":{"total_capacity":1000,"use_capacity":100}}`)), Request: r}, nil
	})}
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "test"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	results := make(chan storageUsage, 8)
	go func() { value, _ := a.cachedStorageUsage(context.Background(), s); results <- value }()
	<-started
	for i := 0; i < 7; i++ {
		go func() { value, _ := a.cachedStorageUsage(context.Background(), s); results <- value }()
	}
	close(release)
	for i := 0; i < 8; i++ {
		if value := <-results; value.Total != 1000 || value.Used != 100 {
			t.Fatal(value)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate concurrent lookups", calls.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.Config = map[string]string{"cookie": "changed"}
	if _, err := a.cachedStorageUsage(ctx, s); err == nil {
		t.Fatal("canceled request succeeded")
	}
	if value, err := a.cachedStorageUsage(context.Background(), s); err != nil || value.Total != 1000 {
		t.Fatal("canceled result poisoned next lookup", value, err)
	}
}

func TestHashLibrariesScrapeParticipation(t *testing.T) {
	for _, name := range []string{"Arrival.cas.strm", "Arrival.ed2k.strm", "Arrival.CAS.strm", "Arrival.iso.cas.strm"} {
		if got := recognizeSTRM(name).Title; got != "Arrival" {
			t.Fatalf("hash marker leaked into title: %s", got)
		}
	}
	for _, kind := range []string{"strm", "cas", "ed2k"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			target := t.TempDir()
			task := Task{ID: "library", Kind: kind, StorageID: s.ID, Source: "/Show", Target: target, SourceLabel: "Show"}
			if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
				t.Fatal(err)
			}
			root, err := a.scrapeRoot(task.ID)
			want := target
			if kind == "strm" {
				want = filepath.Join(target, "Show")
			}
			if err != nil || root != want {
				t.Fatal(root, want, err)
			}
			if err := a.store.update(func(st *State) error { st.Tasks[0].ScrapeExcluded = true; return nil }); err != nil {
				t.Fatal(err)
			}
			if _, err := a.scrapeRoot(task.ID); err == nil {
				t.Fatal("excluded hash library accepted")
			}
			raw, err := osReadTaskJSONForTest(a, kind)
			if err != nil || !strings.Contains(string(raw), `"scrapeExcluded": true`) {
				t.Fatal(string(raw), err)
			}
		})
	}
}

func osReadTaskJSONForTest(a *App, kind string) ([]byte, error) {
	// Read via the module decoder so this also exercises the persisted task flag.
	var tasks []Task
	raw, err := os.ReadFile(filepath.Join(a.store.dir, "task", kind+".json"))
	if err != nil {
		return nil, err
	}
	if err = a.store.decodeJSONConfig("task/"+kind, raw, &tasks); err != nil {
		return nil, err
	}
	return json.MarshalIndent(tasks, "", "  ")
}
