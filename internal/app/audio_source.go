package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

func audioExtension(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".mp3", ".m4a", ".m4b", ".aac", ".flac", ".wav", ".ogg", ".oga", ".opus", ".wma", ".aiff", ".aif", ".alac":
		return true
	}
	return false
}

// Metadata requests stay authenticated and use the same cloud reader as DAV.
func (a *App) audioSource(w http.ResponseWriter, r *http.Request) {
	s, err := a.store.storage(r.URL.Query().Get("storage"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	parent := r.URL.Query().Get("parent")
	files, err := a.listFiles(ctx, s, parent, 0, false)
	if err != nil {
		fail(w, 400, errors.New("无法读取音乐目录"))
		return
	}
	var selected *File
	for _, f := range files {
		if f.ID == r.URL.Query().Get("id") && !f.IsDir && safeName(f.Name) {
			copy := f
			selected = &copy
			break
		}
	}
	if selected == nil {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(path.Ext(selected.Name))
	limit := int64(2 << 20)
	if audioExtension(selected.Name) {
		if r.Method == "GET" {
			value := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
			parts := strings.Split(value, "-")
			if !strings.HasPrefix(r.Header.Get("Range"), "bytes=") || len(parts) != 2 {
				fail(w, 400, errors.New("音乐标签仅支持有界读取"))
				return
			}
			start, e1 := strconv.ParseInt(parts[0], 10, 64)
			end, e2 := strconv.ParseInt(parts[1], 10, 64)
			if e1 != nil || e2 != nil || start < 0 || end < start || end-start >= 1<<20 {
				fail(w, 400, errors.New("音乐标签读取范围过大"))
				return
			}
		}
	} else {
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" {
			limit = 8 << 20
		} else if ext != ".lrc" {
			http.NotFound(w, r)
			return
		}
		if selected.Size < 0 || selected.Size > limit {
			fail(w, 400, errors.New("封面或歌词超过读取限额"))
			return
		}
	}
	file, err := (mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: parent}}).OpenFile(ctx, "/"+selected.Name, os.O_RDONLY, 0)
	if err != nil {
		fail(w, 400, errors.New("无法读取音乐资料"))
		return
	}
	defer file.Close()
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if ext == ".lrc" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	http.ServeContent(w, r, selected.Name, selected.Modified, file)
}
