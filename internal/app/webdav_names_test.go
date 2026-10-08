package app

import (
	"context"
	"encoding/xml"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
)

func TestDAVStorageNames(t *testing.T) {
	storages := []Storage{
		{ID: "a", Name: "Movies", Enabled: true},
		{ID: "b", Name: "Movies", Enabled: true},
		{ID: "c", Name: "Movies (a)", Enabled: true},
		{ID: "d", Name: "a", Enabled: true},
		{ID: "e", Name: "../invalid", Enabled: true},
		{ID: "f", Name: "", Enabled: true},
		{ID: "g", Name: "Hidden", Enabled: false},
	}
	names := davStorageNames(storages)
	want := map[string]string{"a": "Movies (a) (2)", "b": "Movies (b)", "c": "Movies (a)", "d": "a (d)", "e": "e", "f": "f"}
	if len(names) != len(want) {
		t.Fatal(names)
	}
	for id, name := range want {
		if names[id] != name {
			t.Fatalf("%s: got %q, want %q", id, names[id], name)
		}
	}
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.Storages = storages
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	fs := davFS{a}
	for id, name := range want {
		for _, segment := range []string{id, name} {
			s, _, err := fs.resolve(context.Background(), "/"+segment)
			if err != nil || s.ID != id {
				t.Fatalf("%q resolved to %q: %v", segment, s.ID, err)
			}
		}
	}
}

func TestDAVNamedPoolHTTP(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	name := "电影 & 100% #1"
	if err := a.store.update(func(st *State) error {
		st.Settings.WebDAVEnabled = true
		st.Storages[0].Name = name
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(s.Config["root"], "Movies", "movie.mp4"), "0123456789")
	h := a.Handler(t.TempDir())
	setupTest(t, a, h)
	dav := func(method, target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, nil)
		r.SetBasicAuth("admin", "a-secure-password-123")
		r.Header.Set("Depth", "1")
		if method == "GET" {
			r.Header.Set("Range", "bytes=2-5")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := dav("PROPFIND", "/dav/")
	var listing struct {
		Responses []struct {
			Href  string `xml:"href"`
			Props []struct {
				Name string `xml:"prop>displayname"`
			} `xml:"propstat"`
		} `xml:"response"`
	}
	if w.Code != 207 || xml.Unmarshal(w.Body.Bytes(), &listing) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	found := false
	for _, response := range listing.Responses {
		decoded, err := url.PathUnescape(response.Href)
		if err != nil {
			t.Fatal(err)
		}
		if decoded == "/dav/"+name+"/" {
			found = true
			if len(response.Props) == 0 || response.Props[0].Name != name {
				t.Fatal(response)
			}
			if child := dav("PROPFIND", response.Href); child.Code != 207 {
				t.Fatal(child.Code, child.Body.String())
			}
		}
	}
	if !found {
		t.Fatal(w.Body.String())
	}
	for _, prefix := range []string{name, s.ID} {
		target := "/dav/" + url.PathEscape(prefix) + "/Movies/movie.mp4"
		if w := dav("GET", target); w.Code != 206 || w.Body.String() != "2345" {
			t.Fatal(w.Code, w.Body.String())
		}
		if w := dav("HEAD", target); w.Code != 200 || w.Body.Len() != 0 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if err := a.store.update(func(st *State) error {
		st.Storages[0].Name = "Renamed"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"Renamed", s.ID} {
		if w := dav("GET", "/dav/"+prefix+"/Movies/movie.mp4"); w.Code != 206 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := dav("GET", "/dav/"+url.PathEscape(name)+"/Movies/movie.mp4"); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := a.store.update(func(st *State) error {
		st.Storages[0].Enabled = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"Renamed", s.ID} {
		if w := dav("GET", "/dav/"+url.PathEscape(prefix)+"/Movies/movie.mp4"); w.Code != 404 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
