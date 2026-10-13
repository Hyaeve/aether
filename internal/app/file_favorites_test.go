package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestFileFavoritesSharedSessionsPersistenceAndMigration(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	c1 := setup.Result().Cookies()[0]
	login := request(t, h, "POST", "/api/auth/login", credentials{Username: "owner", Password: "x"}, nil)
	c2 := login.Result().Cookies()[0]
	first := FileFavorite{Storage: s.ID, ID: "/A", Name: "A", History: []FavoriteCrumb{{ID: "/", Name: "A"}}}
	second := FileFavorite{Storage: s.ID, ID: "/B", Name: "B", History: []FavoriteCrumb{{ID: "/", Name: "B"}}}
	post := func(action string, items []FileFavorite, cookie *http.Cookie) int {
		return request(t, h, "POST", "/api/files/favorites", map[string]any{"action": action, "items": items}, cookie).Code
	}
	if post("add", []FileFavorite{first}, nil) != 401 || request(t, h, "GET", "/api/files/favorites", nil, nil).Code != 401 {
		t.Fatal("unauthenticated favorites allowed")
	}
	var wg sync.WaitGroup
	for i, f := range []FileFavorite{first, second} {
		wg.Add(1)
		go func(f FileFavorite, cookie *http.Cookie) {
			defer wg.Done()
			if post("add", []FileFavorite{f}, cookie) != 200 {
				t.Error("concurrent add failed")
			}
		}(f, []*http.Cookie{c1, c2}[i])
	}
	wg.Wait()
	get := func(cookie *http.Cookie) []FileFavorite {
		var items []FileFavorite
		w := request(t, h, "GET", "/api/files/favorites", nil, cookie)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &items) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return items
	}
	if len(get(c1)) != 2 || len(get(c2)) != 2 {
		t.Fatal("sessions did not share additions")
	}
	if post("remove", []FileFavorite{first}, c2) != 200 || len(get(c1)) != 1 {
		t.Fatal("removal not shared")
	}
	w := request(t, h, "GET", "/api/state", nil, c1)
	var snapshot struct {
		FileFavorites []FileFavorite `json:"fileFavorites"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil || len(snapshot.FileFavorites) != 1 || snapshot.FileFavorites[0].Deleted {
		t.Fatal("state poll did not expose active favorites", w.Body.String())
	}
	if post("import", []FileFavorite{first, second}, c1) != 200 || len(get(c2)) != 1 {
		t.Fatal("stale migration resurrected removed favorite")
	}
	second.History = append(second.History, FavoriteCrumb{ID: "/B", Name: "child"})
	if post("add", []FileFavorite{second}, c1) != 200 || len(get(c2)[0].History) != 1 {
		t.Fatal("legacy trail not trimmed")
	}
	store, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.initRuntime(filepath.Join(a.dataDir, "runtime")); err != nil {
		t.Fatal(err)
	}
	if items := visibleFavorites(store.snapshot().FileFavorites); len(items) != 1 || items[0].ID != second.ID {
		t.Fatal("favorites not persisted", items)
	}
	raw, err := os.ReadFile(filepath.Join(a.store.runtimeDir, "state.json"))
	var persisted struct {
		FileFavorites []FileFavorite `json:"fileFavorites"`
	}
	if err != nil || store.decodeJSONConfig("runtime/state", raw, &persisted) != nil || len(persisted.FileFavorites) != 2 {
		t.Fatal("favorites and removal marker not saved as runtime data", err)
	}
	raw, err = os.ReadFile(filepath.Join(a.store.dir, "state.json"))
	var config State
	if err != nil || store.decodeJSONConfig("state", raw, &config) != nil || len(config.FileFavorites) != 0 {
		t.Fatal("favorites leaked into configuration", err)
	}
	if post("add", []FileFavorite{first}, c2) != 200 || len(get(c1)) != 2 {
		t.Fatal("explicit re-add failed")
	}
	// Renaming the one management account preserves its preferences.
	w = request(t, h, "PUT", "/api/account", credentials{Username: "renamed", Password: "y"}, c1)
	if w.Code != 200 || len(get(w.Result().Cookies()[0])) != 2 {
		t.Fatal("account rename lost favorites", w.Body.String())
	}
}

func TestFileFavoritesValidationAndImportDoesNotOverwrite(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	w := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := w.Result().Cookies()[0]
	item := FileFavorite{Storage: s.ID, ID: "/A", Name: "A", History: []FavoriteCrumb{{ID: "/", Name: "A"}}}
	post := func(action string, f FileFavorite) int {
		return request(t, h, "POST", "/api/files/favorites", map[string]any{"action": action, "items": []FileFavorite{f}}, cookie).Code
	}
	for _, change := range []func(*FileFavorite){func(f *FileFavorite) { f.Storage = "missing" }, func(f *FileFavorite) { f.ID = "/" }, func(f *FileFavorite) { f.History = nil }, func(f *FileFavorite) { f.Name = "../bad" }, func(f *FileFavorite) { f.Name = "bad\x00name" }, func(f *FileFavorite) { f.ID = "bad\x00id" }} {
		bad := item
		change(&bad)
		if post("add", bad) != 400 {
			t.Fatal("invalid favorite accepted", bad)
		}
	}
	if post("add", item) != 200 {
		t.Fatal("add failed")
	}
	item.Name = "stale"
	if post("import", item) != 200 || a.store.snapshot().FileFavorites[0].Name != "A" {
		t.Fatal("import overwrote current favorite")
	}
	if post("toggle", item) != 400 {
		t.Fatal("invalid action accepted")
	}
}
