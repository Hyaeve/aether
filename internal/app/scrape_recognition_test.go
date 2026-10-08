package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScrapeDirectoryAndEpisodeRecognition(t *testing.T) {
	for _, tc := range []struct {
		path, title, kind string
		season, episode   int
	}{
		{"庆余年/第2季/第十二集.strm", "庆余年", "tv", 2, 12},
		{"Show/Season 2/03.mkv.strm", "Show", "tv", 2, 3},
		{"Show/Specials/E01.strm", "Show", "tv", 0, 1},
		{"Show/Show.1x03.strm", "Show", "tv", 1, 3},
		{"Show/Show.EP02.strm", "Show", "tv", 1, 2},
		{"庆余年第二季/庆余年第二季第03集.strm", "庆余年", "tv", 2, 3},
		{"Show/Show.S01E02_1080p.strm", "Show", "tv", 1, 2},
		{"Movies/Film.2020.DTS5.1.1080p.strm", "Film", "movie", 0, 0},
		{"1917.2019.strm", "1917", "movie", 0, 0},
	} {
		t.Run(tc.path, func(t *testing.T) {
			item := recognizeSTRMPath(tc.path)
			if item.Title != tc.title || item.Kind != tc.kind || item.Season != tc.season || item.Episode != tc.episode {
				t.Fatal(item)
			}
		})
	}
}

func TestScrapeReparsesLegacyAndGroupsSiblings(t *testing.T) {
	dir := t.TempDir()
	paths := []string{"Show/01.strm", "Show/02.strm", "Show/Season 2/03.strm", "Film.2020.strm"}
	previous := scrapeIndex{}
	for _, p := range paths {
		writeTest(t, filepath.Join(dir, filepath.FromSlash(p)), "https://example.test/video")
		previous.Items = append(previous.Items, scrapeItem{Path: p, Title: p, Kind: "movie", Status: "pending"})
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	items, err := scanSTRM(context.Background(), root, previous, scrapeSettings{})
	if err != nil {
		t.Fatal(err)
	}
	works := scrapeWorks(items)
	if len(works) != 2 {
		t.Fatal(works)
	}
	for _, work := range works {
		if work.Kind == "tv" && (work.Count != 3 || work.Title != "Show") {
			t.Fatal(work)
		}
	}
	for i := range items {
		if items[i].Kind == "tv" {
			items[i].TMDB = 55
			items[i].Manual = true
		}
	}
	again, err := scanSTRM(context.Background(), root, scrapeIndex{Items: items}, scrapeSettings{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range again {
		if item.Kind == "tv" && item.TMDB != 55 {
			t.Fatal("manual match lost", item)
		}
	}
}
