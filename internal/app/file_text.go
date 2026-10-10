package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

// Text edits use the same rooted, staged publication as DAV writes.
func (a *App) fileText(w http.ResponseWriter, r *http.Request) {
	a.textMu.Lock()
	defer a.textMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	s, err := a.store.storage(r.URL.Query().Get("storage"))
	if err != nil {
		fail(w, 404, errors.New("存储不可用"))
		return
	}
	parent := r.URL.Query().Get("parent")
	files, err := a.rawList(ctx, s, parent)
	if err != nil {
		fail(w, 400, err)
		return
	}
	var chosen *File
	for _, f := range files {
		if f.ID == r.URL.Query().Get("id") && !f.IsDir && safeName(f.Name) {
			copy := f
			chosen = &copy
			break
		}
	}
	if chosen == nil {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(path.Ext(chosen.Name))
	if ext != ".strm" && ext != ".nfo" {
		fail(w, 400, errors.New("仅支持 STRM 和 NFO 编辑"))
		return
	}
	if chosen.Size > 2<<20 {
		fail(w, 400, errors.New("文本超过 2 MiB"))
		return
	}
	fs := mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: parent}}
	f, err := fs.OpenFile(ctx, "/"+chosen.Name, os.O_RDONLY, 0)
	if err != nil {
		fail(w, 400, err)
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	perm := os.FileMode(0644)
	if info, statErr := f.Stat(); statErr == nil && s.Type == "local" {
		perm = info.Mode().Perm()
	}
	f.Close()
	if err != nil || len(data) > 2<<20 || !utf8.Valid(data) {
		fail(w, 400, errors.New("无法读取有效 UTF-8 文本"))
		return
	}
	sum := sha256.Sum256(data)
	revision := hex.EncodeToString(sum[:])
	w.Header().Set("Cache-Control", "private, no-store")
	if r.Method == "GET" {
		jsonResponse(w, 200, map[string]string{"content": string(data), "revision": revision})
		return
	}
	var input struct {
		Content  string `json:"content"`
		Revision string `json:"revision"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 13<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fail(w, 400, errors.New("请求数据格式不正确"))
		return
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		fail(w, 400, errors.New("请求包含多余内容"))
		return
	}
	if input.Revision != revision {
		fail(w, 409, errors.New("文件已被修改，请重新打开"))
		return
	}
	if len(input.Content) > 2<<20 || !utf8.ValidString(input.Content) {
		fail(w, 400, errors.New("文本超过限制"))
		return
	}
	ctx = context.WithValue(ctx, mountPutLength{}, int64(len(input.Content)))
	out, err := fs.OpenFile(ctx, "/"+chosen.Name, os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		fail(w, 400, err)
		return
	}
	_, err = io.WriteString(out, input.Content)
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	a.cache.clear()
	a.store.event("info", "files", "编辑文件："+chosen.Name)
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
