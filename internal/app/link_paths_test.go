package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"aether/internal/linkcore/pathmap"
)

func TestLinkMediaMapping(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	file := filepath.Join(root, "book.strm")
	if err := os.WriteFile(file, []byte("https://example.com/audio.m4a"), 0600); err != nil {
		t.Fatal(err)
	}
	link := MediaLink{Name: "ABS", Type: "audiobookshelf", Address: "http://127.0.0.1:13378", Port: 15994, APIKey: "test", Mode: "always", MediaRoot: root, UpstreamRoot: "/audiobooks"}
	if err := validateLink(&link, nil); err != nil {
		t.Fatal(err)
	}
	u := a.linkUpstream(link)
	if !slices.Contains(u.StrmRoots, "/audiobooks") {
		t.Fatal("same mount excluded")
	}
	mapper := pathmap.New([]pathmap.Rule{{From: u.PathMappings[0].From, To: u.PathMappings[0].To}}, u.StrmRoots)
	got, found, err := mapper.Locate("/audiobooks/book.strm")
	if err != nil || !found || filepath.Clean(got) != filepath.Clean(file) {
		t.Fatalf("%s %v %v", got, found, err)
	}
	if mapper.IsWithinRoots("/config/master.key") {
		t.Fatal("private config allowed")
	}
	link.MediaRoot = "/"
	if validateLink(&link, nil) == nil {
		t.Fatal("filesystem root allowed")
	}
}
