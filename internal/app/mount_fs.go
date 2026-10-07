package app

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/net/webdav"
)

// The private DAV bridge reuses cloud readers without exposing the public DAV
// service or its users. Local writes are confined by os.Root to the chosen subtree.
type mountFS struct {
	app    *App
	config MountConfig
}

func mountStorageName(s Storage) string {
	if safeName(s.Name) {
		return s.Name
	}
	return s.ID
}

func (d mountFS) includesStorage(s Storage) bool {
	if !s.Enabled {
		return false
	}
	if s.Type == "local" {
		root, err := filepath.EvalSymlinks(s.Config["root"])
		return err == nil && !pathOverlaps(root, mountTarget(d.config))
	}
	return true
}

func (d mountFS) storageNames() map[string]string {
	names, used := map[string]string{}, map[string]bool{}
	storages := d.app.store.snapshot().Storages
	sort.Slice(storages, func(i, j int) bool { return storages[i].ID < storages[j].ID })
	// Reserve literal names before assigning duplicate suffixes.
	for _, s := range storages {
		if d.includesStorage(s) {
			used[strings.ToLower(mountStorageName(s))] = true
		}
	}
	seen := map[string]bool{}
	for _, s := range storages {
		if !d.includesStorage(s) {
			continue
		}
		name := mountStorageName(s)
		if seen[strings.ToLower(name)] {
			for n := 2; ; n++ {
				candidate := fmt.Sprintf("%s (%d)", name, n)
				if !used[strings.ToLower(candidate)] {
					name = candidate
					break
				}
			}
		}
		seen[strings.ToLower(mountStorageName(s))] = true
		used[strings.ToLower(name)] = true
		names[s.ID] = name
	}
	return names
}

func (d mountFS) selectPath(name string) (Storage, string, string, error) {
	rel, err := relative(name)
	if err != nil {
		return Storage{}, "", "", os.ErrPermission
	}
	key, source := d.config.StorageID, d.config.Source
	if key == "" {
		if rel == "." {
			return Storage{}, "/", ".", nil
		}
		parts := strings.SplitN(rel, "/", 2)
		names := d.storageNames()
		for _, storage := range d.app.store.snapshot().Storages {
			if d.includesStorage(storage) && names[storage.ID] == parts[0] {
				key, source = storage.ID, rootOf(storage)
				break
			}
		}
		if key == "" {
			return Storage{}, "", "", os.ErrNotExist
		}
		rel = "."
		if len(parts) > 1 {
			rel = parts[1]
		}
	}
	storage, err := d.app.store.storage(key)
	if err != nil {
		return Storage{}, "", "", os.ErrNotExist
	}
	if source == "" || source == "/" {
		source = rootOf(storage)
	}
	return storage, source, rel, nil
}

func (d mountFS) localRoot(s Storage, source string) (*os.Root, error) {
	real, err := filepath.EvalSymlinks(s.Config["root"])
	subtree, sourceErr := relative(source)
	if err != nil || sourceErr != nil || pathOverlaps(filepath.Join(real, filepath.FromSlash(subtree)), mountTarget(d.config)) {
		return nil, os.ErrPermission
	}
	root, err := os.OpenRoot(real)
	if err != nil {
		return nil, err
	}
	rel, err := relative(source)
	if err != nil {
		root.Close()
		return nil, os.ErrPermission
	}
	if rel == "." {
		return root, nil
	}
	sub, err := root.OpenRoot(rel)
	root.Close()
	return sub, err
}

func mountReadContext(ctx context.Context, s Storage, source, rel string) (context.Context, string) {
	ctx = context.WithValue(ctx, davGrantsKey{}, []DAVGrant{{Name: "source", StorageID: s.ID, Directory: source}})
	return ctx, path.Join("/source", rel)
}

func (d mountFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	s, source, rel, err := d.selectPath(name)
	if err != nil {
		return nil, err
	}
	if s.ID == "" {
		return davInfo{File{Name: "/", IsDir: true}}, nil
	}
	if s.Type == "local" {
		root, err := d.localRoot(s, source)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		return root.Stat(rel)
	}
	ctx, name = mountReadContext(ctx, s, source, rel)
	return (davFS{d.app}).Stat(ctx, name)
}

func (d mountFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	s, source, rel, err := d.selectPath(name)
	if err != nil {
		return nil, err
	}
	if s.ID == "" {
		if flag != os.O_RDONLY {
			return nil, os.ErrPermission
		}
		file := &davFile{ctx: ctx, info: davInfo{File{Name: "/", IsDir: true}}}
		names := d.storageNames()
		for _, storage := range d.app.store.snapshot().Storages {
			if d.includesStorage(storage) {
				file.entries = append(file.entries, davInfo{File{Name: names[storage.ID], IsDir: true}})
			}
		}
		return file, nil
	}
	if flag != os.O_RDONLY && d.config.ReadOnly {
		return nil, os.ErrPermission
	}
	if s.Type == "local" {
		root, err := d.localRoot(s, source)
		if err != nil {
			return nil, err
		}
		defer root.Close()
		file, err := root.OpenFile(rel, flag, perm)
		if err == nil && flag != os.O_RDONLY {
			d.app.cache.clear()
		}
		return file, err
	}
	if flag != os.O_RDONLY {
		return d.openCloudWrite(ctx, s, source, rel, flag)
	}
	ctx, name = mountReadContext(ctx, s, source, rel)
	return (davFS{d.app}).OpenFile(ctx, name, flag, perm)
}

func (d mountFS) writable(name string) (*os.Root, string, error) {
	if d.config.ReadOnly {
		return nil, "", os.ErrPermission
	}
	s, source, rel, err := d.selectPath(name)
	if err != nil {
		return nil, "", err
	}
	if s.Type != "local" || rel == "." {
		return nil, "", os.ErrPermission
	}
	root, err := d.localRoot(s, source)
	return root, rel, err
}

func (d mountFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	s, source, rel, err := d.selectPath(name)
	if err != nil {
		return err
	}
	if s.ID != "" && s.Type != "local" {
		if d.config.ReadOnly || rel == "." {
			return os.ErrPermission
		}
		parent, err := d.cloudParent(ctx, s, source, rel)
		if err != nil {
			return err
		}
		defer d.app.cache.clear()
		return d.app.cloudMkdir(ctx, s, parent, path.Base(rel))
	}
	root, rel, err := d.writable(name)
	if err != nil {
		return err
	}
	defer root.Close()
	defer d.app.cache.clear()
	return root.Mkdir(rel, perm)
}

func (d mountFS) RemoveAll(ctx context.Context, name string) error {
	s, source, rel, err := d.selectPath(name)
	if err != nil {
		return err
	}
	if s.ID != "" && s.Type != "local" {
		if d.config.ReadOnly {
			return os.ErrPermission
		}
		if rel == "." {
			return os.ErrPermission
		}
		parent, err := d.cloudParent(ctx, s, source, rel)
		if err != nil {
			return err
		}
		target, err := d.app.cloudByName(ctx, s, parent, path.Base(rel))
		if err != nil || target == nil {
			return os.ErrNotExist
		}
		defer d.app.cache.clear()
		return d.app.cloudFileAction(ctx, s, s, fileActionRequest{Source: parent, IDs: []string{target.ID}, Action: "delete"})
	}
	root, rel, err := d.writable(name)
	if err != nil {
		return err
	}
	defer root.Close()
	defer d.app.cache.clear()
	// Honor the pool's trash policy for deletes initiated through a mount too.
	s, _, _, _ = d.selectPath(name)
	if s.Config["deleteMode"] != "permanent" {
		if rel == ".aether-trash" || strings.HasPrefix(rel, ".aether-trash/") {
			return os.ErrPermission
		}
		trash := path.Join(".aether-trash", id())
		if err := root.MkdirAll(trash, 0700); err != nil {
			return err
		}
		return root.Rename(rel, path.Join(trash, path.Base(rel)))
	}
	return root.RemoveAll(rel)
}

func (d mountFS) Rename(ctx context.Context, oldName, newName string) error {
	src, source, oldRel, err := d.selectPath(oldName)
	if err != nil {
		return err
	}
	dst, targetSource, newRel, err := d.selectPath(newName)
	if err != nil {
		return err
	}
	if src.ID != "" && src.Type != "local" {
		if d.config.ReadOnly || src.ID != dst.ID || source != targetSource {
			return os.ErrPermission
		}
		if oldRel == "." || newRel == "." {
			return os.ErrPermission
		}
		parent, err := d.cloudParent(ctx, src, source, oldRel)
		if err != nil {
			return err
		}
		target, err := d.app.cloudByName(ctx, src, parent, path.Base(oldRel))
		if err != nil || target == nil {
			return os.ErrNotExist
		}
		defer d.app.cache.clear()
		if path.Dir(oldRel) != path.Dir(newRel) {
			if path.Base(oldRel) != path.Base(newRel) {
				return os.ErrPermission
			}
			destination, err := d.cloudParent(ctx, dst, targetSource, newRel)
			if err != nil {
				return err
			}
			return d.app.cloudFileAction(ctx, src, src, fileActionRequest{Source: parent, Target: destination, IDs: []string{target.ID}, Action: "move"})
		}
		return d.app.cloudFileAction(ctx, src, src, fileActionRequest{Source: parent, IDs: []string{target.ID}, Action: "rename", Name: path.Base(newRel)})
	}
	root, from, err := d.writable(oldName)
	if err != nil {
		return err
	}
	defer root.Close()
	_, _, to, err := d.selectPath(newName)
	if err != nil || src.ID != dst.ID || source != targetSource || to == "." {
		return os.ErrPermission
	}
	defer d.app.cache.clear()
	return root.Rename(from, to)
}
