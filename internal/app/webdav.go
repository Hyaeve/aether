package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/webdav"
)

type davFS struct{ a *App }

func (d davFS) Mkdir(context.Context, string, os.FileMode) error { return os.ErrPermission }
func (d davFS) RemoveAll(context.Context, string) error          { return os.ErrPermission }
func (d davFS) Rename(context.Context, string, string) error     { return os.ErrPermission }

type davInfo struct{ file File }

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

func (d davFS) list(ctx context.Context, s Storage, dir string) ([]File, error) {
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
	for _, v := range d.a.store.snapshot().Storages {
		if v.Enabled && v.ID == parts[0] {
			s = v
			break
		}
	}
	if s.ID == "" {
		return s, File{}, os.ErrNotExist
	}
	current := File{ID: rootOf(s), Name: s.ID, IsDir: true}
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
	return davInfo{f}, err
}

func (d davFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	if flag != os.O_RDONLY {
		return nil, os.ErrPermission
	}
	s, f, err := d.resolve(ctx, name)
	if err != nil {
		return nil, err
	}
	df := &davFile{ctx: ctx, info: davInfo{f}}
	if f.IsDir {
		files := []File{}
		if s.ID == "" {
			for _, s := range d.a.store.snapshot().Storages {
				if s.Enabled {
					files = append(files, File{ID: s.ID, Name: s.ID, IsDir: true})
				}
			}
		} else {
			files, err = d.list(ctx, s, f.ID)
			if err != nil {
				return nil, err
			}
		}
		for _, f := range files {
			df.entries = append(df.entries, davInfo{f})
		}
		return df, nil
	}
	download, err := d.a.download(ctx, s, f.ID, f.PickCode)
	if err != nil {
		return nil, err
	}
	if s.Type == "local" {
		root, err := os.OpenRoot(s.Config["root"])
		if err != nil {
			return nil, err
		}
		local, err := root.Open(download.Local)
		root.Close()
		if err != nil {
			return nil, err
		}
		df.local = local
	} else {
		df.download = download
	}
	return df, nil
}

type davFile struct {
	ctx      context.Context
	info     davInfo
	entries  []os.FileInfo
	index    int
	local    *os.File
	download Download
	body     io.ReadCloser
	offset   int64
}

func (f *davFile) Stat() (os.FileInfo, error) { return f.info, nil }
func (f *davFile) Write([]byte) (int, error)  { return 0, os.ErrPermission }
func (f *davFile) Close() error {
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
	if f.local != nil {
		return f.local.Read(b)
	}
	if f.info.IsDir() {
		return 0, os.ErrInvalid
	}
	if f.body == nil {
		req, err := http.NewRequestWithContext(f.ctx, "GET", f.download.URL, nil)
		if err != nil {
			return 0, err
		}
		req.Header = f.download.Headers.Clone()
		if f.offset > 0 {
			req.Header.Set("Range", "bytes="+strconv.FormatInt(f.offset, 10)+"-")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err
		}
		if res.StatusCode >= 400 || (f.offset > 0 && res.StatusCode != 206) {
			res.Body.Close()
			return 0, errors.New("上游不支持当前读取范围")
		}
		f.body = res.Body
	}
	n, err := f.body.Read(b)
	f.offset += int64(n)
	return n, err
}
