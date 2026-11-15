package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"
)

type organizeRunState struct {
	Status     string    `json:"status"`
	Message    string    `json:"message"`
	Done       int       `json:"done"`
	Total      int       `json:"total"`
	Succeeded  int       `json:"succeeded"`
	Failed     int       `json:"failed"`
	Skipped    int       `json:"skipped"`
	LastError  string    `json:"lastError,omitempty"`
	Scanned    bool      `json:"scanned"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
}
type organizeTaskView struct {
	organizeTaskDefinition
	Runtime *organizeRunState `json:"runtime,omitempty"`
}
type organizeContextKey struct{}

func organizeRunKey(id string) string { return "organize:" + id }
func organizeActive(status string) bool {
	return status == "queued" || status == "running" || status == "stopping"
}

func (a *App) organizeNodes(t organizeTaskDefinition) ([]taskFileNode, error) {
	return organizeNodes(t, a.store.snapshotWithLogLimit(0))
}

func organizeNodes(t organizeTaskDefinition, st State) ([]taskFileNode, error) {
	s, err := backupStorage(st, t.StorageID)
	if err != nil {
		return nil, errors.New("待整理存储池不存在或已停用")
	}
	d, err := backupStorage(st, t.TargetStorageID)
	if err != nil {
		return nil, errors.New("归档存储池不存在或已停用")
	}
	nodes := []taskFileNode{{storage: s, id: t.Source}, {storage: d, id: t.Target}}
	if t.FailureDirectory != "" {
		nodes = append(nodes, taskFileNode{storage: s, id: t.FailureDirectory})
	}
	return nodes, nil
}
func (a *App) organizeTaskAction(w http.ResponseWriter, r *http.Request) {
	key, action := r.PathValue("id"), r.PathValue("action")
	if action == "stop" {
		a.runMu.Lock()
		defer a.runMu.Unlock()
		cancel := a.running[organizeRunKey(key)]
		if cancel == nil {
			fail(w, 409, errors.New("整理归档任务当前未执行"))
			return
		}
		if err := a.organizeUpdate(key, func(run *organizeRunState) { run.Status = "stopping"; run.Message = "正在停止整理归档" }); err != nil {
			fail(w, 500, err)
			return
		}
		cancel()
		jsonResponse(w, 200, map[string]string{"message": "正在停止整理归档"})
		return
	}
	if action != "run" {
		http.NotFound(w, r)
		return
	}
	if err := a.startOrganize(key); err != nil {
		fail(w, 409, err)
		return
	}
	jsonResponse(w, 202, map[string]string{"message": "整理归档任务已进入后台队列"})
}
func (a *App) organizeUpdate(key string, fn func(*organizeRunState)) error {
	return a.store.update(func(st *State) error {
		if st.OrganizeRuntime == nil {
			st.OrganizeRuntime = map[string]organizeRunState{}
		}
		run := st.OrganizeRuntime[key]
		found := false
		for _, task := range st.OrganizeTasks {
			if task.ID == key {
				found = true
				break
			}
		}
		if !found {
			return errors.New("整理归档任务已移除")
		}
		fn(&run)
		st.OrganizeRuntime[key] = run
		return nil
	})
}
func (a *App) startOrganize(key string) error {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if _, ok := a.running[organizeRunKey(key)]; ok {
		return errors.New("整理归档任务正在执行，请勿重复启动")
	}
	st := a.store.snapshotWithLogLimit(0)
	var task organizeTaskDefinition
	for _, t := range st.OrganizeTasks {
		if t.ID == key {
			task = t
			break
		}
	}
	if task.ID == "" {
		return errors.New("整理归档任务不存在")
	}
	if err := validateOrganizeDefinition(&task, st, true); err != nil {
		return err
	}
	if task.NamingRuleID == "" || task.MediaCategoryRuleID == "" {
		return errors.New("请选择命名规则和媒体分类")
	}
	tmdb := pluginDefaults("tmdb", st.Plugins["tmdb"])
	if !tmdb.Enabled || tmdb.APIKey == "" {
		return errors.New("请先配置并启用TMDB识别")
	}
	if reason := dependencyPause(st, task.StorageID, task.TargetStorageID); reason != "" {
		return errors.New(reason)
	}
	ctx, cancel := context.WithCancel(context.WithValue(a.ctx, organizeContextKey{}, organizeRunKey(key)))
	if err := a.organizeUpdate(key, func(run *organizeRunState) {
		*run = organizeRunState{Status: "queued", Message: "等待核对目录并扫描媒体", StartedAt: time.Now()}
	}); err != nil {
		cancel()
		return err
	}
	a.running[organizeRunKey(key)] = cancel
	a.runningStorage[organizeRunKey(key)] = task.StorageID
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer cancel()
		err := a.executeOrganize(ctx, task, st)
		a.runMu.Lock()
		defer a.runMu.Unlock()
		defer delete(a.running, organizeRunKey(key))
		defer delete(a.runningStorage, organizeRunKey(key))
		persistErr := a.organizeUpdate(key, func(run *organizeRunState) {
			run.FinishedAt = time.Now()
			run.Status = "completed"
			if err != nil {
				run.Status = "error"
				run.Message = organizeError(err)
			} else {
				run.Message = fmt.Sprintf("整理完成：成功 %d，失败 %d，跳过 %d", run.Succeeded, run.Failed, run.Skipped)
				if run.Failed > 0 {
					run.Status = "error"
					run.Message += "。" + run.LastError
				}
			}
			if errors.Is(err, context.Canceled) {
				run.Status = "stopped"
				run.Message = "整理已停止，已完成的文件保留"
			}
		})
		if persistErr != nil {
			a.logger.Printf("persist organize result: %v", persistErr)
		}
		a.cache.clear()
		if err != nil && !errors.Is(err, context.Canceled) {
			a.store.event("warn", "tasks", task.Name+"："+organizeError(err))
		} else {
			a.store.event("info", "tasks", task.Name+"：整理归档结束")
		}
	}()
	return nil
}

// Keep actionable application errors while keeping upstream URLs and filesystem
// details out of persisted messages.
func organizeError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if strings.ContainsAny(message, "\r\n") || strings.Contains(message, "://") || strings.Contains(message, "\\") {
		return safeStorageError(err)
	}
	for _, r := range message {
		if r >= '\u3400' && r <= '\u9fff' {
			return message
		}
	}
	return safeStorageError(err)
}

type organizeRunner struct {
	a              *App
	t              organizeTaskDefinition
	st             State
	source, target Storage
	rec            compiledRecognition
	naming         namingDefinition
	categories     mediaCategoryDefinition
	upgrade        *upgradeDefinition
	client         *http.Client
	tmdb           PluginConfig
	writeMu        sync.Mutex
	rootInfo       map[string]os.FileInfo
	directoryInfo  map[string]os.FileInfo
	sourceDirs     map[string]organizeEntry
}

func (h *organizeRunner) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	st := h.a.store.snapshotWithLogLimit(0)
	for _, s := range []Storage{h.source, h.target} {
		if err := h.a.cloudStorageCurrent(s); err != nil {
			return err
		}
		if reason := dependencyPause(st, s.ID); reason != "" {
			return errors.New(reason)
		}
		if info := h.rootInfo[s.ID]; info != nil {
			now, err := os.Stat(s.Config["root"])
			if err != nil || !os.SameFile(info, now) {
				return errors.New("本地存储根目录身份已变化，整理停止")
			}
		}
	}
	for _, node := range []taskFileNode{{storage: h.source, id: h.t.Source}, {storage: h.target, id: h.t.Target}, {storage: h.source, id: h.t.FailureDirectory}} {
		if saved := h.directoryInfo[node.storage.ID+"\x00"+node.id]; saved != nil {
			current, err := organizeLocalDir(node.storage, node.id)
			if err != nil || !os.SameFile(saved, current) {
				return errors.New("整理目录身份已变化，任务停止")
			}
		}
	}
	for _, t := range st.OrganizeTasks {
		if t.ID == h.t.ID {
			if validateOrganizeDefinition(&t, st, false) != nil || !reflect.DeepEqual(t, h.t) {
				return errors.New("整理归档配置已变化，任务停止")
			}
			return nil
		}
	}
	return errors.New("整理归档任务已移除")
}
func (a *App) executeOrganize(ctx context.Context, t organizeTaskDefinition, st State) error {
	nodes, err := organizeNodes(t, st)
	if err != nil {
		return err
	}
	h := &organizeRunner{a: a, t: t, st: st, source: nodes[0].storage, target: nodes[1].storage, rootInfo: map[string]os.FileInfo{}, directoryInfo: map[string]os.FileInfo{}, sourceDirs: map[string]organizeEntry{}, tmdb: pluginDefaults("tmdb", st.Plugins["tmdb"])}
	for _, p := range st.RulePresets {
		switch p.ID {
		case t.NamingRuleID:
			h.naming = defaultNaming(p.Naming)
		case t.MediaCategoryRuleID:
			h.categories = defaultMediaCategory(p.MediaCategory)
		case t.UpgradeRuleID:
			c := defaultUpgrade(p.Upgrade)
			h.upgrade = &c
		}
	}
	h.rec, err = compileRecognitionSettings(recognitionDefaults(st.RecognitionRules))
	if err != nil {
		return err
	}
	var closeIdle func()
	h.client, closeIdle, err = pluginClient(st.Plugins["proxy"])
	if err != nil {
		return err
	}
	defer closeIdle()
	for _, s := range []Storage{h.source, h.target} {
		if s.Type == "local" {
			info, err := os.Stat(s.Config["root"])
			if err != nil {
				return err
			}
			h.rootInfo[s.ID] = info
		}
	}
	ctx = a.tmdbContext(ctx)
	scopeCtx, cancelScope := context.WithTimeout(ctx, 30*time.Second)
	resolver := taskScopeResolver{a, scopeCtx, map[string][]File{}}
	for i := 0; i < len(nodes) && err == nil; i++ {
		for j := i + 1; j < len(nodes) && err == nil; j++ {
			var overlap bool
			overlap, err = resolver.overlap(nodes[i], nodes[j])
			if overlap {
				err = errors.New("待整理、归档与冗余目录不能互相包含")
			}
		}
	}
	for _, node := range nodes {
		if node.storage.Type == "local" && err == nil {
			var location string
			location, err = taskLocalPath(node.storage, node.id)
			for _, dir := range []string{a.store.dir, a.dataDir} {
				if dir == "" {
					continue
				}
				protected, protectErr := taskLocalPath(Storage{Type: "local", Config: map[string]string{"root": dir}}, "/")
				if protectErr != nil || backupPathsOverlap(location, protected) || backupPathsOverlap(protected, location) {
					err = errors.New("整理目录不能包含配置或数据目录")
				}
			}
			if err == nil {
				var info os.FileInfo
				info, err = organizeLocalDir(node.storage, node.id)
				if err == nil {
					h.directoryInfo[node.storage.ID+"\x00"+node.id] = info
				}
			}
		}
	}
	cancelScope()
	if err == nil {
		a.runMu.Lock()
		err = a.checkTaskStartScopes(ctx, Task{ID: organizeRunKey(t.ID), Kind: "organize", Name: t.Name, StorageID: t.StorageID, Source: t.Source}, h.source)
		a.runMu.Unlock()
	}
	if err != nil {
		return err
	}
	if err = h.check(ctx); err != nil {
		return err
	}
	if err = a.organizeUpdate(t.ID, func(run *organizeRunState) {
		if run.Status == "queued" {
			run.Status = "running"
			run.Message = "正在扫描待整理目录"
		}
	}); err != nil {
		return err
	}
	entries, err := h.scan(ctx)
	if err != nil {
		return err
	}
	if err = a.organizeUpdate(t.ID, func(run *organizeRunState) {
		run.Total = len(entries)
		run.Scanned = true
		run.Message = "正在识别和归档"
	}); err != nil {
		return err
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan organizeEntry)
	var workers sync.WaitGroup
	var fatal error
	var fatalMu sync.Mutex
	for i := 0; i < t.ProcessConcurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for entry := range jobs {
				if workCtx.Err() != nil {
					return
				}
				status, message := h.process(workCtx, entry)
				if err := a.organizeUpdate(t.ID, func(run *organizeRunState) {
					run.Done++
					run.Message = entry.file.Name + "：" + message
					switch status {
					case "completed":
						run.Succeeded++
					case "skipped":
						run.Skipped++
					default:
						run.Failed++
						run.LastError = run.Message
					}
				}); err != nil {
					fatalMu.Lock()
					fatal = err
					fatalMu.Unlock()
					cancel()
					return
				}
			}
		}()
	}
	for _, entry := range entries {
		select {
		case jobs <- entry:
		case <-workCtx.Done():
			break
		}
		if workCtx.Err() != nil {
			break
		}
	}
	close(jobs)
	workers.Wait()
	if fatal != nil {
		return fatal
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return nil
}
