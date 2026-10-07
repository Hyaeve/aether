package app

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"
)

// Validate the manifest before streaming; do not finalize a ZIP on partial failure.
func (a *App) fileArchive(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 24*time.Hour)
	defer cancel()
	s, err := a.store.storage(r.URL.Query().Get("storage"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	rootID := r.URL.Query().Get("id")
	items, err := a.listFiles(ctx, s, r.URL.Query().Get("parent"), 0, false)
	if err != nil {
		fail(w, 400, err)
		return
	}
	var selected *File
	for _, item := range items {
		if item.ID == rootID && item.IsDir {
			copy := item
			selected = &copy
			break
		}
	}
	if selected == nil || !safeName(selected.Name) {
		fail(w, 400, errors.New("目录不存在，请刷新后重试"))
		return
	}
	type archiveEntry struct {
		File
		Name string
	}
	manifest := []archiveEntry{}
	seen := map[string]bool{}
	names := map[string]bool{}
	var scan func(File, string, int) error
	scan = func(file File, prefix string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 128 || len(manifest) >= 100000 || !safeName(file.Name) || strings.ContainsAny(file.Name, "\x00\r\n") || names[prefix] {
			return errors.New("目录过大、存在重复名称或文件名不安全")
		}
		names[prefix] = true
		manifest = append(manifest, archiveEntry{file, prefix})
		if !file.IsDir {
			return nil
		}
		if seen[file.ID] {
			return errors.New("目录存在循环")
		}
		seen[file.ID] = true
		id := file.ID
		if s.Type != "local" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
		items, err := a.listFiles(ctx, s, id, 0, false)
		if err != nil {
			return err
		}
		for _, item := range items {
			if err := scan(item, prefix+"/"+item.Name, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := scan(*selected, selected.Name, 0); err != nil {
		fail(w, 400, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": selected.Name + ".zip"}))
	w.Header().Set("Cache-Control", "no-store")
	archive := zip.NewWriter(countWriter{w, &a.downloaded})
	write := func() error {
		for _, item := range manifest {
			header := &zip.FileHeader{Name: item.Name, Method: zip.Store}
			header.Modified = item.Modified
			if item.IsDir {
				header.Name += "/"
				header.SetMode(os.ModeDir | 0755)
				if _, err := archive.CreateHeader(header); err != nil {
					return err
				}
				continue
			}
			header.SetMode(0600)
			d, err := a.downloadWithUA(ctx, s, item.ID, item.PickCode, r.UserAgent())
			if err != nil {
				return err
			}
			var reader io.ReadCloser
			if s.Type == "local" {
				root, err := os.OpenRoot(s.Config["root"])
				if err != nil {
					return err
				}
				f, err := root.Open(d.Local)
				root.Close()
				if err != nil {
					return err
				}
				reader = f
			} else {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
				if err != nil {
					return err
				}
				req.Header = d.Headers.Clone()
				// Never forward cloud credentials across redirects.
				client := &http.Client{Transport: apiClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
				res, err := client.Do(req)
				if err != nil {
					return errors.New("读取上游文件失败")
				}
				if res.StatusCode != http.StatusOK {
					res.Body.Close()
					return fmt.Errorf("下载文件失败：HTTP %d", res.StatusCode)
				}
				reader = res.Body
			}
			dst, err := archive.CreateHeader(header)
			if err != nil {
				reader.Close()
				return err
			}
			n, err := io.Copy(dst, reader)
			reader.Close()
			if err != nil {
				return errors.New("文件传输中断")
			}
			if item.Size > 0 && n != item.Size {
				return errors.New("文件大小发生变化，请重试")
			}
		}
		return archive.Close()
	}
	if err := write(); err != nil {
		a.store.event("warn", "files", "文件夹归档下载未完成")
		panic(http.ErrAbortHandler)
	}
}
