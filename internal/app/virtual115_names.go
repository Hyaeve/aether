package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var virtual115ShareName = regexp.MustCompile(`^([a-zA-Z0-9]{1,128})(?:-([a-zA-Z0-9]{1,32}))?$`)
var virtual115ParentID = regexp.MustCompile(`^[0-9]{1,24}$`)

const virtual115NamesLimit = 8 << 20

type virtual115NamedFile struct {
	Key    string `json:"key"`
	Parent string `json:"parent"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA1   string `json:"sha1"`
}

type virtual115Names struct {
	Version int                   `json:"version"`
	Files   []virtual115NamedFile `json:"files"`
}

type virtual115IdentityKey struct{}

func virtual115SharePart(code, pass string) string {
	if pass != "" {
		return code + "-" + pass
	}
	return code
}

func virtual115ResourceKey(code, pass string, item shareEntry) (string, error) {
	key := code + "_" + pass + "_" + item.ID + strings.ToLower(path.Ext(item.Name))
	if !virtual115Key.MatchString(key) || !virtual115ShareName.MatchString(virtual115SharePart(code, pass)) ||
		!safeName(item.Name) || strings.ContainsAny(item.Name, "\x00\r\n") || item.IsDir {
		return "", errors.New("分享文件不适用于115链库格式")
	}
	return key, nil
}

func virtual115NamesBinding(code, pass string) []byte {
	return []byte("virtual115-names-v1:" + code + "\x00" + pass)
}

func (a *App) virtual115NamesPath(code, pass string) string {
	sum := sha256.Sum256(virtual115NamesBinding(code, pass))
	return filepath.Join(a.dataDir, "virtual-library-names", hex.EncodeToString(sum[:])+".enc")
}

func validateVirtual115Names(code, pass string, names virtual115Names) error {
	if names.Version != 1 || len(names.Files) > 5000 {
		return errors.New("115分享文件名索引版本或条目数量无效")
	}
	for _, file := range names.Files {
		match := virtual115Key.FindStringSubmatch(file.Key)
		if match == nil || match[1] != code || match[2] != pass || !safeName(file.Name) || len(file.Name) > 4096 ||
			strings.ContainsAny(file.Name, "\x00\r\n") || strings.ToLower(path.Ext(file.Name)) != match[4] || file.Size < 0 ||
			file.SHA1 != "" && !simulcastSHA1(file.SHA1) {
			return errors.New("115分享文件名索引内容无效")
		}
		if file.Parent != "root" && !virtual115ParentID.MatchString(file.Parent) {
			return errors.New("115分享文件名索引目录无效")
		}
	}
	return nil
}

func (a *App) saveVirtual115Names(code, pass string, names virtual115Names) error {
	if err := validateVirtual115Names(code, pass, names); err != nil {
		return err
	}
	raw, err := json.Marshal(names)
	if err != nil || len(raw) > virtual115NamesLimit {
		return errors.New("115分享文件名索引超过8MiB")
	}
	nonce := make([]byte, a.store.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	raw = a.store.aead.Seal(nonce, nonce, raw, virtual115NamesBinding(code, pass))
	a.strmIndexMu.Lock()
	defer a.strmIndexMu.Unlock()
	target := a.virtual115NamesPath(code, pass)
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	if _, err := os.Stat(target); os.IsNotExist(err) {
		files, err := os.ReadDir(filepath.Dir(target))
		if err != nil {
			return err
		}
		if len(files) >= 10000 {
			return errors.New("115分享文件名索引已达10000个分享上限")
		}
	} else if err != nil {
		return err
	}
	return atomicWrite(target, raw)
}

func (a *App) readVirtual115Names(code, pass string) (virtual115Names, error) {
	var names virtual115Names
	f, err := os.Open(a.virtual115NamesPath(code, pass))
	if err != nil {
		return names, err
	}
	defer f.Close()
	limit := int64(virtual115NamesLimit + a.store.aead.NonceSize() + a.store.aead.Overhead())
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return names, errors.New("115分享文件名索引类型或大小无效")
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	n := a.store.aead.NonceSize()
	if err != nil || len(raw) < n || int64(len(raw)) > limit {
		return names, errors.New("115分享文件名索引读取失败")
	}
	raw, err = a.store.aead.Open(nil, raw[:n], raw[n:], virtual115NamesBinding(code, pass))
	if err != nil || json.Unmarshal(raw, &names) != nil {
		return names, errors.New("115分享文件名索引校验失败")
	}
	return names, validateVirtual115Names(code, pass, names)
}

func (a *App) playVirtual115Name(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	match := virtual115ShareName.FindStringSubmatch(r.PathValue("share"))
	name := r.PathValue("file")
	if match == nil || !safeName(name) || strings.ContainsAny(name, "\x00\r\n") {
		http.NotFound(w, r)
		return
	}
	names, err := a.readVirtual115Names(match[1], match[2])
	if os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		fail(w, http.StatusForbidden, err)
		return
	}
	var selected *virtual115NamedFile
	for _, file := range names.Files {
		if file.Name != name {
			continue
		}
		if selected != nil {
			a.store.event("warn", "virtual", "115链库播放名称冲突："+name)
			fail(w, http.StatusConflict, errors.New("分享内存在多个同名文件，无法仅凭文件名定位，未执行转存"))
			return
		}
		copy := file
		selected = &copy
	}
	if selected == nil {
		http.NotFound(w, r)
		return
	}
	r.SetPathValue("key", selected.Key)
	a.playVirtualShare(w, r.WithContext(context.WithValue(r.Context(), virtual115IdentityKey{}, *selected)))
}

func virtual115FilenameURL(base, code, pass, name string) string {
	return strings.TrimRight(base, "/") + "/115-share/" + url.PathEscape(virtual115SharePart(code, pass)) + "/" + shareFilenamePath(name)
}
