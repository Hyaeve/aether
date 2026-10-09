package app

import (
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
	a.scrapeMu.Lock()
	defer a.scrapeMu.Unlock()
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
	count, pointers := 0, 0
	err = fs.WalkDir(library.FS(), ".", func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := a.ctx.Err(); err != nil {
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
		ext := strings.ToLower(path.Ext(p))
		if ext == ".strm" || ext == ".cas" || ext == ".ed2k" {
			pointers++
			return nil
		}
		if !strings.Contains(";.nfo;.jpg;.jpeg;.png;.webp;.srt;.ass;.vtt;.sub;", ";"+ext+";") {
			return errors.New("生成库含非生成文件，不能全量重置：" + p)
		}
		return nil
	})
	library.Close()
	if err != nil {
		return err
	}
	if count > 1 && pointers == 0 {
		return errors.New("目录中未发现生成指针文件，不能确认是任务生成库")
	}
	if err := root.RemoveAll(filepath.Base(name)); err != nil {
		return err
	}
	a.store.event("info", "tasks", "全量重置生成库："+t.Name+"："+name)
	return nil
}
