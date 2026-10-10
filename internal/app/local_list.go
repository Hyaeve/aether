package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
)

const localListBatchSize = 256

func localRawList(ctx context.Context, s Storage, dir string) ([]File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, err := relative(dir)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.Config["root"])
	if err != nil {
		return nil, errors.New("无法打开本地根目录")
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []File{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Root-backed ReadDir also loads metadata eagerly. Readdir avoids the
		// additional DirEntry wrappers while keeping descriptor-relative stats.
		infos, err := f.Readdir(localListBatchSize)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if cap(out) == 0 && len(infos) > 0 {
			out = make([]File, 0, len(infos))
		}
		for _, info := range infos {
			if info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			out = append(out, File{ID: path.Join("/", dir, info.Name()), Name: info.Name(), IsDir: info.IsDir(), Size: info.Size(), Modified: info.ModTime()})
		}
		if errors.Is(err, io.EOF) {
			return out, nil
		}
	}
}
