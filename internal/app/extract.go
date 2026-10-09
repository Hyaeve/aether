package app

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
	"github.com/ulikunitz/xz"
	zip "github.com/yeka/zip"
)

const extractInputLimit = int64(10 << 30)
const extractOutputLimit = int64(20 << 30)
const extractEntryLimit = 10000

func archiveFormat(name string) string {
	name = strings.ToLower(name)
	for _, suffix := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tgz", ".tbz2", ".txz", ".zip", ".7z", ".rar", ".tar"} {
		if strings.HasSuffix(name, suffix) {
			return suffix
		}
	}
	return ""
}

func extractName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\:\x00\r\n") || len(name) > 4096 {
		return false
	}
	for _, c := range name {
		if c < 32 || c == 127 {
			return false
		}
	}
	parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
	if len(parts) > 64 {
		return false
	}
	for _, part := range parts {
		if !safeName(part) || strings.TrimSpace(part) != part || len(part) > 255 || strings.HasSuffix(part, ".") {
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9' {
			return false
		}
	}
	return true
}

type extraction struct {
	ctx   context.Context
	root  *os.Root
	names map[string]bool
	count int
	files int
	bytes int64
}

func (e *extraction) entry(name string, mode fs.FileMode, size int64, reader io.Reader) error {
	if err := e.ctx.Err(); err != nil {
		return err
	}
	for strings.HasPrefix(name, "./") {
		name = strings.TrimPrefix(name, "./")
	}
	if (name == "" || name == ".") && mode.IsDir() {
		return nil
	}
	if e.count >= extractEntryLimit || !extractName(name) || mode.Type() != 0 && !mode.IsDir() || size < 0 || size > extractOutputLimit-e.bytes {
		return errors.New("压缩包包含不安全路径、链接、特殊文件，或超过解压限额")
	}
	name = strings.TrimSuffix(name, "/")
	key := strings.ToLower(name)
	if e.names[key] {
		return errors.New("压缩包包含重复路径")
	}
	e.names[key] = true
	e.count++
	if mode.IsDir() {
		return e.root.MkdirAll(name, 0700)
	}
	if err := e.root.MkdirAll(path.Dir(name), 0700); err != nil {
		return err
	}
	out, err := e.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(&cancelReader{ctx: e.ctx, reader: reader}, size+1))
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n != size {
		return errors.New("压缩包内容大小不符或不完整")
	}
	e.bytes += n
	e.files++
	return nil
}

type extractionReaderAt struct {
	ctx    context.Context
	reader io.ReaderAt
}

func (r extractionReaderAt) ReadAt(b []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.ReadAt(b, off)
}

func extractArchive(ctx context.Context, input, output, format, password string) (int, int64, error) {
	root, err := os.OpenRoot(output)
	if err != nil {
		return 0, 0, err
	}
	defer root.Close()
	e := extraction{ctx: ctx, root: root, names: map[string]bool{}}
	file, err := os.Open(input)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > extractInputLimit {
		return 0, 0, errors.New("压缩包过大")
	}
	readAt := extractionReaderAt{ctx, file}
	switch format {
	case ".zip":
		archive, err := zip.NewReader(readAt, info.Size())
		if err != nil {
			return 0, 0, err
		}
		if len(archive.File) > extractEntryLimit {
			return 0, 0, errors.New("压缩包条目超过10000项")
		}
		for _, f := range archive.File {
			if f.UncompressedSize64 > uint64(extractOutputLimit-e.bytes) {
				return 0, 0, errors.New("解压内容超过20GiB")
			}
			if f.IsEncrypted() {
				if password == "" {
					return 0, 0, errors.New("压缩包需要密码")
				}
				f.SetPassword(password)
			}
			r, err := f.Open()
			if err != nil {
				return 0, 0, err
			}
			err = e.entry(f.Name, f.Mode(), int64(f.UncompressedSize64), r)
			closeErr := r.Close()
			if err != nil {
				return 0, 0, err
			}
			if closeErr != nil {
				return 0, 0, closeErr
			}
		}
	case ".7z":
		archive, err := sevenzip.NewReaderWithPassword(readAt, info.Size(), password)
		if err != nil {
			return 0, 0, err
		}
		if len(archive.File) > extractEntryLimit {
			return 0, 0, errors.New("压缩包条目超过10000项")
		}
		for _, f := range archive.File {
			if f.UncompressedSize > uint64(extractOutputLimit-e.bytes) {
				return 0, 0, errors.New("解压内容超过20GiB")
			}
			r, err := f.Open()
			if err != nil {
				return 0, 0, err
			}
			hash := crc32.NewIEEE()
			err = e.entry(f.Name, f.Mode(), int64(f.UncompressedSize), io.TeeReader(r, hash))
			closeErr := r.Close()
			if err != nil {
				return 0, 0, err
			}
			if closeErr != nil {
				return 0, 0, closeErr
			}
			if !f.Mode().IsDir() && f.CRC32 != 0 && hash.Sum32() != f.CRC32 {
				return 0, 0, errors.New("7z文件校验失败")
			}
		}
	case ".rar":
		archive, err := rardecode.NewReader(&cancelReader{ctx: ctx, reader: file}, rardecode.Password(password), rardecode.MaxDictionarySize(128<<20))
		if err != nil {
			return 0, 0, err
		}
		for {
			f, err := archive.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return 0, 0, err
			}
			if f.LinkType != 0 || f.LinkTarget != "" || f.UnKnownSize {
				return 0, 0, errors.New("不支持压缩包链接或未知大小")
			}
			if err = e.entry(f.Name, f.Mode(), f.UnPackedSize, archive); err != nil {
				return 0, 0, err
			}
		}
	default:
		var reader io.Reader = &cancelReader{ctx: ctx, reader: file}
		switch format {
		case ".tar":
		case ".tar.gz", ".tgz":
			g, err := gzip.NewReader(reader)
			if err != nil {
				return 0, 0, err
			}
			defer g.Close()
			reader = g
		case ".tar.bz2", ".tbz2":
			reader = bzip2.NewReader(reader)
		case ".tar.xz", ".txz":
			x, err := (xz.ReaderConfig{DictCap: 128 << 20}).NewReader(reader)
			if err != nil {
				return 0, 0, err
			}
			reader = x
		default:
			return 0, 0, errors.New("不支持的压缩格式")
		}
		archive := tar.NewReader(reader)
		for {
			f, err := archive.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return 0, 0, err
			}
			mode := fs.FileMode(0600)
			switch f.Typeflag {
			case tar.TypeDir:
				mode = fs.ModeDir | 0700
			case tar.TypeReg, tar.TypeRegA:
			default:
				return 0, 0, errors.New("压缩包含链接或特殊文件")
			}
			if err = e.entry(f.Name, mode, f.Size, archive); err != nil {
				return 0, 0, err
			}
		}
		// Read through compressed trailers too, so a broken gzip/xz checksum cannot be published.
		if n, err := io.Copy(io.Discard, io.LimitReader(&cancelReader{ctx: ctx, reader: reader}, (1<<20)+1)); err != nil || n > 1<<20 {
			return 0, 0, errors.New("压缩包尾部校验失败或包含过多附加内容")
		}
	}
	if e.count == 0 {
		return 0, 0, errors.New("压缩包为空")
	}
	return e.files, e.bytes, nil
}

func (a *App) stageArchive(ctx context.Context, s Storage, f File, ua, target string) error {
	d, err := a.downloadWithUA(ctx, s, f.ID, f.PickCode, ua)
	if err != nil {
		return errors.New("读取压缩包链接失败")
	}
	var reader io.ReadCloser
	if s.Type == "local" {
		root, err := os.OpenRoot(s.Config["root"])
		if err != nil {
			return err
		}
		reader, err = root.Open(d.Local)
		root.Close()
		if err != nil {
			return err
		}
	} else {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
		if err != nil {
			return errors.New("压缩包地址无效")
		}
		req.Header = d.Headers.Clone()
		client := &http.Client{Transport: apiClient.Transport, Timeout: 30 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := client.Do(req)
		if err != nil {
			return errors.New("读取上游压缩包失败")
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return fmt.Errorf("压缩包读取 HTTP %d", res.StatusCode)
		}
		reader = res.Body
	}
	defer reader.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	progress := a.beginTransfer(ctx, "download", s, f.Name, f.Size)
	n, err := io.Copy(out, io.LimitReader(&cancelReader{ctx: ctx, reader: transferReader{reader, progress}}, extractInputLimit+1))
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil && (n > extractInputLimit || f.Size > 0 && n != f.Size) {
		err = errors.New("压缩包大小变化或超过10GiB")
	}
	progress.finish(err)
	return err
}

func (a *App) fileExtract(w http.ResponseWriter, r *http.Request) {
	var in struct {
		StorageID string `json:"storageId"`
		Parent    string `json:"parent"`
		ID        string `json:"id"`
		Name      string `json:"name"`
		Password  string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !extractName(in.Name) || strings.Contains(in.Name, "/") || len([]rune(in.Password)) > 128 {
		fail(w, 400, errors.New("解压目录名称或密码长度无效"))
		return
	}
	s, err := a.store.storage(in.StorageID)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if !a.extracting.CompareAndSwap(0, 1) {
		fail(w, 409, errors.New("已有压缩包正在解压，请等待完成"))
		return
	}
	defer a.extracting.Store(0)
	a.runMu.Lock()
	busy := len(a.running) != 0
	a.runMu.Unlock()
	if busy {
		fail(w, 409, errors.New("有任务正在执行，请等待完成后解压"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	items, err := a.rawList(ctx, s, in.Parent)
	if err != nil {
		fail(w, 400, errors.New("目录不可访问"))
		return
	}
	var selected *File
	for _, f := range items {
		if strings.EqualFold(f.Name, in.Name) {
			fail(w, 409, errors.New("解压目录已存在，请更换名称"))
			return
		}
		if f.ID == in.ID && !f.IsDir {
			copy := f
			selected = &copy
		}
	}
	if selected == nil || archiveFormat(selected.Name) == "" || selected.Size > extractInputLimit {
		fail(w, 400, errors.New("请选择10GiB以内的ZIP、7z、RAR或TAR压缩包"))
		return
	}
	base := filepath.Join(a.dataDir, "cache", "extract")
	if err = os.MkdirAll(base, 0700); err != nil {
		fail(w, 500, errors.New("无法创建解压暂存目录"))
		return
	}
	dir, err := os.MkdirTemp(base, "job-")
	if err != nil {
		fail(w, 500, errors.New("无法创建解压暂存目录"))
		return
	}
	defer os.RemoveAll(dir)
	input, output := filepath.Join(dir, "archive"), filepath.Join(dir, "files")
	if err = os.Mkdir(output, 0700); err != nil {
		fail(w, 500, err)
		return
	}
	if err = a.stageArchive(ctx, s, *selected, r.UserAgent(), input); err != nil {
		fail(w, 502, err)
		return
	}
	count, size, err := extractArchive(ctx, input, output, archiveFormat(selected.Name), in.Password)
	if err != nil {
		a.store.event("warn", "files", "压缩包解压校验失败，未写入目标存储")
		fail(w, 400, errors.New("解压失败，请检查密码、压缩包完整性或格式；不支持分卷、链接及超限内容"))
		return
	}
	// Verify all entries/passwords before creating anything in the destination.
	current, err := a.store.storage(s.ID)
	if err != nil || shareConfig(current) != shareConfig(s) {
		fail(w, 409, errors.New("存储配置已变化，请重新操作"))
		return
	}
	if err = a.createDirectory(ctx, s, in.Parent, in.Name); err != nil {
		fail(w, 400, err)
		return
	}
	processed := 0
	err = filepath.WalkDir(output, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == output {
			return nil
		}
		rel, err := filepath.Rel(output, name)
		if err != nil {
			return err
		}
		rel = in.Name + "/" + filepath.ToSlash(rel)
		if entry.IsDir() {
			_, _, err = a.uploadParent(ctx, s, in.Parent, rel+"/placeholder")
			return err
		}
		if err = a.storeUploadedFile(ctx, s, in.Parent, rel, name); err == nil {
			processed++
		}
		return err
	})
	a.cache.clear()
	if err != nil {
		a.store.event("error", "files", fmt.Sprintf("解压写入失败，已写入%d/%d个文件", processed, count))
		fail(w, 500, fmt.Errorf("解压写入失败，已写入%d/%d个文件；请检查目标目录后重试，已有文件不会覆盖", processed, count))
		return
	}
	a.store.event("info", "files", fmt.Sprintf("压缩包解压完成：%d个文件，%d字节", count, size))
	jsonResponse(w, 200, map[string]any{"processed": count, "bytes": size, "directory": in.Name})
}
