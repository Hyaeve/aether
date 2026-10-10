package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

func scrapeFileType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".strm", ".nfo":
		return "text"
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif":
		return "image"
	}
	return ""
}

// os.Root contains all reads and writes within the selected task's library.
func (a *App) scrapeFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method == "PUT" {
		a.scrapeMu.Lock()
		defer a.scrapeMu.Unlock()
		if a.scrapeProgress.Running {
			fail(w, 409, errors.New("刮削执行中，暂不可编辑"))
			return
		}
	}
	base, err := a.scrapeRoot(r.URL.Query().Get("taskId"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	name := r.URL.Query().Get("path")
	if name == "" {
		name = "."
	}
	if !fs.ValidPath(name) {
		fail(w, 400, errors.New("无效的库文件路径"))
		return
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		fail(w, 400, errors.New("STRM 库不可读取"))
		return
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		fail(w, 404, errors.New("库文件不存在或不可读取"))
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fail(w, 400, errors.New("读取库文件信息失败"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if info.IsDir() && r.Method == "GET" {
		entries := []map[string]any{}
		for {
			batch, readErr := f.ReadDir(256)
			for _, entry := range batch {
				if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() && scrapeFileType(entry.Name()) == "" {
					continue
				}
				meta, err := entry.Info()
				if err != nil || !meta.IsDir() && !meta.Mode().IsRegular() {
					continue
				}
				p := path.Join(name, entry.Name())
				q := url.Values{"taskId": {r.URL.Query().Get("taskId")}, "path": {p}}
				entries = append(entries, map[string]any{"id": p, "name": entry.Name(), "isDir": entry.IsDir(), "size": meta.Size(), "modified": meta.ModTime(), "url": "/api/strm-scrape/files?" + q.Encode()})
			}
			if len(entries) > 10000 {
				fail(w, 400, errors.New("当前目录超过 10000 项，请细分目录"))
				return
			}
			if r.Context().Err() != nil {
				return
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					fail(w, 400, errors.New("读取目录失败"))
					return
				}
				break
			}
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i]["isDir"] != entries[j]["isDir"] {
				return entries[i]["isDir"].(bool)
			}
			return entries[i]["name"].(string) < entries[j]["name"].(string)
		})
		jsonResponse(w, 200, entries)
		return
	}
	if !info.Mode().IsRegular() || scrapeFileType(name) == "" {
		fail(w, 400, errors.New("仅支持 STRM、NFO 和图片文件"))
		return
	}
	if scrapeFileType(name) == "image" {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		if info.Size() > 20<<20 {
			fail(w, 400, errors.New("图片超过 20 MiB"))
			return
		}
		header := make([]byte, 512)
		n, _ := f.Read(header)
		f.Seek(0, io.SeekStart)
		mime := http.DetectContentType(header[:n])
		if !strings.HasPrefix(mime, "image/") {
			fail(w, 400, errors.New("无效的图片"))
			return
		}
		w.Header().Set("Content-Type", mime)
		http.ServeContent(w, r, info.Name(), info.ModTime(), f)
		return
	}
	if info.Size() > 2<<20 {
		fail(w, 400, errors.New("文本超过 2 MiB"))
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if err != nil || len(data) > 2<<20 || !utf8.Valid(data) {
		fail(w, 400, errors.New("文本必须为有效 UTF-8 且不超过 2 MiB"))
		return
	}
	sum := sha256.Sum256(data)
	revision := hex.EncodeToString(sum[:])
	if r.Method == "GET" {
		jsonResponse(w, 200, map[string]string{"content": string(data), "revision": revision})
		return
	}
	if r.Method != "PUT" {
		w.WriteHeader(405)
		return
	}
	var input struct {
		Content  string `json:"content"`
		Revision string `json:"revision"`
	}
	// Escaped JSON can take six bytes per UTF-8 input byte.
	r.Body = http.MaxBytesReader(w, r.Body, 13<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fail(w, 400, errors.New("请求数据格式不正确"))
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		fail(w, 400, errors.New("请求包含多余内容"))
		return
	}
	if input.Revision != revision {
		fail(w, 409, errors.New("文件已被修改，请重新打开"))
		return
	}
	if !utf8.ValidString(input.Content) || len(input.Content) > 2<<20 {
		fail(w, 400, errors.New("文本超过限制"))
		return
	}
	// Write a sibling temporary file, then publish through the same rooted handle.
	temp := path.Join(path.Dir(name), ".aether-edit-"+id())
	out, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		fail(w, 500, errors.New("无法创建编辑临时文件"))
		return
	}
	defer root.Remove(temp)
	_, err = io.WriteString(out, input.Content)
	if err == nil {
		err = out.Chmod(info.Mode().Perm())
	}
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = root.Rename(temp, name)
	}
	if err != nil {
		fail(w, 500, errors.New("保存库文件失败"))
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
