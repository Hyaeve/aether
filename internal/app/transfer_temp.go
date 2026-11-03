package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// All application staging writers share a budget. Existing files, including
// failed uploads retained for diagnosis, count against it after a restart.
type transferTempBucket struct {
	root   string
	used   int64
	active map[*transferTempFile]bool
}
type transferTempFile struct {
	*os.File
	app            *App
	bucket         *transferTempBucket
	size, reserved int64
	closed         bool
}

func (a *App) transferTempLimit() int64 { return a.transferConfig().TempMaxGiB * (1 << 30) }

func (b *transferTempBucket) refresh() error {
	var total int64
	err := filepath.WalkDir(b.root, func(name string, d fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("临时文件目录不能包含符号链接")
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("临时文件目录包含非普通文件")
		}
		if info.Size() > (1<<63-1)-total {
			return errors.New("临时文件体积超出可计算范围")
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return err
	}
	for f := range b.active {
		info, err := f.File.Stat()
		if err != nil {
			return err
		}
		f.size = info.Size()
		if f.reserved > f.size {
			total += f.reserved - f.size
		}
	}
	b.used = total
	return nil
}

func (a *App) newTransferTemp(dir, pattern string, expected int64) (*transferTempFile, error) {
	a.tempMu.Lock()
	defer a.tempMu.Unlock()
	root, err := filepath.Abs(a.transferTempDirectory())
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, abs)
	// Some internal callers supply their own isolated staging directory.
	if err != nil || rel == ".." || len(rel) > 3 && rel[:3] == ".."+string(filepath.Separator) {
		root = abs
	}
	if resolved, e := filepath.EvalSymlinks(root); e == nil {
		root = resolved
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	if a.tempBuckets == nil {
		a.tempBuckets = map[string]*transferTempBucket{}
	}
	b := a.tempBuckets[root]
	if b == nil {
		b = &transferTempBucket{root: root, active: map[*transferTempFile]bool{}}
		a.tempBuckets[root] = b
	}
	if err := b.refresh(); err != nil {
		return nil, err
	}
	reserve := max(expected, 0)
	if reserve > a.transferTempLimit()-b.used {
		return nil, a.tempLimitError()
	}
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	wrapped := &transferTempFile{File: f, app: a, bucket: b, reserved: reserve}
	b.active[wrapped] = true
	b.used += reserve
	return wrapped, nil
}
func (a *App) tempLimitError() error {
	return fmt.Errorf("临时文件体积达到上限（%d G），请等待其他传输完成或清理已确认无用的暂存文件", a.transferConfig().TempMaxGiB)
}

// Called while tempMu is held. Reserving before every growth also covers
// unknown-length browser/WebDAV/FUSE uploads and concurrent random writes.
func (f *transferTempFile) allow(end int64) error {
	if f.closed {
		return os.ErrClosed
	}
	if end < 0 {
		return errors.New("临时文件大小无效")
	}
	if end > f.size && f.bucket.used > f.app.transferTempLimit() {
		if err := f.bucket.refresh(); err != nil {
			return err
		}
		if f.bucket.used > f.app.transferTempLimit() {
			return f.app.tempLimitError()
		}
	}
	need := max(end, f.size, f.reserved) - max(f.size, f.reserved)
	if need == 0 {
		return nil
	}
	if need > f.app.transferTempLimit()-f.bucket.used {
		if err := f.bucket.refresh(); err != nil {
			return err
		}
		if need > f.app.transferTempLimit()-f.bucket.used {
			return f.app.tempLimitError()
		}
	}
	f.bucket.used += need
	f.reserved = max(f.reserved, end)
	return nil
}
func (f *transferTempFile) Write(p []byte) (int, error) {
	f.app.tempMu.Lock()
	defer f.app.tempMu.Unlock()
	off, err := f.File.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if int64(len(p)) > (1<<63-1)-off {
		return 0, errors.New("临时文件大小无效")
	}
	if err = f.allow(off + int64(len(p))); err != nil {
		return 0, err
	}
	n, err := f.File.Write(p)
	f.size = max(f.size, off+int64(n))
	return n, err
}
func (f *transferTempFile) WriteAt(p []byte, off int64) (int, error) {
	f.app.tempMu.Lock()
	defer f.app.tempMu.Unlock()
	if off < 0 || int64(len(p)) > (1<<63-1)-off {
		return 0, errors.New("临时文件偏移无效")
	}
	if err := f.allow(off + int64(len(p))); err != nil {
		return 0, err
	}
	n, err := f.File.WriteAt(p, off)
	f.size = max(f.size, off+int64(n))
	return n, err
}
func (f *transferTempFile) Truncate(size int64) error {
	f.app.tempMu.Lock()
	defer f.app.tempMu.Unlock()
	if err := f.allow(size); err != nil {
		return err
	}
	if err := f.File.Truncate(size); err != nil {
		return err
	}
	f.size = size
	return nil
}
func (f *transferTempFile) WriteString(s string) (int, error) { return f.Write([]byte(s)) }
func (f *transferTempFile) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{f}, r)
}
func (f *transferTempFile) Close() error {
	f.app.tempMu.Lock()
	defer f.app.tempMu.Unlock()
	if f.closed {
		return os.ErrClosed
	}
	f.closed = true
	delete(f.bucket.active, f)
	f.bucket.used -= max(f.reserved, f.size) - f.size
	return f.File.Close()
}
