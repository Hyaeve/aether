package app

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type casExportRequest struct {
	StorageID  string   `json:"storageId"`
	Source     string   `json:"source"`
	IDs        []string `json:"ids"`
	Extensions string   `json:"extensions"`
	Directory  string   `json:"directory"`
	Confirm    bool     `json:"confirm"`
}
type casExportItem struct {
	File File
	Path string
}

func (a *App) casExportDirectory(value string) error {
	if value == "" {
		return nil
	}
	if !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return errors.New("导出目录须为容器内绝对路径")
	}
	info, err := os.Lstat(value)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("导出目录不存在或不是普通目录")
	}
	resolved, err := filepath.EvalSymlinks(value)
	if err != nil || resolved != value {
		return errors.New("导出目录不能包含符号链接")
	}
	for _, protected := range []string{a.store.dir, filepath.Join(a.dataDir, "runtime"), filepath.Join(a.dataDir, "log"), filepath.Join(a.dataDir, "cache")} {
		absolute, err := filepath.Abs(protected)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(absolute, value)
		if err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return errors.New("不能导出至系统配置或运行目录")
		}
	}
	return nil
}
func (a *App) casExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		jsonResponse(w, 200, map[string]string{"directory": a.store.snapshotWithLogLimit(0).CASExportDirectory})
		return
	}
	if r.Method == http.MethodPut {
		var v struct {
			Directory string `json:"directory"`
		}
		if !decode(w, r, &v) {
			return
		}
		if err := a.casExportDirectory(v.Directory); err != nil {
			fail(w, 400, err)
			return
		}
		if err := a.store.update(func(st *State) error { st.CASExportDirectory = v.Directory; return nil }); err != nil {
			fail(w, 400, err)
			return
		}
		jsonResponse(w, 200, v)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, PUT, POST")
		w.WriteHeader(405)
		return
	}
	var v casExportRequest
	if !decode(w, r, &v) {
		return
	}
	if !v.Confirm {
		fail(w, 400, errors.New("请二次确认导出CAS"))
		return
	}
	s, err := a.store.storage(v.StorageID)
	if err != nil || !s.Enabled || s.Type != "local" && s.Type != "mobile" {
		fail(w, 400, errors.New("CAS导出仅支持本地或移动存储"))
		return
	}
	if err := storageAuthBlocked(s, time.Now()); err != nil {
		fail(w, 409, errors.New("源存储健康异常，暂不可导出"))
		return
	}
	if err := a.casExportDirectory(v.Directory); err != nil {
		fail(w, 400, err)
		return
	}
	ext, err := normalizeExtensions(v.Extensions)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if len(v.IDs) > 10000 || len(v.Source) > 4096 {
		fail(w, 400, errors.New("导出范围过大"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	lease, err := a.acquireTransferResource(ctx, "scan")
	if err != nil {
		fail(w, 408, err)
		return
	}
	items, single, err := a.casExportFiles(ctx, s, v.Source, v.IDs, ext)
	lease()
	if err != nil {
		fail(w, 400, err)
		return
	}
	if len(items) == 0 {
		fail(w, 400, errors.New("所选范围没有符合扩展名的媒体文件"))
		return
	}
	casExportProgress(ctx, 0, len(items))
	release, err := a.controlPreprocess(ctx)
	if err != nil {
		fail(w, 408, err)
		return
	}
	defer release()
	tempDir := filepath.Join(a.transferTempDirectory(), "cas-export")
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		fail(w, 500, errors.New("无法创建导出暂存目录"))
		return
	}
	temp, err := os.CreateTemp(tempDir, "aether-cas-export-*")
	if err != nil {
		fail(w, 500, errors.New("无法创建导出暂存"))
		return
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	name := "CAS-" + time.Now().Format("20060102-150405") + "-" + id()[:8] + ".zip"
	var archive *zip.Writer
	if single {
		name = items[0].File.Name + ".cas"
	} else {
		archive = zip.NewWriter(temp)
	}
	paths := map[string]bool{}
	for index, item := range items {
		if err := ctx.Err(); err != nil {
			fail(w, 408, errors.New("导出已取消或超时"))
			return
		}
		current, e := a.store.storage(s.ID)
		if e != nil || !current.Enabled || rapid115Stamp(current.Config) != rapid115Stamp(s.Config) {
			fail(w, 409, errors.New("源存储配置已变化"))
			return
		}
		info, e := a.generateCASInfo(ctx, s, item.File)
		if e != nil {
			a.casFailureLog(s, item.File.Name, "导出特征获取", e)
			fail(w, 400, errors.New("CAS特征获取失败；未输出不完整文件，请查看系统日志"))
			return
		}
		raw, e := json.Marshal(info)
		if e != nil {
			fail(w, 500, e)
			return
		}
		output := io.Writer(temp)
		if archive != nil {
			member := item.Path + ".cas"
			key := strings.ToLower(member)
			if paths[key] {
				fail(w, 400, errors.New("导出路径存在重名冲突"))
				return
			}
			paths[key] = true
			output, e = archive.Create(member)
			if e != nil {
				fail(w, 500, e)
				return
			}
		}
		if _, e = output.Write(raw); e != nil {
			fail(w, 500, errors.New("CAS导出写入失败"))
			return
		}
		if stat, e := temp.Stat(); e != nil || stat.Size() > 64<<20 {
			fail(w, 400, errors.New("导出包超过64MiB"))
			return
		}
		casExportProgress(ctx, index+1, len(items))
	}
	if archive != nil {
		if err = archive.Close(); err != nil {
			fail(w, 500, errors.New("导出包写入失败"))
			return
		}
	}
	if err = temp.Sync(); err != nil {
		fail(w, 500, err)
		return
	}
	if _, err = temp.Seek(0, io.SeekStart); err != nil {
		fail(w, 500, err)
		return
	}
	current, e := a.store.storage(s.ID)
	if e != nil || !current.Enabled || rapid115Stamp(current.Config) != rapid115Stamp(s.Config) {
		fail(w, 409, errors.New("源存储配置已变化，未发布导出"))
		return
	}
	if v.Directory != "" {
		if err := a.casExportDirectory(v.Directory); err != nil {
			fail(w, 400, err)
			return
		}
		root, e := os.OpenRoot(v.Directory)
		if e != nil {
			fail(w, 400, e)
			return
		}
		defer root.Close()
		stage := ".aether-cas-export-" + id()
		defer root.Remove(stage)
		out, e := root.OpenFile(stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if e != nil {
			fail(w, 400, errors.New("导出目标已存在或不可写，不覆盖"))
			return
		}
		_, e = io.Copy(out, &cancelReader{ctx, temp})
		if e == nil {
			e = out.Sync()
		}
		closeErr := out.Close()
		if e != nil || closeErr != nil {
			fail(w, 500, errors.New("导出未完成"))
			return
		}
		if e = root.Link(stage, name); e != nil {
			fail(w, 400, errors.New("无法发布导出文件，目标已存在或不可写；不覆盖"))
			return
		}
		jsonResponse(w, 200, map[string]any{"path": filepath.Join(v.Directory, name), "count": len(items)})
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		_, _ = io.Copy(w, &cancelReader{ctx, temp})
	}
	a.store.event("info", "files", fmt.Sprintf("CAS导出完成：存储池 %q，%d个文件", s.Name, len(items)))
}

func (a *App) casExportFiles(ctx context.Context, s Storage, source string, ids []string, ext string) ([]casExportItem, bool, error) {
	var localRoot *os.Root
	if s.Type == "local" {
		var err error
		localRoot, err = os.OpenRoot(s.Config["root"])
		if err != nil {
			return nil, false, err
		}
		defer localRoot.Close()
		name, err := relative(source)
		if err != nil {
			return nil, false, err
		}
		info, err := localRoot.Lstat(name)
		if err != nil || !info.IsDir() {
			return nil, false, errors.New("导出源目录不是普通目录")
		}
	}
	selected := map[string]bool{}
	for _, key := range ids {
		if key == "" || selected[key] {
			return nil, false, errors.New("导出选择无效")
		}
		selected[key] = true
	}
	found := map[string]bool{}
	visited := map[string]bool{}
	items := []casExportItem{}
	count := 0
	selectedFile := false
	var walk func(string, string, int, bool) error
	walk = func(dir, prefix string, depth int, included bool) error {
		if depth > 64 || visited[dir] {
			return errors.New("目录过深或出现循环")
		}
		visited[dir] = true
		files, err := a.rawList(ctx, s, dir)
		if err != nil {
			return err
		}
		for _, f := range files {
			count++
			if count > 10000 {
				return errors.New("导出扫描最多10000项")
			}
			if !safeName(f.Name) {
				return errors.New("导出文件名无效")
			}
			chosen := selected[f.ID]
			if chosen {
				found[f.ID] = true
				if !f.IsDir {
					selectedFile = true
				}
			}
			include := included || chosen || len(ids) == 0
			outputPath := path.Join(prefix, f.Name)
			if f.IsDir {
				if err := walk(f.ID, outputPath, depth+1, include); err != nil {
					return err
				}
				continue
			}
			if !include || a.transferIgnored(f.Name) || !taskMedia(Task{MediaExtensions: ext}, f.Name) {
				continue
			}
			if localRoot != nil {
				name, e := relative(f.ID)
				if e != nil {
					return e
				}
				info, e := localRoot.Lstat(name)
				if e != nil {
					return e
				}
				if !info.Mode().IsRegular() {
					continue
				}
			}
			items = append(items, casExportItem{f, outputPath})
		}
		return ctx.Err()
	}
	if err := walk(source, "", 0, false); err != nil {
		return nil, false, err
	}
	if len(found) != len(selected) {
		return nil, false, errors.New("所选项目已移动、删除或不在当前目录范围")
	}
	// Directory and cloud listings need not be ordered; ZIP entries must be.
	for i := range items {
		items[i].Path = filepath.ToSlash(items[i].Path)
	}
	slices.SortFunc(items, func(a, b casExportItem) int {
		return strings.Compare(a.Path+".cas", b.Path+".cas")
	})
	return items, len(ids) == 1 && selectedFile && len(items) == 1, nil
}
