package app

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type featureCleanupItem struct {
	file                         File
	parent                       string
	identity                     os.FileInfo
	digest, output, outputDigest string
	protected                    bool
}

type featureCleanup struct {
	task          Task
	source        Storage
	local         *os.Root
	rootIdentity  os.FileInfo
	scopeIdentity os.FileInfo
	items         map[string]*featureCleanupItem
	files, dirs   []*featureCleanupItem
}

func (a *App) newFeatureCleanup(t Task, s Storage) (*featureCleanup, error) {
	if t.CompletionAction != "delete" || (t.Kind != "cas" && t.Kind != "ed2k") {
		return nil, nil
	}
	if t.Kind == "cas" && t.CASOperation == "restore" {
		return nil, errors.New("旧 CAS 恢复任务不能删除源文件")
	}
	if t.Source == "" || t.Source == "/" || t.Source == rootOf(s) {
		return nil, errors.New("删除源文件时须选择存储池内的子目录，不能清理根目录")
	}
	c := &featureCleanup{task: t, source: s, items: map[string]*featureCleanupItem{}}
	if s.Type == "local" {
		rel, err := relative(t.Source)
		if err != nil || rel == "." || rel == ".aether-trash" || strings.HasPrefix(rel, ".aether-trash/") {
			return nil, errors.New("不能清理根目录或回收站")
		}
		from, err := canonicalOutput(filepath.Join(s.Config["root"], filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		base, target, err := a.outputLocation(t.Target)
		if err != nil {
			return nil, err
		}
		to, err := canonicalOutput(filepath.Join(base, filepath.FromSlash(target)))
		if err != nil {
			return nil, err
		}
		if pathOverlaps(from, to) {
			return nil, errors.New("删除源文件时，生成目录不能与源目录重叠")
		}
		c.local, err = os.OpenRoot(s.Config["root"])
		if err != nil {
			return nil, err
		}
		c.rootIdentity, err = os.Stat(s.Config["root"])
		if err != nil {
			c.close()
			return nil, err
		}
		c.scopeIdentity, err = c.local.Lstat(rel)
		if err != nil || !c.scopeIdentity.IsDir() || c.scopeIdentity.Mode()&os.ModeSymlink != 0 {
			c.close()
			return nil, errors.New("源目录不存在或为符号链接")
		}
		for parent := path.Dir(rel); parent != "."; parent = path.Dir(parent) {
			info, err := c.local.Lstat(parent)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				c.close()
				return nil, errors.New("源目录不能经过符号链接")
			}
		}
	}
	return c, nil
}

func (c *featureCleanup) close() {
	if c != nil && c.local != nil {
		_ = c.local.Close()
	}
}

func hashFeatureFile(ctx context.Context, in io.Reader) (string, error) {
	h := sha256.New()
	_, err := io.Copy(h, &cancelReader{ctx, in})
	return hex.EncodeToString(h.Sum(nil)), err
}

func (c *featureCleanup) observe(ctx context.Context, a *App, parent string, f File, skipDigest ...bool) error {
	if len(c.items) >= 100000 {
		return errors.New("源清理扫描超过十万项，未进行清理")
	}
	if _, exists := c.items[f.ID]; exists {
		return errors.New("源目录包含重复项目，未进行清理")
	}
	item := &featureCleanupItem{file: f, parent: parent}
	c.items[f.ID] = item
	if c.local != nil {
		rel, err := relative(f.ID)
		if err != nil {
			return err
		}
		scope, _ := relative(c.task.Source)
		if !strings.HasPrefix(rel, scope+"/") {
			return errors.New("源项目不在所选目录内")
		}
		item.identity, err = c.local.Lstat(rel)
		if err != nil || item.identity.Mode()&os.ModeSymlink != 0 {
			return errors.New("源文件不存在或是符号链接，未进行清理")
		}
	}
	if f.IsDir {
		return nil
	}
	feature := c.task.Kind == "cas" && (strings.EqualFold(path.Ext(f.Name), ".cas") || strings.HasSuffix(strings.ToLower(f.Name), ".cas.strm"))
	item.protected = taskMedia(c.task, f.Name) || excludedType(f.Name, c.task.MetadataExtensions) || feature || excluded(f.Name, c.task.ExcludeFiles) || excludedType(f.Name, c.task.ExcludeTypes)
	if len(skipDigest) > 0 && skipDigest[0] {
		item.protected = true
		c.files = append(c.files, item)
		return nil
	}
	var err error
	item.digest, err = c.sourceDigest(ctx, a, item)
	if err != nil {
		return err
	}
	c.files = append(c.files, item)
	return nil
}

func (c *featureCleanup) sourceDigest(ctx context.Context, a *App, item *featureCleanupItem) (string, error) {
	if c.local != nil {
		rel, err := relative(item.file.ID)
		if err != nil {
			return "", err
		}
		in, err := c.local.Open(rel)
		if err != nil {
			return "", err
		}
		defer in.Close()
		before, err := in.Stat()
		if err != nil || !before.Mode().IsRegular() || !os.SameFile(item.identity, before) || before.Size() != item.file.Size || !before.ModTime().Equal(item.file.Modified) {
			return "", errors.New("源文件在扫描期间变化，保持不变")
		}
		digest, err := hashFeatureSource(ctx, in, c.task.Kind == "ed2k")
		after, e := in.Stat()
		if e != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
			return "", errors.New("源文件在读取期间变化，保持不变")
		}
		return digest, err
	}
	release, err := a.acquireTransferResource(ctx, "feature-hash")
	if err != nil {
		return "", err
	}
	defer release()
	ctx = context.WithValue(ctx, davFreshReadKey{}, true)
	in, err := (mountFS{app: a, config: MountConfig{StorageID: c.source.ID, Source: item.parent}}).OpenFile(ctx, "/"+item.file.Name, os.O_RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer in.Close()
	limited := &io.LimitedReader{R: in, N: item.file.Size + 1}
	digest, err := hashFeatureSource(ctx, limited, c.task.Kind == "ed2k")
	if err == nil && limited.N != 1 {
		return "", errors.New("源文件读取大小与扫描记录不一致，未进行清理")
	}
	return digest, err
}

func hashFeatureSource(ctx context.Context, in io.Reader, ed2k bool) (string, error) {
	sha, md := sha256.New(), md5.New()
	reader := io.TeeReader(&cancelReader{ctx, in}, io.MultiWriter(sha, md))
	var feature string
	var err error
	if ed2k {
		feature, err = ed2kHash(reader)
	} else {
		_, err = io.Copy(io.Discard, reader)
	}
	return hex.EncodeToString(sha.Sum(nil)) + ":" + hex.EncodeToString(md.Sum(nil)) + ":" + feature, err
}

func (c *featureCleanup) verifyCAS(f File, info CASInfo, featureSource bool) error {
	if c == nil || featureSource {
		return nil
	}
	item := c.items[f.ID]
	parts := strings.Split(item.digest, ":")
	if len(parts) != 3 || info.SHA256 != "" && info.SHA256 != parts[0] || info.MD5 != "" && info.MD5 != parts[1] {
		return errors.New("CAS特征与扫描时源文件不一致，保留源文件")
	}
	return nil
}

func (c *featureCleanup) verifyED2K(f File, info ED2KInfo) error {
	if c == nil {
		return nil
	}
	parts := strings.Split(c.items[f.ID].digest, ":")
	if len(parts) != 3 || parts[2] != info.Hash {
		return errors.New("ED2K特征与扫描时源文件不一致，保留源文件")
	}
	return nil
}

func (c *featureCleanup) directory(parent string, f File) {
	if c != nil {
		c.dirs = append(c.dirs, c.items[f.ID])
	}
}

func (c *featureCleanup) published(ctx context.Context, root *os.Root, f File, name string, metadata bool) error {
	if c == nil {
		return nil
	}
	item := c.items[f.ID]
	if item == nil {
		return errors.New("源清理缺少扫描记录")
	}
	in, err := root.Open(name)
	if err != nil {
		return err
	}
	digest, err := hashFeatureFile(ctx, in)
	in.Close()
	if err != nil {
		return err
	}
	if metadata && digest != strings.Split(item.digest, ":")[0] {
		return errors.New("元数据复制后校验失败，保留源文件")
	}
	item.output, item.outputDigest = name, digest
	return nil
}

func sameFeatureFile(a, b File) bool {
	return a.ID == b.ID && a.Name == b.Name && a.IsDir == b.IsDir && a.Size == b.Size && a.Modified.Equal(b.Modified) && a.Created.Equal(b.Created) && a.SHA256 == b.SHA256 && a.MD5 == b.MD5 && a.PickCode == b.PickCode
}

func (c *featureCleanup) checkSource(ctx context.Context, a *App, item *featureCleanupItem) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := a.store.storage(c.source.ID)
	if err != nil || !current.Enabled || !sameBackupStorage(current, c.source) {
		return errors.New("源存储配置已变化，停止清理")
	}
	if c.local != nil {
		rootNow, err := os.Stat(c.source.Config["root"])
		if err != nil || !os.SameFile(c.rootIdentity, rootNow) {
			return errors.New("源存储根目录已变化，停止清理")
		}
		scope, _ := relative(c.task.Source)
		scopeNow, err := c.local.Lstat(scope)
		if err != nil || !os.SameFile(c.scopeIdentity, scopeNow) || scopeNow.Mode()&os.ModeSymlink != 0 {
			return errors.New("所选源目录已变化，停止清理")
		}
		rel, err := relative(item.file.ID)
		if err != nil {
			return err
		}
		now, err := c.local.Lstat(rel)
		if err != nil || now.Mode()&os.ModeSymlink != 0 || !os.SameFile(item.identity, now) {
			return errors.New("源项目已变化，保留原目录结构")
		}
		for parent := path.Dir(rel); parent != scope && parent != "."; parent = path.Dir(parent) {
			ancestor := c.items["/"+parent]
			now, err := c.local.Lstat(parent)
			if ancestor == nil || err != nil || now.Mode()&os.ModeSymlink != 0 || !os.SameFile(ancestor.identity, now) {
				return errors.New("源项目的父目录已变化，停止清理")
			}
		}
	} else {
		files, err := a.rawList(ctx, current, item.parent)
		if err != nil {
			return err
		}
		found := false
		for _, f := range files {
			if f.ID == item.file.ID {
				found = sameFeatureFile(f, item.file)
				break
			}
		}
		if !found {
			return errors.New("云端源项目已变化，保留原目录结构")
		}
	}
	return nil
}

func (c *featureCleanup) finish(ctx context.Context, a *App, output *os.Root) error {
	defer a.cache.clear()
	for _, item := range c.files {
		if item.protected && item.output == "" {
			continue
		}
		if err := c.checkSource(ctx, a, item); err != nil {
			return err
		}
		if item.output != "" {
			in, err := output.Open(item.output)
			if err != nil {
				return errors.New("生成文件已被移除，保留源文件")
			}
			digest, err := hashFeatureFile(ctx, in)
			in.Close()
			if err != nil {
				return err
			}
			if digest != item.outputDigest {
				return errors.New("生成文件已变化，保留源文件")
			}
		}
		digest, err := c.sourceDigest(ctx, a, item)
		if err != nil {
			return err
		}
		if digest != item.digest {
			return errors.New("清理前源文件变化，保留源文件")
		}
		if err := c.checkSource(ctx, a, item); err != nil {
			return err
		}
		if c.local != nil {
			rel, _ := relative(item.file.ID)
			if c.source.Config["deleteMode"] == "permanent" {
				err = c.local.Remove(rel)
			} else {
				trash := path.Join(".aether-trash", id(), item.file.Name)
				if err = c.local.MkdirAll(path.Dir(trash), 0700); err == nil {
					err = c.local.Rename(rel, trash)
				}
			}
		} else {
			err = a.cloudFileAction(ctx, c.source, c.source, fileActionRequest{Action: "delete", Source: item.parent, IDs: []string{item.file.ID}})
		}
		if err != nil {
			return fmt.Errorf("源文件清理失败：%s：%w", item.file.Name, err)
		}
	}
	for _, item := range c.dirs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if c.local != nil {
			if err := c.checkSource(ctx, a, item); err != nil {
				return err
			}
			rel, _ := relative(item.file.ID)
			// Remove, never RemoveAll: new or unfinished files keep their ancestors.
			if err := c.local.Remove(rel); err != nil {
				in, openErr := c.local.Open(rel)
				if openErr != nil {
					return err
				}
				children, readErr := in.ReadDir(1)
				in.Close()
				if len(children) == 0 && (readErr == nil || errors.Is(readErr, io.EOF)) {
					return err
				}
			}
		} else {
			children, err := a.rawList(ctx, c.source, item.file.ID)
			if err != nil {
				return err
			}
			if len(children) != 0 {
				continue
			}
			// Remote DELETE has no portable atomic "only if empty" operation.
			// Keep empty remote directories rather than risk a late arrival.
		}
	}
	return nil
}
