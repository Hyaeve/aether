package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestQuarkCascadeUsesConfiguredExtensionsOnDisk(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	card.CascadeCleanup = "nfo;txt"
	if err := a.store.update(func(st *State) error { st.QuarkLibraryCards[0] = card; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	root := quarkLibraryRoot(card)
	for _, name := range []string{"Movie.nfo", "Movie.txt", "Movie-poster.jpg", "personal.txt"} {
		writeTest(t, filepath.Join(root, name), name)
	}
	m.files = []shareEntry{{ID: "next", Name: "Next.mkv", Size: 10, Token: "file-token"}}
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Movie.nfo", "Movie.txt", "Movie.mkv.share.strm"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatal("selected stale file retained", name, err)
		}
	}
	for _, name := range []string{"Movie-poster.jpg", "personal.txt", "Next.mkv.share.strm"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal("unselected file removed", name, err)
		}
	}
}

func TestShareLibraryCascadeExtensionSelection(t *testing.T) {
	for _, value := range []string{"", "on", "off", "jpg;nfo", ".TXT;png"} {
		card := quarkLibraryCard{Secret: "https://pan.quark.cn/s/abcdef", DirectoryName: "Film", CascadeCleanup: value}
		if err := validateQuarkLibraryCard(&card); err != nil {
			t.Fatal(value, err)
		}
	}
	for _, value := range []string{"strm", "png;cas", "ed2k", "../jpg", "*.nfo"} {
		card := quarkLibraryCard{DirectoryName: "Film", CascadeCleanup: value}
		if validateShareLibraryFields(&card) == nil {
			t.Fatal("invalid cascade accepted", value)
		}
	}
	a := testApp(t)
	dir := t.TempDir()
	for _, name := range []string{"Film.mkv.share.strm", "Film.nfo", "Film.jpg", "Film.txt", "Other.txt"} {
		writeTest(t, filepath.Join(dir, name), name)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	digest, err := quarkLibraryDigest(root, "Film.mkv.share.strm")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]quarkLibraryFile{"media": {CardID: "c", Relative: "Film.mkv.share.strm", Media: true, Digest: digest}}
	for _, selection := range []struct {
		value    string
		expected []string
	}{{"nfo", []string{"Film.nfo"}}, {"jpg;txt", []string{"Film.jpg", "Film.txt"}}} {
		card := quarkLibraryCard{ID: "c", CascadeCleanup: selection.value}
		plan, err := mobileLibraryCompanions(a.ctx, root, card, files, map[string]bool{})
		if err != nil || len(plan) != len(selection.expected) {
			t.Fatal(selection, plan, err)
		}
		for i, p := range plan {
			if p.name != selection.expected[i] {
				t.Fatal("unselected/unrelated companion", p.name, selection)
			}
		}
		plan, err = mobileLibraryCompanions(a.ctx, root, card, files, map[string]bool{"Film.mkv.share.strm": true})
		if err != nil || len(plan) != 0 {
			t.Fatal("live media companions selected", plan, err)
		}
	}
}
