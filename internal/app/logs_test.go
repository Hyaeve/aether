package app

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

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
	if err != nil || len(reloaded.snapshot().Logs) != len(entries) {
		t.Fatal("logs did not persist", err)
	}
}

func TestLogBoundAndPublicAddress(t *testing.T) {
	a := testApp(t)
	_ = a.store.update(func(st *State) error { st.Logs = make([]LogEntry, 2000); return nil })
	a.store.event("debug", "files", "listed directory")
	entries := a.store.snapshot().Logs
	if len(entries) != 2000 || entries[1999].Module != "files" {
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
