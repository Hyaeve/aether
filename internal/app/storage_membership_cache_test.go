package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStorageMembershipReturnsBeforeSlowQuota(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	s.Config["cookie"] = "UID=1_A1;CID=test;SEID=secret"
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	oldClient, oldTransport := apiClient, http.DefaultTransport
	t.Cleanup(func() { apiClient, http.DefaultTransport = oldClient, oldTransport })
	transport := casTransport(func(r *http.Request) (*http.Response, error) {
		raw := `{"state":true,"data":{"user_id":"1","vip_info":{"level_name":"年费VIP"}}}`
		if r.URL.Host != "passportapi.115.com" {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			raw = `{"state":true,"data":{"space_info":{"all_total":{"size":"1000"},"all_use":{"size":"250"}}}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})
	apiClient, http.DefaultTransport = &http.Client{Transport: transport}, transport
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	if request(t, h, "GET", "/api/storages/pan/membership", nil, nil).Code != 401 {
		t.Fatal("membership exposed without authentication")
	}
	quota := make(chan error, 1)
	go func() { _, err := a.cachedStorageUsage(context.Background(), s); quota <- err }()
	<-started
	ready := make(chan bool, 1)
	go func() {
		w := request(t, h, "GET", "/api/storages/pan/membership", nil, cookie)
		ready <- w.Code == 200 && strings.Contains(w.Body.String(), `"tier":"annual"`)
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("membership response missing tier")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("badge waited for blocked quota")
	}
	select {
	case <-quota:
		t.Fatal("quota was not blocked")
	default:
	}
	once.Do(func() { close(release) })
	if err := <-quota; err != nil {
		t.Fatal(err)
	}
	value, _ := savedStorageUsage(a.store.snapshot().Storages[0])
	if value.Total != 1000 || value.Membership == nil {
		t.Fatal("separate lookups lost quota or membership", value)
	}
}

func TestStorageMembershipNavigationDoesNotCancelSharedLookup(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	s, err := a.store.storage(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	oldClient, oldTransport := apiClient, http.DefaultTransport
	var calls atomic.Int32
	transport := casTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"state":true,"data":{"user_id":"333554321","vip_info":{"level_name":"永久VIP"}}}`)), Request: r}, nil
	})
	apiClient, http.DefaultTransport = &http.Client{Transport: transport}, transport
	defer func() { apiClient, http.DefaultTransport = oldClient, oldTransport }()
	ctx, cancel := context.WithCancel(a.ctx)
	done := make(chan error, 1)
	go func() { _, err := a.cachedStorageMembership(ctx, s); done <- err }()
	<-started
	cancel()
	close(release)
	if err := <-done; err != context.Canceled {
		t.Fatal("initiating page did not receive cancellation", err)
	}
	value, err := a.cachedStorageMembership(a.ctx, s)
	if err != nil || value == nil || value.Tier != "permanent" || calls.Load() != 1 {
		t.Fatal("navigation restarted or discarded shared membership", value, err, calls.Load())
	}
	stored, err := a.store.storage(s.ID)
	if err != nil || stored.Usage == nil || stored.Usage.Membership == nil {
		t.Fatal("background result not persisted", err)
	}
}

func TestStorageMembershipLastKnownBadgeDoesNotAuthorizeCAS(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	m := membership("annual", "年费VIP", time.Time{})
	m.CheckedAt = time.Now().Add(-6 * time.Minute)
	s.Usage = &storageUsageState{storageUsage: storageUsage{Membership: m, Total: 100, Used: 25}, Stamp: storageRevision(s)}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	oldClient, oldTransport := apiClient, http.DefaultTransport
	transport := casTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("unavailable")), Request: r}, nil
	})
	apiClient, http.DefaultTransport = &http.Client{Transport: transport}, transport
	defer func() { apiClient, http.DefaultTransport = oldClient, oldTransport }()
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "GET", "/api/storages/"+s.ID+"/membership", nil, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"tier":"annual"`) {
		t.Fatal("last known display badge lost", w.Code, w.Body.String())
	}
	stored, err := a.store.storage(s.ID)
	if err != nil || displayMembership(stored.Usage.Membership) == nil {
		t.Fatal("last known metadata not retained", err)
	}
	usage, _ := savedStorageUsage(stored)
	if usage.Membership != nil || freshMembership(stored.Usage.Membership) != nil {
		t.Fatal("display cache leaked into CAS capability checks")
	}
	expired := time.Now().Add(-time.Second)
	m.ExpiresAt = &expired
	if displayMembership(m) != nil {
		t.Fatal("expired tier displayed")
	}
}

func TestStorageMembershipCoalescesAndRejectsChangedCredentials(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	// The fixture inserted s.Config by reference. Real callers obtain a detached
	// snapshot, which must not change while a request hashes or reads credentials.
	s, err := a.store.storage(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	oldClient, oldTransport := apiClient, http.DefaultTransport
	t.Cleanup(func() { apiClient, http.DefaultTransport = oldClient, oldTransport })
	transport := casTransport(func(r *http.Request) (*http.Response, error) {
		if count.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"state":true,"data":{"user_id":"333554321","vip_info":{"level_name":"永久VIP"}}}`)), Request: r}, nil
	})
	apiClient, http.DefaultTransport = &http.Client{Transport: transport}, transport
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	response := make(chan int, 1)
	go func() { response <- request(t, h, "GET", "/api/storages/pan/membership", nil, cookie).Code }()
	<-started
	results := make(chan *storageMembership, 3)
	for i := 0; i < 3; i++ {
		go func() { m, _ := a.cachedStorageMembership(context.Background(), s); results <- m }()
	}
	a.store.update(func(st *State) error { st.Storages[0].Config["cookie"] += ";KID=changed"; return nil })
	once.Do(func() { close(release) })
	if code := <-response; code != 409 {
		t.Fatal("stale badge returned for changed credentials", code)
	}
	for i := 0; i < 3; i++ {
		if m := <-results; m == nil || m.Tier != "permanent" {
			t.Fatal(m)
		}
	}
	if count.Load() != 1 {
		t.Fatal("membership lookup duplicated", count.Load())
	}
	if saved, ok := savedStorageUsage(a.store.snapshot().Storages[0]); ok && saved.Membership != nil {
		t.Fatal("old badge persisted under new credentials")
	}
}
