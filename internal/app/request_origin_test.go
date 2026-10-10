package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginOriginBehindProxy(t *testing.T) {
	t.Setenv("AETHER_TRUSTED_PROXIES", "192.0.2.10/32")
	a := testApp(t)
	h := a.Handler(t.TempDir())
	setupTest(t, a, h)
	if err := a.store.update(func(st *State) error { st.Settings.PublicURL = "https://public.example/aether"; return nil }); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(credentials{Username: "admin", Password: "a-secure-password-123"})
	cases := []struct {
		name, address, origin, peer, host, proto string
		want                                     int
	}{
		{"direct", "http://aether.example/api/auth/login", "http://aether.example", "198.51.100.1:4321", "", "", 200},
		{"default port and case", "http://AETHER.example:80/api/auth/login", "http://aether.EXAMPLE", "198.51.100.1:4321", "", "", 200},
		{"IPv6", "http://[2001:db8::1]:80/api/auth/login", "http://[2001:db8::1]", "[2001:db8::2]:4321", "", "", 200},
		{"configured public", "http://internal:15151/api/auth/login", "https://public.example", "198.51.100.1:4321", "", "", 200},
		{"trusted proxy", "http://internal:15151/api/auth/login", "https://external.example", "192.0.2.10:4321", "external.example:443", "https", 200},
		{"preserved Host", "http://external.example/api/auth/login", "https://external.example", "192.0.2.10:4321", "", "https", 200},
		{"spoofed forwarding", "http://internal:15151/api/auth/login", "https://external.example", "198.51.100.1:4321", "external.example", "https", 403},
		{"ambiguous forwarding", "http://internal:15151/api/auth/login", "https://external.example", "192.0.2.10:4321", "external.example, attacker.example", "https", 403},
		{"host prefix attack", "http://aether.example/api/auth/login", "http://aether.example.attacker.test", "198.51.100.1:4321", "", "", 403},
		{"scheme differs", "http://aether.example/api/auth/login", "https://aether.example", "198.51.100.1:4321", "", "", 403},
		{"null", "http://aether.example/api/auth/login", "null", "198.51.100.1:4321", "", "", 403},
		{"userinfo", "http://aether.example/api/auth/login", "http://user@aether.example", "198.51.100.1:4321", "", "", 403},
		{"path", "http://aether.example/api/auth/login", "http://aether.example/path", "198.51.100.1:4321", "", "", 403},
		{"query", "http://aether.example/api/auth/login", "http://aether.example?q=x", "198.51.100.1:4321", "", "", 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.address, bytes.NewReader(body))
			r.RemoteAddr = tc.peer
			r.Header.Set("Origin", tc.origin)
			if tc.host != "" {
				r.Header.Set("X-Forwarded-Host", tc.host)
			}
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.want == 403 && !strings.Contains(w.Body.String(), "跨站请求已拒绝") {
				t.Fatal(w.Body.String())
			}
		})
	}
	r := httptest.NewRequest(http.MethodPost, "http://aether.example/api/auth/login", bytes.NewReader(body))
	r.Header.Add("Origin", "http://aether.example")
	r.Header.Add("Origin", "https://attacker.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("ambiguous Origin accepted", w.Code)
	}
}
