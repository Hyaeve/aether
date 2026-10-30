package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

type taskFileNode struct {
	storage Storage
	id      string
	child   string
}

// Only fresh directory listings establish opaque-ID ancestry. Presentation
// breadcrumbs and the file-list cache are not authority for write protection.
type taskScopeResolver struct {
	app   *App
	ctx   context.Context
	lists map[string][]File
}

func (r *taskScopeResolver) list(s Storage, dir string) ([]File, error) {
	key := storageRevision(s) + "\x00" + dir
	if files, ok := r.lists[key]; ok {
		return files, nil
	}
	files, err := r.app.rawList(r.ctx, s, dir)
	if err == nil {
		r.lists[key] = files
	}
	return files, err
}

func taskLocalPath(s Storage, dir string) (string, error) {
	absolute, err := backupLocalPath(s, dir)
	if err != nil {
		return "", err
	}
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(absolute)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			if runtime.GOOS == "windows" {
				resolved = strings.ToLower(resolved)
			}
			return filepath.ToSlash(resolved), nil
		}
		if !os.IsNotExist(err) || filepath.Dir(absolute) == absolute {
			return "", err
		}
		suffix = append(suffix, filepath.Base(absolute))
		absolute = filepath.Dir(absolute)
	}
}

func (r *taskScopeResolver) node(n taskFileNode) (string, error) {
	s, dir := n.storage, backupDir(n.storage, n.id)
	if n.child != "" {
		for _, part := range strings.Split(n.child, "/") {
			if !safeName(part) {
				return "", errors.New("文件路径无效")
			}
		}
	}
	if backupPathBased(s) {
		if n.child != "" {
			dir = path.Join(dir, n.child)
		}
		if s.Type == "local" {
			return taskLocalPath(s, dir)
		}
		return path.Clean("/" + backupScopePath(s, dir)), nil
	}
	// Start at the real account root so aliases with different configured roots
	// cannot bypass the lock. This read-only search is bounded and fails closed.
	account := s
	account.Config = maps.Clone(s.Config)
	if account.Config == nil {
		account.Config = map[string]string{}
	}
	switch s.Type {
	case "115", "quark":
		account.Config["root"] = "0"
	case "tianyi":
		account.Config["root"] = "-11"
	case "mobile":
		account.Config["root"] = "/"
	default:
		return "", errors.New("无法核验存储目录关系")
	}
	type branch struct {
		id, trail string
		depth     int
	}
	root := rootOf(account)
	queue := []branch{{id: root, trail: "/"}}
	seen := map[string]bool{}
	found := ""
	entries := 0
	for i := 0; i < len(queue); i++ {
		current := queue[i]
		if current.id == dir {
			found = current.trail
			break
		}
		if seen[current.id] {
			continue
		}
		seen[current.id] = true
		if current.depth >= 128 || len(seen) > 10000 {
			return "", errors.New("目录关系核验超过安全范围")
		}
		files, err := r.list(account, current.id)
		if err != nil {
			return "", err
		}
		entries += len(files)
		if entries > 100000 {
			return "", errors.New("目录关系核验超过安全范围")
		}
		for _, f := range files {
			if !safeName(f.ID) || strings.ContainsAny(f.ID, "\x00\r\n") {
				return "", errors.New("云盘返回无效的文件标识")
			}
			trail := path.Join(current.trail, f.ID)
			if f.ID == dir {
				found = trail
				break
			}
			if f.IsDir {
				queue = append(queue, branch{f.ID, trail, current.depth + 1})
			}
		}
		if found != "" {
			break
		}
	}
	if found == "" {
		return "", errors.New("目录已变化，无法核验任务保护范围")
	}
	if n.child != "" {
		for _, part := range strings.Split(n.child, "/") {
			files, err := r.list(account, dir)
			if err != nil {
				return "", err
			}
			next := ""
			for _, f := range files {
				if f.Name == part {
					if !safeName(f.ID) || strings.ContainsAny(f.ID, "\x00\r\n") || next != "" && next != f.ID {
						return "", errors.New("云盘同名项目或文件标识异常，无法核验任务保护范围")
					}
					next = f.ID
				}
			}
			if next == "" {
				// No existing child can contain the active source.
				return path.Join(found, "new:"+n.child), nil
			}
			found, dir = path.Join(found, next), next
		}
	}
	return found, nil
}

func taskPathContains(parent, child string) bool {
	return parent == child || strings.HasPrefix(child, strings.TrimRight(parent, "/")+"/")
}

func (r *taskScopeResolver) overlap(left, right taskFileNode) (bool, error) {
	if left.storage.Type != "local" || right.storage.Type != "local" {
		if !backupSameTree(left.storage, right.storage) {
			return false, nil
		}
	}
	l, err := r.node(left)
	if err != nil {
		return false, err
	}
	rp, err := r.node(right)
	return taskPathContains(l, rp) || taskPathContains(rp, l), err
}

func taskDescription(task Task) string {
	kind := map[string]string{"strm": "STRM ", "cas": "CAS ", "ed2k": "ED2K ", "cache": "目录缓存"}[task.Kind]
	name := strings.TrimSpace(task.Name)
	if name == "" {
		name = task.ID
	}
	if name == "" {
		name = "未命名"
	}
	return fmt.Sprintf("%s任务「%s」", kind, name)
}

// Keep reporting deterministic when multiple active tasks could overlap.
func (a *App) runningTasks() []Task {
	tasks := map[string]Task{}
	for _, task := range a.store.snapshotWithLogLimit(0).Tasks {
		tasks[task.ID] = task
	}
	ids := make([]string, 0, len(a.running))
	for id := range a.running {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	active := make([]Task, 0, len(ids))
	for _, id := range ids {
		task, ok := tasks[id]
		if !ok {
			task.ID = id
		}
		active = append(active, task)
	}
	return active
}

// Callers hold runMu through their mutation, preventing a new task from
// registering between scope validation and the filesystem/cloud operation.
func (a *App) checkTaskFileNodes(ctx context.Context, nodes []taskFileNode) error {
	if len(a.running) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	resolver := taskScopeResolver{a, ctx, map[string][]File{}}
	for _, t := range a.runningTasks() {
		s, err := a.store.storage(t.StorageID)
		if err != nil {
			return fmt.Errorf("无法核验运行中的%s的存储目录，未执行文件操作", taskDescription(t))
		}
		protected, err := a.taskRunNodes(t, s)
		if err != nil {
			return fmt.Errorf("无法核验运行中的%s的保护范围：%w", taskDescription(t), err)
		}
		for _, scope := range protected {
			for _, n := range nodes {
				overlap, err := resolver.overlap(scope, n)
				if err != nil {
					return fmt.Errorf("无法核验运行中的%s的目录关系：%w", taskDescription(t), err)
				}
				if overlap {
					return fmt.Errorf("%s正在使用此源目录或生成目录，暂不能操作该目录、其内容或影响它的上级目录", taskDescription(t))
				}
			}
		}
	}
	return nil
}

func (a *App) checkTaskFileAction(ctx context.Context, s, target Storage, req fileActionRequest) error {
	if len(a.running) == 0 {
		return nil
	}
	nodes, err := a.taskFileActionNodes(ctx, s, target, req)
	if err != nil {
		return err
	}
	return a.checkTaskFileNodes(ctx, nodes)
}

func (a *App) taskFileActionNodes(ctx context.Context, s, target Storage, req fileActionRequest) ([]taskFileNode, error) {
	if req.Action == "mkdir" {
		return []taskFileNode{{s, req.Source, req.Name}}, nil
	}
	nodes := make([]taskFileNode, 0, len(req.IDs)*2)
	for _, fid := range req.IDs {
		nodes = append(nodes, taskFileNode{storage: s, id: fid})
	}
	if req.Action != "copy" && req.Action != "move" && req.Action != "rename" {
		return nodes, nil
	}
	if req.Action == "rename" && backupPathBased(s) {
		for _, fid := range req.IDs {
			nodes = append(nodes, taskFileNode{s, path.Dir(fid), req.Name})
		}
		return nodes, nil
	}
	files, err := a.rawList(ctx, s, backupDir(s, req.Source))
	if err != nil {
		return nil, err
	}
	for _, fid := range req.IDs {
		found := false
		for _, f := range files {
			if f.ID == fid || backupPathBased(s) && path.Clean("/"+f.ID) == path.Clean("/"+fid) {
				if req.Action == "rename" {
					nodes = append(nodes, taskFileNode{s, req.Source, req.Name})
				} else {
					nodes = append(nodes, taskFileNode{target, req.Target, f.Name})
				}
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("源目录已变化，请刷新后重试")
		}
	}
	return nodes, nil
}

func (a *App) reserveTaskFileAction(ctx context.Context, s, target Storage, req fileActionRequest) (func(), error) {
	nodes, err := a.taskFileActionNodes(ctx, s, target, req)
	if err != nil {
		return nil, err
	}
	return a.reserveTaskFileNodes(ctx, nodes)
}

func (a *App) reserveTaskFileNodes(ctx context.Context, nodes []taskFileNode) (func(), error) {
	if err := a.checkTaskFileNodes(ctx, nodes); err != nil {
		return nil, err
	}
	if a.taskFileUses == nil {
		a.taskFileUses = map[string][]taskFileNode{}
	}
	key := id()
	a.taskFileUses[key] = nodes
	return func() {
		a.runMu.Lock()
		delete(a.taskFileUses, key)
		a.runMu.Unlock()
	}, nil
}

func (a *App) taskRunNodes(task Task, s Storage) ([]taskFileNode, error) {
	nodes := []taskFileNode{{storage: s, id: task.Source}}
	if task.Kind == "cache" {
		return nodes, nil
	}
	base, rel, err := a.outputLocation(task.Target)
	if err != nil {
		return nil, err
	}
	if (task.Kind == "cas" || task.Kind == "strm") && backupDir(s, task.Source) != rootOf(s) {
		label := task.SourceLabel
		if backupPathBased(s) {
			label = path.Base(strings.TrimRight(task.Source, "/"))
		}
		if !safeName(label) {
			return nil, errors.New("源目录名称无效，请重新选择")
		}
		rel = filepath.Join(rel, label)
	}
	return append(nodes, taskFileNode{storage: Storage{Type: "local", Config: map[string]string{"root": base}}, id: filepath.ToSlash(rel)}), nil
}

func (a *App) checkTaskStartScopes(ctx context.Context, task Task, s Storage) error {
	if len(a.running) == 0 && len(a.taskFileUses) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	resolver := taskScopeResolver{a, ctx, map[string][]File{}}
	left, err := a.taskRunNodes(task, s)
	if err != nil {
		return err
	}
	for _, reserved := range a.taskFileUses {
		for _, l := range left {
			for _, r := range reserved {
				overlap, err := resolver.overlap(l, r)
				if err != nil {
					return fmt.Errorf("%s无法核验与文件操作的目录关系：%w", taskDescription(task), err)
				}
				if overlap {
					return fmt.Errorf("%s涉及的目录正在进行文件操作或传输，请稍后执行", taskDescription(task))
				}
			}
		}
	}
	for _, active := range a.runningTasks() {
		peer, err := a.store.storage(active.StorageID)
		if err != nil {
			return fmt.Errorf("无法核验运行中的%s的存储目录，未启动新任务", taskDescription(active))
		}
		right, err := a.taskRunNodes(active, peer)
		if err != nil {
			return fmt.Errorf("无法核验运行中的%s的保护范围：%w", taskDescription(active), err)
		}
		for _, l := range left {
			for _, r := range right {
				overlap, err := resolver.overlap(l, r)
				if err != nil {
					return fmt.Errorf("无法核验与运行中的%s的目录关系：%w", taskDescription(active), err)
				}
				if overlap {
					return fmt.Errorf("%s正在执行，所选任务的源目录或生成目录与其重叠，请等待完成后再执行", taskDescription(active))
				}
			}
		}
	}
	return nil
}

func (a *App) refreshTaskInterval(storageID string) {
	interval := 200
	for _, task := range a.store.snapshotWithLogLimit(0).Tasks {
		if _, ok := a.running[task.ID]; ok && task.StorageID == storageID {
			interval = max(interval, task.APIInterval)
		}
	}
	a.gateMu.Lock()
	a.intervals[storageID] = interval
	a.gateMu.Unlock()
}
