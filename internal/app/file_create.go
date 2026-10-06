package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) createDirectory(ctx context.Context, s Storage, parent, name string) error {
	if !safeName(name) {
		return errors.New("目录名称无效")
	}
	if parent == "" || parent == "/" {
		parent = rootOf(s)
	}
	if s.Type != "local" {
		return a.cloudMkdir(ctx, s, parent, name)
	}
	rel, err := relative(parent)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(s.Config["root"])
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Mkdir(path.Join(rel, name), 0755)
}

// Resolve uploaded folder paths under the chosen directory, never as absolute IDs.
func (a *App) uploadParent(ctx context.Context, s Storage, parent, name string) (string, string, error) {
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", "", errors.New("上传路径无效")
	}
	parts := strings.Split(name, "/")
	if len(parts) > 128 {
		return "", "", errors.New("上传目录层级过深")
	}
	for _, part := range parts {
		if !safeName(part) {
			return "", "", errors.New("上传文件名无效")
		}
	}
	if parent == "" || parent == "/" {
		parent = rootOf(s)
	}
	for _, part := range parts[:len(parts)-1] {
		files, err := a.rawList(ctx, s, parent)
		if err != nil {
			return "", "", err
		}
		next := ""
		for _, f := range files {
			if f.Name == part {
				if !f.IsDir {
					return "", "", errors.New("上传路径与已有文件冲突")
				}
				next = f.ID
			}
		}
		if next == "" {
			if err := a.createDirectory(ctx, s, parent, part); err != nil {
				return "", "", err
			}
			// Some cloud providers expose a newly created folder after a short delay.
			for attempt := 0; attempt < 5 && next == ""; attempt++ {
				list, err := a.rawList(ctx, s, parent)
				if err != nil {
					return "", "", err
				}
				for _, f := range list {
					if f.Name == part && f.IsDir {
						next = f.ID
					}
				}
				if next == "" {
					select {
					case <-ctx.Done():
						return "", "", ctx.Err()
					case <-time.After(300 * time.Millisecond):
					}
				}
			}
			if next == "" {
				return "", "", errors.New("存储尚未确认新建目录，请刷新后重试")
			}
		}
		parent = next
	}
	return parent, parts[len(parts)-1], nil
}

func (a *App) storeUploadedFile(ctx context.Context, s Storage, parent, name, staged string) error {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if len(a.running) != 0 {
		return errors.New("有任务正在执行，请等待完成后上传")
	}
	current, err := a.store.storage(s.ID)
	if err != nil {
		return err
	}
	s = current
	parent, name, err = a.uploadParent(ctx, s, parent, name)
	if err != nil {
		return err
	}
	defer a.cache.clear()
	if s.Type != "local" {
		existing, err := a.cloudByName(ctx, s, parent, name)
		if err != nil {
			return err
		}
		if existing != nil {
			return errors.New("同名文件已存在，不覆盖")
		}
		return a.publishCloudFile(ctx, s, parent, name, staged)
	}
	rel, err := relative(parent)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(s.Config["root"])
	if err != nil {
		return err
	}
	defer root.Close()
	in, err := os.Open(staged)
	if err != nil {
		return err
	}
	defer in.Close()
	target := path.Join(rel, name)
	out, err := root.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, &cancelReader{ctx: ctx, reader: in})
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = root.Remove(target)
	}
	return err
}

type cancelReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *cancelReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (a *App) receiveFile(ctx context.Context, s Storage, parent, name string, body io.Reader, size int64) error {
	const limit = int64(100 << 30)
	if size > limit {
		return errors.New("文件过大（单文件上限 100 GiB）")
	}
	parts := strings.Split(name, "/")
	if len(parts) > 128 {
		return errors.New("上传目录层级过深")
	}
	for _, part := range parts {
		if !safeName(part) || strings.ContainsAny(part, "\x00\r\n") {
			return errors.New("上传路径无效")
		}
	}
	dir := filepath.Join(a.dataDir, "cache", "file-upload")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	n, err := io.Copy(f, io.LimitReader(&cancelReader{ctx, body}, limit+1))
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if n > limit || (size >= 0 && n != size) {
		return errors.New("文件过大或上传内容不完整（单文件上限 100 GiB）")
	}
	if err := a.storeUploadedFile(ctx, s, parent, name, f.Name()); err != nil {
		return err
	}
	a.uploaded.Add(n)
	a.store.event("info", "files", "文件已保存："+name)
	return nil
}

func (a *App) uploadFile(w http.ResponseWriter, r *http.Request) {
	s, err := a.store.storage(r.URL.Query().Get("storage"))
	if err == nil {
		err = a.receiveFile(r.Context(), s, r.URL.Query().Get("parent"), r.URL.Query().Get("name"), r.Body, r.ContentLength)
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *App) offlineFile(w http.ResponseWriter, r *http.Request) {
	var input struct{ StorageID, Parent, Name, URL string }
	if !decode(w, r, &input) {
		return
	}
	u, err := url.Parse(input.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || !safeName(input.Name) {
		fail(w, 400, errors.New("请输入 HTTP(S) 下载地址和有效文件名"))
		return
	}
	s, err := a.store.storage(input.StorageID)
	if err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.User != nil {
			return errors.New("下载重定向无效")
		}
		return nil
	}}
	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	res, err := client.Do(req)
	if err == nil {
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			err = fmt.Errorf("下载服务返回 %d", res.StatusCode)
		} else {
			err = a.receiveFile(ctx, s, input.Parent, input.Name, res.Body, res.ContentLength)
		}
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
