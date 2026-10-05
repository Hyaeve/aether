package app

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func freeLinkPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestMediaLinksLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := newWithDirectories(ctx, t.TempDir(), t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.startLinks()
	defer a.closeLinks()
	h := a.Handler(t.TempDir())
	response := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil)
	cookie := response.Result().Cookies()[0]
	if request(t, h, "GET", "/api/links", nil, nil).Code != 401 {
		t.Fatal("unprotected links")
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("media-ui")) }))
	defer upstream.Close()
	link := MediaLink{Name: "Audio", Type: "audiobookshelf", Address: upstream.URL, APIKey: "secret", Port: freeLinkPort(t), Mode: "always", Enabled: true}
	response = request(t, h, "POST", "/api/links", link, cookie)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	link = a.store.snapshot().Links[0]
	listed := request(t, h, "GET", "/api/links", nil, cookie)
	if strings.Contains(listed.Body.String(), "secret") {
		t.Fatal("credential leaked")
	}
	address := "http://127.0.0.1:" + fmtPort(link.Port)
	client := &http.Client{Timeout: 2 * time.Second}
	res, err := client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	link.BlockedUA = "test-blocked"
	if request(t, h, "PUT", "/api/links/"+link.ID, link, cookie).Code != 200 {
		t.Fatal("update")
	}
	req, _ := http.NewRequest("GET", address, nil)
	req.Header.Set("User-Agent", "test-blocked")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("UA not blocked")
	}
	occupied, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	bad := link
	bad.Port = occupied.Addr().(*net.TCPAddr).Port
	if request(t, h, "PUT", "/api/links/"+link.ID, bad, cookie).Code != 400 {
		t.Fatal("accepted occupied port")
	}
	res, err = client.Get(address)
	if err != nil {
		t.Fatal("old listener not restored", err)
	}
	res.Body.Close()
	link.Enabled = false
	if request(t, h, "PUT", "/api/links/"+link.ID, link, cookie).Code != 200 {
		t.Fatal("disable")
	}
	if res, err = client.Get(address); err == nil {
		res.Body.Close()
		t.Fatal("listener still active")
	}
	reloaded, err := NewStore(a.store.dir)
	if err != nil || reloaded.snapshot().Links[0].APIKey != "secret" {
		t.Fatal("encrypted persistence", err)
	}
	if request(t, h, "DELETE", "/api/links/"+link.ID, nil, cookie).Code != 200 {
		t.Fatal("delete")
	}
}

func fmtPort(port int) string { return strconv.Itoa(port) }

func TestLogRetentionAndMigration(t *testing.T) {
	a := testApp(t)
	a.store.mu.Lock()
	a.store.state.Settings.LogDays = 15
	a.store.state.Settings.LogMaxEntries = 100
	a.store.state.Logs = []LogEntry{{Time: time.Now().AddDate(0, 0, -16), Message: "expired"}}
	for i := 0; i < 110; i++ {
		a.store.state.Logs = append(a.store.state.Logs, LogEntry{Time: time.Now(), Message: "recent"})
	}
	a.store.mu.Unlock()
	a.store.pruneLogs()
	if len(a.store.snapshot().Logs) != 100 {
		t.Fatal("log retention")
	}
	data, err := os.ReadFile(filepath.Join(a.store.logDir, "system.json"))
	if err != nil || strings.Contains(string(data), "expired") {
		t.Fatal("disk retention", err)
	}
	reloaded, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.snapshot().Logs) != 0 {
		t.Fatal("logs still in config state")
	}
	if err := reloaded.initLogs(a.store.logDir); err != nil || len(reloaded.snapshot().Logs) != 100 {
		t.Fatal("log restart", err)
	}
}
