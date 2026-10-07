package app

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogSnapshotReadOnlyAndStateSummary(t *testing.T) {
	a := testApp(t)
	now := time.Now()
	a.store.mu.Lock()
	a.store.state.Logs = []LogEntry{{Time: now.AddDate(0, 0, -20), Message: "expired"}}
	for i := 0; i < 20000; i++ {
		a.store.state.Logs = append(a.store.state.Logs, LogEntry{Time: now, Level: "info", Module: "system", Message: "retained"})
	}
	a.store.state.Logs[20000].Message = "latest"
	a.store.mu.Unlock()
	before, err := os.ReadFile(filepath.Join(a.store.logDir, "system.json"))
	if err != nil {
		t.Fatal(err)
	}
	logs := a.store.logSnapshot()
	if len(logs) != 20000 || logs[0].Message != "retained" || logs[19999].Message != "latest" {
		t.Fatal("log snapshot must retain all in-range records")
	}
	logs[0].Message = "changed"
	summary := a.store.snapshotWithLogLimit(30)
	if len(summary.Logs) != 30 || summary.Logs[29].Message != "latest" {
		t.Fatal("state summary must only copy recent logs")
	}
	summary.Logs[29].Message = "changed"
	full := a.store.logSnapshot()
	if full[0].Message != "retained" || full[19999].Message != "latest" {
		t.Fatal("snapshots must not alias stored logs")
	}
	a.store.mu.Lock()
	a.store.state.Settings.LogMaxEntries = 30000
	a.store.mu.Unlock()
	if len(a.store.logSnapshot()) != 20000 {
		t.Fatal("expired logs must be excluded even below entry limit")
	}
	after, err := os.ReadFile(filepath.Join(a.store.logDir, "system.json"))
	if err != nil || string(before) != string(after) {
		t.Fatal("reading logs must not rewrite persisted logs", err)
	}
}

func TestLogsEndpointAndAuditRedaction(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	if request(t, h, "GET", "/api/logs", nil, nil).Code != 401 {
		t.Fatal("logs must require authentication")
	}
	w := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "private-password"}, nil)
	cookie := w.Result().Cookies()[0]
	request(t, h, "POST", "/api/auth/login?token=private-token", credentials{Username: "admin", Password: "private-password"}, nil)
	a.store.event("success", "tasks", "finished")
	a.store.event("cancelled", "tasks", "stopped")
	w = request(t, h, "GET", "/api/logs", nil, cookie)
	if w.Code != 200 || strings.Contains(w.Body.String(), "private-") {
		t.Fatal(w.Code, w.Body.String())
	}
	var entries []LogEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if entries[len(entries)-1].Level != "warn" || entries[len(entries)-2].Level != "info" {
		t.Fatal(entries)
	}
	if entries[len(entries)-3].Module != "audit" {
		t.Fatal("missing audit category")
	}
	reloaded, err := NewStore(a.store.dir)
	if err == nil {
		err = reloaded.initLogs(a.store.logDir)
	}
	if err != nil || len(reloaded.snapshot().Logs) != len(entries) {
		t.Fatal("logs did not persist", err)
	}
}

func TestLogBoundAndPublicAddress(t *testing.T) {
	a := testApp(t)
	_ = a.store.update(func(st *State) error {
		st.Logs = make([]LogEntry, 20000)
		for i := range st.Logs {
			st.Logs[i].Time = time.Now()
		}
		return nil
	})
	a.store.event("debug", "files", "listed directory")
	entries := a.store.snapshot().Logs
	if len(entries) != 20000 || entries[19999].Module != "files" {
		t.Fatal("invalid ring bound")
	}
	t.Setenv("AETHER_ADDR", "192.168.50.20:15160")
	u, err := url.Parse(defaultPublicURL())
	if err != nil || u.Host != "192.168.50.20:15160" {
		t.Fatal(u, err)
	}
}

func TestDAVToggleOffline(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	w := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "test"}, nil)
	cookie := w.Result().Cookies()[0]
	_ = a.store.update(func(st *State) error {
		st.DAVUsers = []DAVUser{{ID: "offline", Username: "reader", Password: "hash", Enabled: true, Grants: []DAVGrant{{ID: "g", Name: "media", StorageID: "missing", Directory: "/Movies"}}}}
		return nil
	})
	if request(t, h, "PATCH", "/api/webdav/users/offline", map[string]bool{"enabled": false}, nil).Code != 401 {
		t.Fatal("unprotected toggle")
	}
	if request(t, h, "PATCH", "/api/webdav/users/offline", map[string]bool{"enabled": false}, cookie).Code != 200 {
		t.Fatal("offline toggle failed")
	}
	user := a.store.snapshot().DAVUsers[0]
	if user.Enabled || user.Password != "hash" || user.Grants[0].Directory != "/Movies" {
		t.Fatal("toggle altered credentials or permissions")
	}
}
