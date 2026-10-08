package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/webdav"
)

func TestDAVAdminWritesAndInterruptedPUT(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "cloud"}[remote], func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			root := s.Config["root"]
			if remote {
				up := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()})
				defer up.Close()
				s.Type, s.Config = "webdav", map[string]string{"address": up.URL, "root": "/"}
			}
			if err := a.store.update(func(st *State) error { st.Settings.WebDAVEnabled = true; st.Storages = []Storage{s}; return nil }); err != nil {
				t.Fatal(err)
			}
			h := a.Handler(t.TempDir())
			setupTest(t, a, h)
			base := "/dav/" + url.PathEscape(s.Name)
			call := func(method, name, body string, length int64) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, base+name, strings.NewReader(body))
				r.SetBasicAuth("admin", "a-secure-password-123")
				if length >= 0 {
					r.ContentLength = length
				}
				r.Header.Set("Depth", "1")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w
			}
			if w := call("MKCOL", "/Books", "", 0); w.Code != 201 {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := call("PUT", "/Books/test.txt", "original", -1); w.Code != 201 {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := call("PUT", "/Books/test.txt", "short", 99); w.Code < 400 {
				t.Fatal("incomplete PUT accepted")
			}
			data, err := os.ReadFile(filepath.Join(root, "Books", "test.txt"))
			if err != nil || string(data) != "original" {
				t.Fatal(string(data), err)
			}
			if w := call("PUT", "/Books/test.txt", "replacement", -1); w.Code != 201 {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := call("PROPFIND", "/Books/", "", 0); w.Code != 207 || !strings.Contains(w.Body.String(), "test.txt") {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := call("GET", "/Books/test.txt", "", 0); w.Code != 200 || w.Body.String() != "replacement" {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := call("PUT", "/%2e%2e/escape.txt", "bad", -1); w.Code < 400 {
				t.Fatal("escape allowed", w.Code)
			}
		})
	}
}

func TestDAVListingDoesNotDownload(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "cloud", Name: "Cloud", Type: "openlist", Enabled: true, Config: map[string]string{"address": "https://upstream.example", "token": "test"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; st.Settings.WebDAVEnabled = true; return nil })
	original := apiClient
	defer func() { apiClient = original }()
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/fs/list" {
			t.Errorf("listing attempted download: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":200,"data":{"content":[{"name":"unknown.extension","size":42,"is_dir":false}],"total":1}}`)), Request: r}, nil
	})}
	h := a.Handler(t.TempDir())
	setupTest(t, a, h)
	r := httptest.NewRequest("PROPFIND", "/dav/Cloud/", nil)
	r.SetBasicAuth("admin", "a-secure-password-123")
	r.Header.Set("Depth", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 207 || !strings.Contains(w.Body.String(), "unknown.extension") || strings.Contains(w.Body.String(), "500 Internal") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestDAVLazyLocalSeek(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "movie.mp4"), "0123456789")
	f, err := (davFS{a}).OpenFile(context.Background(), "/"+s.ID+"/movie.mp4", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.Seek(4, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	if err != nil || string(data) != "456789" {
		t.Fatal(string(data), err)
	}
}

func TestDAVRedirectStripsCredentialsAndKeepsRange(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials leaked")
		}
		if r.Header.Get("Range") != "bytes=4-" {
			t.Error("range lost")
		}
		w.WriteHeader(206)
		w.Write([]byte("456789"))
	}))
	defer final.Close()
	start := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer start.Close()
	f := &davFile{ctx: context.Background(), info: davInfo{File{Name: "movie.mp4", Size: 10}}, download: Download{URL: start.URL, Headers: http.Header{"Authorization": {"Basic secret"}, "Cookie": {"secret=value"}}}}
	defer f.Close()
	f.Seek(4, io.SeekStart)
	data, err := io.ReadAll(f)
	if err != nil || string(data) != "456789" {
		t.Fatal(string(data), err)
	}
}
