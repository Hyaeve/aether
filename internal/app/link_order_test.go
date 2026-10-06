package app

import "testing"

func TestLinkOrder(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	if err := a.store.update(func(st *State) error {
		st.Links = []MediaLink{{ID: "a"}, {ID: "b"}, {ID: "c"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if request(t, h, "POST", "/api/links/reorder", map[string]string{"id": "a", "target": "c"}, nil).Code != 401 {
		t.Fatal("authentication")
	}
	if request(t, h, "GET", "/api/links/reorder", nil, cookie).Code != 405 {
		t.Fatal("method")
	}
	for _, scenario := range []struct{ id, target, want string }{{"a", "c", "bca"}, {"a", "b", "abc"}, {"b", "b", "abc"}} {
		res := request(t, h, "POST", "/api/links/reorder", map[string]string{"id": scenario.id, "target": scenario.target}, cookie)
		if res.Code != 200 {
			t.Fatal(res.Body.String())
		}
		store, err := NewStore(a.store.dir)
		if err != nil {
			t.Fatal(err)
		}
		order := ""
		for _, link := range store.snapshot().Links {
			order += link.ID
		}
		if order != scenario.want {
			t.Fatalf("order %s", order)
		}
	}
	if request(t, h, "POST", "/api/links/reorder", map[string]string{"id": "missing", "target": "a"}, cookie).Code != 400 {
		t.Fatal("invalid move accepted")
	}
	if a.store.snapshot().Links[0].ID != "a" {
		t.Fatal("invalid move changed order")
	}
}
