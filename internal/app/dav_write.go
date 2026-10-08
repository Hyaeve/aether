package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
)

// Stage in the destination directory so an interrupted PUT cannot truncate the original.
type davLocalWrite struct {
	*os.File
	root         *os.Root
	app          *App
	ctx          context.Context
	temp, target string
	expected     int64
	closed       bool
	writeErr     error
	storage      Storage
}

func (f *davLocalWrite) Write(data []byte) (int, error) {
	n, err := f.File.Write(data)
	if err != nil {
		f.writeErr = err
	}
	return n, err
}
func (f *davLocalWrite) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{f}, r)
}

func (d mountFS) openDAVLocalWrite(ctx context.Context, s Storage, source, rel string, flag int, perm os.FileMode) (*davLocalWrite, error) {
	if flag&os.O_TRUNC == 0 || !safeName(path.Base(rel)) {
		return nil, os.ErrPermission
	}
	root, err := d.localRoot(s, source)
	if err != nil {
		return nil, err
	}
	if info, err := root.Lstat(rel); err == nil {
		if !info.Mode().IsRegular() {
			root.Close()
			return nil, os.ErrPermission
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		root.Close()
		return nil, err
	}
	temp := path.Join(path.Dir(rel), ".aether-upload-"+id())
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_RDWR, perm)
	if err != nil {
		root.Close()
		return nil, err
	}
	expected := int64(-1)
	if value, ok := ctx.Value(mountPutLength{}).(int64); ok {
		expected = value
	}
	return &davLocalWrite{File: file, root: root, app: d.app, ctx: ctx, temp: temp, target: rel, expected: expected, storage: s}, nil
}

func (f *davLocalWrite) Stat() (os.FileInfo, error) {
	info, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	return davInfo{File{Name: path.Base(f.target), Size: info.Size(), Modified: info.ModTime()}}, nil
}
func (f *davLocalWrite) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	defer f.root.Close()
	defer f.root.Remove(f.temp)
	info, err := f.File.Stat()
	if err == nil {
		err = f.File.Sync()
	}
	if closeErr := f.File.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = f.ctx.Err()
	}
	if err == nil {
		err = f.writeErr
	}
	if body, ok := f.ctx.Value(mountPutFailure{}).(*mountRequestBody); ok && body.err != nil {
		err = body.err
	}
	if err == nil && f.expected >= 0 && info.Size() != f.expected {
		err = errors.New("上传内容不完整")
	}
	if err != nil {
		return err
	}
	f.app.runMu.Lock()
	defer f.app.runMu.Unlock()
	current, err := f.app.store.storage(f.storage.ID)
	if err != nil {
		return err
	}
	before, _ := json.Marshal(f.storage.Config)
	after, _ := json.Marshal(current.Config)
	if string(before) != string(after) {
		return errors.New("存储配置已变化，写入已停止")
	}
	if err = f.root.Rename(f.temp, f.target); err != nil {
		return err
	}
	f.app.cache.clear()
	f.app.uploaded.Add(info.Size())
	return nil
}
