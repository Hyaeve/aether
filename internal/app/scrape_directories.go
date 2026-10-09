package app

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"sort"
)

func (a *App) scrapeDirectories(w http.ResponseWriter, r *http.Request) {
	base, err := a.scrapeRoot(r.URL.Query().Get("taskId"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = "."
	}
	if !fs.ValidPath(dir) {
		fail(w, 400, errors.New("无效的库目录"))
		return
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		fail(w, 400, errors.New("STRM库目录不可读取"))
		return
	}
	defer root.Close()
	file, err := root.Open(dir)
	if err != nil {
		fail(w, 400, errors.New("子目录不可读取"))
		return
	}
	defer file.Close()
	dirs := []string{}
	for {
		if r.Context().Err() != nil {
			return
		}
		entries, readErr := file.ReadDir(256)
		for _, entry := range entries {
			if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
				dirs = append(dirs, path.Join(dir, entry.Name()))
			}
		}
		if len(dirs) > 5000 {
			fail(w, 400, errors.New("当前层级子目录超过5000项"))
			return
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				fail(w, 400, errors.New("读取子目录失败"))
				return
			}
			break
		}
	}
	sort.Strings(dirs)
	jsonResponse(w, 200, map[string]any{"root": base, "directories": dirs})
}
