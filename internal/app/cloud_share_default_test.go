package app

import (
	"net/http"
	"strings"
	"testing"
)

func TestCloud115FixedCodeUsesProviderDefault(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "fixed-default", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	writes := 0
	shareMock(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/files":
			return `{"state":true,"cid":"0","count":1,"data":[{"fid":"10","n":"film.mp4","s":4,"fc":0}]}`
		case "/share/send":
			writes++
			return `{"state":true,"data":{"share_code":"created","receive_code":"ABCD"}}`
		case "/share/updateshare":
			writes++
			r.ParseForm()
			if r.Form.Get("receive_code") != "ABCD" {
				t.Fatal("provider default was overwritten", r.Form)
			}
			return `{"state":true}`
		}
		t.Fatal("unexpected endpoint", r.URL)
		return ""
	})
	options := cloudShareOptions{Days: 15, CodeMode: "fixed", Autofill: true}
	if err := validateCloudShareOptions("115", options); err != nil {
		t.Fatal(err)
	}
	w := request(t, h, "POST", "/api/cloud/shares", map[string]any{"storageId": s.ID, "parent": "/", "ids": []string{"10"}, "options": options}, cookie)
	if w.Code != 200 || writes != 2 || !strings.Contains(w.Body.String(), "password=ABCD") {
		t.Fatal(w.Code, writes, w.Body.String())
	}
}
