package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type organizeEntry struct {
	file             File
	parent, relative string
	identity         os.FileInfo
	black            bool
}

func organizeLocalDir(storage Storage, dir string) (os.FileInfo, error) {
	name, err := relative(dir)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(storage.Config["root"])
	if err != nil {
		return nil, err
	}
	defer root.Close()
	current := "."
	var info os.FileInfo
	for _, part := range strings.Split(name, "/") {
		current = path.Join(current, part)
		info, err = root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("整理路径包含符号链接或非目录项")
		}
	}
	return info, nil
}

func (h *organizeRunner) uploadParent(ctx context.Context, s Storage, parent, name string) (string, string, error) {
	if err := h.check(ctx); err != nil {
		return "", "", err
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if !validLocalDirectoryName(part) {
			return "", "", errors.New("归档路径包含无效名称")
		}
	}
	if s.Type != "local" {
		return h.a.uploadParent(ctx, s, parent, name)
	}
	root, err := os.OpenRoot(s.Config["root"])
	if err != nil {
		return "", "", err
	}
	defer root.Close()
	current, err := relative(parent)
	if err != nil {
		return "", "", err
	}
	if _, err = organizeLocalDir(s, parent); err != nil {
		return "", "", err
	}
	for _, part := range parts[:len(parts)-1] {
		current = path.Join(current, part)
		info, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err = root.Mkdir(current, 0755); err != nil {
				return "", "", err
			}
			continue
		}
		if err != nil {
			return "", "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", errors.New("归档路径与文件或符号链接冲突")
		}
	}
	return path.Join("/", current), parts[len(parts)-1], nil
}

func organizeExcluded(name string, dir bool, filter string) bool {
	parts := strings.Split(name, "/")
	for _, key := range strings.Split(filter, ";") {
		if key == "" {
			continue
		}
		if strings.HasPrefix(key, "/") {
			for i, p := range parts {
				if (dir || i < len(parts)-1) && strings.Contains(p, key[1:]) {
					return true
				}
			}
		} else if !dir && strings.HasPrefix(key, ".") {
			if strings.HasSuffix(strings.ToLower(path.Base(name)), key) {
				return true
			}
		} else if !dir && strings.Contains(path.Base(name), key) {
			return true
		}
	}
	return false
}
func (h *organizeRunner) scan(ctx context.Context) ([]organizeEntry, error) {
	var entries []organizeEntry
	seen := map[string]bool{}
	count := 0
	var walk func(string, string, int) error
	walk = func(parent, rel string, depth int) error {
		if err := h.check(ctx); err != nil {
			return err
		}
		if h.source.Type == "local" {
			if _, err := organizeLocalDir(h.source, parent); err != nil {
				return err
			}
		}
		if depth > 128 || seen[parent] {
			return errors.New("整理目录过深或存在循环")
		}
		seen[parent] = true
		files, err := h.a.rawList(ctx, h.source, parent)
		if err != nil {
			return err
		}
		for _, f := range files {
			count++
			if count > 100000 {
				return errors.New("本次整理超过100000项，请缩小源目录")
			}
			if !safeName(f.Name) {
				return errors.New("源目录含无效文件名称")
			}
			if strings.HasPrefix(f.Name, ".aether-") {
				continue
			}
			relName := path.Join(rel, f.Name)
			if organizeExcluded(relName, f.IsDir, h.t.ExcludeList) {
				continue
			}
			e := organizeEntry{file: f, parent: parent, relative: relName, black: h.rec.blacklistMatch(relName, f.IsDir)}
			if h.source.Type == "local" {
				root, err := os.OpenRoot(h.source.Config["root"])
				if err != nil {
					return err
				}
				name, err := relative(f.ID)
				if err != nil {
					root.Close()
					return err
				}
				e.identity, err = root.Lstat(name)
				root.Close()
				if err != nil {
					return err
				}
				if e.identity.Mode()&os.ModeSymlink != 0 {
					continue
				}
				if !e.identity.IsDir() && !e.identity.Mode().IsRegular() {
					continue
				}
			}
			if f.IsDir && !e.black {
				h.sourceDirs[f.ID] = e
				if err = walk(f.ID, relName, depth+1); err != nil {
					return err
				}
				continue
			}
			if e.black || !f.IsDir && directoryScrapeMedia(f.Name) {
				entries = append(entries, e)
			}
		}
		return nil
	}
	err := walk(h.t.Source, "", 0)
	return entries, err
}

func (h *organizeRunner) current(ctx context.Context, s Storage, e organizeEntry) error {
	if err := h.check(ctx); err != nil {
		return err
	}
	if s.Type == "local" {
		root, err := os.OpenRoot(s.Config["root"])
		if err != nil {
			return err
		}
		defer root.Close()
		rel, err := relative(e.file.ID)
		if err != nil {
			return err
		}
		info, err := root.Lstat(rel)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || e.identity != nil && !os.SameFile(e.identity, info) || info.IsDir() != e.file.IsDir || !info.IsDir() && (info.Size() != e.file.Size || !info.ModTime().Equal(e.file.Modified)) {
			return errors.New("源文件身份或内容已变化，保留源文件")
		}
		return nil
	}
	files, err := h.a.rawList(ctx, s, e.parent)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.ID == e.file.ID && f.Name == e.file.Name && f.IsDir == e.file.IsDir && f.Size == e.file.Size && f.Modified.Equal(e.file.Modified) {
			return nil
		}
	}
	return errors.New("云端源文件已变化，保留文件")
}

// Do not overwrite an existing destination. Recheck identities before removing sources.
func (h *organizeRunner) place(ctx context.Context, s Storage, e organizeEntry, d Storage, parent, name, method string) (File, error) {
	if !validLocalDirectoryName(name) {
		return File{}, errors.New("归档名称无效")
	}
	if err := h.current(ctx, s, e); err != nil {
		return File{}, err
	}
	old, err := h.a.cloudByName(ctx, d, parent, name)
	if err != nil {
		return File{}, err
	}
	if old != nil {
		return File{}, errors.New("目标已存在同名文件，未覆盖")
	}
	if s.Type == "local" && d.Type == "local" {
		if _, err := organizeLocalDir(s, e.parent); err != nil {
			return File{}, err
		}
		if _, err := organizeLocalDir(d, parent); err != nil {
			return File{}, err
		}
		a, b := s.Config["root"], d.Config["root"]
		src, err := os.OpenRoot(a)
		if err != nil {
			return File{}, err
		}
		defer src.Close()
		dst, err := os.OpenRoot(b)
		if err != nil {
			return File{}, err
		}
		defer dst.Close()
		from, err := relative(e.file.ID)
		if err != nil {
			return File{}, err
		}
		to, err := relative(path.Join(parent, name))
		if err != nil {
			return File{}, err
		}
		if _, err = dst.Lstat(to); err == nil {
			return File{}, errors.New("目标已存在同名项目，未覆盖")
		} else if !errors.Is(err, os.ErrNotExist) {
			return File{}, err
		}
		if e.file.IsDir {
			if method != "move" {
				return File{}, errors.New("目录归类仅支持移动")
			}
			fromAbs, fromErr := taskLocalPath(s, from)
			toAbs, toErr := taskLocalPath(d, to)
			if fromErr != nil || toErr != nil || backupPathsOverlap(fromAbs, toAbs) {
				return File{}, errors.New("目录不能移入自身")
			}
			if a != b {
				return File{}, errors.New("本地目录仅支持在同一存储根内归类")
			}
			err = src.Rename(from, to)
		} else {
			switch method {
			case "symlink":
				err = os.Symlink(filepath.Join(a, filepath.FromSlash(from)), filepath.Join(b, filepath.FromSlash(to)))
			case "hardlink":
				err = os.Link(filepath.Join(a, filepath.FromSlash(from)), filepath.Join(b, filepath.FromSlash(to)))
			case "move", "copy":
				// Publish through a no-clobber hard link after a durable temporary copy.
				if method == "move" {
					err = os.Link(filepath.Join(a, filepath.FromSlash(from)), filepath.Join(b, filepath.FromSlash(to)))
					if err == nil {
						if err = h.current(ctx, s, e); err == nil {
							err = src.Remove(from)
						}
						break
					}
				}
				temp := path.Join(path.Dir(to), ".aether-organize-"+id())
				var in, out *os.File
				in, err = src.Open(from)
				if err != nil {
					break
				}
				out, err = dst.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err != nil {
					in.Close()
					break
				}
				_, err = io.Copy(out, &cancelReader{ctx, in})
				in.Close()
				if err == nil {
					err = out.Sync()
				}
				err = errors.Join(err, out.Close())
				if err == nil {
					err = h.current(ctx, s, e)
				}
				if err == nil {
					err = dst.Link(temp, to)
				}
				_ = dst.Remove(temp)
				if err == nil && method == "move" {
					err = h.current(ctx, s, e)
					if err == nil {
						err = src.Remove(from)
					}
				}
			default:
				err = errors.New("整理方式无效")
			}
		}
		if err != nil {
			return File{}, err
		}
		return File{ID: path.Join(parent, name), Name: name, Size: e.file.Size, IsDir: e.file.IsDir}, nil
	}
	if method != "move" && method != "copy" {
		return File{}, errors.New("云存储不支持此整理方式")
	}
	if s.ID != d.ID {
		if e.file.IsDir {
			return File{}, errors.New("跨账号目录归类暂不支持，保留目录")
		}
		var digest string
		ctx = context.WithValue(ctx, transferSourceKey{}, "整理归档")
		if err = h.a.copyBackupFile(ctx, s, d, e.file, parent, name, "skip", &digest); err != nil {
			return File{}, err
		}
		actual, err := h.a.backupFileDigest(ctx, d, parent, name)
		if err != nil || actual == "" || actual != digest {
			return File{}, errors.New("跨池归档内容校验失败，源文件保留")
		}
		if method == "move" {
			current, err := h.a.backupFileDigest(ctx, s, e.parent, e.file.Name)
			if err != nil || current != digest {
				return File{}, errors.New("跨池归档期间源文件已变化，保留源文件")
			}
		}
	} else {
		// Copy/move using the provider's native operation; rename in the destination,
		// never rename the original merely to prepare a copy.
		if name != e.file.Name {
			f, err := h.a.cloudByName(ctx, d, parent, e.file.Name)
			if err != nil {
				return File{}, err
			}
			if f != nil {
				return File{}, errors.New("归档目录存在同名原始文件，未提交")
			}
		}
		if err = h.a.cloudFileAction(ctx, s, d, fileActionRequest{Source: e.parent, StorageID: s.ID, Action: method, IDs: []string{e.file.ID}, TargetStorage: d.ID, Target: parent}); err != nil {
			return File{}, err
		}
		var copied *File
		for i := 0; i < 5; i++ {
			copied, err = h.a.cloudByName(ctx, d, parent, e.file.Name)
			if err != nil {
				return File{}, err
			}
			if copied != nil {
				break
			}
			select {
			case <-ctx.Done():
				return File{}, ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
		if copied == nil || copied.IsDir != e.file.IsDir || copied.Size != e.file.Size {
			return File{}, errors.New("云端整理结果未确认，请核对目标目录；未重复提交")
		}
		if copied.Name != name {
			if err = h.a.cloudFileAction(ctx, d, d, fileActionRequest{Source: parent, StorageID: d.ID, Action: "rename", IDs: []string{copied.ID}, Name: name}); err != nil {
				return File{}, fmt.Errorf("已%s至目标但改名失败：%w", method, err)
			}
		}
	}
	result, err := h.a.cloudByName(ctx, d, parent, name)
	if err != nil {
		return File{}, err
	}
	if result == nil || result.Size != e.file.Size {
		return File{}, errors.New("目标文件尚未确认，不删除源文件")
	}
	if s.ID != d.ID && method == "move" {
		if err = h.current(ctx, s, e); err != nil {
			return File{}, err
		}
		if err = h.recycleFrom(ctx, s, e); err != nil {
			return File{}, err
		}
	}
	return *result, nil
}

func (h *organizeRunner) recycle(ctx context.Context, e organizeEntry) error {
	return h.recycleFrom(ctx, h.source, e)
}

func (h *organizeRunner) recycleFrom(ctx context.Context, source Storage, e organizeEntry) error {
	if err := h.current(ctx, source, e); err != nil {
		return err
	}
	if source.Type != "local" {
		s := source
		s.Config = maps.Clone(s.Config)
		delete(s.Config, "deleteMode")
		return h.a.cloudFileAction(ctx, s, s, fileActionRequest{Source: e.parent, StorageID: s.ID, Action: "delete", IDs: []string{e.file.ID}})
	}
	parent, _, err := h.uploadParent(ctx, source, "/", ".aether-trash/"+id()+"/"+e.file.Name)
	if err != nil {
		return err
	}
	_, err = h.place(ctx, source, e, source, parent, e.file.Name, "move")
	return err
}
func (h *organizeRunner) divert(ctx context.Context, e organizeEntry, black bool) error {
	if h.t.FailureDirectory == "" {
		if black {
			return h.recycle(ctx, e)
		}
		return nil
	}
	return h.divertFrom(ctx, h.source, e)
}

func (h *organizeRunner) divertFrom(ctx context.Context, source Storage, e organizeEntry) error {
	name := e.file.Name
	for i := 0; i < 10000; i++ {
		if i > 0 {
			suffix := recognitionFilenameSuffix(e.file.Name)
			name = fmt.Sprintf("%s_%d%s", strings.TrimSuffix(e.file.Name, suffix), i, suffix)
		}
		old, err := h.a.cloudByName(ctx, h.source, h.t.FailureDirectory, name)
		if err != nil {
			return err
		}
		if old != nil {
			continue
		}
		_, err = h.place(ctx, source, e, h.source, h.t.FailureDirectory, name, "move")
		return err
	}
	return errors.New("冗余目录同名条目过多")
}
func (h *organizeRunner) digest(ctx context.Context, s Storage, f File) (string, error) {
	if validOrganizeDigest(f.SHA256, 32) {
		return "sha256:" + strings.ToLower(f.SHA256), nil
	}
	if validOrganizeDigest(f.MD5, 16) {
		return "md5:" + strings.ToLower(f.MD5), nil
	}
	if s.Type == "115" {
		c, err := client115(ctx, s)
		if err != nil {
			return "", err
		}
		info, err := c.GetFile(f.ID)
		if err != nil {
			return "", err
		}
		if info.FileID != f.ID || info.Size != f.Size || info.Name != f.Name {
			return "", errors.New("哈希查询文件身份不符")
		}
		if simulcastSHA1(info.Sha1) {
			return "sha1:" + strings.ToLower(info.Sha1), nil
		}
	}
	if s.Type != "local" {
		return "", nil
	}
	root, err := os.OpenRoot(s.Config["root"])
	if err != nil {
		return "", err
	}
	defer root.Close()
	rel, err := relative(f.ID)
	if err != nil {
		return "", err
	}
	file, err := root.Open(rel)
	if err != nil {
		return "", err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return "", errors.New("不能读取文件校验值")
	}
	sum := sha256.New()
	if _, err = io.Copy(sum, &cancelReader{ctx, file}); err != nil {
		return "", err
	}
	after, err := file.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", errors.New("计算期间文件已变化")
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil)), nil
}

func validOrganizeDigest(value string, size int) bool {
	if len(value) != size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// Only directories touched by a completed move are eligible for cleanup.
// Work upwards while each directory is empty, leaving the selected root intact.
func (h *organizeRunner) cleanup(ctx context.Context, entry organizeEntry) error {
	if h.t.Method != "move" {
		return nil
	}
	if h.t.MetadataExtensions == "" {
		files, err := h.a.rawList(ctx, h.source, entry.parent)
		if err != nil {
			return err
		}
		for _, f := range files {
			rel := path.Join(path.Dir(entry.relative), f.Name)
			if !f.IsDir && organizeCompanionName(entry.file.Name, f.Name, "srt;ass;ssa;sub;vtt") != "" && !organizeExcluded(rel, false, h.t.ExcludeList) && !h.rec.blacklistMatch(rel, false) {
				if err = h.recycle(ctx, organizeEntry{file: f, parent: entry.parent, relative: rel}); err != nil {
					return err
				}
			}
		}
	}
	for current := entry.parent; current != h.t.Source; {
		dir, ok := h.sourceDirs[current]
		if !ok {
			break
		}
		if err := h.check(ctx); err != nil {
			return err
		}
		if h.source.Type == "local" {
			if err := h.current(ctx, h.source, dir); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
			root, err := os.OpenRoot(h.source.Config["root"])
			if err != nil {
				return err
			}
			rel, err := relative(current)
			if err == nil {
				var d *os.File
				d, err = root.Open(rel)
				if err == nil {
					var names []string
					names, err = d.Readdirnames(1)
					d.Close()
					if len(names) > 0 {
						root.Close()
						return nil
					}
					if errors.Is(err, io.EOF) {
						err = root.Remove(rel)
					}
				}
			}
			root.Close()
			if err != nil {
				return err
			}
		} else {
			files, err := h.a.rawList(ctx, h.source, current)
			if err != nil {
				return err
			}
			if len(files) > 0 {
				return nil
			}
			if err = h.current(ctx, h.source, dir); err != nil {
				return err
			}
			s := h.source
			s.Config = maps.Clone(s.Config)
			delete(s.Config, "deleteMode")
			if err = h.a.cloudFileAction(ctx, s, s, fileActionRequest{Source: dir.parent, Action: "delete", IDs: []string{dir.file.ID}, emptyOnly: true}); err != nil {
				return err
			}
		}
		current = dir.parent
	}
	return nil
}
