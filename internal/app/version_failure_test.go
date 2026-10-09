package app

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestVersionFailureIncludesCurrent(t *testing.T) {
	original := http.DefaultTransport
	http.DefaultTransport = casTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("network unavailable") })
	t.Cleanup(func() { http.DefaultTransport = original })
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "GET", "/api/version/check", nil, cookie)
	if w.Code != 502 || !strings.Contains(w.Body.String(), "当前版本 v"+Version) || !strings.Contains(w.Body.String(), "GHCR") {
		t.Fatal(w.Code, w.Body.String())
	}
	if !a.versionExpiry.IsZero() {
		t.Fatal("failed update check must not be cached as successful")
	}
}
