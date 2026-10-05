package app

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func (a *App) configBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	var total int64
	err := filepath.WalkDir(a.store.dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			if path != a.store.dir && (entry.Name() == "log" || entry.Name() == "cache") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "state.enc" && entry.Name() != "master.key" && filepath.Ext(path) != ".json" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if total > 32<<20 {
			return fs.ErrInvalid
		}
		relative, err := filepath.Rel(a.store.dir, path)
		if err != nil {
			return err
		}
		dst, err := archive.Create(filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(dst, io.LimitReader(src, info.Size()))
		return err
	})
	if err == nil {
		err = archive.Close()
	}
	if err != nil {
		fail(w, 500, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="aether-config-`+time.Now().Format("20060102")+`.zip"`)
	w.Write(buffer.Bytes())
}
