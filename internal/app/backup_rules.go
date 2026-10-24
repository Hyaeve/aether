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
	"time"
)

type BackupRule struct {
	HealthPause    string           `json:"healthPause,omitempty"`
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Enabled        bool             `json:"enabled"`
	SourceID       string           `json:"sourceId"`
	Source         string           `json:"source"`
	SourceLabel    string           `json:"sourceLabel"`
	TargetID       string           `json:"targetId"`
	Target         string           `json:"target"`
	TargetLabel    string           `json:"targetLabel"`
	Sources        []BackupLocation `json:"sources,omitempty"`
	Targets        []BackupLocation `json:"targets,omitempty"`
	Filters        []BackupFilter   `json:"filters,omitempty"`
	CaseSensitive  bool             `json:"caseSensitive"`
	ScanInterval   int              `json:"scanInterval"`
	MonitorEnabled bool             `json:"monitorEnabled"`
	CompletionRule string           `json:"completionRule"`
	SyncMode       string           `json:"syncMode"`
	DeletionRule   string           `json:"deletionRule"`
	SyncDelete     bool             `json:"syncDelete"`
	ConflictRule   string           `json:"conflictRule"`
	ConflictMarker string           `json:"conflictMarker"`
	HistoryDays    int              `json:"historyDays"`
	DeleteLimit    int              `json:"deleteLimit"`
	TargetOnly     string           `json:"targetOnly"`
	SyncMarker     bool             `json:"syncMarker"`
	FullScan       bool             `json:"fullScan"`
	FullScanEvery  int              `json:"fullScanEvery"`
	FullScanHours  int              `json:"fullScanHours"`
	SyncPending    string           `json:"syncPending,omitempty"`
	deleteApproval string
	forceFullSync  bool
	Deleted        int       `json:"deleted"`
	Replace        string    `json:"replace"`
	Extensions     string    `json:"extensions"`
	Exclude        string    `json:"exclude"`
	MinSize        int64     `json:"minSize"`
	MaxSize        int64     `json:"maxSize"`
	Cron           string    `json:"cron"`
	Status         string    `json:"status"`
	Message        string    `json:"message"`
	Scanned        int       `json:"scanned"`
	Copied         int       `json:"copied"`
	Skipped        int       `json:"skipped"`
	Phase          string    `json:"phase"`
	Total          int       `json:"total"`
	Processed      int       `json:"processed"`
	LastRun        time.Time `json:"lastRun"`
	NextRun        time.Time `json:"nextRun"`
}

func backupNext(r BackupRule, now time.Time) time.Time {
	if !r.Enabled {
		return time.Time{}
	}
	var next time.Time
	if r.ScanInterval > 0 {
		next = now.Add(time.Duration(r.ScanInterval) * time.Second)
	}
	if strings.TrimSpace(r.Cron) != "" {
		if s, err := cronParser.Parse(r.Cron); err == nil {
			cronNext := s.Next(now)
			if next.IsZero() || cronNext.Before(next) {
				next = cronNext
			}
		}
	}
	return next
}

func backupStorage(st State, id string) (Storage, error) {
	for _, s := range st.Storages {
		if s.ID == id && s.Enabled {
			return s, nil
		}
	}
	return Storage{}, errors.New("备份存储不存在或已停用")
}

func backupDir(s Storage, dir string) string {
	if dir == "" || dir == "/" {
		return rootOf(s)
	}
	return dir
}

func backupLocalPath(s Storage, dir string) (string, error) {
	rel, err := relative(backupDir(s, dir))
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(s.Config["root"], filepath.FromSlash(rel)))
}

func backupPathsOverlap(a, b string) bool {
	r, err := filepath.Rel(a, b)
	return err == nil && (r == "." || r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)))
}

func backupSameTree(a, b Storage) bool {
	if a.ID == b.ID {
		return true
	}
	if a.Type != b.Type || a.Type == "local" {
		return false
	}
	// Multiple pools can expose different roots of the same remote account.
	for _, key := range []string{"cookie", "authorization", "username", "password", "accessToken", "refreshToken", "address", "token"} {
		if a.Config[key] != b.Config[key] {
			return false
		}
	}
	return true
}

func validateBackup(r *BackupRule, st State) error {
	r.HealthPause = ""
	if err := validateBackupOptions(r, st); err != nil {
		return err
	}
	sources, targets := backupLocations(*r)
	if r.SyncMode == "two_way" {
		all := append(append([]BackupLocation{}, sources...), targets...)
		for i := range all {
			for j := i + 1; j < len(all); j++ {
				pair := backupPair(*r, all[i], all[j])
				if err := validateBackupPair(&pair, st); err != nil {
					return err
				}
			}
		}
	}
	for _, source := range sources {
		for _, target := range targets {
			pair := backupPair(*r, source, target)
			if err := validateBackupPair(&pair, st); err != nil {
				return err
			}
			r.Name, r.Cron, r.Extensions = pair.Name, pair.Cron, pair.Extensions
		}
	}
	return nil
}

func validateBackupPair(r *BackupRule, st State) error {
	r.Name, r.Cron = strings.TrimSpace(r.Name), strings.TrimSpace(r.Cron)
	if r.Name == "" || len(r.Name) > 200 {
		return errors.New("请填写备份名称（最多200字节）")
	}
	s, err := backupStorage(st, r.SourceID)
	if err != nil {
		return err
	}
	t, err := backupStorage(st, r.TargetID)
	if err != nil {
		return err
	}
	if r.Source == "" || r.Target == "" {
		return errors.New("请选择源目录和目标目录")
	}
	if len(r.Source) > 4096 || len(r.Target) > 4096 || len(r.Exclude) > 2048 {
		return errors.New("备份路径或筛选内容过长")
	}
	if r.Replace != "skip" && r.Replace != "overwrite" {
		return errors.New("同名文件策略无效")
	}
	if r.MinSize < 0 || r.MaxSize < 0 || r.MaxSize > 0 && r.MaxSize < r.MinSize {
		return errors.New("文件大小范围无效")
	}
	r.Extensions, err = normalizeExtensions(r.Extensions)
	if err != nil {
		return err
	}
	if r.Cron != "" {
		if _, err := cronParser.Parse(r.Cron); err != nil {
			return errors.New("Cron 表达式无效")
		}
	}
	if backupSameTree(s, t) && backupDir(s, r.Source) == backupDir(t, r.Target) {
		return errors.New("源目录和目标目录不能相同")
	}
	if s.Type == "local" && t.Type == "local" {
		a, err := backupLocalPath(s, r.Source)
		if err != nil {
			return err
		}
		b, err := backupLocalPath(t, r.Target)
		if err != nil {
			return err
		}
		// Resolve aliases as well as paths from two different local pools.
		a, err = filepath.EvalSymlinks(a)
		if err != nil {
			return err
		}
		b, err = filepath.EvalSymlinks(b)
		if err != nil {
			return err
		}
		if backupPathsOverlap(a, b) || backupPathsOverlap(b, a) {
			return errors.New("本地备份源与目标不能重叠")
		}
	}
	return nil
}

func (a *App) backupUpdate(id string, fn func(*BackupRule)) error {
	return a.store.update(func(st *State) error {
		for i := range st.BackupRules {
			if st.BackupRules[i].ID == id {
				fn(&st.BackupRules[i])
				return nil
			}
		}
		return errors.New("备份规则不存在")
	})
}

func (a *App) backupRulesAPI(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if action := req.PathValue("action"); action != "" {
		var err error
		switch action {
		case "run":
			err = a.startBackup(id)
		case "confirm-deletions":
			err = a.startBackupApproved(id)
		case "stop":
			a.backupMu.Lock()
			if cancel := a.backupRuns[id]; cancel != nil {
				cancel()
			} else {
				err = errors.New("备份未运行")
			}
			a.backupMu.Unlock()
		default:
			http.NotFound(w, req)
			return
		}
		if err != nil {
			fail(w, 409, err)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	if req.Method == "GET" && id == "" {
		st := a.store.snapshotWithLogLimit(0)
		rules := st.BackupRules
		for i := range rules {
			rules[i].HealthPause = backupHealthPause(st, rules[i])
		}
		if rules == nil {
			rules = []BackupRule{}
		}
		jsonResponse(w, 200, rules)
		return
	}
	if req.Method != "POST" && req.Method != "PUT" && req.Method != "DELETE" || req.Method == "POST" && id != "" || req.Method != "POST" && id == "" {
		w.WriteHeader(405)
		return
	}
	var input BackupRule
	if req.Method != "DELETE" {
		if !decode(w, req, &input) {
			return
		}
		if err := validateBackup(&input, a.store.snapshotWithLogLimit(0)); err != nil {
			fail(w, 400, err)
			return
		}
	}
	a.backupMu.Lock()
	defer a.backupMu.Unlock()
	if a.backupRuns[id] != nil {
		fail(w, 409, errors.New("请先停止备份"))
		return
	}
	err := a.store.update(func(st *State) error {
		if req.Method == "POST" {
			if len(st.BackupRules) >= 100 {
				return errors.New("最多100条备份规则")
			}
			input.ID, input.Status, input.Message = newAutomationID(), "idle", ""
			input.SyncPending = ""
			input.Scanned, input.Copied, input.Skipped, input.Deleted = 0, 0, 0, 0
			input.Phase, input.Total, input.Processed = "", 0, 0
			input.LastRun = time.Time{}
			input.NextRun = backupNext(input, time.Now())
			st.BackupRules = append(st.BackupRules, input)
			return nil
		}
		for i, old := range st.BackupRules {
			if old.ID != id {
				continue
			}
			if req.Method == "DELETE" {
				st.BackupRules = append(st.BackupRules[:i], st.BackupRules[i+1:]...)
			} else {
				input.ID, input.Status, input.Message, input.LastRun = id, old.Status, old.Message, old.LastRun
				input.Scanned, input.Copied, input.Skipped = old.Scanned, old.Copied, old.Skipped
				input.Deleted = old.Deleted
				input.Phase, input.Total, input.Processed = old.Phase, old.Total, old.Processed
				input.SyncPending = ""
				input.NextRun = backupNext(input, time.Now())
				st.BackupRules[i] = input
			}
			return nil
		}
		return errors.New("备份规则不存在")
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *App) startBackup(id string) error {
	return a.startBackupWithApproval(id, false, true)
}

func (a *App) startBackupApproved(id string) error {
	return a.startBackupWithApproval(id, true, true)
}

func (a *App) startBackupScheduled(id string) error {
	return a.startBackupWithApproval(id, false, false)
}

func (a *App) startBackupWithApproval(id string, approve, full bool) error {
	a.backupMu.Lock()
	defer a.backupMu.Unlock()
	if a.ctx.Err() != nil {
		return a.ctx.Err()
	}
	if _, running := a.backupRuns[id]; running {
		return errors.New("此备份正在执行，请等待完成")
	}
	st := a.store.snapshotWithLogLimit(0)
	var rule BackupRule
	for _, r := range st.BackupRules {
		if r.ID == id {
			rule = r
			break
		}
	}
	if rule.ID == "" {
		return errors.New("备份规则不存在")
	}
	if reason := backupHealthPause(st, rule); reason != "" {
		return errors.New(reason)
	}
	rule.forceFullSync = full
	if !rule.Enabled {
		return errors.New("备份规则已停用")
	}
	if approve {
		if rule.SyncPending == "" {
			return errors.New("没有待确认的同步删除计划")
		}
		rule.deleteApproval = rule.SyncPending
	}
	if rule.SyncPending != "" && !approve && !full {
		return errors.New("同步删除等待手动确认")
	}
	if err := validateBackup(&rule, st); err != nil {
		return err
	}
	sources, targets := backupLocations(rule)
	for _, location := range append(append([]BackupLocation{}, sources...), targets...) {
		storage, err := backupStorage(st, location.StorageID)
		if err != nil {
			return err
		}
		if err = storageAuthBlocked(storage, time.Now()); err != nil {
			return err
		}
	}
	parent, cancelParent := context.WithTimeout(a.ctx, 24*time.Hour)
	ctx, causeCancel := context.WithCancelCause(parent)
	cancel := func() { causeCancel(context.Canceled); cancelParent() }
	rule.Scanned, rule.Copied, rule.Skipped, rule.Deleted = 0, 0, 0, 0
	rule.Phase, rule.Total, rule.Processed = "scan", 0, 0
	if err := a.backupUpdate(id, func(r *BackupRule) {
		r.Status, r.Message, r.LastRun = "running", "扫描源目录", time.Now()
		r.Scanned, r.Copied, r.Skipped, r.Deleted = 0, 0, 0, 0
		r.Phase, r.Total, r.Processed = "scan", 0, 0
		r.NextRun = backupNext(*r, time.Now())
	}); err != nil {
		cancel()
		return err
	}
	if a.backupRuns == nil {
		a.backupRuns = map[string]context.CancelFunc{}
	}
	a.backupRuns[id] = cancel
	if a.backupHealthCancels == nil {
		a.backupHealthCancels = map[string]func(){}
	}
	a.backupHealthCancels[id] = func() { causeCancel(errStorageHealthPaused) }
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer cancel()
		err := a.executeBackup(ctx, &rule)
		status, level := "completed", "info"
		if err != nil {
			status, level = "failed", "error"
			if ctx.Err() != nil {
				status, level = "stopped", "warn"
			}
			if errors.Is(err, errBackupPaused) {
				status, level = "paused", "info"
			}
		}
		if errors.Is(context.Cause(ctx), errStorageHealthPaused) {
			status, level = "health-paused", "warn"
			err = errStorageHealthPaused
		}
		message := fmt.Sprintf("扫描 %d · 备份 %d · 跳过 %d · 清理 %d", rule.Scanned, rule.Copied, rule.Skipped, rule.Deleted)
		if err != nil {
			message += "：" + err.Error()
		}
		a.backupMu.Lock()
		defer a.backupMu.Unlock()
		if e := a.store.update(func(st *State) error {
			for i := range st.BackupRules {
				if st.BackupRules[i].ID != id {
					continue
				}
				r := &st.BackupRules[i]
				if status == "health-paused" && backupHealthPause(*st, *r) == "" {
					status, message = "idle", "健康已恢复，等待执行"
				}
				r.Status, r.Message = status, message
				r.Scanned, r.Copied, r.Skipped = rule.Scanned, rule.Copied, rule.Skipped
				r.Deleted = rule.Deleted
				r.Phase, r.Total, r.Processed = status, rule.Total, rule.Processed
				r.NextRun = backupNext(*r, time.Now())
			}
			return nil
		}); e != nil {
			a.logger.Printf("backup state save failed: %v", e)
		}
		delete(a.backupRuns, id)
		delete(a.backupHealthCancels, id)
		a.store.event(level, "files", rule.Name+"："+message)
	}()
	return nil
}

func backupAccept(r BackupRule, f File) bool {
	if r.Extensions != "" && !excludedType(f.Name, r.Extensions) {
		return false
	}
	if f.Size < r.MinSize || r.MaxSize > 0 && f.Size > r.MaxSize {
		return false
	}
	for _, word := range strings.Split(r.Exclude, ";") {
		if word = strings.TrimSpace(word); word != "" && strings.Contains(strings.ToLower(f.Name), strings.ToLower(word)) {
			return false
		}
	}
	return true
}

func sameBackupStorage(a, b Storage) bool {
	if a.Type != b.Type || len(a.Config) != len(b.Config) {
		return false
	}
	for key, value := range a.Config {
		if b.Config[key] != value {
			return false
		}
	}
	return true
}

func (a *App) copyBackupFile(ctx context.Context, source, target Storage, f File, parent, name, replace string, digest ...*string) (resultErr error) {
	ctx = context.WithValue(ctx, transferSourceKey{}, "备份")
	if err := a.checkBackupMutation(ctx); err != nil {
		return err
	}
	release, err := a.acquireTransferResource(ctx, "preprocess")
	if err != nil {
		return err
	}
	defer release()
	const limit = int64(100 << 30)
	if f.Size < 0 || f.Size > limit {
		return errors.New("备份单文件上限100GiB")
	}
	rootDir := a.transferTempDirectory()
	if err := os.MkdirAll(rootDir, 0700); err != nil {
		return err
	}
	cache, _ := ctx.Value(backupStageResourceKey{}).(*backupStageResource)
	reuse := cache != nil && a.transferConfig().CrossCloudTemp && source.Type != "local"
	stage := backupStageResource{}
	if reuse && cache.name != "" {
		stage = *cache
	} else {
		stage, err = a.stageBackupSource(ctx, source, f, rootDir)
		if err != nil {
			return err
		}
		if reuse {
			*cache = stage
		} else {
			defer os.Remove(stage.name)
		}
	}
	if len(digest) > 0 {
		*digest[0] = stage.digest
	}
	n := stage.size
	release()
	progress := a.beginTransfer(ctx, "upload", target, name, n,
		transferLocations{From: a.transferFileLocation(source, f.ID), To: a.transferDirectory(target, parent)})
	defer func() { progress.finish(resultErr) }()
	if err := progress.wait(); err != nil {
		return err
	}
	if progress != nil {
		ctx = context.WithValue(progress.ctx, transferProgressResourceKey{}, progress)
	}
	ctx = context.WithValue(ctx, transferUploadResourceKey{}, true)
	ctx = context.WithValue(ctx, transferAppResourceKey{}, a)
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if len(a.running) != 0 {
		return errors.New("有任务正在执行，请稍后备份")
	}
	for _, original := range []Storage{source, target} {
		current, err := a.store.storage(original.ID)
		if err != nil {
			return err
		}
		if !sameBackupStorage(original, current) {
			return errors.New("备份存储配置已变更，停止发布")
		}
	}
	if err := a.checkBackupMutation(ctx); err != nil {
		return err
	}
	if target.Type != "local" {
		old, err := a.cloudByName(ctx, target, parent, name)
		if err != nil {
			return err
		}
		if old != nil && replace == "skip" {
			return errors.New("目标文件在备份期间出现，请重新扫描")
		}
		if err := a.publishCloudFile(ctx, target, parent, name, stage.name); err != nil {
			return err
		}
	} else {
		root, err := os.OpenRoot(target.Config["root"])
		if err != nil {
			return err
		}
		defer root.Close()
		rel, err := relative(parent)
		if err != nil {
			return err
		}
		temp := path.Join(rel, ".aether-backup-"+id())
		out, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer root.Remove(temp)
		in, err := os.Open(stage.name)
		if err != nil {
			out.Close()
			return err
		}
		_, err = io.Copy(out, transferReader{&cancelReader{ctx, in}, progress})
		in.Close()
		if err == nil {
			err = out.Sync()
		}
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if replace == "skip" {
			if err := a.checkBackupMutation(ctx); err != nil {
				return err
			}
			err = root.Link(temp, path.Join(rel, name))
		} else {
			if err := a.checkBackupMutation(ctx); err != nil {
				return err
			}
			err = root.Rename(temp, path.Join(rel, name))
		}
		if err != nil {
			return err
		}
	}
	a.uploaded.Add(n)
	return nil
}
