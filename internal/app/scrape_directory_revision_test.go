package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScrapeDirectoryFirstAndSTRMOnly(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"电视剧/长安/Season 01/长安.S01E01.strm",
		"电视剧/长安/Season 01/大结局.STRM",
		"电视剧/长安/Season 02/第二集.strm",
		"电视剧/长安/特别篇/幕后.strm",
		"电视剧/长安/预告.strm",
		"电视剧/数字剧/01.strm", "电视剧/数字剧/02.strm",
		"电影/降临 (2016)/Arrival.1080p.strm", "电影/降临 (2016)/Arrival.2160p.strm",
		"Movies/One.2020.strm", "Movies/Two.2021.strm",
		"电影/降临 (2016)/poster.jpg", "电视剧/长安/tvshow.nfo",
		"电视剧/长安/Season 01/原视频.mkv", "readme.txt", "bad.strm.bak",
	}
	old := scrapeIndex{}
	for _, p := range files {
		writeTest(t, filepath.Join(dir, filepath.FromSlash(p)), "https://example.test/media")
		old.Items = append(old.Items, scrapeItem{Path: p, Title: p, Kind: "movie", TMDB: 99, Status: "ok"})
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	items, err := scanSTRM(context.Background(), root, old, scrapeSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 11 {
		t.Fatalf("non-STRM entered scan: %#v", items)
	}
	works := scrapeWorks(items)
	if len(works) != 5 {
		t.Fatalf("expected 5 directory/flat works: %#v", works)
	}
	found := map[string]scrapeWork{}
	for _, w := range works {
		found[w.Title] = w
	}
	if w := found["长安"]; w.Kind != "tv" || w.Count != 5 || w.TMDB != 0 {
		t.Fatalf("series split or stale movie: %#v", w)
	}
	if w := found["数字剧"]; w.Kind != "tv" || w.Count != 2 {
		t.Fatal(w)
	}
	if w := found["降临"]; w.Kind != "movie" || w.Count != 2 || w.Year != "2016" {
		t.Fatal(w)
	}
}

func TestScrapeIndexRejectsNonSTRMLegacyRows(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	index := scrapeIndex{Root: root, Items: []scrapeItem{{Path: "Film.strm"}, {Path: "poster.jpg"}, {Path: "../outside.strm"}, {Path: "tvshow.nfo"}}}
	if err := a.saveScrapeIndex("legacy", index); err != nil {
		t.Fatal(err)
	}
	got := a.loadScrapeIndex("legacy", root)
	if len(got.Items) != 1 || got.Items[0].Path != "Film.strm" {
		t.Fatal(got)
	}
}
