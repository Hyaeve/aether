package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	strmReplaceMaxEntries = 10000
	strmReplaceMaxDepth   = 32
	strmReplaceMaxBytes   = 1 << 20
	strmReplaceHistory    = 16
)

type strmReplaceRequest struct {
	Directory string `json:"directory"`
	Find      string `json:"find"`
	Replace   string `json:"replace"`
	Confirmed bool   `json:"confirmed"`
}

type strmReplaceTask struct {
	ID         string     `json:"id"`
	Directory  string     `json:"directory"`
	Status     string     `json:"status"`
	Scanned    int        `json:"scanned"`
	Processed  int        `json:"processed"`
	Changed    int        `json:"changed"`
	Skipped    int        `json:"skipped"`
	Error      string     `json:"error"`
	Time       time.Time  `json:"time"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type strmReplaceManager struct {
	mu    sync.Mutex
	tasks []strmReplaceTask
}

var strmReplaceManagers sync.Map // *App -> bounded, memory-only task history

func (a *App) strmReplacements() *strmReplaceManager {
	m, _ := strmReplaceManagers.LoadOrStore(a, &strmReplaceManager{})
	return m.(*strmReplaceManager)
}

// RegisterStrmReplace installs only this plugin's administrator-protected routes.
func (a *App) RegisterStrmReplace(mux *http.ServeMux) {
	mux.Handle("GET /api/strm-replace", a.protected(http.HandlerFunc(a.strmReplace)))
	mux.Handle("POST /api/strm-replace", a.protected(http.HandlerFunc(a.strmReplace)))
}

func (a *App) strmReplace(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	m := a.strmReplacements()
	if r.Method == http.MethodGet {
		m.mu.Lock()
		defer m.mu.Unlock()
		if taskID := r.URL.Query().Get("id"); taskID != "" {
			for _, task := range m.tasks {
				if task.ID == taskID {
					jsonResponse(w, 200, task)
					return
				}
			}
			fail(w, 404, errors.New("任务不存在或已过期"))
			return
		}
		tasks := append([]strmReplaceTask{}, m.tasks...)
		jsonResponse(w, 200, map[string]any{"tasks": tasks})
		return
	}
	var in strmReplaceRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		fail(w, 400, errors.New("请求数据格式不正确"))
		return
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, errors.New("请求只能包含一个 JSON 对象"))
		return
	}
	if !in.Confirmed || in.Find == "" || len(in.Find) > 16384 || len(in.Replace) > 16384 || !filepath.IsAbs(in.Directory) || strings.ContainsRune(in.Directory, 0) {
		fail(w, 400, errors.New("请选择绝对目录、填写查找内容并二次确认（字段上限 16 KiB）"))
		return
	}
	in.Directory = filepath.Clean(in.Directory)
	m.mu.Lock()
	if len(m.tasks) > 0 && m.tasks[0].Status == "running" {
		m.mu.Unlock()
		fail(w, 409, errors.New("已有 STRM 替换任务正在执行"))
		return
	}
	now := time.Now().UTC()
	task := strmReplaceTask{ID: id(), Directory: in.Directory, Status: "running", Time: now, UpdatedAt: now}
	m.tasks = append([]strmReplaceTask{task}, m.tasks...)
	if len(m.tasks) > strmReplaceHistory {
		m.tasks = m.tasks[:strmReplaceHistory]
	}
	m.mu.Unlock()
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		m.run(ctx, in, task)
	}()
	jsonResponse(w, http.StatusAccepted, task)
}

func (m *strmReplaceManager) publish(task strmReplaceTask) {
	task.UpdatedAt = time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].ID == task.ID {
			m.tasks[i] = task
			return
		}
	}
}

func (m *strmReplaceManager) run(parent context.Context, in strmReplaceRequest, task strmReplaceTask) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	err := scanStrmReplace(ctx, in, &task, func() { m.publish(task) })
	task.Status = "completed"
	if err != nil {
		task.Status = "failed"
		task.Error = err.Error()
	}
	now := time.Now().UTC()
	task.FinishedAt = &now
	m.publish(task)
}

// Open each directory relative to its pinned parent; reject symlink components.
func openStrmReplaceRoot(directory string) (*os.Root, error) {
	volume := filepath.VolumeName(directory)
	root, err := os.OpenRoot(volume + string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(directory, volume), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		next, e := strmReplaceSubroot(root, part)
		root.Close()
		if e != nil {
			return nil, e
		}
		root = next
	}
	return root, nil
}

func strmReplaceSubroot(root *os.Root, name string) (*os.Root, error) {
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("目录不是普通目录或包含符号链接")
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	after, err := child.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		child.Close()
		return nil, errors.New("扫描期间目录已改变")
	}
	return child, nil
}

func scanStrmReplace(ctx context.Context, in strmReplaceRequest, task *strmReplaceTask, progress func()) error {
	root, err := openStrmReplaceRoot(in.Directory)
	if err != nil {
		return err
	}
	defer root.Close()
	var walk func(*os.Root, int) error
	walk = func(root *os.Root, depth int) error {
		dir, err := root.Open(".")
		if err != nil {
			return err
		}
		defer dir.Close()
		for {
			entries, readErr := dir.ReadDir(128)
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				if task.Scanned >= strmReplaceMaxEntries {
					return errors.New("扫描数量超过 10000 项，任务停止；已完成的替换保留")
				}
				task.Scanned++
				info, err := root.Lstat(entry.Name())
				if err != nil {
					return err
				}
				if info.Mode()&os.ModeSymlink != 0 {
					task.Skipped++
					progress()
					continue
				}
				if info.IsDir() {
					if depth >= strmReplaceMaxDepth {
						return errors.New("目录深度超过 32 层，任务停止")
					}
					child, err := strmReplaceSubroot(root, entry.Name())
					if err != nil {
						return err
					}
					err = walk(child, depth+1)
					child.Close()
					if err != nil {
						return err
					}
				} else if !info.Mode().IsRegular() {
					task.Skipped++
				} else if strings.EqualFold(filepath.Ext(entry.Name()), ".strm") {
					if info.Size() > strmReplaceMaxBytes {
						task.Skipped++
						progress()
						continue
					}
					changed, err := replaceStrmFile(root, entry.Name(), in.Find, in.Replace, nil)
					if err != nil {
						return fmt.Errorf("%s: %w", entry.Name(), err)
					}
					task.Processed++
					if changed {
						task.Changed++
					}
				}
				progress()
			}
			if readErr == io.EOF {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	return walk(root, 0)
}

func readStrmFile(root *os.Root, name string) ([]byte, os.FileInfo, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > strmReplaceMaxBytes {
		return nil, nil, errors.New("文件不是普通文件或超过 1 MiB")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !sameStrmFile(info, opened) {
		return nil, nil, errors.New("文件已改变")
	}
	data, err := io.ReadAll(io.LimitReader(f, strmReplaceMaxBytes+1))
	if err != nil {
		return nil, nil, err
	}
	after, err := f.Stat()
	if err != nil || !sameStrmFile(info, after) || int64(len(data)) != info.Size() {
		return nil, nil, errors.New("读取期间文件已改变或超过大小限制")
	}
	return data, info, nil
}

func sameStrmFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// beforeCommit is an internal test seam. No HTTP caller can supply it.
// Revalidation detects concurrent edits, but portable rename is not a filesystem CAS.
func replaceStrmFile(root *os.Root, name, find, replacement string, beforeCommit func()) (bool, error) {
	if find == "" {
		return false, errors.New("查找内容不能为空")
	}
	data, info, err := readStrmFile(root, name)
	if err != nil {
		return false, err
	}
	count := bytes.Count(data, []byte(find))
	if count == 0 || find == replacement {
		return false, nil
	}
	outputSize := int64(len(data)) + int64(count)*(int64(len(replacement))-int64(len(find)))
	if outputSize > strmReplaceMaxBytes {
		return false, errors.New("替换后内容超过 1 MiB")
	}
	output := bytes.ReplaceAll(data, []byte(find), []byte(replacement))
	temp := ".aether-strm-replace-" + id()
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return false, err
	}
	defer root.Remove(temp)
	_, err = f.Write(output)
	if err == nil {
		err = f.Chmod(info.Mode().Perm())
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	if beforeCommit != nil {
		beforeCommit()
	}
	current, currentInfo, err := readStrmFile(root, name)
	if err != nil {
		return false, err
	}
	if !sameStrmFile(info, currentInfo) || !bytes.Equal(data, current) {
		return false, errors.New("文件已被外部修改，未覆盖")
	}
	finalInfo, err := root.Lstat(name)
	if err != nil || !sameStrmFile(info, finalInfo) {
		return false, errors.New("发布前文件已改变，未覆盖")
	}
	if err := root.Rename(temp, name); err != nil {
		return false, err
	}
	return true, nil
}
