package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func libraryRecoveryRecords(st State, provider string) (map[string]quarkLibraryOwnership, map[string]quarkLibraryFile, map[string]quarkLibraryResult) {
	switch provider {
	case "mobile":
		return st.MobileLibraryRuntime.Roots, st.MobileLibraryRuntime.Files, st.MobileLibraryRuntime.Results
	case "115":
		return st.Library115Runtime.Roots, st.Library115Runtime.Files, st.Library115Runtime.Results
	default:
		return st.QuarkLibraryRuntime.Roots, st.QuarkLibraryRuntime.Files, st.QuarkLibraryRuntime.Results
	}
}

func TestShareLibraryManualExecutionRecoversIdentity(t *testing.T) {
	for _, provider := range []string{"quark", "mobile", "115"} {
		for _, mode := range []string{"remounted", "copied", "empty"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				var a *App
				var card quarkLibraryCard
				var handler http.Handler
				var cookie *http.Cookie
				var run func() (quarkLibraryResult, error)
				switch provider {
				case "quark":
					a = testApp(t)
					mockQuarkLibrary(t, a)
					card = newQuarkLibraryCard(t, a, "mkv")
					handler = a.Handler(t.TempDir())
					cookie = setupTest(t, a, handler)
					run = func() (quarkLibraryResult, error) { return a.runQuarkLibrary(a.ctx, card) }
				case "mobile":
					a = testApp(t)
					_, card = mockMobileLibrary(t, a)
					handler, cookie = mobileLibraryHandler(t, a)
					run = func() (quarkLibraryResult, error) { return a.runMobileLibrary(a.ctx, card) }
				case "115":
					f := newVirtual115NameFixture(t, "")
					a, handler, cookie = f.a, f.handler, f.cookie
					card = quarkLibraryCard{ID: "snapshot", StorageID: "115", Secret: "https://115.com/s/sample", Name: "Show", ShareName: "Show", Directory: t.TempDir(), DirectoryName: "Show", MediaExtensions: "mkv", Enabled: true}
					if err := a.store.update(func(st *State) error { st.Library115Cards = []quarkLibraryCard{card}; return nil }); err != nil {
						t.Fatal(err)
					}
					run = func() (quarkLibraryResult, error) { return a.runLibrary115(a.ctx, card) }
				}
				if _, err := run(); err != nil {
					t.Fatal(err)
				}
				roots, files, _ := libraryRecoveryRecords(a.store.snapshot(), provider)
				before := roots[card.ID]
				content := map[string]string{}
				for _, file := range files {
					if file.CardID == card.ID {
						raw, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), file.Relative))
						if err != nil {
							t.Fatal(err)
						}
						content[file.Relative] = string(raw)
					}
				}
				if len(content) == 0 || before.Identity == "" {
					t.Fatal("missing baseline outputs or identity")
				}
				if mode == "remounted" {
					if err := a.store.update(func(st *State) error {
						r, _, _ := libraryRecoveryRecords(*st, provider)
						old := r[card.ID]
						old.Identity = "previous-volume:" + old.Identity
						r[card.ID] = old
						before = old
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Rename(quarkLibraryRoot(card), quarkLibraryRoot(card)+"-original"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(quarkLibraryRoot(card), 0700); err != nil {
						t.Fatal(err)
					}
					if mode == "copied" {
						for name, raw := range content {
							writeTest(t, filepath.Join(quarkLibraryRoot(card), name), raw)
						}
					}
				}
				// Ordinary/background execution must not silently adopt this root.
				if _, err := run(); err == nil {
					t.Fatal("automatic execution adopted a changed directory")
				}
				endpoint := "/api/virtual-library/" + provider + "/" + card.ID + "/run"
				if w := request(t, handler, "POST", endpoint, nil, nil); w.Code != 401 {
					t.Fatal("unauthorized recovery", w.Code)
				}
				if w := request(t, handler, "POST", endpoint, nil, cookie); w.Code != http.StatusAccepted {
					t.Fatal(w.Code, w.Body.String())
				}
				a.wg.Wait()
				roots, _, results := libraryRecoveryRecords(a.store.snapshot(), provider)
				owner := roots[card.ID]
				result := results[card.ID]
				if result.Status != "completed" || owner.Identity == "" || owner.Identity == before.Identity {
					t.Fatal("manual recovery failed", owner, result)
				}
				if mode == "empty" && result.Generated != len(content) {
					t.Fatal("missing outputs were not regenerated", result)
				}
				for name, want := range content {
					if raw, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), name)); err != nil || string(raw) != want {
						t.Fatal("output changed during recovery", name, err)
					}
					if mode == "copied" {
						if raw, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card)+"-original", name)); err != nil || string(raw) != want {
							t.Fatal("original directory changed", name, err)
						}
					}
				}
				if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), quarkLibraryMarker)); !os.IsNotExist(err) {
					t.Fatal("recovery created an owner marker", err)
				}
				if _, err := run(); err != nil {
					t.Fatal("recovered directory still fails ordinary execution", err)
				}
				reopened, err := NewStore(a.store.dir)
				if err == nil {
					err = reopened.initModules()
				}
				if err == nil {
					err = reopened.initRuntime(filepath.Join(a.dataDir, "runtime"))
				}
				if err != nil {
					t.Fatal(err)
				}
				persisted, _, _ := libraryRecoveryRecords(reopened.snapshot(), provider)
				if persisted[card.ID] != owner {
					t.Fatal("recovered identity was not durable")
				}
				if err := a.store.update(func(st *State) error {
					r, _, _ := libraryRecoveryRecords(*st, provider)
					old := r[card.ID]
					old.Identity = before.Identity
					r[card.ID] = old
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if _, err := run(); err == nil {
					t.Fatal("manual approval leaked into a later automatic run")
				}
			})
		}
	}
}

func TestShareLibraryIdentityRecoveryInventory(t *testing.T) {
	for _, mode := range []string{"intact", "missing-output", "valid-legacy", "modified", "unknown", "no-evidence", "foreign-marker", "late-file", "late-new-file", "late-root", "canceled", "configuration-changed", "save-failed"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			card := quarkLibraryCard{ID: "inventory", Name: "Inventory", Directory: t.TempDir(), DirectoryName: "Library"}
			directory := quarkLibraryRoot(card)
			writeTest(t, filepath.Join(directory, "Film.mkv.share.strm"), "registered strm")
			writeTest(t, filepath.Join(directory, "Film.nfo"), "companion")
			sum := sha256.Sum256([]byte("registered strm"))
			files := map[string]quarkLibraryFile{"video": {CardID: card.ID, Relative: "Film.mkv.share.strm", Digest: hex.EncodeToString(sum[:]), Media: true}}
			previous := quarkLibraryOwnership{Directory: directory, Identity: "old-device:old-node", Token: "old-token"}
			switch mode {
			case "missing-output":
				files["missing"] = quarkLibraryFile{CardID: card.ID, Relative: "Missing.mkv.share.strm", Digest: "missing-digest", Media: true}
			case "valid-legacy":
				writeTest(t, filepath.Join(directory, quarkLibraryMarker), previous.Token)
			case "modified":
				writeTest(t, filepath.Join(directory, "Film.mkv.share.strm"), "external edit")
			case "unknown":
				writeTest(t, filepath.Join(directory, "personal.txt"), "unrelated")
			case "no-evidence":
				if err := os.Remove(filepath.Join(directory, "Film.mkv.share.strm")); err != nil {
					t.Fatal(err)
				}
			case "foreign-marker":
				writeTest(t, filepath.Join(directory, quarkLibraryMarker), "foreign-token")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			checks := 0
			check := func() error {
				checks++
				if checks == 2 {
					switch mode {
					case "late-file":
						writeTest(t, filepath.Join(directory, "Film.mkv.share.strm"), "late external edit")
					case "late-new-file":
						writeTest(t, filepath.Join(directory, "personal.txt"), "late unrelated")
					case "late-root":
						if err := os.Rename(directory, directory+"-original"); err != nil {
							t.Fatal(err)
						}
						writeTest(t, filepath.Join(directory, "personal.txt"), "new root")
					case "canceled":
						cancel()
					case "configuration-changed":
						return errors.New("card changed")
					}
				}
				return ctx.Err()
			}
			root, err := openStrmReplaceRoot(directory)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			before, err := scanQuarkLibraryDisk(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			saved := previous
			err = a.ensureLibraryIdentity(ctx, card, root, previous, files, true, check, func(old, next quarkLibraryOwnership) error {
				if old != previous || mode == "save-failed" {
					return errors.New("record changed or disk write failed")
				}
				saved = next
				return nil
			})
			allowed := mode == "intact" || mode == "missing-output" || mode == "valid-legacy"
			if (err == nil) != allowed || (saved != previous) != allowed {
				t.Fatal("unexpected recovery/record change", mode, saved, err)
			}
			if allowed {
				if err := verifyQuarkLibraryOwner(root, saved); err != nil {
					t.Fatal(err)
				}
				if _, err := root.Lstat(quarkLibraryMarker); !os.IsNotExist(err) {
					t.Fatal("legacy marker remained", err)
				}
			}
			after, scanErr := scanQuarkLibraryDisk(context.Background(), root)
			if scanErr != nil || len(after) < len(before) {
				t.Fatal("recovery deleted user files", scanErr)
			}
			if mode == "foreign-marker" {
				if raw, err := root.ReadFile(quarkLibraryMarker); err != nil || string(raw) != "foreign-token" {
					t.Fatal("foreign marker changed", err)
				}
			}
		})
	}
}

func TestShareLibraryInvalidCleanupCannotRecoverManualRun(t *testing.T) {
	for _, provider := range []string{"quark", "mobile"} {
		t.Run(provider, func(t *testing.T) {
			a := testApp(t)
			var card quarkLibraryCard
			var run func() (quarkLibraryResult, error)
			var clean func(context.Context, quarkLibraryCard) (int, error)
			if provider == "quark" {
				mockQuarkLibrary(t, a)
				card = newQuarkLibraryCard(t, a, "mkv")
				run = func() (quarkLibraryResult, error) { return a.runQuarkLibrary(a.ctx, card) }
				clean = a.cleanInvalidQuarkLibrary
			} else {
				_, card = mockMobileLibrary(t, a)
				run = func() (quarkLibraryResult, error) { return a.runMobileLibrary(a.ctx, card) }
				clean = a.cleanInvalidMobileLibrary
			}
			if _, err := run(); err != nil {
				t.Fatal(err)
			}
			if err := a.store.update(func(st *State) error {
				roots, _, _ := libraryRecoveryRecords(*st, provider)
				owner := roots[card.ID]
				owner.Identity = "previous-device:previous-node"
				roots[card.ID] = owner
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			roots, files, _ := libraryRecoveryRecords(a.store.snapshot(), provider)
			before := roots[card.ID]
			ctx := context.WithValue(a.ctx, libraryManualRunKey{}, true)
			if removed, err := clean(ctx, card); err == nil || removed != 0 {
				t.Fatal("invalid share cleanup inherited manual recovery", removed, err)
			}
			for _, file := range files {
				if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), file.Relative)); err != nil {
					t.Fatal("cleanup deleted unverified output", err)
				}
			}
			roots, _, _ = libraryRecoveryRecords(a.store.snapshot(), provider)
			if roots[card.ID] != before {
				t.Fatal("cleanup changed identity")
			}
		})
	}
}
