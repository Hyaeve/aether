package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
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
