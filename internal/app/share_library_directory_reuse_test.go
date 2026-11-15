package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShareLibraryRemovedCardDirectoryCanBeReusedAfterRetainedOutputIsMoved(t *testing.T) {
	for _, provider := range []string{"quark", "mobile", "cas", "115"} {
		t.Run(provider, func(t *testing.T) {
			a := testApp(t)
			directory := t.TempDir()
			removed := quarkLibraryCard{ID: "removed", Secret: "https://pan.quark.cn/s/abcdef", Name: "Removed", ShareName: "Removed", Directory: directory, DirectoryName: "Old", CascadeCleanup: "off"}
			kept := removed
			kept.ID, kept.Secret, kept.Name, kept.ShareName, kept.DirectoryName = "kept", "https://pan.quark.cn/s/ghijkl", "Kept", "Kept", "Current"
			if provider == "mobile" || provider == "cas" {
				removed.Secret, kept.Secret = mobileLibrarySecret("abcdef", ""), mobileLibrarySecret("ghijkl", "")
			}
			if provider == "115" {
				removed.Secret, kept.Secret = "https://115.com/s/abcdef", "https://115.com/s/ghijkl"
				removed.StorageID, kept.StorageID = "pan", "pan"
			}
			writeTest(t, filepath.Join(quarkLibraryRoot(removed), "old.share.strm"), "retained output")
			writeTest(t, filepath.Join(quarkLibraryRoot(kept), "current.share.strm"), "current output")
			if err := a.store.update(func(st *State) error {
				cards := []quarkLibraryCard{removed, kept}
				switch provider {
				case "quark":
					st.QuarkLibraryCards = cards
				case "mobile":
					st.MobileLibraryCards = cards
				case "cas":
					st.CASLibraryCards = cards
				case "115":
					st.Library115Cards = cards
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			endpoint := "/api/virtual-library/" + provider + "/"
			if w := request(t, h, "DELETE", endpoint+removed.ID, nil, cookie); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			next := kept
			next.DirectoryName = removed.DirectoryName
			if err := shareLibraryNamesAvailable(a.store.snapshot(), next); err != nil {
				t.Fatal("removed card still reserves its name", err)
			}
			raw, _ := json.Marshal(next)
			body := map[string]any{}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			delete(body, "secret")
			delete(body, "shareName")
			delete(body, "scheduleAnchor")
			body["url"] = next.Secret
			w := request(t, h, "PUT", endpoint+kept.ID, body, cookie)
			var failure struct{ Error, Field string }
			if w.Code != 409 || json.Unmarshal(w.Body.Bytes(), &failure) != nil || failure.Field != "directoryName" || !strings.Contains(failure.Error, "生成文件保留") || !strings.Contains(failure.Error, "移走") {
				t.Fatal("retained disk output must explain the recovery action", w.Code, w.Body.String())
			}
			for name, want := range map[string]string{filepath.Join(quarkLibraryRoot(removed), "old.share.strm"): "retained output", filepath.Join(quarkLibraryRoot(kept), "current.share.strm"): "current output"} {
				if got, err := os.ReadFile(name); err != nil || string(got) != want {
					t.Fatal("rejected rename altered files", name, string(got), err)
				}
			}
			if err := os.Rename(quarkLibraryRoot(removed), filepath.Join(directory, "Retained")); err != nil {
				t.Fatal(err)
			}
			if w := request(t, h, "PUT", endpoint+kept.ID, body, cookie); w.Code != 200 {
				t.Fatal("free directory name was not reusable", w.Code, w.Body.String())
			}
			if got, err := os.ReadFile(filepath.Join(quarkLibraryRoot(next), "current.share.strm")); err != nil || string(got) != "current output" {
				t.Fatal("rename lost current files", string(got), err)
			}
			if _, err := os.Stat(quarkLibraryRoot(kept)); !os.IsNotExist(err) {
				t.Fatal("old current directory remains", err)
			}
		})
	}
}

func TestShareLibraryDiskConflictStillAllocatesBotSuffix(t *testing.T) {
	card := quarkLibraryCard{ID: "new", Directory: t.TempDir(), DirectoryName: "Series"}
	writeTest(t, filepath.Join(quarkLibraryRoot(card), "keep.txt"), "retained")
	err := shareLibraryDirectoryAvailable(State{}, card, quarkLibraryCard{})
	if !errors.Is(err, errShareLibraryDirectory) {
		t.Fatal("disk conflict lost its duplicate-name classification", err)
	}
	name, err := telegramLibraryDirectory(State{}, card)
	if err != nil || name != "Series_1" {
		t.Fatal("bot stopped allocating a free name", name, err)
	}
}
