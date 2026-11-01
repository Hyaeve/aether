package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"time"
)

type strmDeleteState struct {
	ID      string    `json:"id"`
	State   string    `json:"state"`
	Count   int       `json:"count"`
	RetryAt time.Time `json:"retryAt"`
}

var errSTRMDeletePaused = errors.New("删除超限，源存储健康检查未通过，任务暂停")

func strmDeleteThreshold(t Task) int {
	if t.DeleteThreshold == nil {
		return 1000
	}
	return *t.DeleteThreshold
}

func strmDeleteRetry(t Task, now time.Time) time.Time {
	if t.STRMSource == "life" && t.Cron == "" {
		return time.Date(now.Year(), now.Month(), now.Day()+1, now.Hour(), now.Minute(), 0, 0, now.Location())
	}
	t.STRMDeleteState = nil
	t.Enabled = true
	return nextRun(t, now)
}

func (a *App) authorizeSTRMDeletion(ctx context.Context, t Task, s Storage, count int, validate func() error) error {
	if count <= strmDeleteThreshold(t) {
		return validate()
	}
	guard := &strmDeleteState{ID: id(), State: "checking", Count: count, RetryAt: strmDeleteRetry(t, time.Now())}
	if err := a.store.update(func(st *State) error {
		for i := range st.Tasks {
			current := &st.Tasks[i]
			expected := t
			if t.fullSyncGeneration || t.resetGeneration {
				expected.Mode = current.Mode
			}
			if current.ID == t.ID && strmTaskConfig(*current) == strmTaskConfig(expected) {
				current.STRMDeleteState = guard
				current.Message = fmt.Sprintf("待删除%d条，超过阈值%d条，正在检查源存储", count, strmDeleteThreshold(t))
				return nil
			}
		}
		return errors.New("STRM任务配置已改变，未执行删除")
	}); err != nil {
		return err
	}
	// A failed probe can cancel the run through the shared storage-health gate.
	// Persist the deletion hold independently of that cancellation.
	checkErr := a.checkStorageHealth(ctx, s)
	if checkErr != nil {
		if err := a.store.update(func(st *State) error {
			for i := range st.Tasks {
				current := &st.Tasks[i]
				if current.ID == t.ID && current.STRMDeleteState != nil && current.STRMDeleteState.ID == guard.ID {
					copy := *guard
					copy.State, copy.RetryAt = "waiting", strmDeleteRetry(*current, time.Now())
					current.STRMDeleteState = &copy
					current.Status, current.Message = "delete-paused", errSTRMDeletePaused.Error()
					current.NextRun = copy.RetryAt
				}
			}
			return nil
		}); err != nil {
			return errors.Join(errSTRMDeletePaused, err)
		}
		return errSTRMDeletePaused
	}
	validationErr := validate()
	releaseErr := a.store.update(func(st *State) error {
		for i := range st.Tasks {
			current := &st.Tasks[i]
			if current.ID == t.ID && current.STRMDeleteState != nil && current.STRMDeleteState.ID == guard.ID {
				current.STRMDeleteState = nil
				if validationErr == nil {
					current.Message = fmt.Sprintf("源存储健康检查通过，继续删除%d条", count)
				} else {
					current.Message = "源存储健康正常，但删除计划已变化，未执行删除"
				}
				return nil
			}
		}
		return errors.New("删除保护状态已改变，未执行删除")
	})
	return errors.Join(validationErr, releaseErr)
}

func (a *App) startSTRMDeleteCheck(taskID string, scheduled bool) error {
	a.runMu.Lock()
	var task Task
	for _, current := range a.store.snapshotWithLogLimit(0).Tasks {
		if current.ID == taskID {
			task = current
		}
	}
	if task.Kind != "strm" || task.STRMDeleteState == nil {
		a.runMu.Unlock()
		return errors.New("该任务没有等待中的删除健康检查")
	}
	if _, running := a.running[taskID]; running {
		a.runMu.Unlock()
		return errors.New("任务或健康检查正在执行")
	}
	s, err := a.store.storage(task.StorageID)
	if err != nil {
		a.runMu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.running[taskID], a.runningStorage[taskID] = cancel, task.StorageID
	guard := *task.STRMDeleteState
	if err := a.store.update(func(st *State) error {
		for i := range st.Tasks {
			if st.Tasks[i].ID == taskID {
				copy := guard
				copy.State = "checking"
				st.Tasks[i].STRMDeleteState = &copy
				st.Tasks[i].Message = "删除超限，正在检查源存储"
			}
		}
		return nil
	}); err != nil {
		delete(a.running, taskID)
		delete(a.runningStorage, taskID)
		cancel()
		a.runMu.Unlock()
		return err
	}
	a.wg.Add(1)
	a.runMu.Unlock()
	go func() {
		defer a.wg.Done()
		defer cancel()
		checkErr := a.checkStorageHealth(ctx, s)
		a.runMu.Lock()
		resume := false
		persistErr := a.store.update(func(st *State) error {
			for i := range st.Tasks {
				current := &st.Tasks[i]
				if current.ID != taskID || current.STRMDeleteState == nil || current.STRMDeleteState.ID != guard.ID {
					continue
				}
				if checkErr == nil && ctx.Err() == nil && strmTaskConfig(*current) == strmTaskConfig(task) {
					current.STRMDeleteState = nil
					current.Status, current.Message = "idle", "源存储健康检查通过，可以执行任务"
					current.NextRun = nextRun(*current, time.Now())
					resume = scheduled && current.Enabled
				} else {
					copy := guard
					copy.State, copy.RetryAt = "waiting", strmDeleteRetry(*current, time.Now())
					current.STRMDeleteState = &copy
					current.Status, current.Message = "delete-paused", errSTRMDeletePaused.Error()
					current.NextRun = copy.RetryAt
				}
			}
			return nil
		})
		delete(a.running, taskID)
		delete(a.runningStorage, taskID)
		a.runMu.Unlock()
		if persistErr != nil {
			a.logger.Printf("persist STRM deletion check: %v", persistErr)
			return
		}
		if resume {
			if err := a.startTaskWithTrigger(a.ctx, taskID, nil, "scheduled"); err != nil {
				a.store.event("warn", "tasks", "STRM任务「"+task.Name+"」健康恢复后未启动："+err.Error())
			}
		}
	}()
	return nil
}

type strmDeleteCandidate struct {
	name, digest string
	info         os.FileInfo
}

func (h *strmTaskSync) validateDeletePlan(ctx context.Context, candidates []strmDeleteCandidate) error {
	if err := h.check(ctx); err != nil {
		return err
	}
	if err := h.cleanupScopes(); err != nil {
		return err
	}
	for _, item := range candidates {
		digest, info, err := strmOutputDigest(h.root, item.name)
		if err != nil || digest != item.digest || !sameStrmFile(item.info, info) {
			return errors.New("待删除文件在核验期间已改变，未执行删除")
		}
	}
	return nil
}

func (h *strmTaskSync) deleteOutputs(ctx context.Context, names []string) error {
	if len(names) == 0 && len(h.manifest.PendingCascade) == 0 {
		return nil
	}
	if err := h.cleanupScopes(); err != nil {
		return err
	}
	if err := h.ensure(ctx, nil); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	gone, seen := map[string]bool{}, map[string]bool{}
	candidates := []strmDeleteCandidate{}
	for _, name := range names {
		if seen[name] || h.manifest.Files[name] == "" {
			continue
		}
		seen[name] = true
		digest, info, err := strmOutputDigest(h.root, name)
		if os.IsNotExist(err) {
			gone[name] = true
			continue
		}
		if err != nil || digest != h.manifest.Files[name] {
			continue
		}
		gone[name] = true
		candidates = append(candidates, strmDeleteCandidate{name, digest, info})
	}
	sidecars, err := h.planCascade(ctx, gone)
	if err != nil {
		return err
	}
	for _, item := range sidecars {
		if !gone[item.name] {
			candidates = append(candidates, item)
		}
	}
	if err := h.validateDeletePlan(ctx, candidates); err != nil {
		return err
	}
	validate := func() error {
		if err := h.validateDeletePlan(ctx, candidates); err != nil {
			return err
		}
		if len(candidates) <= strmDeleteThreshold(h.task) {
			return nil
		}
		if h.task.STRMSource != "life" || h.task.fullSyncGeneration || h.task.resetGeneration {
			fresh, err := h.freshOutputs(ctx)
			if err != nil {
				return fmt.Errorf("健康检查后源目录重新核验失败，未删除：%w", err)
			}
			for name := range gone {
				if fresh[name] {
					return errors.New("健康检查期间源文件已恢复，未执行删除")
				}
			}
		} else {
			if err := h.validateLifeDeletions(ctx, gone); err != nil {
				return err
			}
		}
		currentSidecars, err := h.planCascade(ctx, gone)
		if err != nil || len(currentSidecars) != len(sidecars) {
			return errors.New("健康检查期间级联删除范围已改变，未删除")
		}
		planned := map[string]string{}
		for _, item := range sidecars {
			planned[item.name] = item.digest
		}
		for _, item := range currentSidecars {
			if planned[item.name] != item.digest {
				return errors.New("健康检查期间级联文件已改变，未删除")
			}
		}
		return h.validateDeletePlan(ctx, candidates)
	}
	if err := h.app.authorizeSTRMDeletion(ctx, h.task, h.storage, len(candidates), validate); err != nil {
		return err
	}
	if h.task.CascadeDelete {
		queued := map[string]bool{}
		for _, name := range h.manifest.PendingCascade {
			queued[name] = true
		}
		for name := range gone {
			if path.Ext(name) == ".strm" && !queued[name] {
				h.manifest.PendingCascade = append(h.manifest.PendingCascade, name)
			}
		}
		if err := h.save(); err != nil {
			return err
		}
	}
	for _, item := range candidates {
		if err := h.validateDeletePlan(ctx, []strmDeleteCandidate{item}); err != nil {
			return err
		}
		parent, err := quarkLibraryParent(h.root, item.name)
		if err != nil {
			return err
		}
		live, err := quarkLibraryParent(h.root, item.name)
		if err != nil {
			parent.Close()
			return err
		}
		left, leftErr := parent.Stat(".")
		right, rightErr := live.Stat(".")
		live.Close()
		info, statErr := parent.Lstat(path.Base(item.name))
		if leftErr != nil || rightErr != nil || !os.SameFile(left, right) || statErr != nil || !sameStrmFile(item.info, info) {
			parent.Close()
			return errors.New("待删除文件身份已改变，已停止清理")
		}
		err = h.check(ctx)
		if err == nil {
			err = recordSTRMLifeWork(ctx)
		}
		if err == nil {
			err = parent.Remove(path.Base(item.name))
		}
		parent.Close()
		if err != nil {
			return err
		}
		delete(h.manifest.Files, item.name)
		h.removed++
	}
	for name := range gone {
		delete(h.manifest.Files, name)
	}
	h.manifest.PendingCascade = nil
	return nil
}

func (h *strmTaskSync) validateLifeDeletions(ctx context.Context, gone map[string]bool) error {
	directories := map[string][]File{}
	list := func(parent string) ([]File, error) {
		if items, found := directories[parent]; found {
			return items, nil
		}
		items, err := h.app.rawList(ctx, h.storage, parent)
		if err == nil {
			directories[parent] = items
		}
		return items, err
	}
	for id, parent := range h.lifeDeleteParents {
		items, err := list(parent)
		if err != nil {
			return errors.New("健康检查后删除事件的父目录无法核验，未删除")
		}
		for _, item := range items {
			if item.ID == id {
				return errors.New("健康检查期间删除事件的源文件已恢复，未删除")
			}
		}
	}
	for id, source := range h.sourcesBefore {
		if source.Directory || !gone[h.lifeOutput(source)] {
			continue
		}
		covered := false
		for parent, depth := id, 0; depth < 128; depth++ {
			if _, deleted := h.lifeDeleteParents[parent]; deleted {
				covered = true
				break
			}
			ancestor, known := h.sourcesBefore[parent]
			if !known || ancestor.Parent == parent {
				break
			}
			parent = ancestor.Parent
		}
		if covered {
			continue
		}
		items, err := list(source.Parent)
		if err != nil {
			return errors.New("健康检查后生活事件父目录无法核验，未删除")
		}
		for _, item := range items {
			if item.IsDir || item.ID != id && item.Name != source.Name {
				continue
			}
			relative, inside, err := h.app.strmLifeRelative(ctx, h.storage, h.task, source.Parent, item.Name)
			if err != nil {
				return err
			}
			if inside && h.lifeOutput(strmLifeSource{Relative: relative, Name: item.Name}) == h.lifeOutput(source) {
				return errors.New("健康检查期间生活事件源文件已恢复，未删除")
			}
		}
	}
	return nil
}
