package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type fileActionRequest struct {
	Source        string   `json:"source"`
	StorageID     string   `json:"storageId"`
	Action        string   `json:"action"`
	IDs           []string `json:"ids"`
	Name          string   `json:"name"`
	TargetStorage string   `json:"targetStorage"`
	Target        string   `json:"target"`
}

func (a *App) fileAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var req fileActionRequest
	if !decode(w, r, &req) {
		return
	}
	s, err := a.store.storage(req.StorageID)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if req.Action == "mkdir" {
		a.runMu.Lock()
		defer a.runMu.Unlock()
		if len(a.running) != 0 {
			fail(w, 409, errors.New("有任务正在执行，请等待完成后操作文件"))
			return
		}
		if err := a.createDirectory(r.Context(), s, req.Source, req.Name); err != nil {
			fail(w, 400, err)
			return
		}
		a.cache.clear()
		a.store.event("info", "files", "创建目录："+req.Name)
		jsonResponse(w, 200, map[string]int{"processed": 1})
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > 200 {
		fail(w, 400, errors.New("请选择1–200个项目"))
		return
	}
	if req.Action != "rename" && req.Action != "move" && req.Action != "copy" && req.Action != "delete" {
		fail(w, 400, errors.New("不支持的文件操作"))
		return
	}
	if req.Action == "rename" && (len(req.IDs) != 1 || !safeName(req.Name)) {
		fail(w, 400, errors.New("重命名需要一个项目和有效名称"))
		return
	}
	target := s
	if req.Action == "copy" || req.Action == "move" {
		target, err = a.store.storage(req.TargetStorage)
		if err != nil || target.Type != s.Type {
			fail(w, 400, errors.New("只能在同类型的启用存储池中操作"))
			return
		}
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if len(a.running) != 0 {
		fail(w, 409, errors.New("有任务正在执行，请等待完成后操作文件"))
		return
	}
	if s.Type != "local" {
		var progress *transferProgress
		if req.Action == "copy" || req.Action == "move" {
			progress = a.beginTransfer(r.Context(), "copy", s, fmt.Sprintf("%s · %d 项", req.Action, len(req.IDs)), 0)
		}
		err := a.cloudFileAction(r.Context(), s, target, req)
		progress.finish(err)
		a.cache.clear()
		if err != nil {
			fail(w, 400, err)
			return
		}
		jsonResponse(w, 200, map[string]int{"processed": len(req.IDs)})
		return
	}
	src, err := os.OpenRoot(s.Config["root"])
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer src.Close()
	dst, err := os.OpenRoot(target.Config["root"])
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer dst.Close()
	// Validate the whole selection before modifying any entry.
	names := make([]string, 0, len(req.IDs))
	dests := make([]string, 0, len(req.IDs))
	for _, raw := range req.IDs {
		name, err := relative(raw)
		if err != nil || name == "." || name == ".aether-trash" || strings.HasPrefix(name, ".aether-trash/") {
			fail(w, 400, errors.New("不能操作根目录或回收站"))
			return
		}
		info, err := src.Lstat(name)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			fail(w, 400, errors.New("项目不存在或为符号链接"))
			return
		}
		for _, previous := range names {
			if name == previous || strings.HasPrefix(name, previous+"/") || strings.HasPrefix(previous, name+"/") {
				fail(w, 400, errors.New("不能同时选择父目录与其子项目"))
				return
			}
		}
		out := ""
		switch req.Action {
		case "rename":
			out = path.Join(path.Dir(name), req.Name)
		case "copy", "move":
			dir, err := relative(req.Target)
			if err != nil {
				fail(w, 400, err)
				return
			}
			stat, err := dst.Stat(dir)
			if err != nil || !stat.IsDir() {
				fail(w, 400, errors.New("目标目录不存在"))
				return
			}
			out = path.Join(dir, path.Base(name))
		case "delete":
			if s.Config["deleteMode"] != "permanent" {
				out = path.Join(".aether-trash", id(), path.Base(name))
			}
		}
		if out != "" {
			from, _ := filepath.Abs(filepath.Join(s.Config["root"], filepath.FromSlash(name)))
			to, _ := filepath.Abs(filepath.Join(target.Config["root"], filepath.FromSlash(out)))
			// Resolve configured roots too, preventing aliases from copying a directory into itself.
			from, err = filepath.EvalSymlinks(from)
			if err != nil {
				fail(w, 400, err)
				return
			}
			parent := filepath.Dir(to)
			if req.Action != "delete" {
				parent, err = filepath.EvalSymlinks(parent)
				if err != nil {
					fail(w, 400, err)
					return
				}
				to = filepath.Join(parent, filepath.Base(to))
				if strings.EqualFold(from, to) || (info.IsDir() && strings.HasPrefix(strings.ToLower(to), strings.ToLower(from)+string(os.PathSeparator))) {
					fail(w, 400, errors.New("目标不能是自身或子目录"))
					return
				}
			}
			if _, err := dst.Lstat(out); !os.IsNotExist(err) {
				fail(w, 409, errors.New("目标名称已存在或不可访问"))
				return
			}
		}
		names = append(names, name)
		dests = append(dests, out)
	}
	done := 0
	for i, name := range names {
		if err = r.Context().Err(); err != nil {
			break
		}
		out := dests[i]
		switch req.Action {
		case "rename":
			err = src.Rename(name, out)
		case "delete":
			if out == "" {
				err = src.RemoveAll(name)
			} else if err = src.MkdirAll(path.Dir(out), 0700); err == nil {
				err = src.Rename(name, out)
			}
		case "copy", "move":
			progress := a.beginTransfer(r.Context(), "copy", s, path.Base(name), 0)
			if req.Action == "move" && s.ID == target.ID {
				err = src.Rename(name, out)
			} else {
				err = copyLocalTree(context.WithValue(r.Context(), copyProgressKey{}, progress), src, dst, name, out, 0)
				if err == nil && req.Action == "move" {
					err = src.RemoveAll(name)
				}
			}
			progress.finish(err)
		}
		if err != nil {
			break
		}
		done++
	}
	a.cache.clear()
	if err != nil {
		a.store.event("error", "files", fmt.Sprintf("%s 完成%d项后失败", req.Action, done))
		fail(w, 400, fmt.Errorf("已完成 %d 项，后续操作失败：%w", done, err))
		return
	}
	a.store.event("info", "files", fmt.Sprintf("%s 完成%d项", req.Action, done))
	jsonResponse(w, 200, map[string]int{"processed": done})
}

type copyProgressKey struct{}

func copyLocalTree(ctx context.Context, src, dst *os.Root, from, to string, depth int) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 128 {
		return errors.New("目录层级过深")
	}
	info, err := src.Lstat(from)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("不复制符号链接")
	}
	if info.IsDir() {
		if err := dst.Mkdir(to, 0755); err != nil {
			return err
		}
		defer func() {
			if err != nil {
				_ = dst.RemoveAll(to)
			}
		}()
		dir, e := src.Open(from)
		if e != nil {
			return e
		}
		entries, e := dir.ReadDir(-1)
		dir.Close()
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if err = copyLocalTree(ctx, src, dst, path.Join(from, entry.Name()), path.Join(to, entry.Name()), depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return errors.New("仅支持普通文件和目录")
	}
	in, err := src.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := dst.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() {
		out.Close()
		if err != nil {
			dst.Remove(to)
		}
	}()
	buf := make([]byte, 1<<20)
	var copied int64
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		n, e := in.Read(buf)
		if n > 0 {
			if _, err = out.Write(buf[:n]); err != nil {
				return err
			}
			copied += int64(n)
			if progress, ok := ctx.Value(copyProgressKey{}).(*transferProgress); ok {
				progress.add(n)
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
	}
	after, statErr := in.Stat()
	if statErr != nil {
		return statErr
	}
	if copied != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return errors.New("源文件在复制期间变化，已中止操作并保留源文件")
	}
	if err = out.Sync(); err != nil {
		return err
	}
	return out.Close()
}
