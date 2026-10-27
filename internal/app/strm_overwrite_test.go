package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSTRMOverwriteSettingsDefaultsPersistenceAndScope(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	read := func() map[string]any {
		t.Helper()
		w := request(t, h, "GET", "/api/strm/settings", nil, cookie)
		var value map[string]any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &value) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return value
	}
	if read()["overwriteMode"] != "skip" {
		t.Fatal("new and legacy configurations must default to skip")
	}
	for _, mode := range []string{"skip", "overwrite"} {
		w := request(t, h, "PUT", "/api/strm/settings", map[string]any{"address": "http://localhost:15151", "format": "pickcode_name", "overwriteMode": mode}, cookie)
		if w.Code != 200 || read()["overwriteMode"] != mode {
			t.Fatal("mode not saved", mode, w.Code, w.Body.String())
		}
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = reopened
	if read()["overwriteMode"] != "overwrite" {
		t.Fatal("mode was lost on restart")
	}
	for _, mode := range []any{"invalid", true, 7} {
		w := request(t, h, "PUT", "/api/strm/settings", map[string]any{"address": "http://localhost:15151", "format": "pickcode_name", "overwriteMode": mode}, cookie)
		if w.Code != 400 || read()["overwriteMode"] != "overwrite" {
			t.Fatal("invalid mode changed configuration", mode, w.Code)
		}
	}
	// Old settings clients and unrelated general settings cannot reset this field.
	w := request(t, h, "PUT", "/api/strm/settings", map[string]any{"address": "http://localhost:15152", "format": "pickcode"}, cookie)
	if w.Code != 200 || read()["overwriteMode"] != "overwrite" {
		t.Fatal("omitted field reset the selected mode", w.Code)
	}
	general := a.store.snapshot().Settings
	general.STRMOverwriteMode = ""
	w = request(t, h, "PUT", "/api/settings", general, cookie)
	if w.Code != 200 || read()["overwriteMode"] != "overwrite" {
		t.Fatal("general settings lost the independent STRM mode", w.Code)
	}
	if w := request(t, h, "PUT", "/api/strm/settings", map[string]any{}, nil); w.Code != http.StatusUnauthorized {
		t.Fatal("STRM settings are not protected", w.Code)
	}
	for _, address := range []string{"", "http://localhost:15151"} {
		st := a.store.snapshot()
		st.Settings.STRMURL, st.Settings.STRMOverwriteMode = address, "invalid"
		if err := validateResourceState(&st); err == nil {
			t.Fatal("invalid imported overwrite mode was accepted", address)
		}
	}
}

func TestSTRMOverwriteDoesNotChangeFeatureOrMetadataPolicy(t *testing.T) {
	for _, kind := range []string{"cas", "ed2k", "metadata"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			if err := a.store.update(func(st *State) error { st.Settings.STRMOverwriteMode = "overwrite"; return nil }); err != nil {
				t.Fatal(err)
			}
			name, suffix := "movie.mp4", "."+kind+".strm"
			task := Task{Kind: kind, Source: "/", Target: "out", Mode: "full"}
			if kind == "ed2k" {
				suffix = ".ed2k"
			} else if kind == "metadata" {
				name, suffix = "movie.nfo", ""
				task.Kind, task.MetadataExtensions = "strm", "nfo"
			}
			writeTest(t, filepath.Join(s.Config["root"], name), "media")
			n, err := a.executeTask(a.ctx, task, s)
			if err != nil || n != 1 {
				t.Fatal(n, err)
			}
			target := filepath.Join(a.outputDir, "out", name+suffix)
			writeTest(t, target, "retained existing output\n")
			task.Mode = "incremental"
			n, err = a.executeTask(a.ctx, task, s)
			body, readErr := os.ReadFile(target)
			if err != nil || readErr != nil || n != 0 || string(body) != "retained existing output\n" {
				t.Fatal("STRM policy leaked into feature or metadata writes", n, err, readErr, string(body))
			}
		})
	}
}

func TestSTRMOverwriteModeControlsGeneration(t *testing.T) {
	for _, mode := range []string{"default", "skip", "overwrite"} {
		for _, taskMode := range []string{"full", "incremental"} {
			t.Run(mode+"/"+taskMode, func(t *testing.T) {
				a := testApp(t)
				s := addLocal(t, a)
				writeTest(t, filepath.Join(s.Config["root"], "movie.mkv"), "media")
				target := filepath.Join(a.outputDir, "out", "movie.strm")
				writeTest(t, target, "existing STRM\n")
				if mode != "default" {
					h := a.Handler(t.TempDir())
					cookie := setupTest(t, a, h)
					w := request(t, h, "PUT", "/api/strm/settings", map[string]any{"address": "http://localhost:15151", "format": "pickcode_name", "overwriteMode": mode}, cookie)
					if w.Code != 200 {
						t.Fatal(w.Code, w.Body.String())
					}
				}
				n, err := a.executeTask(a.ctx, Task{Kind: "strm", Source: "/", Target: "out", Mode: taskMode}, s)
				body, readErr := os.ReadFile(target)
				if err != nil || readErr != nil {
					t.Fatal(err, readErr)
				}
				if mode == "overwrite" {
					if n != 1 || strings.TrimSpace(string(body)) != filepath.Join(s.Config["root"], "movie.mkv") {
						t.Fatal("overwrite selection did not regenerate existing STRM", n, string(body))
					}
				} else if n != 0 || string(body) != "existing STRM\n" {
					t.Fatal("default or explicit skip replaced an existing STRM", n, string(body))
				}
			})
		}
	}
}
