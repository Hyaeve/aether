package app

import "testing"

func TestStorageOrder(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}, {ID: "c", Name: "C"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	move := map[string]string{"id": "a", "target": "c"}
	if request(t, h, "POST", "/api/storages/reorder", move, nil).Code != 401 {
		t.Fatal("missing authentication")
	}
	if request(t, h, "GET", "/api/storages/reorder", nil, cookie).Code != 405 {
		t.Fatal("method")
	}
	for _, scenario := range []struct{ id, target, want string }{
		{"a", "c", "bca"}, {"a", "b", "abc"}, {"b", "b", "abc"},
	} {
		response := request(t, h, "POST", "/api/storages/reorder", map[string]string{"id": scenario.id, "target": scenario.target}, cookie)
		if response.Code != 200 {
			t.Fatal(response.Body.String())
		}
		store, err := NewStore(a.store.dir)
		if err != nil {
			t.Fatal(err)
		}
		order := ""
		for _, storage := range store.snapshot().Storages {
			order += storage.ID
		}
		if order != scenario.want {
			t.Fatalf("order %s != %s", order, scenario.want)
		}
	}
	for _, move := range []map[string]string{{"id": "missing", "target": "a"}, {"id": "a", "target": "missing"}, {}} {
		if request(t, h, "POST", "/api/storages/reorder", move, cookie).Code != 400 {
			t.Fatal("invalid move accepted")
		}
	}
	if a.store.snapshot().Storages[0].ID != "a" {
		t.Fatal("invalid move changed order")
	}
}
