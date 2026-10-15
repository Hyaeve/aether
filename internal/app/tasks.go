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
	if reason := taskHealthPause(a.store.snapshotWithLogLimit(0), task); reason != "" {
		return errors.New(reason)
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
	if err := storageAuthBlocked(s, time.Now()); err != nil {
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
	ctx, causeCancel := context.WithCancelCause(parent)
	cancel := func() { causeCancel(context.Canceled) }
	a.running[taskID] = cancel
	if a.taskHealthCancels == nil {
		a.taskHealthCancels = map[string]func(){}
	}
	a.taskHealthCancels[taskID] = func() { causeCancel(errStorageHealthPaused) }
	a.runningStorage[taskID] = task.StorageID
	a.gateMu.Lock()
	a.intervals[task.StorageID] = task.APIInterval
	a.gateMu.Unlock()
	a.taskUpdate(taskID, func(t *Task) {
		t.Status = "running"
		t.Message = "正在扫描"
		t.Processed = 0
		t.CASGenerated, t.CASFailed = 0, 0
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
		if errors.Is(context.Cause(ctx), errStorageHealthPaused) {
			status, message, err = "health-paused", errStorageHealthPaused.Error(), errStorageHealthPaused
		}
		if persistErr := a.store.update(func(st *State) error {
			for i := range st.Tasks {
				if st.Tasks[i].ID == taskID {
					if status == "health-paused" && taskHealthPause(*st, st.Tasks[i]) == "" {
						status, message = "idle", "健康已恢复，等待执行"
					}
					st.Tasks[i].Status, st.Tasks[i].Message, st.Tasks[i].Processed = status, message, count
				}
			}
			return nil
		}); persistErr != nil {
			a.logger.Printf("persist task: %v", persistErr)
		}
		a.store.event(status, "tasks", task.Name+"："+message)
		a.runMu.Lock()
		delete(a.running, taskID)
		delete(a.runningStorage, taskID)
		delete(a.taskHealthCancels, taskID)
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
	cleanup, err := a.newFeatureCleanup(t, s)
	if err != nil {
		return 0, err
	}
	defer cleanup.close()
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
	generated, failed := 0, 0
	defer func() {
		if t.Kind == "cas" || t.Kind == "ed2k" {
			a.taskUpdate(t.ID, func(task *Task) { task.CASGenerated, task.CASFailed = generated, failed })
		}
	}()
	var skippedCAS []string
	flushSkippedCAS := func() {
		if len(skippedCAS) == 0 {
			return
		}
		a.store.event("warn", "tasks", fmt.Sprintf("CAS任务 %s：本批跳过 %d 个特征异常文件：%s", t.Name, len(skippedCAS), strings.Join(skippedCAS, "；")))
		skippedCAS = nil
	}
	defer flushSkippedCAS()
	var binding Storage
	if t.Kind == "cas" && t.CASOperation == "restore" {
		var err error
		binding, err = a.casBinding(t, s)
		if err != nil {
			return 0, err
		}
	}
	generateCAS := t.Kind == "cas" && t.CASOperation != "restore"
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
		files, err := a.listFiles(ctx, s, dir, t.CacheTTL, t.Kind == "cache" || cleanup != nil)
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
		if cleanup != nil {
			for _, f := range files {
				if safeName(f.Name) {
					if err := cleanup.observe(ctx, a, dir, f); err != nil {
						return err
					}
				}
			}
		}
		precomputed := map[string]casPrecomputed{}
		for fileIndex, f := range files {
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
				cleanup.directory(dir, f)
				continue
			}
			var info *CASInfo
			featureSource := generateCAS && (strings.EqualFold(path.Ext(f.Name), ".cas") || strings.HasSuffix(strings.ToLower(f.Name), ".cas.strm"))
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
			} else if (t.Kind != "strm" && t.Kind != "ed2k" && t.Kind != "cas") || (!taskMedia(t, f.Name) && !metadata && !featureSource) || excluded(f.Name, t.ExcludeFiles) || excludedType(f.Name, t.ExcludeTypes) {
				continue
			}
			if featureSource {
				parsed, e := a.readCAS(ctx, s, f)
				if e != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					failed++
					reason := "CAS特征损坏或读取失败"
					if errors.Is(e, errCASIncomplete) {
						reason = errCASIncomplete.Error()
					}
					skippedCAS = append(skippedCAS, path.Join(t.Source, child)+"（"+reason+"）")
					if len(skippedCAS) >= 50 {
						flushSkippedCAS()
					}
					continue
				}
				if !taskMedia(t, parsed.Name) || excludedType(parsed.Name, t.ExcludeTypes) {
					continue
				}
				info = &parsed
				child, metadata = path.Join(rel, parsed.Name), false
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
				if err := cleanup.published(ctx, root, f, metadataPath, true); err != nil {
					return err
				}
				count++
				continue
			}
			suffix := ".strm"
			if t.Kind == "ed2k" {
				suffix = ".ed2k.strm"
			}
			if t.Kind == "ed2k" && t.ED2KBindingID == "" && t.ID == "" {
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
				if err := cleanup.verifyED2K(f, info); err != nil {
					return err
				}
				data := []byte(info.URI() + "\n")
				if t.ED2KBindingID == "" && t.ID != "" {
					data = []byte(a.generatedStreamURL(streamClaim{ED2K: &info, Redirect: true}) + "\n")
				}
				if t.ED2KBindingID != "" {
					info.Binding = a.ed2kBindingStamp(t, s, binding)
					data = []byte(a.signedStreamURL(streamClaim{Storage: binding.ID, File: f.ID, TaskID: t.ID, ED2K: &info, Redirect: true}) + "\n")
				}
				if err := writeCASOutput(root, filename, data, t.Mode == "incremental", cleanup != nil); err != nil {
					return err
				}
				if err := cleanup.published(ctx, root, f, filename, false); err != nil {
					return err
				}
				count++
				generated++
				if t.ID != "" {
					a.taskUpdate(t.ID, func(task *Task) { task.Processed, task.CASGenerated = count, generated })
				}
				continue
			}
			if generateCAS {
				feature := info
				err = nil
				if feature == nil {
					key := f.ID + "\x00" + f.Name
					result, ok := precomputed[key]
					if !ok {
						for key, value := range a.precomputeRemoteCAS(ctx, t, s, files[fileIndex:], rel, outputDir, target) {
							precomputed[key] = value
						}
						result, ok = precomputed[key]
					}
					if !ok {
						result.info, result.err = a.generateCASInfo(ctx, s, f)
					}
					parsed, e := result.info, result.err
					feature, err = &parsed, e
					delete(precomputed, key)
				}
				if errors.Is(err, errCASMissingHash) {
					failed++
					delete(outputs, strings.ToLower(filename))
					skippedCAS = append(skippedCAS, path.Join(t.Source, child)+"（CAS信息缺失，不满足cas.strm生成要求）")
					if len(skippedCAS) >= 50 {
						flushSkippedCAS()
					}
					continue
				}
				if err != nil {
					failed++
					return fmt.Errorf("%s: %w", f.Name, err)
				}
				feature.Provider, feature.PlaybackURL, feature.RetentionHours = "", "", 0
				if err := cleanup.verifyCAS(f, *feature, featureSource); err != nil {
					return err
				}
				link := a.generatedStreamURL(streamClaim{CAS: feature, Redirect: true})
				if err := writeCASOutput(root, filename, []byte(link+"\n"), t.Mode == "incremental", cleanup != nil); err != nil {
					failed++
					return err
				}
				if err := cleanup.published(ctx, root, f, filename, false); err != nil {
					return err
				}
				count++
				generated++
				if t.ID != "" && time.Since(lastProgress) >= time.Second {
					lastProgress = time.Now()
					a.taskUpdate(t.ID, func(task *Task) {
						task.Processed, task.CASGenerated, task.CASFailed = count, generated, failed
						task.Message = "正在生成 · " + child
					})
				}
				continue
			}
			claim := streamClaim{Storage: s.ID, File: f.ID, Pick: f.PickCode, CAS: info}
			if info != nil {
				claim.TaskID, claim.RetentionHours = t.ID, casRetentionHours(t.RetentionHours)
			}
			link := a.signedStreamURL(claim)
			if t.Kind == "strm" && (s.Type == "mobile" || s.Type == "tianyi") {
				link = a.generatedStreamURL(claim)
			}
			if t.Kind == "strm" && s.Type == "local" {
				link = filepath.Join(s.Config["root"], filepath.FromSlash(f.ID))
			}
			if t.Kind == "strm" && s.Type == "webdav" {
				u, e := url.Parse(strings.TrimRight(s.Config["address"], "/"))
				if e != nil {
					return e
				}
				u.Path = path.Join(u.Path, s.Config["root"], f.ID)
				u.RawPath = ""
				u.User = nil
				u.RawQuery = ""
				u.Fragment = ""
				link = u.String()
			}
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
	err = walk(t.Source, sourceRel, 0)
	if err == nil && cleanup != nil {
		err = cleanup.finish(ctx, a, root)
	}
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
	return a.signedStreamPayload(claim)
}

func (a *App) signedStreamPayload(payload any) string {
	st := a.store.snapshot()
	b, _ := json.Marshal(payload)
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
				ctxED2K, cancelED2K := context.WithTimeout(a.ctx, 30*time.Second)
				if _, err := a.cleanupED2K(ctxED2K, nil); err != nil && a.ctx.Err() == nil {
					a.logger.Printf("ED2K cleanup: %v", err)
				}
				cancelED2K()
				ctxSim, cancelSim := context.WithTimeout(a.ctx, 30*time.Second)
				if err := a.simulcast.cleanup(ctxSim); err != nil && a.ctx.Err() == nil {
					a.logger.Printf("simulcast cleanup: %v", err)
				}
				cancelSim()
			}
			st := a.store.snapshot()
			for _, rule := range st.BackupRules {
				if rule.Enabled && backupHealthPause(st, rule) == "" && rule.Status != "running" && !rule.NextRun.IsZero() && !now.Before(rule.NextRun) {
					if err := a.startBackupScheduled(rule.ID); err != nil {
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
				if t.Enabled && taskHealthPause(st, t) == "" && !t.NextRun.IsZero() && !now.Before(t.NextRun) {
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
