package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func nextRun(t Task, now time.Time) time.Time {
	if !t.Enabled {
		return time.Time{}
	}
	if t.Kind == "cache" {
		return now.Add(time.Duration(t.Interval) * time.Minute)
	}
	if t.Cron == "" {
		return time.Time{}
	}
	s, err := cronParser.Parse(t.Cron)
	if err != nil {
		return time.Time{}
	}
	return s.Next(now)
}

func excluded(value, filter string) bool {
	value = strings.ToLower(value)
	for _, s := range strings.Split(filter, ";") {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" && strings.Contains(value, s) {
			return true
		}
	}
	return false
}

func excludedType(name, filter string) bool {
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	for _, s := range strings.Split(filter, ";") {
		if strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), ".") == ext && ext != "" {
			return true
		}
	}
	return false
}

func isVideo(name string) bool {
	return strings.Contains(";mp4;mkv;avi;mov;wmv;flv;ts;m2ts;webm;mpg;mpeg;iso;m4v;", ";"+strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")+";")
}

func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\:`)
}

func (a *App) waitAPI(ctx context.Context, storageID string) error {
	a.gateMu.Lock()
	interval := a.intervals[storageID]
	if interval < 200 {
		interval = 200
	}
	now := time.Now()
	at := a.gates[storageID]
	if at.Before(now) {
		at = now
	}
	a.gates[storageID] = at.Add(time.Duration(interval) * time.Millisecond)
	a.gateMu.Unlock()
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *App) taskUpdate(taskID string, fn func(*Task)) {
	if err := a.store.update(func(st *State) error {
		for i := range st.Tasks {
			if st.Tasks[i].ID == taskID {
				fn(&st.Tasks[i])
				break
			}
		}
		return nil
	}); err != nil {
		a.logger.Printf("persist task: %v", err)
	}
}

func (a *App) startTask(taskID string) error {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	var task Task
	for _, t := range a.store.snapshot().Tasks {
		if t.ID == taskID {
			task = t
			break
		}
	}
	if task.ID == "" {
		return errors.New("任务不存在")
	}
	if _, ok := a.running[taskID]; ok {
		return errors.New("任务正在执行")
	}
	for _, storage := range a.runningStorage {
		if storage == task.StorageID {
			return errors.New("该存储池已有任务执行中")
		}
	}
	s, err := a.store.storage(task.StorageID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.running[taskID] = cancel
	a.runningStorage[taskID] = task.StorageID
	a.gateMu.Lock()
	a.intervals[task.StorageID] = task.APIInterval
	a.gateMu.Unlock()
	a.taskUpdate(taskID, func(t *Task) {
		t.Status = "running"
		t.Message = "正在扫描"
		t.Processed = 0
		t.LastRun = time.Now()
		t.NextRun = nextRun(*t, time.Now())
	})
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer cancel()
		count, err := a.executeTask(ctx, task, s)
		status, message := "success", fmt.Sprintf("执行完成，处理 %d 项", count)
		if err != nil {
			status, message = "error", err.Error()
			if errors.Is(err, context.Canceled) {
				status, message = "cancelled", "任务已停止"
			}
		}
		a.taskUpdate(taskID, func(t *Task) { t.Status = status; t.Message = message; t.Processed = count })
		a.store.log(status, task.Name+"："+message)
		a.runMu.Lock()
		delete(a.running, taskID)
		delete(a.runningStorage, taskID)
		a.runMu.Unlock()
	}()
	return nil
}

func (a *App) outputRelative(target string) (string, error) {
	root, err := filepath.Abs(a.outputDir)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		return relative(target)
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("生成目录必须位于 STRM 根目录 " + root + " 内")
	}
	return relative(rel)
}

func (a *App) executeTask(ctx context.Context, t Task, s Storage) (int, error) {
	var root *os.Root
	target := ""
	if t.Kind == "strm" || t.Kind == "cas" {
		var err error
		target, err = a.outputRelative(t.Target)
		if err != nil {
			return 0, err
		}
	}
	defer func() {
		if root != nil {
			_ = root.Close()
		}
	}()
	count := 0
	visited := map[string]bool{}
	outputs := map[string]bool{}
	var walk func(string, string, int) error
	walk = func(dir, rel string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if visited[dir] {
			return nil
		}
		if depth > 128 {
			return errors.New("目录嵌套超过安全上限 128 层")
		}
		visited[dir] = true
		if err := a.waitAPI(ctx, s.ID); err != nil {
			return err
		}
		files, err := a.listFiles(ctx, s, dir, t.CacheTTL, t.Kind == "cache")
		if err != nil {
			return err
		}
		if t.Kind == "cache" {
			count++
		}
		for _, f := range files {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !safeName(f.Name) {
				continue
			}
			child := path.Join(rel, f.Name)
			if f.IsDir {
				if excluded(child, t.ExcludeDirs) {
					continue
				}
				if t.Kind == "cache" && t.Depth > 0 && depth+1 >= t.Depth {
					continue
				}
				if err := walk(f.ID, child, depth+1); err != nil {
					return err
				}
				continue
			}
			var info *CASInfo
			if t.Kind == "cas" {
				if !strings.EqualFold(path.Ext(f.Name), ".cas") || excluded(f.Name, t.ExcludeFiles) {
					continue
				}
				parsed, err := a.readCAS(ctx, s, f)
				if err != nil {
					return fmt.Errorf("%s: %w", f.Name, err)
				}
				if excludedType(parsed.Name, t.ExcludeTypes) {
					continue
				}
				info = &parsed
				child = path.Join(rel, parsed.Name)
			} else if t.Kind != "strm" || !isVideo(f.Name) || excluded(f.Name, t.ExcludeFiles) || excludedType(f.Name, t.ExcludeTypes) {
				continue
			}
			// Create the output root only when a matching file actually needs writing.
			if root == nil {
				if err := os.MkdirAll(a.outputDir, 0755); err != nil {
					return err
				}
				root, err = os.OpenRoot(a.outputDir)
				if err != nil {
					return err
				}
			}
			// Retain the source extension so movie.mp4 and movie.mkv never collide.
			filename := path.Join(target, child+".strm")
			if outputs[strings.ToLower(filename)] {
				return fmt.Errorf("输出文件名冲突：%s", filename)
			}
			outputs[strings.ToLower(filename)] = true
			if t.Mode == "incremental" {
				if _, err := root.Stat(filename); err == nil {
					continue
				}
			}
			if err := root.MkdirAll(path.Dir(filename), 0755); err != nil {
				return err
			}
			flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
			if t.Mode == "incremental" {
				flag = os.O_CREATE | os.O_WRONLY | os.O_EXCL
			}
			fh, err := root.OpenFile(filename, flag, 0644)
			if os.IsExist(err) && t.Mode == "incremental" {
				continue
			}
			if err != nil {
				return err
			}
			link := a.signedStreamURL(streamClaim{Storage: s.ID, File: f.ID, Pick: f.PickCode, CAS: info})
			_, err = fh.WriteString(link + "\n")
			closeErr := fh.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			count++
		}
		return nil
	}
	err := walk(t.Source, "", 0)
	return count, err
}

type streamClaim struct {
	Storage string   `json:"s"`
	File    string   `json:"f"`
	Pick    string   `json:"p"`
	CAS     *CASInfo `json:"cas,omitempty"`
}

func (a *App) streamURL(sid, fid, pick string) string {
	return a.signedStreamURL(streamClaim{Storage: sid, File: fid, Pick: pick})
}

func (a *App) signedStreamURL(claim streamClaim) string {
	st := a.store.snapshot()
	b, _ := json.Marshal(claim)
	token := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, []byte(st.SignKey))
	mac.Write([]byte(token))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return strings.TrimRight(st.Settings.PublicURL, "/") + "/stream/" + token + "?sign=" + url.QueryEscape(sig)
}

func (a *App) scheduler() {
	defer a.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastSnapshot := time.Now()
	lastCASCleanup := time.Time{}
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-ticker.C:
			if now.Sub(lastCASCleanup) >= time.Minute {
				lastCASCleanup = now
				ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
				if _, err := a.cleanupCAS(ctx); err != nil && a.ctx.Err() == nil {
					a.logger.Printf("CAS cleanup: %v", err)
				}
				cancel()
			}
			st := a.store.snapshot()
			for _, t := range st.Tasks {
				if t.Enabled && !t.NextRun.IsZero() && !now.Before(t.NextRun) {
					if err := a.startTask(t.ID); err != nil {
						a.taskUpdate(t.ID, func(t *Task) { t.NextRun = now.Add(time.Minute) })
					}
				}
			}
			if st.Settings.CachePersist && now.Sub(lastSnapshot) >= time.Duration(st.Settings.SnapshotInterval)*time.Minute {
				if err := a.cache.persist(a.dataDir); err != nil {
					a.logger.Printf("cache snapshot: %v", err)
				}
				lastSnapshot = now
			}
		}
	}
}
