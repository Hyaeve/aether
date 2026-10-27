package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func strm115Fixture(t *testing.T) (*App, Storage, File) {
	t.Helper()
	a := testApp(t)
	s := Storage{ID: "pan", Type: "115", Enabled: true, Config: map[string]string{"cookie": "UID=333554321_A1;CID=c;SEID=s"}}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{s}
		st.Settings.PublicURL = "http://10.0.0.31:15151"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return a, s, File{ID: "file", PickCode: "bdgpkr558p3ig88v6", Name: "爱的迫降.S01E01.4K.24fps.SDR.H265.10bit.AAC.mkv"}
}

func Test115STRMCanonicalPathAndEscapedNames(t *testing.T) {
	for _, name := range []string{"爱的迫降.S01E01.4K.24fps.SDR.H265.10bit.AAC.mkv", "movie with spaces #?%&+.mkv", "literal%2Fname.mkv", "movie.verylongextension"} {
		t.Run(name, func(t *testing.T) {
			a, s, f := strm115Fixture(t)
			f.Name = name
			link, err := a.publicSTRMURL(s, f)
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(link)
			if err != nil || u.Path != "/d/333554321/"+f.PickCode+"/"+name || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(link, " \r\n") {
				t.Fatal("expected bare pickcode followed by one escaped filename segment", link, err)
			}
			ref, err := a.readSTRMReference("333554321/" + f.PickCode)
			if err != nil || ref.Name != name || ref.Pick != f.PickCode || ref.Storage != s.ID {
				t.Fatal("canonical index missing", ref, err)
			}
		})
	}
}

func Test115STRMNamedRouteRejectsLegacyAndUnregisteredPaths(t *testing.T) {
	a, s, f := strm115Fixture(t)
	link, err := a.publicSTRMURL(s, f)
	if err != nil {
		t.Fatal(err)
	}
	// A real old encrypted index must not make the retired syntax playable.
	legacyKey := "333554321/" + f.PickCode + ".mkv"
	plain, _ := json.Marshal(strmReference{s.ID, f.ID, f.PickCode, f.Name})
	nonce := make([]byte, a.store.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(a.referencePath(legacyKey), a.store.aead.Seal(nonce, nonce, plain, []byte(legacyKey))); err != nil {
		t.Fatal(err)
	}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.UserAgent() != "STRM-Path-Test/1" {
			t.Error("player UA was lost")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"state":false,"errno":10008,"error":"mock-rejected"}`)), Request: r}, nil
	})}
	h := a.Handler(t.TempDir())
	for _, method := range []string{"GET", "HEAD"} {
		r := httptest.NewRequest(method, link, nil)
		r.Header.Set("User-Agent", "STRM-Path-Test/1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 502 {
			t.Fatal("new named path did not reach the existing download pipeline", method, w.Code)
		}
	}
	if calls != 2 {
		t.Fatal("new path bypassed normal playback", calls)
	}
	prefix := "/d/333554321/" + f.PickCode
	for _, target := range []string{
		prefix + ".mkv?/" + url.PathEscape(f.Name),
		prefix + ".mkv",
		prefix + ".mkv/" + url.PathEscape(f.Name),
		prefix + "?/" + url.PathEscape(f.Name),
		prefix + "?%2F" + url.PathEscape(f.Name),
		prefix + "/wrong-name.mkv",
		prefix + "/" + url.PathEscape(f.Name) + "/extra",
		"/d/333554321/unregistered123/" + url.PathEscape(f.Name),
		"/d/999999999/" + f.PickCode + "/" + url.PathEscape(f.Name),
	} {
		if w := request(t, h, "GET", target, nil, nil); w.Code != 404 || calls != 2 {
			t.Fatal("retired or unauthorized path was accepted", target, w.Code, calls)
		}
	}
	if w := request(t, h, "POST", link, nil, nil); w.Code != 405 || calls != 2 {
		t.Fatal("unsupported method reached the cloud", w.Code, calls)
	}
	if _, err := os.Stat(a.referencePath(legacyKey)); err != nil {
		t.Fatal("retiring the route must not delete existing index data", err)
	}
}

func Test115STRMTaskWritesCanonicalURL(t *testing.T) {
	a, s, f := strm115Fixture(t)
	if err := a.store.update(func(st *State) error { st.Settings.CacheEnabled = true; return nil }); err != nil {
		t.Fatal(err)
	}
	a.cache.put(s.ID+":"+rootOf(s), []File{f}, 60, a.store.snapshot().Settings)
	task := Task{Kind: "strm", Source: "/", Target: "generated", Mode: "full"}
	n, err := a.executeTask(context.Background(), task, s)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	body, err := os.ReadFile(filepath.Join(a.outputDir, "generated", strings.TrimSuffix(f.Name, ".mkv")+".strm"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(strings.TrimSpace(string(body)))
	if err != nil || u.Path != "/d/333554321/"+f.PickCode+"/"+f.Name || u.RawQuery != "" {
		t.Fatal("task output still uses the retired syntax", string(body), err)
	}
}
