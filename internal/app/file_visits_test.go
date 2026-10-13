package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFileVisitsSharedPersistentDeepestBranches(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	c1 := setup.Result().Cookies()[0]
	c2 := request(t, h, "POST", "/api/auth/login", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	makeVisit := func(id string, history ...FavoriteCrumb) FileVisit {
		return FileVisit{Storage: s.ID, ID: id, History: history}
	}
	b := makeVisit("/A/B", FavoriteCrumb{ID: "/", Name: "A"}, FavoriteCrumb{ID: "/A", Name: "B"})
	c := makeVisit("/A/B/C", append(append([]FavoriteCrumb{}, b.History...), FavoriteCrumb{ID: b.ID, Name: "C"})...)
	d := makeVisit("/A/B/D", append(append([]FavoriteCrumb{}, b.History...), FavoriteCrumb{ID: b.ID, Name: "D"})...)
	if request(t, h, "POST", "/api/files/visits", map[string]any{"action": "visit", "items": []FileVisit{b}}, nil).Code != 401 {
		t.Fatal("unauthenticated history allowed")
	}
	for _, v := range []FileVisit{b, c, b, d, b} {
		w := request(t, h, "POST", "/api/files/visits", map[string]any{"action": "visit", "items": []FileVisit{v}}, c1)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := request(t, h, "GET", "/api/state", nil, c2)
	var state State
	if json.Unmarshal(w.Body.Bytes(), &state) != nil || len(state.FileVisits) != 2 || state.FileVisits[0].ID != d.ID || state.FileVisits[1].ID != c.ID {
		t.Fatal("shared deepest sibling histories lost", w.Body.String())
	}
	for _, v := range []FileVisit{makeVisit("/A", FavoriteCrumb{ID: "/", Name: "A"}), makeVisit("/", b.History...), {Storage: "missing", ID: b.ID, History: b.History}} {
		if request(t, h, "POST", "/api/files/visits", map[string]any{"action": "visit", "items": []FileVisit{v}}, c1).Code != 400 {
			t.Fatal("invalid visit accepted", v)
		}
	}
	store, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if len(store.snapshot().FileVisits) != 2 {
		t.Fatal("restart lost history")
	}
	raw, err := os.ReadFile(filepath.Join(a.store.dir, "state.json"))
	var config State
	if err != nil || store.decodeJSONConfig("state", raw, &config) != nil || len(config.FileVisits) != 0 {
		t.Fatal("history leaked into configuration", err)
	}
	for i := 0; i < 6; i++ {
		v := makeVisit("/other/"+string(rune('a'+i)), FavoriteCrumb{ID: "/", Name: "other"}, FavoriteCrumb{ID: "/other", Name: string(rune('a' + i))})
		if request(t, h, "POST", "/api/files/visits", map[string]any{"action": "visit", "items": []FileVisit{v}}, c2).Code != 200 {
			t.Fatal("visit failed")
		}
	}
	prior := a.store.snapshot().FileVisits[0].ID
	if request(t, h, "POST", "/api/files/visits", map[string]any{"action": "import", "items": []FileVisit{b}}, c1).Code != 200 || len(a.store.snapshot().FileVisits) != 5 || a.store.snapshot().FileVisits[0].ID != prior {
		t.Fatal("stale import overwrote current recent history")
	}
}

func TestFileFavoritesMigrateConfigToData(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	legacy := FileFavorite{Storage: s.ID, ID: "/A", Name: "A", History: []FavoriteCrumb{{ID: "/", Name: "A"}}}
	values := a.store.jsonValues()
	values["state"].(map[string]any)["fileFavorites"] = []FileFavorite{legacy}
	raw, err := a.store.encodeJSONConfig("state", values["state"])
	if err != nil {
		t.Fatal(err)
	}
	if err = durableConfigWrite(filepath.Join(a.store.dir, "state.json"), raw); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if len(reopened.snapshot().FileFavorites) != 1 {
		t.Fatal("legacy favorites lost")
	}
	reopenedAgain, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopenedAgain.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if len(reopenedAgain.snapshot().FileFavorites) != 1 {
		t.Fatal("migrated favorites did not persist")
	}
	if err := reopenedAgain.update(func(st *State) error {
		st.RuntimeSeparated = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reimported, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reimported.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if len(reimported.snapshot().FileFavorites) != 1 {
		t.Fatal("configuration import marker discarded file records")
	}
}
