package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSharePreferencesSharedPersistenceAndProviderIsolation(t *testing.T) {
	a := testApp(t)
	storages := []Storage{
		{ID: "115-a", Type: "115", Enabled: true},
		{ID: "115-b", Type: "115", Enabled: true},
		{ID: "quark", Type: "quark", Enabled: true},
		{ID: "mobile", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native"}},
	}
	if err := a.store.update(func(st *State) error { st.Storages = storages; return nil }); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	login := request(t, h, "POST", "/api/auth/login", credentials{Username: "owner", Password: "x"}, nil)
	other := login.Result().Cookies()[0]
	for _, sid := range []string{"115-a", "quark", "115-b", "mobile"} {
		w := request(t, h, "PUT", "/api/files/share-preferences", map[string]any{"storageId": sid, "parent": "folder-" + sid, "label": "电影", "history": []FavoriteCrumb{{ID: "/", Name: "资料"}}}, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := request(t, h, "GET", "/api/files/share-preferences", nil, other)
	var got shareTransferPreferences
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Last["115"] != "115-b" || got.Last["quark"] != "quark" || got.Last["mobile"] != "mobile" || len(got.Directories) != 4 {
		t.Fatal("cross-client preferences lost", w.Code, w.Body.String())
	}
	store, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	restored := visibleSharePreferences(store.snapshot())
	if restored.Last["115"] != "115-b" || restored.Directories["115-b"].Parent != "folder-115-b" || len(restored.Directories["115-a"].History) != 1 {
		t.Fatal("preferences did not survive reopening", restored)
	}
	if err := a.store.update(func(st *State) error {
		st.Storages[1].Enabled = false
		st.Storages = st.Storages[:3]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w = request(t, h, "GET", "/api/files/share-preferences", nil, cookie)
	if w.Code != 200 || strings.Contains(w.Body.String(), "115-b") || strings.Contains(w.Body.String(), "mobile") {
		t.Fatal("unavailable destination exposed", w.Body.String())
	}
}

func TestSharePreferencesValidationAndAuthentication(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{{ID: "pool", Type: "115", Enabled: true}, {ID: "disabled", Type: "quark"}, {ID: "local", Type: "local", Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	for _, method := range []string{"GET", "PUT"} {
		if w := request(t, h, method, "/api/files/share-preferences", nil, nil); w.Code != http.StatusUnauthorized {
			t.Fatal("unprotected preferences", method, w.Code)
		}
	}
	for _, tc := range []struct{ storage, parent, label string }{
		{"missing", "/", "根目录"}, {"disabled", "/", "根目录"}, {"local", "/", "根目录"},
		{"pool", "", "根目录"}, {"pool", "bad\x00id", "目录"}, {"pool", "/", "../escape"},
	} {
		w := request(t, h, "PUT", "/api/files/share-preferences", map[string]any{"storageId": tc.storage, "parent": tc.parent, "label": tc.label}, cookie)
		if w.Code != 400 || a.store.snapshot().ShareTargets != nil {
			t.Fatal("invalid preferences accepted", tc, w.Code, w.Body.String())
		}
	}
	w := request(t, h, "PUT", "/api/files/share-preferences", map[string]any{"storageId": "pool", "parent": "/", "label": "根目录", "history": []FavoriteCrumb{{ID: "/", Name: "bad/name"}}}, cookie)
	if w.Code != 400 {
		t.Fatal("invalid breadcrumb accepted", w.Code)
	}
}
