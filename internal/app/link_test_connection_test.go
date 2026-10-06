package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLinkConnection(t *testing.T) {
	a := testApp(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer upstream.Close()
	if err := a.store.update(func(st *State) error {
		st.Links = append(st.Links, MediaLink{ID: "connection", Address: upstream.URL})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	if request(t, h, "POST", "/api/links/connection/test", nil, nil).Code != 401 {
		t.Fatal("unauthorized")
	}
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	for _, tc := range []struct {
		method, id string
		code       int
	}{{"POST", "connection", 200}, {"GET", "connection", 405}, {"POST", "missing", 404}} {
		if res := request(t, h, tc.method, "/api/links/"+tc.id+"/test", nil, cookie); res.Code != tc.code {
			t.Fatal(res.Code, res.Body.String())
		}
	}
	upstream.Close()
	if request(t, h, "POST", "/api/links/connection/test", nil, cookie).Code != 502 {
		t.Fatal("offline accepted")
	}
}
