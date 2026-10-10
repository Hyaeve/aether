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
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Enabled      bool             `json:"enabled"`
	SourceID     string           `json:"sourceId"`
	Source       string           `json:"source"`
	SourceLabel  string           `json:"sourceLabel"`
	TargetID     string           `json:"targetId"`
	Target       string           `json:"target"`
	TargetLabel  string           `json:"targetLabel"`
	Sources      []BackupLocation `json:"sources,omitempty"`
	Targets      []BackupLocation `json:"targets,omitempty"`
	Filters      []BackupFilter   `json:"filters,omitempty"`
	ScanInterval int              `json:"scanInterval"`
	Replace      string           `json:"replace"`
	Extensions   string           `json:"extensions"`
	Exclude      string           `json:"exclude"`
	MinSize      int64            `json:"minSize"`
	MaxSize      int64            `json:"maxSize"`
	Cron         string           `json:"cron"`
	Status       string           `json:"status"`
	Message      string           `json:"message"`
	Scanned      int              `json:"scanned"`
	Copied       int              `json:"copied"`
	Skipped      int              `json:"skipped"`
	Phase        string           `json:"phase"`
	Total        int              `json:"total"`
	Processed    int              `json:"processed"`
	LastRun      time.Time        `json:"lastRun"`
	NextRun      time.Time        `json:"nextRun"`
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
	if err := validateBackupOptions(r, st); err != nil {
		return err
	}
	sources, targets := backupLocations(*r)
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
		rules := a.store.snapshotWithLogLimit(0).BackupRules
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
			input.Scanned, input.Copied, input.Skipped = 0, 0, 0
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
				input.Phase, input.Total, input.Processed = old.Phase, old.Total, old.Processed
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
	a.backupMu.Lock()
	defer a.backupMu.Unlock()
	if a.ctx.Err() != nil {
		return a.ctx.Err()
	}
	if len(a.backupRuns) != 0 {
		return errors.New("已有备份正在执行，请等待完成")
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
	if !rule.Enabled {
		return errors.New("备份规则已停用")
	}
	if err := validateBackup(&rule, st); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 24*time.Hour)
	rule.Scanned, rule.Copied, rule.Skipped = 0, 0, 0
	rule.Phase, rule.Total, rule.Processed = "scan", 0, 0
	if err := a.backupUpdate(id, func(r *BackupRule) {
		r.Status, r.Message, r.LastRun = "running", "扫描源目录", time.Now()
		r.Scanned, r.Copied, r.Skipped = 0, 0, 0
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
		}
		message := fmt.Sprintf("扫描 %d · 备份 %d · 跳过 %d", rule.Scanned, rule.Copied, rule.Skipped)
		if err != nil {
			message += "：" + err.Error()
		}
		a.backupMu.Lock()
		defer a.backupMu.Unlock()
		if e := a.backupUpdate(id, func(r *BackupRule) {
			r.Status, r.Message = status, message
			r.Scanned, r.Copied, r.Skipped = rule.Scanned, rule.Copied, rule.Skipped
			r.Phase, r.Total, r.Processed = status, rule.Total, rule.Processed
			r.NextRun = backupNext(*r, time.Now())
		}); e != nil {
			a.logger.Printf("backup state save failed: %v", e)
		}
		delete(a.backupRuns, id)
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

func (a *App) copyBackupFile(ctx context.Context, source, target Storage, f File, parent, name, replace string) (resultErr error) {
	const limit = int64(100 << 30)
	if f.Size < 0 || f.Size > limit {
		return errors.New("备份单文件上限100GiB")
	}
	rootDir := filepath.Join(a.dataDir, "cache", "backup")
	if err := os.MkdirAll(rootDir, 0700); err != nil {
		return err
	}
	staged, err := os.CreateTemp(rootDir, "backup-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	var input io.ReadCloser
	var downloadProgress *transferProgress
	if source.Type == "local" {
		var root *os.Root
		root, err = os.OpenRoot(source.Config["root"])
		if err == nil {
			var rel string
			rel, err = relative(f.ID)
			if err == nil {
				input, err = root.Open(rel)
			}
			root.Close()
		}
	} else {
		var download Download
		download, err = a.download(ctx, source, f.ID, f.PickCode)
		if err == nil {
			downloadProgress = a.beginTransfer(ctx, "download", source, f.Name, f.Size)
			reader := &davFile{ctx: ctx, info: davInfo{f}, download: download, progress: downloadProgress}
			if source.Type == "115" {
				reader.refreshDownload = func() (Download, error) {
					info, err := download115API(ctx, source, f.PickCode, pan115ReadUA, true)
					if err != nil {
						return Download{}, err
					}
					return Download{URL: info.Url.Url, Headers: info.Header}, nil
				}
			}
			input = reader
		}
	}
	if err != nil {
		return err
	}
	defer input.Close()
	defer func() { downloadProgress.finish(resultErr) }()
	n, err := io.Copy(staged, io.LimitReader(&cancelReader{ctx, input}, limit+1))
	if err != nil {
		return err
	}
	if n != f.Size || n > limit {
		return errors.New("源文件内容不完整或读取期间大小变化")
	}
	downloadProgress.finish(nil)
	if err := staged.Close(); err != nil {
		return err
	}
	progress := a.beginTransfer(ctx, "upload", target, name, n)
	defer func() { progress.finish(resultErr) }()
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
	if err := ctx.Err(); err != nil {
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
		if err := a.publishCloudFile(ctx, target, parent, name, staged.Name()); err != nil {
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
		in, err := os.Open(staged.Name())
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
			err = root.Link(temp, path.Join(rel, name))
		} else {
			err = root.Rename(temp, path.Join(rel, name))
		}
		if err != nil {
			return err
		}
	}
	a.uploaded.Add(n)
	return nil
}
