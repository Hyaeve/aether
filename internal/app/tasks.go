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

func (a *App) startTask(taskID string, reset ...bool) error {
	return a.startTaskContext(a.ctx, taskID, nil, reset...)
}

func (a *App) startTaskContext(parent context.Context, taskID string, done chan<- error, reset ...bool) error {
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
	if task.Kind == "cas" && task.CASOperation != "generate" {
		return errors.New("旧版 CAS 转 STRM 任务已停用，请重新选择视频源目录并保存")
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
	if len(reset) > 0 && reset[0] {
		if err := a.validateTask(&task); err != nil {
			return err
		}
		if err := a.resetTaskOutput(task, s); err != nil {
			return err
		}
		task.Mode = "full"
	}
	ctx, cancel := context.WithCancel(parent)
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
		a.store.event(status, "tasks", task.Name+"："+message)
		a.runMu.Lock()
		delete(a.running, taskID)
		delete(a.runningStorage, taskID)
		a.runMu.Unlock()
		if done != nil {
			done <- err
		} else if err == nil {
			a.triggerAutomations(taskID)
		}
	}()
	return nil
}

func (a *App) outputRelative(target string) (string, error) {
	if filepath.IsAbs(target) {
		return filepath.Clean(target), nil
	}
	return relative(target)
}

func (a *App) outputLocation(target string) (string, string, error) {
	rel, err := a.outputRelative(target)
	if err != nil {
		return "", "", err
	}
	if filepath.IsAbs(rel) {
		return rel, ".", nil
	}
	return a.outputDir, rel, nil
}

func (a *App) executeTask(ctx context.Context, t Task, s Storage) (int, error) {
	var root *os.Root
	target := ""
	outputDir := a.outputDir
	if t.Kind == "strm" || t.Kind == "cas" || t.Kind == "ed2k" {
		var err error
		outputDir, target, err = a.outputLocation(t.Target)
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
	var skippedCAS []string
	flushSkippedCAS := func() {
		if len(skippedCAS) == 0 {
			return
		}
		a.store.event("warn", "tasks", fmt.Sprintf("CAS任务 %s：本批跳过 %d 个缺少有效哈希的文件：%s", t.Name, len(skippedCAS), strings.Join(skippedCAS, "；")))
		skippedCAS = nil
	}
	defer flushSkippedCAS()
	var binding Storage
	if t.Kind == "cas" {
		var err error
		binding, err = a.casBinding(t, s)
		if err != nil {
			return 0, err
		}
	}
	generateCAS := t.Kind == "cas" && (t.CASOperation == "generate" || (t.CASOperation == "" && s.Type == "local"))
	if t.Kind == "ed2k" && t.ED2KBindingID != "" {
		var err error
		binding, err = a.validateED2KBinding(t, s)
		if err != nil {
			return 0, err
		}
	}
	visited := map[string]bool{}
	outputs := map[string]bool{}
	lastProgress := time.Time{}
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
		files, err := a.listFiles(ctx, s, dir, t.CacheTTL, t.Kind == "cache")
		if err != nil {
			return err
		}
		if t.Kind == "cache" {
			count++
			if time.Since(lastProgress) >= time.Second && t.ID != "" {
				lastProgress = time.Now()
				a.taskUpdate(t.ID, func(task *Task) {
					task.Processed = count
					task.Message = fmt.Sprintf("已缓存 %d 个目录 · %s", count, path.Join(t.SourceLabel, rel))
				})
			}
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
			metadata := excludedType(f.Name, t.MetadataExtensions) && !taskMedia(t, f.Name)
			if t.Kind == "cas" && !generateCAS && !metadata {
				if !strings.EqualFold(path.Ext(f.Name), ".cas") || excluded(f.Name, t.ExcludeFiles) {
					continue
				}
				parsed, err := a.readCAS(ctx, s, f)
				if err != nil {
					return fmt.Errorf("%s: %w", f.Name, err)
				}
				if !taskMedia(t, parsed.Name) || excludedType(parsed.Name, t.ExcludeTypes) {
					continue
				}
				info = &parsed
				child = path.Join(rel, parsed.Name)
			} else if (t.Kind != "strm" && t.Kind != "ed2k" && t.Kind != "cas") || (!taskMedia(t, f.Name) && !metadata) || excluded(f.Name, t.ExcludeFiles) || excludedType(f.Name, t.ExcludeTypes) {
				continue
			}
			// Create the output root only when a matching file actually needs writing.
			if root == nil {
				if err := os.MkdirAll(outputDir, 0755); err != nil {
					return err
				}
				root, err = os.OpenRoot(outputDir)
				if err != nil {
					return err
				}
			}
			if metadata {
				metadataPath := path.Join(target, child)
				if outputs[strings.ToLower(metadataPath)] {
					return fmt.Errorf("输出文件名冲突：%s", metadataPath)
				}
				outputs[strings.ToLower(metadataPath)] = true
				if t.Mode == "incremental" {
					if _, err := root.Stat(metadataPath); err == nil {
						continue
					}
				}
				if err := a.copyTaskMetadata(ctx, s, f, root, metadataPath, t.Mode == "incremental"); err != nil {
					return err
				}
				count++
				continue
			}
			suffix := ".strm"
			if t.Kind == "ed2k" {
				suffix = ".ed2k.strm"
			}
			if t.Kind == "ed2k" && t.ED2KBindingID == "" {
				suffix = ".ed2k"
			}
			if generateCAS {
				suffix = ".cas.strm"
			}
			filename := path.Join(target, taskOutputName(t, s, child, suffix))
			if outputs[strings.ToLower(filename)] {
				return fmt.Errorf("输出文件名冲突：%s", filename)
			}
			outputs[strings.ToLower(filename)] = true
			if t.Mode == "incremental" {
				if _, err := root.Stat(filename); err == nil {
					continue
				}
			}
			if t.Kind == "ed2k" {
				info, err := a.generateED2KInfo(ctx, s, f)
				if err != nil {
					return err
				}
				data := []byte(info.URI() + "\n")
				if t.ED2KBindingID != "" {
					info.Binding = a.ed2kBindingStamp(t, s, binding)
					data = []byte(a.signedStreamURL(streamClaim{Storage: binding.ID, File: f.ID, TaskID: t.ID, ED2K: &info, Redirect: true}) + "\n")
				}
				if err := writeCASOutput(root, filename, data, t.Mode == "incremental"); err != nil {
					return err
				}
				count++
				continue
			}
			if generateCAS {
				info, err := a.generateCASInfo(ctx, s, f)
				if errors.Is(err, errCASMissingHash) {
					delete(outputs, strings.ToLower(filename))
					skippedCAS = append(skippedCAS, child)
					if len(skippedCAS) >= 50 {
						flushSkippedCAS()
					}
					continue
				}
				if err != nil {
					return fmt.Errorf("%s: %w", f.Name, err)
				}
				info.RetentionHours = casRetentionHours(t.RetentionHours)
				info, err = casForBinding(info, binding)
				if err != nil {
					return err
				}
				playbackInfo := info
				info.PlaybackURL = a.signedStreamURL(streamClaim{Storage: binding.ID, File: f.ID, CAS: &playbackInfo, TaskID: t.ID, RetentionHours: casRetentionHours(t.RetentionHours), Redirect: true})
				if err := writeCASOutput(root, filename, []byte(info.PlaybackURL+"\n"), t.Mode == "incremental"); err != nil {
					return err
				}
				count++
				continue
			}
			claim := streamClaim{Storage: s.ID, File: f.ID, Pick: f.PickCode, CAS: info}
			if info != nil {
				claim.TaskID, claim.RetentionHours = t.ID, casRetentionHours(t.RetentionHours)
			}
			link := a.signedStreamURL(claim)
			if t.Kind == "strm" && (s.Type == "115" || s.Type == "quark") {
				link, err = a.publicSTRMURL(s, f)
				if err != nil {
					return err
				}
			}
			if t.Kind == "strm" && s.Type == "openlist" {
				link, err = openlistSTRM(s, f.ID, t.EncodePath)
				if err != nil {
					return err
				}
			}
			if err := writeCASOutput(root, filename, []byte(link+"\n"), t.Mode == "incremental"); err != nil {
				return err
			}
			count++
		}
		return nil
	}
	sourceRel := ""
	if t.Kind == "strm" && t.Source != "" && t.Source != "/" && t.Source != rootOf(s) {
		sourceRel = t.SourceLabel
		if s.Type == "local" || s.Type == "webdav" || s.Type == "openlist" {
			sourceRel = path.Base(strings.TrimRight(t.Source, "/"))
		}
		if !safeName(sourceRel) {
			return 0, errors.New("源目录名称缺失或无效，请重新选择源目录")
		}
	}
	err := walk(t.Source, sourceRel, 0)
	return count, err
}

func openlistSTRM(s Storage, fileID string, encode bool) (string, error) {
	base, err := url.Parse(s.Config["address"])
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil {
		return "", errors.New("OpenList 服务地址无效")
	}
	p := path.Join("/", s.Config["root"], fileID)
	if strings.ContainsAny(p, "\r\n") {
		return "", errors.New("OpenList 文件路径包含换行，无法生成 STRM")
	}
	if encode {
		parts := strings.Split(p, "/")
		for i := range parts {
			parts[i] = url.PathEscape(parts[i])
		}
		p = strings.Join(parts, "/")
	} else {
		// Keep readable Unicode while protecting URL delimiters and literal '%'.
		p = strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(p)
	}
	base.RawQuery, base.Fragment = "", ""
	return strings.TrimRight(base.String(), "/") + "/d" + p, nil
}

type streamClaim struct {
	Redirect       bool      `json:"redirect,omitempty"`
	Storage        string    `json:"s"`
	File           string    `json:"f"`
	Pick           string    `json:"p"`
	CAS            *CASInfo  `json:"cas,omitempty"`
	ED2K           *ED2KInfo `json:"ed2k,omitempty"`
	TaskID         string    `json:"task,omitempty"`
	RetentionHours int       `json:"retentionHours,omitempty"`
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
	lastLogPrune := time.Now()
	lastCASCleanup := time.Time{}
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-ticker.C:
			if now.Sub(lastLogPrune) >= time.Hour {
				a.store.pruneLogs()
				lastLogPrune = now
			}
			if now.Sub(lastCASCleanup) >= time.Minute {
				lastCASCleanup = now
				ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
				if _, err := a.cleanupCAS(ctx); err != nil && a.ctx.Err() == nil {
					a.logger.Printf("CAS cleanup: %v", err)
				}
				cancel()
			}
			st := a.store.snapshot()
			for _, rule := range st.BackupRules {
				if rule.Enabled && rule.Status != "running" && !rule.NextRun.IsZero() && !now.Before(rule.NextRun) {
					if err := a.startBackup(rule.ID); err != nil {
						_ = a.backupUpdate(rule.ID, func(r *BackupRule) { r.NextRun = now.Add(time.Minute) })
					}
				}
			}
			for _, rule := range st.Automations {
				if rule.Enabled && rule.Trigger == "cron" && !rule.NextRun.IsZero() && !now.Before(rule.NextRun) {
					if err := a.startAutomation(rule.ID); err != nil {
						a.automationUpdate(rule.ID, func(r *Automation) { r.NextRun = now.Add(time.Minute) })
					}
				}
			}
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
