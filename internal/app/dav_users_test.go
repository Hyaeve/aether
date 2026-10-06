package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestDAVUsersPermissions(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "Movies", "allowed.mp4"), "allowed")
	writeTest(t, filepath.Join(s.Config["root"], "private.txt"), "private")
	h := a.Handler(t.TempDir())
	w := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "admin"}, nil)
	cookie := w.Result().Cookies()[0]
	_ = a.store.update(func(st *State) error { st.Settings.WebDAVEnabled = true; return nil })
	// Populate the administrator's root cache before a restricted local request.
	_, _ = a.listFiles(context.Background(), s, "/", 30, false)
	user := DAVUser{Username: "reader", Password: "secret", Enabled: true, Grants: []DAVGrant{{Name: "Movies", StorageID: s.ID, Directory: "/Movies", DirectoryLabel: "电影目录"}}}
	if request(t, h, "POST", "/api/webdav/users", user, nil).Code != 401 {
		t.Fatal("unprotected API")
	}
	w = request(t, h, "POST", "/api/webdav/users", user, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var created map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	uid := created["id"]
	w = request(t, h, "GET", "/api/webdav/users", nil, cookie)
	if !strings.Contains(w.Body.String(), "电影目录") {
		t.Fatal("directory label lost")
	}
	if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "$2") {
		t.Fatal("password exposed")
	}
	dav := func(method, target, password string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, nil)
		r.SetBasicAuth("reader", password)
		r.Header.Set("Depth", "1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w = dav("PROPFIND", "/dav/", "secret")
	if w.Code != 207 || !strings.Contains(w.Body.String(), "Movies") || strings.Contains(w.Body.String(), s.ID) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = dav("PROPFIND", "/dav/Movies/", "secret")
	if w.Code != 207 || !strings.Contains(w.Body.String(), "allowed.mp4") || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = dav("GET", "/dav/Movies/allowed.mp4", "secret"); w.Code != 200 || w.Body.String() != "allowed" {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, target := range []string{"/dav/" + s.ID + "/private.txt", "/dav/private.txt", "/dav/Movies/../private.txt", "/dav/Movies/%2e%2e/private.txt"} {
		if w := dav("GET", target, "secret"); w.Code == 200 {
			t.Fatal("unauthorized path", target)
		}
	}
	if dav("DELETE", "/dav/Movies/allowed.mp4", "secret").Code != 405 {
		t.Fatal("write allowed")
	}
	if dav("GET", "/dav/Movies/allowed.mp4", "wrong").Code != 401 {
		t.Fatal("wrong password")
	}
	user.Password = ""
	user.Grants = nil
	if request(t, h, "PUT", "/api/webdav/users/"+uid, user, cookie).Code != 200 {
		t.Fatal("edit failed")
	}
	if dav("GET", "/dav/Movies/allowed.mp4", "secret").Code == 200 {
		t.Fatal("revocation ignored")
	}
	user.Enabled = false
	_ = request(t, h, "PUT", "/api/webdav/users/"+uid, user, cookie)
	if dav("PROPFIND", "/dav/", "secret").Code != 401 {
		t.Fatal("disabled user accepted")
	}
	if request(t, h, "DELETE", "/api/webdav/users/"+uid, nil, cookie).Code != 200 {
		t.Fatal("delete failed")
	}
	if len(a.store.snapshot().DAVUsers) != 0 {
		t.Fatal("user retained")
	}
}
