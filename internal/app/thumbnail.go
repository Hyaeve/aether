package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	thumbnailTTL         = 72 * time.Hour
	thumbnailUploadLimit = 512 << 10
	thumbnailImageLimit  = 256 << 10
	thumbnailDiskLimit   = 256 << 20
	thumbnailEntryLimit  = 10000
	thumbnailHeaderSize  = 16
)

// One service belongs to one App. The parent owns registration and Run/Close.
type thumbnailService struct {
	app            *App
	root           thumbnailRoot
	mu             sync.Mutex
	now            func() time.Time
	slots          chan struct{}
	bytes          int64
	entries        int
	metadataMu     sync.Mutex
	metadataCalls  map[string]*thumbnailMetadataCall
	requestTimeout time.Duration
}

type thumbnailMetadataCall struct {
	done  chan struct{}
	files []File
	err   error
}

// Only overlapping fresh validations share a result; completed observations
// are never reused for a later upload's file-revision check.
func (t *thumbnailService) freshMetadata(ctx context.Context, s Storage, parent string) ([]File, error) {
	key := s.ID + "\x00" + storageRevision(s) + "\x00" + parent
	t.metadataMu.Lock()
	if call := t.metadataCalls[key]; call != nil {
		t.metadataMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
			return call.files, call.err
		}
	}
	if t.metadataCalls == nil {
		t.metadataCalls = make(map[string]*thumbnailMetadataCall)
	}
	call := &thumbnailMetadataCall{done: make(chan struct{})}
	t.metadataCalls[key] = call
	t.metadataMu.Unlock()
	call.files, call.err = t.app.rawList(ctx, s, parent)
	t.metadataMu.Lock()
	delete(t.metadataCalls, key)
	close(call.done)
	t.metadataMu.Unlock()
	return call.files, call.err
}

func (a *App) newThumbnailService() (*thumbnailService, error) {
	root, err := os.OpenRoot(a.dataDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err = root.Mkdir("thumbnails", 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	info, err := root.Lstat("thumbnails")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("invalid thumbnail directory")
	}
	t := &thumbnailService{app: a, root: thumbnailRoot{a.dataDir}, now: time.Now, slots: make(chan struct{}, 3)}
	if err = t.cleanup(); err != nil {
		return nil, err
	}
	return t, nil
}

// No directory handles or workers are retained by construction.
func (t *thumbnailService) Close() error { return nil }

type thumbnailRoot struct{ dataDir string }

func (r thumbnailRoot) open() (*os.Root, error) {
	data, err := os.OpenRoot(r.dataDir)
	if err != nil {
		return nil, err
	}
	defer data.Close()
	info, err := data.Lstat("thumbnails")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("invalid thumbnail directory")
	}
	return data.OpenRoot("thumbnails")
}

func (r thumbnailRoot) Lstat(name string) (os.FileInfo, error) {
	root, err := r.open()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Lstat(name)
}
func (r thumbnailRoot) Open(name string) (*os.File, error) {
	root, err := r.open()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Open(name)
}
func (r thumbnailRoot) OpenFile(name string, flag int, mode os.FileMode) (*os.File, error) {
	root, err := r.open()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenFile(name, flag, mode)
}
func (r thumbnailRoot) Rename(from, to string) error {
	root, err := r.open()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Rename(from, to)
}
func (r thumbnailRoot) Remove(name string) error {
	root, err := r.open()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Remove(name)
}
func (r thumbnailRoot) WriteFile(name string, data []byte, mode os.FileMode) error {
	root, err := r.open()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.WriteFile(name, data, mode)
}
func (r thumbnailRoot) Symlink(target, name string) error {
	root, err := r.open()
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Symlink(target, name)
}

func (t *thumbnailService) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = t.cleanup()
		}
	}
}

func thumbnailKind(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".bmp":
		return "image"
	case ".mp4", ".m4v", ".webm", ".mkv", ".mov", ".avi", ".ts", ".m2ts", ".mpeg", ".mpg":
		return "video"
	}
	return ""
}

func thumbnailIdentifier(value string) bool {
	if value == "" || len(value) > 4096 || strings.ContainsAny(value, "\x00\\\r\n") || strings.Contains(value, "://") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." || segment == "." {
			return false
		}
	}
	return true
}

// No URLs or credentials enter the cache path or leave through revision tokens.
func (t *thumbnailService) key(s Storage, parent string, f File) string {
	raw, _ := json.Marshal(struct {
		Version                             int
		Storage, Revision, Parent, ID, Name string
		Size                                int64
		Modified                            time.Time
	}{1, s.ID, storageRevision(s), parent, f.ID, f.Name, f.Size, f.Modified})
	mac := hmac.New(sha256.New, t.app.store.macKey)
	mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}

func (t *thumbnailService) source(ctx context.Context, r *http.Request, fresh bool) (Storage, File, string, error) {
	q := r.URL.Query()
	for key, values := range q {
		if (key != "storage" && key != "id" && key != "parent") || len(values) != 1 {
			return Storage{}, File{}, "", errors.New("invalid query")
		}
	}
	storageID, fileID, parent := q.Get("storage"), q.Get("id"), q.Get("parent")
	if !thumbnailIdentifier(storageID) || !thumbnailIdentifier(fileID) {
		return Storage{}, File{}, "", errors.New("invalid identifier")
	}
	s, err := t.app.store.storage(storageID)
	if err != nil {
		return Storage{}, File{}, "", err
	}
	if parent == "" {
		parent = rootOf(s)
	}
	if !thumbnailIdentifier(parent) {
		return Storage{}, File{}, "", errors.New("invalid parent")
	}
	var files []File
	if fresh {
		files, err = t.freshMetadata(ctx, s, parent)
	} else {
		files, err = t.app.listFiles(ctx, s, parent, 0, false)
	}
	if err != nil {
		return Storage{}, File{}, "", errors.New("unavailable directory")
	}
	for _, f := range files {
		if f.ID == fileID && !f.IsDir && safeName(f.Name) && thumbnailKind(f.Name) != "" {
			return s, f, parent, nil
		}
	}
	return Storage{}, File{}, "", os.ErrNotExist
}

// Register both GET and POST /api/files/thumbnail; authentication is enforced here too.
func (t *thumbnailService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !t.app.authenticated(r) {
		fail(w, 401, errors.New("请先登录"))
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" || r.Header.Get("Origin") != "" && !t.app.acceptRequestOrigin(r, linkTrustedProxies()) {
		fail(w, 403, errors.New("跨站请求已拒绝"))
		return
	}
	if r.Method != "GET" && r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	timeout := t.requestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	select {
	case t.slots <- struct{}{}:
		defer func() { <-t.slots }()
	case <-ctx.Done():
		w.WriteHeader(408)
		return
	}
	s, f, parent, err := t.source(ctx, r, false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	key := t.key(s, parent, f)
	var data []byte
	if r.Method == "POST" {
		if !hmac.Equal([]byte(r.Header.Get("X-Thumbnail-Revision")), []byte(key)) {
			w.WriteHeader(409)
			return
		}
		media := strings.Split(r.Header.Get("Content-Type"), ";")[0]
		if media != "image/jpeg" && media != "image/png" {
			w.WriteHeader(415)
			return
		}
		// Context cancellation alone cannot interrupt net/http Body.Read.
		control := http.NewResponseController(w)
		deadline, _ := ctx.Deadline()
		if deadlineErr := control.SetReadDeadline(deadline); deadlineErr != nil && !errors.Is(deadlineErr, http.ErrNotSupported) {
			w.WriteHeader(408)
			return
		}
		defer control.SetReadDeadline(time.Time{})
		input, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, thumbnailUploadLimit))
		if readErr != nil {
			var oversized *http.MaxBytesError
			if errors.As(readErr, &oversized) {
				w.WriteHeader(413)
			} else {
				w.WriteHeader(408)
			}
			return
		}
		data, err = normalizeThumbnail(input)
		if err != nil {
			fail(w, 400, errors.New("缩略图格式或尺寸无效"))
			return
		}
		updated, chosen, directory, checkErr := t.source(ctx, r, true)
		if checkErr != nil || t.key(updated, directory, chosen) != key {
			w.WriteHeader(409)
			return
		}
	}
	// Config checks never cover filesystem IO. Revision-keyed publication is
	// inaccessible after credentials change; a post-IO check rejects late results.
	status := func() int {
		if !t.current(s) || ctx.Err() != nil {
			return 409
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		if !t.current(s) || ctx.Err() != nil {
			return 409
		}
		if r.Method == "GET" {
			data, err = t.readLocked(key)
			if err != nil {
				return 404
			}
		} else if err = t.writeLocked(key, data); err != nil {
			return 507
		}
		if !t.current(s) || ctx.Err() != nil {
			if r.Method == "POST" {
				if name, e := thumbnailFilename(key); e == nil {
					if info, e := t.root.Lstat(name); e == nil {
						t.removeLocked(name, info.Size())
					}
				}
			}
			return 409
		}
		return 200
	}()
	if status != 200 {
		if status == 404 {
			w.Header().Set("X-Thumbnail-Revision", key)
		}
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

func (t *thumbnailService) current(s Storage) bool {
	current, err := t.app.store.storage(s.ID)
	return err == nil && storageRevision(current) == storageRevision(s)
}

// ServeSource proxies only a validated directory media entry through the
// existing DAV reader, irrespective of the storage's browser redirect mode.
func (t *thumbnailService) ServeSource(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !t.app.authenticated(r) {
		w.WriteHeader(401)
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" || r.Header.Get("Origin") != "" && !t.app.acceptRequestOrigin(r, linkTrustedProxies()) {
		w.WriteHeader(403)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return
	}
	timeout := t.requestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	select {
	case t.slots <- struct{}{}:
		defer func() { <-t.slots }()
	case <-ctx.Done():
		w.WriteHeader(408)
		return
	}
	s, f, parent, err := t.source(ctx, r, false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fs := mountFS{app: t.app, config: MountConfig{StorageID: s.ID, Source: parent, ReadOnly: true}}
	file, err := fs.OpenFile(ctx, "/"+f.Name, os.O_RDONLY, 0)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() || !t.current(s) || ctx.Err() != nil {
		w.WriteHeader(409)
		return
	}
	control := http.NewResponseController(w)
	deadline, _ := ctx.Deadline()
	if err := control.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		w.WriteHeader(408)
		return
	}
	defer control.SetWriteDeadline(time.Time{})
	http.ServeContent(w, r.WithContext(ctx), f.Name, info.ModTime(), thumbnailSourceReader{ctx, t, s, file})
}

type thumbnailSourceReader struct {
	ctx     context.Context
	service *thumbnailService
	storage Storage
	file    io.ReadSeeker
}

func (r thumbnailSourceReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if !r.service.current(r.storage) {
		return 0, os.ErrPermission
	}
	return r.file.Read(p)
}
func (r thumbnailSourceReader) Seek(offset int64, whence int) (int64, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.file.Seek(offset, whence)
}

func normalizeThumbnail(raw []byte) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "jpeg" && format != "png") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 512 || cfg.Height > 512 {
		return nil, errors.New("invalid image")
	}
	im, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err = jpeg.Encode(&out, im, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	if out.Len() > thumbnailImageLimit {
		return nil, errors.New("oversized thumbnail")
	}
	return out.Bytes(), nil
}

func thumbnailFilename(key string) (string, error) {
	if len(key) != 64 {
		return "", errors.New("invalid cache key")
	}
	if _, err := hex.DecodeString(key); err != nil {
		return "", err
	}
	return key + ".thumb", nil
}

func thumbnailEnvelope(data []byte, now time.Time) []byte {
	raw := make([]byte, thumbnailHeaderSize+len(data))
	copy(raw, "ATHUMB01")
	binary.BigEndian.PutUint64(raw[8:16], uint64(now.UnixNano()))
	copy(raw[16:], data)
	return raw
}

func thumbnailAccess(raw []byte) (time.Time, bool) {
	if len(raw) < 16 || string(raw[:8]) != "ATHUMB01" {
		return time.Time{}, false
	}
	return time.Unix(0, int64(binary.BigEndian.Uint64(raw[8:16]))), true
}

func (t *thumbnailService) readLocked(key string) ([]byte, error) {
	name, err := thumbnailFilename(key)
	if err != nil {
		return nil, err
	}
	info, err := t.root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > thumbnailImageLimit+thumbnailHeaderSize {
		return nil, os.ErrNotExist
	}
	f, err := t.root.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, thumbnailImageLimit+thumbnailHeaderSize+1))
	defer f.Close()
	access, valid := thumbnailAccess(raw)
	if err != nil || !valid || t.now().Sub(access) >= thumbnailTTL {
		f.Close()
		t.removeLocked(name, info.Size())
		return nil, os.ErrNotExist
	}
	data := raw[16:]
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || format != "jpeg" || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 512 || cfg.Height > 512 {
		f.Close()
		t.removeLocked(name, info.Size())
		return nil, os.ErrNotExist
	}
	var timestamp [8]byte
	binary.BigEndian.PutUint64(timestamp[:], uint64(t.now().UnixNano()))
	if _, err = f.WriteAt(timestamp[:], 8); err != nil {
		return nil, err
	}
	return data, nil
}

func (t *thumbnailService) removeLocked(name string, size int64) {
	if t.root.Remove(name) == nil {
		t.bytes -= size
		t.entries--
		if t.bytes < 0 {
			t.bytes = 0
		}
		if t.entries < 0 {
			t.entries = 0
		}
	}
}

func (t *thumbnailService) writeLocked(key string, data []byte) error {
	name, err := thumbnailFilename(key)
	if err != nil {
		return err
	}
	if len(data) > thumbnailImageLimit {
		return errors.New("oversized thumbnail")
	}
	old, err := t.root.Lstat(name)
	oldSize := int64(0)
	if err == nil {
		if !old.Mode().IsRegular() {
			return errors.New("invalid cache entry")
		}
		oldSize = old.Size()
	} else if !os.IsNotExist(err) {
		return err
	}
	raw := thumbnailEnvelope(data, t.now())
	if t.bytes-oldSize+int64(len(raw)) > thumbnailDiskLimit || oldSize == 0 && t.entries >= thumbnailEntryLimit {
		return errors.New("thumbnail cache full")
	}
	tmp := key + "-" + id() + ".tmp"
	f, err := t.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer t.root.Remove(tmp)
	_, err = f.Write(raw)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = t.root.Rename(tmp, name); err != nil {
		return err
	}
	t.bytes += int64(len(raw)) - oldSize
	if oldSize == 0 {
		t.entries++
	}
	return nil
}

func (t *thumbnailService) cleanup() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	dir, err := t.root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	t.bytes, t.entries = 0, 0
	for {
		entries, readErr := dir.ReadDir(256)
		for _, entry := range entries {
			name := entry.Name()
			// An interrupted atomic publish must not leave staging images indefinitely.
			if strings.HasSuffix(name, ".tmp") && len(name) > 65 {
				if _, keyErr := thumbnailFilename(name[:64]); keyErr == nil && name[64] == '-' && entry.Type().IsRegular() {
					if info, statErr := entry.Info(); statErr == nil && t.now().Sub(info.ModTime()) >= thumbnailTTL {
						_ = t.root.Remove(name)
					}
				}
				continue
			}
			if !strings.HasSuffix(name, ".thumb") {
				continue
			}
			if _, err := thumbnailFilename(strings.TrimSuffix(name, ".thumb")); err != nil || !entry.Type().IsRegular() {
				continue
			}
			f, err := t.root.Open(name)
			if err != nil {
				continue
			}
			info, statErr := f.Stat()
			var header [thumbnailHeaderSize]byte
			_, err = io.ReadFull(f, header[:])
			f.Close()
			access, valid := thumbnailAccess(header[:])
			if statErr != nil {
				continue
			}
			if err != nil || !valid || t.now().Sub(access) >= thumbnailTTL || info.Size() > thumbnailImageLimit+thumbnailHeaderSize {
				_ = t.root.Remove(name)
				continue
			}
			t.bytes += info.Size()
			t.entries++
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
