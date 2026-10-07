package app

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionsSurviveRestartAndRevoke(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	w := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := w.Result().Cookies()[0]
	if cookie.MaxAge != 15*86400 {
		t.Fatalf("default expiry: %d", cookie.MaxAge)
	}
	restart := func() *App {
		t.Helper()
		b, err := newWithDirectories(context.Background(), a.store.dir, a.dataDir, a.outputDir)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	b := restart()
	if request(t, b.Handler(t.TempDir()), "GET", "/api/state", nil, cookie).Code != 200 {
		t.Fatal("restart lost session")
	}
	data, err := os.ReadFile(filepath.Join(a.store.dir, "sessions.enc"))
	if err != nil || strings.Contains(string(data), cookie.Value) {
		t.Fatal("session storage is not encrypted")
	}
	w = request(t, b.Handler(t.TempDir()), "POST", "/api/auth/login", credentials{Username: "owner", Password: "x"}, nil)
	second := w.Result().Cookies()[0]
	if request(t, b.Handler(t.TempDir()), "POST", "/api/auth/logout", nil, cookie).Code != 200 {
		t.Fatal("logout")
	}
	b = restart()
	if request(t, b.Handler(t.TempDir()), "GET", "/api/state", nil, cookie).Code != 401 {
		t.Fatal("logout not persisted")
	}
	if request(t, b.Handler(t.TempDir()), "GET", "/api/state", nil, second).Code != 200 {
		t.Fatal("other device revoked")
	}
	w = request(t, b.Handler(t.TempDir()), "PUT", "/api/account", credentials{Username: "owner", Password: "new", SessionDays: 15}, second)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	b = restart()
	if request(t, b.Handler(t.TempDir()), "GET", "/api/state", nil, second).Code != 401 {
		t.Fatal("password did not revoke old token")
	}
	b.sessions[sessionKey("expired")] = time.Now().Add(-time.Second)
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Cookie", "aether_session=expired")
	if b.authenticated(r) {
		t.Fatal("expired token accepted")
	}
}

func TestED2KGeneration(t *testing.T) {
	for text, want := range map[string]string{"": "31d6cfe0d16ae931b73c59d7e0c089c0", "abc": "a448017aaf21d8525fc10ae87aa6729d"} {
		got, err := ed2kHash(strings.NewReader(text))
		if err != nil || got != want {
			t.Fatalf("%q: %s %v", text, got, err)
		}
	}
	a := testApp(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "book.txt"), []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	s := Storage{ID: "ed2k", Type: "local", Enabled: true, Config: map[string]string{"root": root}}
	task := Task{Kind: "ed2k", Source: "/", Mode: "incremental"}
	n, err := a.executeTask(context.Background(), task, s)
	if err != nil || n != 1 {
		t.Fatalf("generate: %d %v", n, err)
	}
	data, err := os.ReadFile(filepath.Join(a.outputDir, "book.txt.ed2k"))
	if err != nil || !strings.Contains(string(data), "ed2k://|file|book.txt|3|a448017aaf21d8525fc10ae87aa6729d|/") {
		t.Fatalf("%s %v", data, err)
	}
	n, err = a.executeTask(context.Background(), task, s)
	if err != nil || n != 0 {
		t.Fatalf("incremental: %d %v", n, err)
	}
}
