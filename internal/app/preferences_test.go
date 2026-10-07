package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAccountSessionPolicy(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	w := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := w.Result().Cookies()[0]
	if cookie.MaxAge != 15*86400 {
		t.Fatal("default expiry", cookie.MaxAge)
	}
	if request(t, h, "PUT", "/api/account", credentials{Username: "new", Password: "y", SessionDays: 14}, nil).Code != 401 {
		t.Fatal("unauthenticated edit")
	}
	for _, days := range []int{-1, 366} {
		if request(t, h, "PUT", "/api/account", credentials{Username: "new", Password: "y", SessionDays: days}, cookie).Code != 400 {
			t.Fatal("invalid expiry")
		}
	}
	w = request(t, h, "PUT", "/api/account", credentials{Username: "new", Password: "y", SessionDays: 14}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	fresh := w.Result().Cookies()[0]
	if fresh.MaxAge != 14*86400 || time.Until(a.sessions[sessionKey(fresh.Value)]) < 13*24*time.Hour {
		t.Fatal("custom expiry not applied")
	}
	if request(t, h, "GET", "/api/state", nil, cookie).Code != 401 {
		t.Fatal("old session retained")
	}
	if request(t, h, "GET", "/api/state", nil, fresh).Code != 200 {
		t.Fatal("new session invalid")
	}
	store, err := NewStore(a.store.dir)
	if err != nil || store.snapshot().Settings.SessionDays != 14 {
		t.Fatal("policy not persisted", err)
	}
}

func TestImageRegistryCheck(t *testing.T) {
	old := Revision
	defer func() { Revision = old }()
	Revision = strings.Repeat("a", 40)
	for _, scenario := range []string{"same", "different", "unknown", "denied", "limited", "invalid", "missing-platform"} {
		t.Run(scenario, func(t *testing.T) {
			revision := Revision
			if scenario == "different" {
				revision = strings.Repeat("b", 40)
			}
			if scenario == "unknown" {
				revision = ""
			}
			calls := 0
			client := &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				status, raw := 200, ""
				switch {
				case r.URL.Path == "/token":
					raw = `{"token":"test-token"}`
					if scenario == "denied" {
						status = 403
					}
					if scenario == "limited" {
						status = 429
					}
				case strings.HasSuffix(r.URL.Path, "/latest"):
					raw = `{"manifests":[{"digest":"sha256:` + strings.Repeat("c", 64) + `","platform":{"os":"linux","architecture":"amd64"}}]}`
					if scenario == "missing-platform" {
						raw = strings.ReplaceAll(raw, "amd64", "arm64")
					}
				case strings.Contains(r.URL.Path, "/manifests/"):
					raw = `{"config":{"digest":"sha256:` + strings.Repeat("d", 64) + `"}}`
					if scenario == "invalid" {
						raw = `{"config":{"digest":"../unsafe"}}`
					}
				case strings.Contains(r.URL.Path, "/blobs/"):
					raw = `{"config":{"Labels":{"org.opencontainers.image.revision":"` + revision + `","org.opencontainers.image.version":"0.1.0"}}}`
				default:
					t.Fatal("unexpected endpoint", r.URL)
				}
				if r.URL.Path != "/token" && r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing token")
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
			})}
			result, err := checkImage(context.Background(), client)
			wantError := scenario == "denied" || scenario == "limited" || scenario == "invalid" || scenario == "missing-platform"
			if (err != nil) != wantError {
				t.Fatal(result, err)
			}
			if err == nil && result["available"] != (scenario == "different") {
				t.Fatal(result)
			}
			if scenario == "unknown" && !strings.Contains(result["message"].(string), "无法确认") {
				t.Fatal(result)
			}
			if calls > 4 {
				t.Fatal("unbounded requests")
			}
		})
	}
}
