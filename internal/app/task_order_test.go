package app

import "testing"

func TestTaskOrderPersistsWithoutChangingOtherKinds(t *testing.T) {
	a := testApp(t)
	a.store.update(func(st *State) error {
		st.Tasks = []Task{{ID: "a", Kind: "strm", Status: "running"}, {ID: "cache", Kind: "cache"}, {ID: "b", Kind: "strm"}, {ID: "c", Kind: "strm"}}
		return nil
	})
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	move := map[string]string{"id": "a", "target": "c"}
	if w := request(t, h, "POST", "/api/tasks/reorder", move, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(t, h, "POST", "/api/tasks/reorder", move, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	store, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"b", "cache", "c", "a"} {
		if store.snapshot().Tasks[i].ID != id {
			t.Fatal(store.snapshot().Tasks)
		}
	}
	for _, target := range []string{"missing", "cache"} {
		if w := request(t, h, "POST", "/api/tasks/reorder", map[string]string{"id": "a", "target": target}, cookie); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
