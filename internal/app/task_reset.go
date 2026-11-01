package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

func (a *App) taskOutputRoot(t Task, s Storage) (string, error) {
	if t.Kind != "strm" && t.Kind != "cas" && t.Kind != "ed2k" {
		return "", errors.New("此任务没有生成目录")
	}
	base, rel, err := a.outputLocation(t.Target)
	if err != nil {
		return "", err
	}
	if t.Kind == "strm" && t.Source != "" && t.Source != "/" && t.Source != rootOf(s) {
		name := t.SourceLabel
		if s.Type == "local" || s.Type == "webdav" || s.Type == "openlist" {
			name = path.Base(strings.TrimRight(t.Source, "/"))
		}
		if !safeName(name) {
			return "", errors.New("源目录名称无效")
		}
		rel = path.Join(rel, name)
	}
	return filepath.Abs(filepath.Join(base, filepath.FromSlash(rel)))
}

// Resolve existing ancestors, including aliases, before comparing deletion bounds.
func canonicalOutput(name string) (string, error) {
	resolved, err := filepath.EvalSymlinks(name)
	if err == nil {
		return filepath.Abs(resolved)
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(name)
	if parent == name {
		return "", err
	}
	base, err := canonicalOutput(parent)
	return filepath.Join(base, filepath.Base(name)), err
}

func (a *App) resetTaskOutput(t Task, s Storage) error {
	return a.resetTaskOutputContext(a.ctx, t, s, true)
}

func (a *App) resetTaskOutputContext(ctx context.Context, t Task, s Storage, apply bool) error {
	if !a.scrapeMu.TryLock() {
		return errors.New("生成库正在重置或刮削，请稍后重试")
	}
	defer a.scrapeMu.Unlock()
	return a.resetTaskOutputLocked(ctx, t, s, apply)
}

func (a *App) resetTaskOutputLocked(ctx context.Context, t Task, s Storage, apply bool) error {
	if a.scrapeProgress.Running {
		return errors.New("请先停止正在运行的STRM刮削")
	}
	name, err := a.taskOutputRoot(t, s)
	if err != nil {
		return err
	}
	if info, e := os.Lstat(name); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("生成库不能是符号链接")
	} else if e != nil && !os.IsNotExist(e) {
		return e
	}
	name, err = canonicalOutput(name)
	if err != nil {
		return err
	}
	defaultRoot, err := canonicalOutput(a.outputDir)
	if err != nil {
		return err
	}
	if filepath.Dir(name) == name || name == defaultRoot {
		return errors.New("不能重置公共生成根目录，请为任务设置独立生成目录")
	}
	st := a.store.snapshotWithLogLimit(0)
	for _, protected := range []string{a.store.dir, a.dataDir} {
		p, e := canonicalOutput(protected)
		if e != nil {
			return e
		}
		if protected == a.store.dir && pathOverlaps(name, p) {
			return errors.New("生成目录与配置目录重叠，不能重置")
		}
		insideData, dataErr := filepath.Rel(p, name)
		insideOutput, outputErr := filepath.Rel(defaultRoot, name)
		if protected == a.dataDir && dataErr == nil && insideData != ".." && !strings.HasPrefix(insideData, ".."+string(filepath.Separator)) && (outputErr != nil || insideOutput == ".." || strings.HasPrefix(insideOutput, ".."+string(filepath.Separator))) {
			return errors.New("不能重置内部数据目录")
		}
		rel, e := filepath.Rel(name, p)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("生成目录包含系统配置或数据，不能重置")
		}
	}
	if runtime.GOOS != "windows" {
		for _, system := range []string{"/etc", "/proc", "/sys", "/dev", "/usr", "/bin", "/sbin", "/boot"} {
			if pathOverlaps(name, system) {
				return errors.New("不能重置系统目录")
			}
		}
	}
	for _, mount := range st.Mounts {
		if pathOverlaps(name, mountTarget(mount)) {
			return errors.New("生成目录与挂载点重叠，不能重置")
		}
	}
	for _, other := range st.Tasks {
		pool, e := a.store.storage(other.StorageID)
		if e != nil {
			continue
		}
		if pool.Type == "local" {
			source, e := canonicalOutput(filepath.Join(pool.Config["root"], filepath.FromSlash(strings.TrimLeft(other.Source, "/"))))
			if e != nil {
				return e
			}
			if pathOverlaps(name, source) {
				return errors.New("生成目录与任务源目录重叠，不能重置")
			}
		}
		if other.ID == t.ID || other.Kind == "cache" {
			continue
		}
		output, e := a.taskOutputRoot(other, pool)
		if e != nil {
			return e
		}
		output, e = canonicalOutput(output)
		if e != nil {
			return e
		}
		if pathOverlaps(name, output) {
			return errors.New("生成目录与其他任务重叠，不能重置")
		}
	}
	root, err := os.OpenRoot(filepath.Dir(name))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	library, err := root.OpenRoot(filepath.Base(name))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer library.Close()
	count, pointers, files := 0, 0, 0
	candidates := []strmDeleteCandidate{}
	rootInfo, err := library.Stat(".")
	if err != nil {
		library.Close()
		return err
	}
	owned := false
	if t.Kind == "strm" && t.ID != "" {
		if _, statErr := library.Lstat(strmOwnerName(t.ID)); statErr == nil {
			h, err := a.beginSTRMSync(t, s, filepath.Dir(name), filepath.Base(name))
			if err != nil || h == nil {
				return errors.New("STRM生成库归属登记不可验证，不能重置")
			}
			token, err := strmReadOwner(library, strmOwnerName(t.ID))
			if err != nil || token != h.manifest.Token {
				return errors.New("STRM生成库归属标记不符，不能重置")
			}
			owned = true
		}
	}
	err = fs.WalkDir(library.FS(), ".", func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 500000 {
			return errors.New("生成库过大，请分目录重置")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("生成库含符号链接，不能全量重置")
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("生成库包含非普通文件，不能全量重置")
		}
		if t.Kind == "strm" && t.ID != "" && p == strmOwnerName(t.ID) {
			h, err := a.beginSTRMSync(t, s, filepath.Dir(name), filepath.Base(name))
			if err != nil || h == nil {
				return errors.New("STRM生成库归属登记不可验证，不能重置")
			}
			token, err := strmReadOwner(library, p)
			if err != nil || token != h.manifest.Token {
				return errors.New("STRM生成库归属标记不符，不能重置")
			}
			return nil
		}
		ext := strings.ToLower(path.Ext(p))
		if path.Base(p) == quarkLibraryMarker || strings.HasPrefix(path.Base(p), ".aether-strm-") && strings.HasSuffix(p, ".owner") {
			return errors.New("生成库含其他归属标记，不能全量重置")
		}
		files++
		if t.Kind == "strm" && apply {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			digest := ""
			if info.Size() <= 64<<20 {
				digest, info, err = strmOutputDigest(library, p)
				if err != nil {
					return err
				}
			}
			candidates = append(candidates, strmDeleteCandidate{p, digest, info})
		}
		if owned {
			return nil
		}
		if ext == ".strm" || ext == ".cas" || ext == ".ed2k" {
			pointers++
			return nil
		}
		if !strings.Contains(";.nfo;.jpg;.jpeg;.png;.webp;.srt;.ass;.vtt;.sub;", ";"+ext+";") {
			return errors.New("生成库含非生成文件，不能全量重置：" + p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if count > 1 && pointers == 0 && !owned {
		return errors.New("目录中未发现生成指针文件，不能确认是任务生成库")
	}
	if !apply {
		return nil
	}
	if t.Kind == "strm" {
		stamp := strmTaskConfig(t)
		validate := func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			current, err := a.store.storage(s.ID)
			if err != nil || storageRevision(current) != storageRevision(s) {
				return errors.New("源存储配置已改变，未执行重置")
			}
			found := false
			for _, current := range a.store.snapshotWithLogLimit(0).Tasks {
				if current.ID == t.ID && strmTaskConfig(current) == stamp {
					found = true
				}
			}
			if !found {
				return errors.New("任务配置已改变，未执行重置")
			}
			live, err := root.OpenRoot(filepath.Base(name))
			if err != nil {
				return err
			}
			defer live.Close()
			info, err := live.Stat(".")
			if err != nil || !os.SameFile(rootInfo, info) {
				return errors.New("生成库身份已改变，未执行重置")
			}
			if owned {
				left, e1 := strmReadOwner(library, strmOwnerName(t.ID))
				right, e2 := strmReadOwner(live, strmOwnerName(t.ID))
				if e1 != nil || e2 != nil || left != right {
					return errors.New("生成库归属标记已改变，未执行重置")
				}
			}
			for _, item := range candidates {
				// Confirmed resets can include large private files; keep the prior
				// allowance without reading their entire contents just to count them.
				info, err := live.Lstat(item.name)
				digest := ""
				if err == nil && item.digest != "" {
					digest, info, err = strmOutputDigest(live, item.name)
				}
				if err != nil || digest != item.digest || !sameStrmFile(item.info, info) {
					return errors.New("生成文件在检查期间已改变，未执行重置")
				}
			}
			actual := 0
			err = fs.WalkDir(live.FS(), ".", func(p string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() && !entry.Type().IsRegular() {
					return errors.New("生成库内容已改变，未执行重置")
				}
				if !entry.IsDir() && p != strmOwnerName(t.ID) {
					actual++
				}
				return ctx.Err()
			})
			if err != nil || actual != files {
				return errors.New("生成库内容数量已改变，未执行重置")
			}
			return a.resetTaskOutputLocked(ctx, t, s, false)
		}
		if err := a.authorizeSTRMDeletion(ctx, t, s, files, validate); err != nil {
			return err
		}
	}
	library.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.RemoveAll(filepath.Base(name)); err != nil {
		return err
	}
	a.store.event("info", "tasks", "全量重置生成库："+t.Name+"："+name)
	return nil
}
