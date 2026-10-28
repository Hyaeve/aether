package app

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestCASRouteIsDedicatedAndRejectsOldStreamCAS(t *testing.T) {
	a := testApp(t)
	info := CASInfo{Name: "movie.mkv", Size: 10, SHA256: strings.Repeat("a", 64)}
	address := a.generatedStreamURL(streamClaim{CAS: &info, Redirect: true})
	u, err := url.Parse(address)
	if err != nil || !strings.HasPrefix(u.Path, "/cas/") {
		t.Fatal("CAS URL did not use its dedicated route", address, err)
	}
	if _, err := decodeCAS([]byte(address), "movie.mkv.cas.strm"); err != nil {
		t.Fatal("new CAS pointer not recognized", err)
	}
	old := strings.Replace(address, "/cas/", "/stream/", 1)
	if _, err := decodeCAS([]byte(old), "movie.mkv.cas.strm"); err == nil {
		t.Fatal("old stream pointer remains compatible")
	}
	h := a.Handler(t.TempDir())
	oldURL, _ := url.Parse(old)
	if w := request(t, h, "GET", oldURL.RequestURI(), nil, nil); w.Code != 404 {
		t.Fatal("old CAS route reached playback", w.Code, w.Body.String())
	}
	ordinary, _ := url.Parse(a.streamURL("missing", "movie.mkv", ""))
	if !strings.HasPrefix(ordinary.Path, "/stream/") {
		t.Fatal("ordinary storage route changed")
	}
	ordinary.Path = strings.Replace(ordinary.Path, "/stream/", "/cas/", 1)
	if w := request(t, h, "GET", ordinary.RequestURI(), nil, nil); w.Code != 404 {
		t.Fatal("ordinary payload accepted by CAS route", w.Code)
	}
	u.RawQuery = "sign=invalid"
	if w := request(t, h, "GET", u.RequestURI(), nil, nil); w.Code != 403 {
		t.Fatal("CAS signature validation bypassed", w.Code)
	}
	if _, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(u.Path, "/cas/")); err != nil {
		t.Fatal("CAS route changed the signed payload", err)
	}
}
