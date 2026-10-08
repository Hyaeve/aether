package app

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
)

// Only known artwork next to a scanned work is exposed, never an arbitrary path.
func openScrapeCover(root *os.Root, item scrapeItem) (*os.File, os.FileInfo) {
	dir := scrapeWorkDir(item.Path)
	names := []string{}
	if !genericMediaDir(path.Base(dir)) {
		for _, base := range []string{"cover", "poster", "folder"} {
			for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
				names = append(names, path.Join(dir, base+ext))
			}
		}
	}
	stem := strings.TrimSuffix(item.Path, path.Ext(item.Path))
	for _, suffix := range []string{"-poster.jpg", "-poster.png", "-cover.jpg", "-cover.png"} {
		names = append(names, stem+suffix)
	}
	for _, name := range names {
		f, err := root.Open(name)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= 20<<20 {
			header := make([]byte, 512)
			n, _ := f.Read(header)
			mime := http.DetectContentType(header[:n])
			if mime == "image/jpeg" || mime == "image/png" || mime == "image/webp" {
				f.Seek(0, io.SeekStart)
				return f, info
			}
		}
		f.Close()
	}
	return nil, nil
}

func (a *App) scrapeCover(w http.ResponseWriter, r *http.Request) {
	rootName, err := a.scrapeRoot(r.URL.Query().Get("taskId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	index := a.loadScrapeIndex(r.URL.Query().Get("taskId"), rootName)
	for _, item := range index.Items {
		if item.Path != r.URL.Query().Get("path") {
			continue
		}
		root, err := os.OpenRoot(rootName)
		if err != nil {
			break
		}
		defer root.Close()
		f, info := openScrapeCover(root, item)
		if f == nil {
			break
		}
		defer f.Close()
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
		return
	}
	http.NotFound(w, r)
}

func localScrapePosters(root *os.Root, task string, items []scrapeItem) {
	for i := range items {
		if f, info := openScrapeCover(root, items[i]); f != nil {
			f.Close()
			q := url.Values{"taskId": {task}, "path": {items[i].Path}, "v": {info.ModTime().UTC().Format("20060102150405.000000000")}}
			items[i].Poster = "/api/strm-scrape/cover?" + q.Encode()
		}
	}
}
