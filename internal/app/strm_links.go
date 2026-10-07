package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var pickcodePath = regexp.MustCompile(`^[a-zA-Z0-9]{10,32}\.[a-zA-Z0-9]{1,12}$`)

type strmReference struct {
	Storage string `json:"storage"`
	File    string `json:"file"`
	Pick    string `json:"pick"`
	Name    string `json:"name"`
}

func (a *App) referencePath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(a.dataDir, "strm-index", hex.EncodeToString(sum[:])+".enc")
}

func (a *App) readSTRMReference(key string) (strmReference, error) {
	var ref strmReference
	b, err := os.ReadFile(a.referencePath(key))
	if err != nil {
		return ref, err
	}
	n := a.store.aead.NonceSize()
	if len(b) < n || len(b) > 64<<10 {
		return ref, errors.New("STRM 映射无效")
	}
	plain, err := a.store.aead.Open(nil, b[:n], b[n:], []byte(key))
	if err == nil {
		err = json.Unmarshal(plain, &ref)
	}
	return ref, err
}

func (a *App) publicSTRMURL(s Storage, f File) (string, error) {
	base := strings.TrimRight(a.store.snapshotWithLogLimit(0).Settings.PublicURL, "/")
	if !safeName(f.Name) {
		return "", errors.New("文件名称无效")
	}
	if s.Type == "115" {
		key := f.PickCode + strings.ToLower(path.Ext(f.Name))
		if !pickcodePath.MatchString(key) {
			return "", errors.New("115 文件缺少有效 pickcode 或扩展名")
		}
		// Only generated references are public, never arbitrary cloud lookups.
		a.strmIndexMu.Lock()
		defer a.strmIndexMu.Unlock()
		old, err := a.readSTRMReference(key)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err == nil && (old.Storage != s.ID || old.File != f.ID) {
			return "", errors.New("115 pickcode 与其他存储的已生成链接冲突")
		}
		ref := strmReference{s.ID, f.ID, f.PickCode, f.Name}
		if err != nil || ref != old {
			b, _ := json.Marshal(ref)
			nonce := make([]byte, a.store.aead.NonceSize())
			if _, err := rand.Read(nonce); err != nil {
				return "", err
			}
			b = a.store.aead.Seal(nonce, nonce, b, []byte(key))
			if err := os.MkdirAll(filepath.Dir(a.referencePath(key)), 0700); err != nil {
				return "", err
			}
			if err := atomicWrite(a.referencePath(key), b); err != nil {
				return "", err
			}
		}
		name := strings.NewReplacer("%", "%25", "#", "%23", "?", "%3F", "\r", "%0D", "\n", "%0A").Replace(f.Name)
		return base + "/d/" + key + "?/" + name, nil
	}
	if s.Type == "quark" {
		file := base64.RawURLEncoding.EncodeToString([]byte(f.ID))
		sig := a.quarkSTRMSign(s.ID, file, f.Name)
		return base + "/api/strm/play/" + url.PathEscape(s.ID) + "/" + file + "/t/" + sig + "/n/" + url.PathEscape(f.Name), nil
	}
	return a.streamURL(s.ID, f.ID, f.PickCode), nil
}

func (a *App) quarkSTRMSign(storage, file, name string) string {
	mac := hmac.New(sha256.New, []byte(a.store.snapshotWithLogLimit(0).SignKey))
	b, _ := json.Marshal([]string{"quark-strm-v1", storage, file, name})
	mac.Write(b)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *App) playSTRMReference(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var ref strmReference
	if key := r.PathValue("key"); key != "" {
		if !pickcodePath.MatchString(key) {
			http.NotFound(w, r)
			return
		}
		var err error
		ref, err = a.readSTRMReference(key)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		s, err := a.store.storage(ref.Storage)
		if err != nil || s.Type != "115" {
			http.NotFound(w, r)
			return
		}
	} else {
		storage, file, name := r.PathValue("storage"), r.PathValue("file"), r.PathValue("name")
		want := a.quarkSTRMSign(storage, file, name)
		if !hmac.Equal([]byte(want), []byte(r.PathValue("signature"))) {
			fail(w, 403, errors.New("无效的 STRM 播放令牌"))
			return
		}
		fid, err := base64.RawURLEncoding.DecodeString(file)
		s, storageErr := a.store.storage(storage)
		if err != nil || storageErr != nil || s.Type != "quark" || !safeName(name) {
			http.NotFound(w, r)
			return
		}
		ref = strmReference{Storage: storage, File: string(fid), Name: name}
	}
	target, _ := url.Parse(a.streamURL(ref.Storage, ref.File, ref.Pick))
	request := r.Clone(r.Context())
	request.URL = target
	request.SetPathValue("token", path.Base(target.Path))
	request.SetPathValue("downloadName", ref.Name)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": ref.Name}))
	a.stream(w, request)
}
