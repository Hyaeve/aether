package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDirectoryTrail115LegacyAndScopedRoot(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "pan", Type: "115", Enabled: true, Config: map[string]string{"cookie": "UID=1_A1; CID=test; SEID=test"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	endpoint := "/api/files/directory-trail?storage=pan&path=deep"
	if w := request(t, h, "GET", endpoint, nil, nil); w.Code != 401 {
		t.Fatal("unauthenticated path lookup", w.Code)
	}
	original := apiClient
	t.Cleanup(func() { apiClient = original })
	last := "deep"
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Host != "webapi.115.com" || r.URL.Path != "/files" || r.URL.Query().Get("limit") != "1" {
			t.Fatal("unexpected protocol", r.Method, r.URL.Path)
		}
		raw := `{"state":true,"path":[{"cid":"0","name":"根目录"},{"cid":"library","name":"资料"},{"cid":"middle","name":"中间"},{"cid":"` + last + `","name":"深目录"}]}`
		return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(raw))}, nil
	})}
	w := request(t, h, "GET", endpoint, nil, cookie)
	var result struct {
		Path  string        `json:"path"`
		Label string        `json:"label"`
		Trail []searchCrumb `json:"trail"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Path != "/资料/中间/深目录" || len(result.Trail) != 3 || result.Trail[1].ID != "library" || result.Label != "深目录" {
		t.Fatal(w.Code, w.Body.String())
	}
	s.Config["root"] = "library"
	if err := a.store.update(func(st *State) error { st.Storages[0] = s; return nil }); err != nil {
		t.Fatal(err)
	}
	w = request(t, h, "GET", endpoint, nil, cookie)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Path != "/中间/深目录" || result.Trail[0].ID != "/" || len(result.Trail) != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
	last = "changed"
	if w = request(t, h, "GET", endpoint, nil, cookie); w.Code != 502 {
		t.Fatal("changed identity accepted", w.Code, w.Body.String())
	}
	last = "deep"
	s.Config["root"] = "outside"
	if err := a.store.update(func(st *State) error { st.Storages[0] = s; return nil }); err != nil {
		t.Fatal(err)
	}
	if w = request(t, h, "GET", endpoint, nil, cookie); w.Code != 400 {
		t.Fatal("outside pool accepted", w.Code)
	}
	if calls != 4 {
		t.Fatal("unexpected read count", calls)
	}
}

func TestBackup115PruningChecksOnlyPeerAncestorsAndCaches(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "pan", Type: "115", Enabled: true, Config: map[string]string{"cookie": "UID=1_A1; CID=test; SEID=test"}}
	original := apiClient
	t.Cleanup(func() { apiClient = original })
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/files" || r.URL.Query().Get("cid") != "target" || r.URL.Query().Get("limit") != "1" {
			t.Fatal("excluded subtree accessed", r.URL.Path, r.URL.RawQuery)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(`{"state":true,"path":[{"cid":"0","name":"根目录"},{"cid":"parent","name":"上层"},{"cid":"target","name":"目标"}]}`))}, nil
	})}
	ctx := context.WithValue(context.Background(), backupAncestryKey{}, backupAncestryCache{})
	for _, excluded := range []string{"cache-one", "cache-two"} {
		if err := a.backupPrunedSafe(ctx, s, excluded, s, "target"); err != nil {
			t.Fatal(err)
		}
	}
	if a.backupPrunedSafe(ctx, s, "parent", s, "target") == nil {
		t.Fatal("nested target accepted")
	}
	if calls != 1 {
		t.Fatal("peer ancestry not shared", calls)
	}
}
