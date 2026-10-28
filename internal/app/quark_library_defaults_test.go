package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuarkLibraryFirstStorageAlwaysIncluded(t *testing.T) {
	st := State{Storages: []Storage{{ID: "off", Type: "quark"}, {ID: "first", Type: "quark", Enabled: true}, {ID: "second", Type: "quark", Enabled: true}}}
	cfg := effectiveQuarkLibrary(st)
	cfg.StorageIDs = []string{"second"}
	st.QuarkLibrary = &cfg
	got := effectiveQuarkLibrary(st)
	if len(got.StorageIDs) != 2 || got.StorageIDs[0] != "first" || got.StorageIDs[1] != "second" {
		t.Fatal("first available Quark pool must remain selected", got.StorageIDs)
	}
	if len(st.QuarkLibrary.StorageIDs) != 1 {
		t.Fatal("reading effective defaults mutated stored config")
	}
}

func TestQuarkLibraryDisabledDraftWithoutDirectory(t *testing.T) {
	card := quarkLibraryCard{ID: "draft", Secret: "https://pan.quark.cn/s/example", Name: "Draft", DirectoryName: "Draft"}
	if err := validateQuarkLibraryCard(&card); err != nil {
		t.Fatal("disabled draft should not require an output directory", err)
	}
	card.Enabled = true
	if err := validateQuarkLibraryCard(&card); err == nil {
		t.Fatal("enabled card without output directory accepted")
	}
	card.Directory = filepath.Join(t.TempDir(), "output")
	if err := validateQuarkLibraryCard(&card); err != nil {
		t.Fatal(err)
	}
}

func TestQuarkLibraryGuardClosesUnpublishedOutput(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	rejected := false
	ctx := context.WithValue(context.Background(), quarkLibraryGuardKey{}, func() error {
		items, _ := os.ReadDir(quarkLibraryRoot(card))
		for _, item := range items {
			if strings.HasPrefix(item.Name(), ".aether-quark-") {
				rejected = true
				return errors.New("TG authorization changed before publication")
			}
		}
		return nil
	})
	_, err := a.runQuarkLibrary(ctx, card)
	if !rejected || err == nil {
		t.Fatal("publication guard did not stop the write", err)
	}
	items, err := os.ReadDir(quarkLibraryRoot(card))
	if err != nil || len(items) != 1 || items[0].Name() != quarkLibraryMarker {
		t.Fatal("unpublished output or open temporary file was left behind", items, err)
	}
}

func TestQuarkLibrarySettingsDefaultDirectoryAPI(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, Storage{ID: "second", Type: "quark", Enabled: true})
		return nil
	})
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	output := t.TempDir()
	cfg := effectiveQuarkLibrary(a.store.snapshot())
	cfg.StorageIDs, cfg.Directory = []string{"second"}, output
	w := request(t, h, "PUT", "/api/virtual-library/quark/settings", cfg, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var saved quarkLibrarySettings
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || saved.Directory != output || len(saved.StorageIDs) != 2 || saved.StorageIDs[0] != "q" {
		t.Fatal("mandatory storage/output missing", saved, err)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || effectiveQuarkLibrary(reopened.snapshot()).Directory != output {
		t.Fatal("default output did not persist", err)
	}
	for _, invalid := range []string{"relative/path", filepath.Join(output, "missing"), output + "\nother"} {
		cfg.Directory = invalid
		w = request(t, h, "PUT", "/api/virtual-library/quark/settings", cfg, cookie)
		if w.Code != 400 || effectiveQuarkLibrary(a.store.snapshot()).Directory != output {
			t.Fatal("invalid output was accepted or changed prior settings", invalid, w.Code)
		}
	}
	w = request(t, h, "PUT", "/api/virtual-library/quark/settings", saved, nil)
	if w.Code != 401 {
		t.Fatal("settings not administrator-protected")
	}
}

func TestQuarkLibraryDraftAPIActivationAndNoStorageEdit(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	body := map[string]any{"url": "https://pan.quark.cn/s/first", "enabled": false}
	w := request(t, h, "POST", "/api/virtual-library/quark", body, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	first := a.store.snapshot().QuarkLibraryCards[0]
	body["url"] = "https://pan.quark.cn/s/second"
	w = request(t, h, "POST", "/api/virtual-library/quark", body, cookie)
	if w.Code != 200 || len(a.store.snapshot().QuarkLibraryCards) != 2 || m.saves != 0 {
		t.Fatal("multiple outputless drafts overlap", w.Code, w.Body.String())
	}
	output := t.TempDir()
	cfg := effectiveQuarkLibrary(a.store.snapshot())
	cfg.Directory, cfg.MediaExtensions, cfg.MetadataExtensions = output, "mkv", ""
	w = request(t, h, "PUT", "/api/virtual-library/quark/settings", cfg, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body["url"], body["enabled"] = first.Secret, true
	w = request(t, h, "PUT", "/api/virtual-library/quark/"+first.ID, body, cookie)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if result := waitQuarkLibrary(t, a, first.ID); result.Status != "completed" {
		t.Fatal(result)
	}
	updated := a.store.snapshot().QuarkLibraryCards[0]
	if updated.ID != first.ID || !updated.Enabled || updated.Directory != output || len(a.store.snapshot().QuarkLibraryRuntime.Files) != 1 {
		t.Fatal("draft not enabled in place using default output")
	}
	second := a.store.snapshot().QuarkLibraryCards[1]
	a.store.update(func(st *State) error { st.Storages = nil; return nil })
	body = map[string]any{"url": second.Secret, "enabled": false, "name": "Edited draft"}
	w = request(t, h, "PUT", "/api/virtual-library/quark/"+second.ID, body, cookie)
	if w.Code != 200 || a.store.snapshot().QuarkLibraryCards[1].Name != "Edited draft" {
		t.Fatal("disabled draft cannot be edited without storage", w.Code, w.Body.String())
	}
	body["enabled"], body["directory"] = true, output
	w = request(t, h, "PUT", "/api/virtual-library/quark/"+second.ID, body, cookie)
	if w.Code != 400 {
		t.Fatal("draft enabled without a usable storage")
	}
}
