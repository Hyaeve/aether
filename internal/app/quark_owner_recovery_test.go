package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuarkLibraryRecoversMissingOwnerOnlyAfterInventory(t *testing.T) {
	for _, mode := range []string{"empty-intent", "unchanged-files", "modified-file", "unrelated-file", "changed-marker", "late-file-change", "late-new-file", "late-foreign-marker", "second-file-change", "second-directory-change"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			m := mockQuarkLibrary(t, a)
			card := newQuarkLibraryCard(t, a, "mkv")
			dir := quarkLibraryRoot(card)
			if mode == "empty-intent" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := a.store.update(func(st *State) error {
					initQuarkLibraryRuntime(st)
					st.QuarkLibraryRuntime.Roots[card.ID] = quarkLibraryOwnership{Directory: dir, Token: "persisted-before-marker"}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(dir, quarkLibraryMarker)); err != nil {
					t.Fatal(err)
				}
				writeTest(t, filepath.Join(dir, "Movie.nfo"), "scraped companion")
			}
			owner := a.store.snapshotWithLogLimit(0).QuarkLibraryRuntime.Roots[card.ID]
			switch mode {
			case "modified-file":
				writeTest(t, filepath.Join(dir, "Movie.mkv.strm"), "modified externally")
			case "unrelated-file":
				writeTest(t, filepath.Join(dir, "unrelated.txt"), "must remain")
			case "changed-marker":
				writeTest(t, filepath.Join(dir, quarkLibraryMarker), "foreign owner")
			}
			ctx := context.Background()
			checks := 0
			if strings.HasPrefix(mode, "late-") || strings.HasPrefix(mode, "second-") {
				ctx = context.WithValue(ctx, quarkLibraryGuardKey{}, func() error {
					checks++
					at := 1
					if strings.HasPrefix(mode, "second-") {
						at = 2
					}
					if checks != at {
						return nil
					}
					if mode == "late-file-change" || mode == "second-file-change" {
						return os.WriteFile(filepath.Join(dir, "Movie.mkv.strm"), []byte("changed during adoption"), 0600)
					}
					if mode == "second-directory-change" {
						if err := os.Rename(dir, dir+"-original"); err != nil {
							return err
						}
						writeTest(t, filepath.Join(dir, "unrelated.txt"), "new directory must remain")
						return nil
					}
					if mode == "late-new-file" {
						return os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("must remain"), 0600)
					}
					return os.WriteFile(filepath.Join(dir, quarkLibraryMarker), []byte("foreign owner"), 0600)
				})
			}
			root, err := openStrmReplaceRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			err = a.ensureQuarkLibraryOwnership(ctx, card, root)
			root.Close()
			allowed := mode == "empty-intent" || mode == "unchanged-files"
			if allowed && err != nil || !allowed && err == nil {
				t.Fatalf("recovery allowed=%t: %v", allowed, err)
			}
			marker, readErr := os.ReadFile(filepath.Join(dir, quarkLibraryMarker))
			if allowed {
				if readErr != nil || string(marker) != owner.Token {
					t.Fatal("owner token was not safely restored", string(marker), readErr)
				}
				if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
					t.Fatal("restored library cannot run", err)
				}
				m.files = nil
				if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
					t.Fatal("restored library cannot reconcile", err)
				}
				if _, err := os.Stat(filepath.Join(dir, "Movie.mkv.strm")); !os.IsNotExist(err) {
					t.Fatal("obsolete registered output was not cleaned", err)
				}
			} else if mode == "changed-marker" || mode == "late-foreign-marker" {
				if readErr != nil || string(marker) != "foreign owner" {
					t.Fatal("foreign marker overwritten", string(marker), readErr)
				}
			} else if !errors.Is(readErr, os.ErrNotExist) {
				t.Fatal("failed recovery left an ownership marker", readErr)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".aether-owner-") {
					t.Fatal("ownership publication leaked a temporary file")
				}
			}
		})
	}
}

func TestQuarkLibraryMissingOwnerNeverAuthorizesExpiry(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	dir := quarkLibraryRoot(card)
	if err := os.Remove(filepath.Join(dir, quarkLibraryMarker)); err != nil {
		t.Fatal(err)
	}
	if err := a.expireQuarkLibraryOutput(context.Background(), card); err == nil {
		t.Fatal("expiry adopted an unverified output directory")
	}
	if _, err := os.Stat(filepath.Join(dir, "Movie.mkv.strm")); err != nil {
		t.Fatal("expiry deleted output without ownership", err)
	}
}
