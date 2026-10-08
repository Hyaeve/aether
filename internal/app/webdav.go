package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/webdav"
)

type davFS struct{ a *App }

func davStorageNames(storages []Storage) map[string]string {
	names := make(map[string]string)
	counts := make(map[string]int)
	reserved := make(map[string]bool)
	for _, s := range storages {
		if s.Enabled {
			counts[s.Name]++
			reserved[s.ID] = true
			reserved[s.Name] = true
		}
	}
	for _, s := range storages {
		if !s.Enabled {
			continue
		}
		name := s.Name
		if strings.TrimSpace(name) == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:\x00") {
			names[s.ID] = s.ID
			continue
		}
		conflict := counts[name] > 1
		for _, other := range storages {
			if other.Enabled && other.ID != s.ID && other.ID == name {
				conflict = true
			}
		}
		if conflict {
			base := name + " (" + s.ID + ")"
			name = base
			for n := 2; reserved[name]; n++ {
				name = base + " (" + strconv.Itoa(n) + ")"
			}
		}
		names[s.ID] = name
		reserved[name] = true
	}
	return names
}

func (d davFS) writePath(ctx context.Context, name string) (mountFS, string, error) {
	if _, restricted := ctx.Value(davGrantsKey{}).([]DAVGrant); restricted {
		return mountFS{}, "", os.ErrPermission
	}
	clean, err := relative(name)
	if err != nil {
		return mountFS{}, "", os.ErrPermission
	}
	parts := strings.SplitN(clean, "/", 2)
	if len(parts) != 2 || parts[1] == "." {
		return mountFS{}, "", os.ErrPermission
	}
	s, _, err := d.resolve(ctx, "/"+parts[0])
	if err != nil || s.ID == "" {
		return mountFS{}, "", os.ErrNotExist
	}
	return mountFS{app: d.a, config: MountConfig{StorageID: s.ID, Source: rootOf(s)}}, parts[1], nil
}

func (d davFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	fs, rel, err := d.writePath(ctx, name)
	if err != nil {
		return err
	}
	return fs.Mkdir(ctx, rel, perm)
}
func (d davFS) RemoveAll(ctx context.Context, name string) error {
	fs, rel, err := d.writePath(ctx, name)
	if err != nil {
		return err
	}
	return fs.RemoveAll(ctx, rel)
}
func (d davFS) Rename(ctx context.Context, from, to string) error {
	fs, oldRel, err := d.writePath(ctx, from)
	if err != nil {
		return err
	}
	dst, newRel, err := d.writePath(ctx, to)
	if err != nil {
		return err
	}
	if fs.config.StorageID != dst.config.StorageID {
		return os.ErrPermission
	}
	return fs.Rename(ctx, oldRel, newRel)
}

type davInfo struct{ file File }

type davWritableInfo struct{ davInfo }

func (i davWritableInfo) Mode() os.FileMode {
	if i.IsDir() {
		return os.ModeDir | 0777
	}
	return 0666
}
func davFileInfo(ctx context.Context, f File) os.FileInfo {
	if _, restricted := ctx.Value(davGrantsKey{}).([]DAVGrant); !restricted {
		return davWritableInfo{davInfo{f}}
	}
	return davInfo{f}
}

func (i davInfo) Name() string { return i.file.Name }
func (i davInfo) Size() int64  { return i.file.Size }
func (i davInfo) Mode() os.FileMode {
	if i.file.IsDir {
		return os.ModeDir | 0555
	}
	return 0444
}
func (i davInfo) ModTime() time.Time { return i.file.Modified }
func (i davInfo) IsDir() bool        { return i.file.IsDir }
func (i davInfo) Sys() any           { return nil }
func (i davInfo) ContentType(context.Context) (string, error) {
	if i.file.IsDir {
		return "", nil
	}
	if value := mime.TypeByExtension(path.Ext(i.file.Name)); value != "" {
		return value, nil
	}
	return "application/octet-stream", nil
}

func (d davFS) list(ctx context.Context, s Storage, dir string) ([]File, error) {
	if _, restricted := ctx.Value(davGrantsKey{}).([]DAVGrant); restricted && s.Type == "local" {
		return d.a.rawList(ctx, s, dir)
	}
	return d.a.listFiles(ctx, s, dir, 0, !d.a.store.snapshot().Settings.WebDAVCache)
}

func (d davFS) resolve(ctx context.Context, name string) (Storage, File, error) {
	clean, err := relative(name)
	if err != nil {
		return Storage{}, File{}, os.ErrPermission
	}
	if clean == "." {
		return Storage{}, File{Name: "/", IsDir: true}, nil
	}
	parts := strings.Split(clean, "/")
	var s Storage
	grants, restricted := ctx.Value(davGrantsKey{}).([]DAVGrant)
	var grant DAVGrant
	storageID := parts[0]
	storages := d.a.store.snapshot().Storages
	names := davStorageNames(storages)
	if restricted {
		for _, g := range grants {
			if g.Name == parts[0] {
				grant = g
				storageID = g.StorageID
				break
			}
		}
		if grant.StorageID == "" {
			return s, File{}, os.ErrNotExist
		}
	} else {
		for id, name := range names {
			if name == parts[0] {
				storageID = id
				break
			}
		}
	}
	for _, v := range storages {
		if v.Enabled && v.ID == storageID {
			s = v
			break
		}
	}
	if s.ID == "" {
		return s, File{}, os.ErrNotExist
	}
	current := File{ID: rootOf(s), Name: names[s.ID], IsDir: true}
	if restricted {
		current = File{ID: grant.Directory, Name: grant.Name, IsDir: true}
		if s.Type == "local" {
			rel, err := relative(grant.Directory)
			if err != nil {
				return s, File{}, os.ErrPermission
			}
			// Confine local symlink resolution to the granted directory, not the whole pool.
			s.Config["root"] = filepath.Join(s.Config["root"], filepath.FromSlash(rel))
			current.ID = "/"
		}
	}
	for _, segment := range parts[1:] {
		if !current.IsDir {
			return s, File{}, os.ErrNotExist
		}
		files, err := d.list(ctx, s, current.ID)
		if err != nil {
			return s, File{}, err
		}
		found := false
		for _, f := range files {
			if f.Name == segment {
				current = f
				found = true
				break
			}
		}
		if !found {
			return s, File{}, os.ErrNotExist
		}
	}
	return s, current, nil
}

func (d davFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	_, f, err := d.resolve(ctx, name)
	return davFileInfo(ctx, f), err
}

func (d davFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	if flag != os.O_RDONLY {
		fs, rel, err := d.writePath(ctx, name)
		if err != nil {
			return nil, err
		}
		s, source, rel, err := fs.selectPath(rel)
		if err != nil {
			return nil, err
		}
		if s.Type == "local" {
			file, err := fs.openDAVLocalWrite(ctx, s, source, rel, flag, perm)
			if err != nil {
				return nil, err
			}
			return d.a.trackedFile(ctx, s, name, file, flag), nil
		}
		file, err := fs.openCloudWrite(ctx, s, source, rel, flag)
		if err != nil {
			return nil, err
		}
		return d.a.trackedFile(ctx, s, name, file, flag), nil
	}
	s, f, err := d.resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	df := &davFile{ctx: ctx, info: davFileInfo(ctx, f)}
	if f.IsDir {
		files := []File{}
		if s.ID == "" {
			grants, restricted := ctx.Value(davGrantsKey{}).([]DAVGrant)
			storages := d.a.store.snapshot().Storages
			names := davStorageNames(storages)
			for _, s := range storages {
				if s.Enabled {
					if restricted {
						for _, g := range grants {
							if g.StorageID == s.ID {
								files = append(files, File{ID: g.Directory, Name: g.Name, IsDir: true})
							}
						}
					} else {
						files = append(files, File{ID: s.ID, Name: names[s.ID], IsDir: true})
					}
				}
			}
		} else {
			files, err = d.list(ctx, s, f.ID)
			if err != nil {
				return nil, err
			}
		}
		for _, f := range files {
			df.entries = append(df.entries, davFileInfo(ctx, f))
		}
		return df, nil
	}
	df.open = func() error {
		df.progress = d.a.beginTransfer(ctx, "download", s, f.Name, f.Size)
		download, err := d.a.download(ctx, s, f.ID, f.PickCode)
		if err != nil {
			return fmt.Errorf("获取文件读取链接失败（存储类型 %s）：%w", s.Type, err)
		}
		if s.Type == "local" {
			root, err := os.OpenRoot(s.Config["root"])
			if err != nil {
				return err
			}
			df.local, err = root.Open(download.Local)
			root.Close()
			if err != nil {
				return err
			}
			_, err = df.local.Seek(df.offset, io.SeekStart)
			return err
		}
		df.download = download
		return nil
	}
	return df, nil
}

type davFile struct {
	ctx      context.Context
	info     os.FileInfo
	entries  []os.FileInfo
	index    int
	local    *os.File
	download Download
	body     io.ReadCloser
	offset   int64
	open     func() error
	progress *transferProgress
}

func (f *davFile) Stat() (os.FileInfo, error) { return f.info, nil }
func (f *davFile) Write([]byte) (int, error)  { return 0, os.ErrPermission }
func (f *davFile) Close() (err error) {
	defer func() { f.progress.finish(err) }()
	if f.local != nil {
		return f.local.Close()
	}
	if f.body != nil {
		return f.body.Close()
	}
	return nil
}
func (f *davFile) Readdir(n int) ([]os.FileInfo, error) {
	if !f.info.IsDir() {
		return nil, errors.New("not a directory")
	}
	if n <= 0 {
		out := f.entries[f.index:]
		f.index = len(f.entries)
		return out, nil
	}
	if f.index >= len(f.entries) {
		return nil, io.EOF
	}
	end := min(f.index+n, len(f.entries))
	out := f.entries[f.index:end]
	f.index = end
	return out, nil
}
func (f *davFile) Seek(offset int64, whence int) (int64, error) {
	if f.info.IsDir() {
		return 0, os.ErrInvalid
	}
	if f.local != nil {
		return f.local.Seek(offset, whence)
	}
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += f.offset
	case io.SeekEnd:
		offset += f.info.Size()
	default:
		return 0, errors.New("invalid seek")
	}
	if offset < 0 {
		return 0, errors.New("negative seek")
	}
	if f.body != nil {
		f.body.Close()
		f.body = nil
	}
	f.offset = offset
	return offset, nil
}
func (f *davFile) Read(b []byte) (int, error) {
	if f.info.IsDir() {
		return 0, os.ErrInvalid
	}
	if len(b) == 0 {
		return 0, nil
	}
	if f.open != nil {
		if err := f.open(); err != nil {
			f.progress.finish(err)
			return 0, err
		}
		f.open = nil
	}
	if f.local != nil {
		n, err := f.local.Read(b)
		f.progress.add(n)
		if err != nil && err != io.EOF {
			f.progress.finish(err)
		}
		return n, err
	}
	if f.body == nil {
		req, err := http.NewRequestWithContext(f.ctx, "GET", f.download.URL, nil)
		if err != nil {
			return 0, err
		}
		req.Header = f.download.Headers.Clone()
		if req.Header == nil {
			req.Header = http.Header{}
		}
		if f.offset > 0 {
			req.Header.Set("Range", "bytes="+strconv.FormatInt(f.offset, 10)+"-")
		}
		client := &http.Client{Transport: apiClient.Transport, CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= 5 || next.URL.User != nil || (next.URL.Scheme != "http" && next.URL.Scheme != "https") {
				return http.ErrUseLastResponse
			}
			if via[len(via)-1].URL.Scheme == "https" && next.URL.Scheme != "https" {
				return http.ErrUseLastResponse
			}
			if next.URL.Host != via[0].URL.Host || next.URL.Scheme != via[0].URL.Scheme {
				next.Header.Del("Authorization")
				next.Header.Del("Cookie")
				next.Header.Del("Proxy-Authorization")
			}
			return nil
		}}
		res, err := client.Do(req)
		if err != nil {
			// net/url errors include signed URLs; never persist those in logs.
			if f.ctx.Err() != nil {
				err = f.ctx.Err()
			} else {
				err = fmt.Errorf("上游读取请求失败（偏移 %d，网络或重定向错误）", f.offset)
			}
			f.progress.finish(err)
			return 0, err
		}
		if (res.StatusCode != 200 && res.StatusCode != 206) || (f.offset > 0 && res.StatusCode != 206) {
			res.Body.Close()
			err := fmt.Errorf("上游读取返回 HTTP %d（偏移 %d）", res.StatusCode, f.offset)
			f.progress.finish(err)
			return 0, err
		}
		f.body = res.Body
	}
	n, err := f.body.Read(b)
	f.progress.add(n)
	if err != nil && err != io.EOF {
		f.progress.finish(err)
	}
	f.offset += int64(n)
	return n, err
}
