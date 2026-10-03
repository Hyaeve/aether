package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReleaseCheck(t *testing.T) {
	for _, tc := range []struct {
		name, body, message string
		status              int
		available, failure  bool
	}{
		{"new", `{"tag_name":"v0.2.0"}`, "发现新版本", 200, true, false},
		{"same", `{"tag_name":"v0.1.0"}`, "无需更新", 200, false, false},
		{"older", `{"tag_name":"v0.0.9"}`, "无需更新", 200, false, false},
		{"no release", `{}`, "尚未发布", 404, false, false},
		{"rate limited", `{}`, "", 403, false, true},
		{"malformed", `not json`, "", 200, false, true},
		{"no tag", `{}`, "", 200, false, true},
		{"non semver", `{"tag_name":"nightly"}`, "核对版本", 200, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("User-Agent") != "Aether/"+Version {
					t.Error("missing application user agent")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer upstream.Close()
			req, _ := http.NewRequest("GET", upstream.URL, nil)
			result, err := checkRelease(upstream.Client(), req)
			if (err != nil) != tc.failure {
				t.Fatalf("unexpected error %v", err)
			}
			if err == nil && (result["available"] != tc.available || !strings.Contains(result["message"].(string), tc.message)) {
				t.Fatalf("unexpected result %#v", result)
			}
		})
	}
}

func TestAuthorizationSessionProtection(t *testing.T) {
	a := testApp(t)
	r := httptest.NewRequest("POST", "/api/authorization/quark/poll", nil)
	r.AddCookie(&http.Cookie{Name: "aether_session", Value: "owner-session"})
	r.SetPathValue("provider", "quark")
	s := authorizationSession{Provider: "quark", Token: "private-token", Cookies: map[string]string{"private-cookie": "secret"}, Expires: time.Now().Add(time.Minute).Unix(), Owner: authorizationOwner(r)}
	token, err := a.sealAuthorization(s)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(token)
	if strings.Contains(string(raw), "private-token") || strings.Contains(string(raw), "secret") {
		t.Fatal("authorization secrets not encrypted")
	}
	got, err := a.openAuthorization(token, r)
	if err != nil || got.Token != s.Token {
		t.Fatalf("valid session rejected: %v", err)
	}
	raw[len(raw)-1] ^= 1
	if _, err := a.openAuthorization(base64.RawURLEncoding.EncodeToString(raw), r); err == nil {
		t.Fatal("tampered session accepted")
	}
	r.SetPathValue("provider", "115")
	if _, err := a.openAuthorization(token, r); err == nil {
		t.Fatal("cross-provider session accepted")
	}
	r.SetPathValue("provider", "quark")
	s.Owner = sha256.Sum256([]byte("another-owner"))
	token, _ = a.sealAuthorization(s)
	if _, err := a.openAuthorization(token, r); err == nil {
		t.Fatal("cross-account session accepted")
	}
	s.Owner = authorizationOwner(r)
	s.Expires = time.Now().Add(-time.Second).Unix()
	token, _ = a.sealAuthorization(s)
	if _, err := a.openAuthorization(token, r); err == nil {
		t.Fatal("expired session accepted")
	}
}

func TestWorkspaceEndpointsRequireLogin(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	for _, endpoint := range []string{"/api/version", "/api/version/check", "/api/authorization/quark/start", "/api/authorization/115/poll"} {
		method := "GET"
		if strings.Contains(endpoint, "authorization") {
			method = "POST"
		}
		if w := request(t, h, method, endpoint, nil, nil); w.Code != 401 {
			t.Fatalf("%s not protected: %d", endpoint, w.Code)
		}
	}
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "a"}, nil)
	cookie := setup.Result().Cookies()[0]
	w := request(t, h, "GET", "/api/version", nil, cookie)
	var result map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	if w.Code != 200 || result["version"] != Version {
		t.Fatal("version endpoint failed")
	}
	for _, value := range []string{"http://example.com", "https://user:password@example.com", "javascript:alert(1)", "https://example.com/?token=secret"} {
		w = request(t, h, "POST", "/api/authorization/115/start", map[string]string{"base": value}, cookie)
		if w.Code != 400 {
			t.Fatalf("invalid proxy accepted: %s", value)
		}
	}
}

func TestStorageDeleteMode(t *testing.T) {
	s := Storage{Name: "local", Type: "local", Config: map[string]string{"root": t.TempDir()}}
	if err := validateStorage(&s); err != nil || s.Config["deleteMode"] != "trash" {
		t.Fatal("safe deletion default missing", err)
	}
	s.Config["deleteMode"] = "permanent"
	if err := validateStorage(&s); err != nil {
		t.Fatal(err)
	}
	s.Config["deleteMode"] = "invalid"
	if err := validateStorage(&s); err == nil {
		t.Fatal("invalid deletion policy accepted")
	}
}
