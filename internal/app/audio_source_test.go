package app

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestAudioSourceAuthenticatedBoundedReads(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	for name, text := range map[string]string{"song.mp3": "0123456789", "song.lrc": "[00:01.00]lyrics", "secret.txt": "private"} {
		writeTest(t, filepath.Join(s.Config["root"], name), text)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "secret"}, nil).Result().Cookies()[0]
	for _, tc := range []struct {
		name, method, span string
		status             int
		body               string
	}{
		{"song.mp3", "GET", "bytes=2-4", 206, "234"},
		{"song.mp3", "HEAD", "", 200, ""},
		{"song.mp3", "GET", "", 400, ""},
		{"song.mp3", "GET", "bytes=0-1048576", 400, ""},
		{"song.mp3", "GET", "bytes=2-", 400, ""},
		{"song.mp3", "GET", "bytes=0-1,4-5", 400, ""},
		{"song.lrc", "GET", "", 200, "[00:01.00]lyrics"},
		{"secret.txt", "GET", "bytes=0-1", 404, ""},
		{"../secret.txt", "GET", "", 404, ""},
	} {
		address := "/api/files/audio-source?" + url.Values{"storage": {s.ID}, "parent": {"/"}, "id": {"/" + tc.name}}.Encode()
		if w := request(t, h, tc.method, address, nil, nil); w.Code != 401 {
			t.Fatal("unauthenticated metadata", w.Code)
		}
		r := httptest.NewRequest(tc.method, address, nil)
		r.AddCookie(cookie)
		r.Header.Set("Range", tc.span)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || tc.body != "" && w.Body.String() != tc.body {
			t.Fatal(tc, w.Code, w.Body.String())
		}
		if w.Code < 300 && (w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff") {
			t.Fatal(w.Header())
		}
		if tc.method == http.MethodHead && strings.TrimSpace(w.Body.String()) != "" {
			t.Fatal("HEAD sent body")
		}
	}
}
