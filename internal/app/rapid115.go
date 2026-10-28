package app

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Rapid115Job struct {
	Filters       []BackupFilter `json:"filters,omitempty"`
	Source        string         `json:"source"`
	StorageID     string         `json:"storageId"`
	Target        string         `json:"target"`
	TargetLabel   string         `json:"targetLabel,omitempty"`
	Extensions    string         `json:"extensions"`
	SuccessAction string         `json:"successAction"`
	FailureAction string         `json:"failureAction"`
}
type Rapid115Watch struct {
	Rapid115Job
	ID      string `json:"id"`
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	Cron    string `json:"cron"`
	Enabled bool   `json:"enabled"`
}
type Rapid115Settings struct {
	Manual  Rapid115Job     `json:"manual"`
	Watches []Rapid115Watch `json:"watches"`
}
type Rapid115Record struct {
	WatchID    string    `json:"watchId,omitempty"`
	SourceKind string    `json:"sourceKind,omitempty"`
	ID         string    `json:"id"`
	Time       time.Time `json:"time"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	PickCode   string    `json:"pickcode"`
	Status     string    `json:"status"`
	Source     string    `json:"source"`
	Message    string    `json:"message,omitempty"`
	Retries    int       `json:"retries,omitempty"`
	NextRetry  time.Time `json:"nextRetry,omitempty"`
	Done       int64     `json:"done,omitempty"`
	Total      int64     `json:"total,omitempty"`
	Phase      string    `json:"phase,omitempty"`
}

type rapid115Retry struct {
	WatchID  string    `json:"watchId"`
	Revision string    `json:"revision"`
	RecordID string    `json:"recordId"`
	Count    int       `json:"count"`
	Next     time.Time `json:"next"`
	Digest   string    `json:"digest,omitempty"`
}

type rapid115KnownFailure struct{ error }

func (e *rapid115KnownFailure) Unwrap() error { return e.error }

func (a *App) rapid115Time() time.Time {
	if a.rapid115Now != nil {
		return a.rapid115Now()
	}
	return time.Now()
}

func normalizeRapid115Job(j Rapid115Job) Rapid115Job {
	rule := BackupRule{Filters: append([]BackupFilter(nil), j.Filters...)}
	if validateBackupFilters(&rule) == nil {
		j.Filters = rule.Filters
	}
	if j.SuccessAction == "" {
		j.SuccessAction = "delete"
	}
	if j.FailureAction == "" {
		j.FailureAction = "skip"
	}
	tokens := strings.FieldsFunc(strings.ToLower(j.Extensions), func(r rune) bool { return r == ';' || r == ',' || r == '，' || r == ' ' || r == '\n' })
	seen := map[string]bool{}
	clean := []string{}
	for _, v := range tokens {
		v = strings.TrimPrefix(v, ".")
		if v != "" && !seen[v] {
			seen[v] = true
			clean = append(clean, v)
		}
	}
	j.Extensions = strings.Join(clean, ";")
	return j
}
func normalizedRapid115(s Rapid115Settings) Rapid115Settings {
	s.Manual = normalizeRapid115Job(s.Manual)
	if s.Watches == nil {
		s.Watches = []Rapid115Watch{}
	}
	for i := range s.Watches {
		s.Watches[i].Rapid115Job = normalizeRapid115Job(s.Watches[i].Rapid115Job)
	}
	return s
}
func rapid115Stamp(v any) string {
	if settings, ok := v.(Rapid115Settings); ok {
		settings.Watches = append([]Rapid115Watch(nil), settings.Watches...)
		sort.Slice(settings.Watches, func(i, j int) bool { return settings.Watches[i].ID < settings.Watches[j].ID })
		v = settings
	}
	return rapid115RawStamp(v)
}
func rapid115RawStamp(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func validateRapid115Job(j Rapid115Job, st State, complete bool, verifyFiles ...bool) error {
	rule := BackupRule{Filters: append([]BackupFilter(nil), j.Filters...)}
	if err := validateBackupFilters(&rule); err != nil {
		return err
	}
	verify := len(verifyFiles) == 0 || verifyFiles[0]
	if j.SuccessAction != "delete" && j.SuccessAction != "keep" || j.FailureAction != "skip" && j.FailureAction != "upload" && j.FailureAction != "delete" {
		return errors.New("秒传处理方式无效")
	}
	if len(j.Extensions) > 4096 || len(j.TargetLabel) > 2048 {
		return errors.New("秒传设置过长")
	}
	for _, ext := range strings.Split(j.Extensions, ";") {
		if strings.ContainsAny(ext, "/\\:\x00") {
			return errors.New("扩展名无效")
		}
	}
	if j.Source != "" {
		if !filepath.IsAbs(j.Source) || filepath.Clean(j.Source) != j.Source || filepath.Dir(j.Source) == j.Source {
			return errors.New("待传目录须为容器内绝对路径")
		}
		if verify {
			fi, err := os.Lstat(j.Source)
			if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
				return errors.New("待传目录不存在或不是普通目录")
			}
			resolved, err := filepath.EvalSymlinks(j.Source)
			if err != nil || resolved != j.Source {
				return errors.New("待传目录不可包含符号链接")
			}
		}
	}
	if j.StorageID != "" && verify {
		found := false
		for _, s := range st.Storages {
			if s.ID == j.StorageID && s.Enabled && s.Type == "115" {
				found = true
			}
		}
		if !found {
			return errors.New("请选择已启用的115存储")
		}
	}
	if j.Target != "" && !simulcastID(j.Target) && j.Target != "/" {
		return errors.New("目标目录无效")
	}
	if complete && (j.Source == "" || j.StorageID == "" || j.Target == "") {
		return errors.New("请选择待传目录和115目标目录")
	}
	return nil
}
func validateRapid115(s Rapid115Settings, st State, verifyFiles ...bool) error {
	if s.Manual.FailureAction == "delete" {
		return errors.New("重试后删除仅适用于监控秒传")
	}
	if err := validateRapid115Job(s.Manual, st, false, verifyFiles...); err != nil {
		return err
	}
	if len(s.Watches) > 64 {
		return errors.New("最多添加64个监控")
	}
	seen := map[string]bool{}
	for _, w := range s.Watches {
		if w.ID == "" || len(w.ID) > 128 || seen[w.ID] || strings.TrimSpace(w.Name) == "" || len(w.Name) > 128 {
			return errors.New("监控名称或标识无效")
		}
		seen[w.ID] = true
		if err := validateRapid115Job(w.Rapid115Job, st, true, verifyFiles...); err != nil {
			return err
		}
		if w.Mode == "cron" {
			if len(w.Cron) > 128 {
				return errors.New("Cron表达式过长")
			}
			legacySchedule := len(verifyFiles) > 1 && verifyFiles[1]
			parser := rawCronParser.Parse
			if !legacySchedule && w.FailureAction == "skip" {
				parser = cronParser.Parse
			}
			if _, err := parser(w.Cron); err != nil {
				return err
			}
			if !legacySchedule && (w.FailureAction == "upload" || w.FailureAction == "delete") {
				if err := validateRapid115RetryCron(w.Cron); err != nil {
					return err
				}
			}
		} else if w.Mode != "watch" {
			return errors.New("监控模式无效")
		}
	}
	return nil
}
func (a *App) rapid115Settings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		jsonResponse(w, 200, normalizedRapid115(a.store.snapshotWithLogLimit(0).Rapid115))
	case http.MethodPut:
		var value Rapid115Settings
		if !decode(w, r, &value) {
			return
		}
		value = normalizedRapid115(value)
		if err := validateRapid115(value, a.store.snapshotWithLogLimit(0)); err != nil {
			fail(w, 400, err)
			return
		}
		err := a.store.update(func(st *State) error {
			// Filesystem probes can block on mounts; never perform them under the store lock.
			if err := validateRapid115(value, *st, false); err != nil {
				return err
			}
			for _, job := range append([]Rapid115Job{value.Manual}, rapid115WatchJobs(value.Watches)...) {
				if job.StorageID == "" {
					continue
				}
				found := false
				for _, s := range st.Storages {
					if s.ID == job.StorageID && s.Type == "115" && s.Enabled {
						found = true
					}
				}
				if !found {
					return errors.New("请选择已启用的115存储")
				}
			}
			st.Rapid115 = value
			revision := rapid115Stamp(value)
			for key, retry := range st.Rapid115Retries {
				if retry.Revision != revision {
					delete(st.Rapid115Retries, key)
					for i := range st.Rapid115Records {
						record := &st.Rapid115Records[i]
						if record.ID == retry.RecordID && record.Status == "retrying" {
							record.Status = "interrupted"
							record.NextRetry = time.Time{}
							record.Message = "监控配置已变化，待重试已取消"
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			fail(w, 400, err)
			return
		}
		jsonResponse(w, 200, value)
	default:
		w.Header().Set("Allow", "GET, PUT")
		w.WriteHeader(405)
	}
}
func rapid115WatchJobs(watches []Rapid115Watch) []Rapid115Job {
	jobs := make([]Rapid115Job, 0, len(watches))
	for _, w := range watches {
		jobs = append(jobs, w.Rapid115Job)
	}
	return jobs
}
func (a *App) rapid115Records(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a.rapid115Mu.Lock()
	manualRunning, monitorRunning := a.rapid115Runs["manual"], a.rapid115Runs["monitor"]
	a.rapid115Mu.Unlock()
	pageSize := 30
	watchID := r.URL.Query().Get("watch")
	if watchID != "" {
		pageSize = 10
	}
	a.store.mu.RLock()
	filtered := make([]Rapid115Record, 0)
	for _, record := range a.store.state.Rapid115Records {
		if watchID == "" || record.WatchID == watchID {
			filtered = append(filtered, record)
		}
	}
	total := len(filtered)
	pages := max(1, (total+pageSize-1)/pageSize)
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil {
		page = 1
	}
	page = max(1, min(page, pages))
	start := min((page-1)*pageSize, total)
	items := append([]Rapid115Record{}, filtered[start:min(start+pageSize, total)]...)
	a.store.mu.RUnlock()
	for i := range items {
		items[i] = a.rapid115RecordProgress(items[i])
	}
	jsonResponse(w, 200, map[string]any{"items": items, "running": manualRunning || monitorRunning, "manualRunning": manualRunning, "monitorRunning": monitorRunning, "page": page, "pages": pages, "total": total, "pageSize": pageSize})
}
func (a *App) rapid115Start(w http.ResponseWriter, r *http.Request) {
	var value struct {
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &value) {
		return
	}
	if !value.Confirm {
		fail(w, 400, errors.New("请确认开始秒传"))
		return
	}
	st := a.store.snapshotWithLogLimit(0)
	job := normalizedRapid115(st.Rapid115).Manual
	if err := validateRapid115Job(job, st, true); err != nil {
		fail(w, 400, err)
		return
	}
	if !a.startRapid115(job, "manual", "手动", rapid115Stamp(st.Rapid115)) {
		fail(w, 409, errors.New("秒传正在执行，请稍后再试"))
		return
	}
	jsonResponse(w, 202, map[string]bool{"started": true})
}
func (a *App) startRapid115(job Rapid115Job, key, label, revision string) bool {
	a.rapid115Mu.Lock()
	defer a.rapid115Mu.Unlock()
	channel := "monitor"
	if key == "manual" {
		channel = "manual"
	}
	if a.rapid115Runs[channel] || a.ctx.Err() != nil {
		return false
	}
	if a.rapid115Runs == nil {
		a.rapid115Runs = map[string]bool{}
	}
	a.rapid115Runs[channel] = true
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer func() { a.rapid115Mu.Lock(); delete(a.rapid115Runs, channel); a.rapid115Mu.Unlock() }()
		if err := a.runRapid115(a.ctx, job, key, label, revision); err != nil && a.ctx.Err() == nil {
			a.store.event("error", "tasks", "115秒传："+err.Error())
		}
	}()
	return true
}
func (a *App) rapid115Current(job Rapid115Job, storage Storage, revision string) bool {
	st := a.store.snapshotWithLogLimit(0)
	if rapid115Stamp(st.Rapid115) != revision {
		return false
	}
	for _, s := range st.Storages {
		if s.ID == job.StorageID {
			return s.Enabled && s.Type == "115" && !storageHealthPaused(s) && reflect.DeepEqual(s.Config, storage.Config)
		}
	}
	return false
}
func rapid115Digest(ctx context.Context, f *os.File) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	h := sha1.New()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(h, rapid115ReadProgress(ctx, upload115Reader{ctx, f}, "计算文件摘要", 0, info.Size())); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil))), nil
}
func rapid115Unchanged(root *os.Root, name string, identity os.FileInfo) bool {
	current, err := root.Lstat(name)
	return err == nil && current.Mode().IsRegular() && os.SameFile(current, identity) && current.Size() == identity.Size() && current.ModTime().Equal(identity.ModTime())
}

// Journal the intent before contacting the cloud. Ambiguous writes are not replayed,
// even after a restart; a changed local file or changed job is a new attempt.
func (a *App) rapid115Intent(key string, record Rapid115Record) (bool, error) {
	fresh := false
	err := a.store.update(func(st *State) error {
		if st.Rapid115Attempts[key] {
			return nil
		}
		if retry, ok := st.Rapid115Retries[key]; ok {
			for i := range st.Rapid115Records {
				previous := &st.Rapid115Records[i]
				if previous.ID == retry.RecordID {
					previous.Status = "failed"
					previous.NextRetry = time.Time{}
					previous.Message = fmt.Sprintf("秒传未成功，已进入第%d次重试", retry.Count)
				}
			}
		}
		if len(st.Rapid115Attempts) >= 100000 {
			return errors.New("秒传去重记录达到上限，请整理监控目录")
		}
		if st.Rapid115Attempts == nil {
			st.Rapid115Attempts = map[string]bool{}
		}
		st.Rapid115Attempts[key] = true
		st.Rapid115Records = append([]Rapid115Record{record}, st.Rapid115Records...)
		if len(st.Rapid115Records) > 5000 {
			st.Rapid115Records = st.Rapid115Records[:5000]
		}
		fresh = true
		return nil
	})
	return fresh, err
}
func (a *App) rapid115Finish(record Rapid115Record) error {
	record = a.rapid115RecordProgress(record)
	return a.store.update(func(st *State) error {
		for i := range st.Rapid115Records {
			if st.Rapid115Records[i].ID == record.ID {
				st.Rapid115Records[i] = record
				break
			}
		}
		return nil
	})
}
func (a *App) runRapid115(ctx context.Context, job Rapid115Job, key, label, revision string) error {
	st := a.store.snapshotWithLogLimit(0)
	if err := validateRapid115Job(job, st, true); err != nil {
		return err
	}
	filters, err := compileBackupFilters(job.Filters)
	if err != nil {
		return err
	}
	for _, protected := range []string{a.store.dir, filepath.Join(a.dataDir, "runtime"), filepath.Join(a.dataDir, "cache"), filepath.Join(a.dataDir, "log")} {
		protected, err := filepath.Abs(protected)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(job.Source, protected)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("待传目录不可包含系统配置或运行数据")
		}
		inside, err := filepath.Rel(protected, job.Source)
		if err == nil && inside != ".." && !strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			return errors.New("待传目录不可位于系统配置或运行数据中")
		}
	}
	storage, err := backupStorage(st, job.StorageID)
	if err != nil {
		return err
	}
	if !a.rapid115Current(job, storage, revision) {
		return errors.New("秒传设置或存储状态已变化")
	}
	root, err := os.OpenRoot(job.Source)
	if err != nil {
		return err
	}
	defer root.Close()
	scope, err := os.Stat(job.Source)
	if err != nil {
		return err
	}
	count := 0
	ancestors := map[string]os.FileInfo{".": scope}
	monitor, cron := false, false
	for _, watch := range st.Rapid115.Watches {
		if watch.ID == key {
			monitor, cron = true, watch.Mode == "cron"
		}
	}
	if !monitor && job.FailureAction == "delete" {
		return errors.New("重试后删除仅适用于监控秒传")
	}
	return fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 100000 {
			return errors.New("待传目录超过100000项")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if name != "." && backupNameExcluded(filters, File{Name: d.Name(), IsDir: d.IsDir()}, name) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if name != "." && !backupFilterAccept(filters, File{Name: d.Name(), IsDir: true}, name) {
				return fs.SkipDir
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			ancestors[name] = fi
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if !fi.Mode().IsRegular() || strings.HasPrefix(d.Name(), ".aether-") || a.transferIgnored(d.Name()) {
			return nil
		}
		if !backupFilterAccept(filters, File{Name: d.Name(), Size: fi.Size()}, name) {
			return nil
		}
		if job.Extensions != "" {
			ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
			if !strings.Contains(";"+job.Extensions+";", ";"+ext+";") {
				return nil
			}
		}
		if label != "手动" && time.Since(fi.ModTime()) < 2*time.Second {
			return nil
		}
		if !a.rapid115Current(job, storage, revision) {
			return errors.New("秒传设置或存储状态已变化，已停止")
		}
		localFile := filepath.Join(job.Source, name)
		a.rapid115Mu.Lock()
		if a.rapid115Files[localFile] {
			a.rapid115Mu.Unlock()
			a.store.event("warn", "tasks", fmt.Sprintf("115秒传：%s；%s；另一秒传任务正在处理此文件，未重复提交", label, filepath.Base(name)))
			return nil
		}
		if a.rapid115Files == nil {
			a.rapid115Files = map[string]bool{}
		}
		a.rapid115Files[localFile] = true
		a.rapid115Mu.Unlock()
		defer func() { a.rapid115Mu.Lock(); delete(a.rapid115Files, localFile); a.rapid115Mu.Unlock() }()
		record := Rapid115Record{ID: id(), Time: time.Now(), Name: filepath.Base(name), Size: fi.Size(), Status: "running", Source: label}
		record.SourceKind = "manual"
		if key != "manual" {
			record.WatchID, record.SourceKind = key, "monitor"
		}
		attempt := rapid115Stamp([]any{key, job, storage.Config, name, fi.Size(), fi.ModTime().UnixNano()})
		expectedDigest := ""
		if retry, ok := a.store.snapshotWithLogLimit(0).Rapid115Retries[attempt]; ok {
			if retry.Count < 1 || retry.Count > 3 {
				return errors.New("重试记录无效，未执行")
			}
			if a.rapid115Time().Before(retry.Next) {
				return nil
			}
			record.Retries = retry.Count
			expectedDigest = retry.Digest
		}
		fresh, err := a.rapid115Intent(attempt, record)
		if err != nil {
			return err
		}
		if !fresh {
			return nil
		}
		recordID, reporting := record.ID, true
		progressCtx := context.WithValue(ctx, rapid115ReportKey{}, func(phase string, done, total int64) {
			a.rapid115Mu.Lock()
			defer a.rapid115Mu.Unlock()
			if !reporting {
				return
			}
			if a.rapid115Progress == nil {
				a.rapid115Progress = map[string]rapid115Live{}
			}
			a.rapid115Progress[recordID] = rapid115Live{Phase: phase, Done: done, Total: total}
		})
		defer func() {
			a.rapid115Mu.Lock()
			reporting = false
			delete(a.rapid115Progress, recordID)
			a.rapid115Mu.Unlock()
		}()
		rapid115Report(progressCtx, "等待传输资源", 0, 0)
		record.Status = "failed"
		missed := false
		sourceDigest := ""
		uploadErr := func() error {
			ctx, cancel := context.WithTimeout(progressCtx, 30*time.Minute)
			defer cancel()
			ctx, releaseUpload, err := a.controlCloudUpload(ctx, name, filepath.Join(job.Source, name))
			if err != nil {
				return err
			}
			defer releaseUpload()
			releaseHash, err := a.controlPreprocess(ctx)
			if err != nil {
				return err
			}
			defer releaseHash()
			f, err := root.Open(name)
			if err != nil {
				return err
			}
			defer f.Close()
			opened, err := f.Stat()
			if err != nil || !os.SameFile(fi, opened) || !rapid115Unchanged(root, name, fi) {
				return errors.New("源文件已变化，未处理")
			}
			sha, err := rapid115Digest(ctx, f)
			if err != nil {
				return err
			}
			if !rapid115Unchanged(root, name, fi) {
				return errors.New("计算期间源文件已变化")
			}
			sourceDigest = sha
			if expectedDigest != "" && sourceDigest != expectedDigest {
				return errors.New("等待重试期间源文件内容已变化，已停止并保留源文件")
			}
			parent, base, err := a.uploadParent(ctx, storage, job.Target, filepath.ToSlash(name))
			if err != nil {
				return err
			}
			if !a.rapid115Current(job, storage, revision) {
				return errors.New("存储或设置已变化")
			}
			// Never overwrite or adopt a same-name cloud object.
			files, err := a.rawList(ctx, storage, parent)
			if err != nil {
				return err
			}
			for _, existing := range files {
				if strings.EqualFold(existing.Name, base) {
					return errors.New("目标已有同名文件，未覆盖或清理源文件")
				}
			}
			if !a.rapid115Current(job, storage, revision) {
				return errors.New("存储或设置已变化")
			}
			call := a.rapid115Upload
			if call == nil {
				call = a.upload115RapidOnly
			}
			allowUpload := job.FailureAction == "upload" && (!monitor || record.Retries == 3)
			rapid115Report(ctx, "提交秒传", 0, 0)
			rapid, pick, err := call(ctx, storage, parent, base, f, fi.Size(), allowUpload)
			if err != nil {
				var known *rapid115KnownFailure
				missed = errors.As(err, &known) && ctx.Err() == nil
				return err
			}
			if !rapid && pick == "" {
				missed = true
				if monitor && job.FailureAction == "delete" && record.Retries == 3 {
					if err := a.rapid115RemoveSource(ctx, job, storage, revision, root, scope, ancestors, name, fi, f, sha); err != nil {
						return err
					}
					record.Status, record.Message = "deleted", "重试3次秒传仍未命中，已删除源文件"
					return nil
				}
				return errors.New("未命中秒传，已跳过并保留源文件")
			}
			if pick == "" {
				return errors.New("115未返回可校验文件，已保留源文件")
			}
			record.PickCode = pick
			record.Status = "success"
			record.Message = "秒传成功"
			if record.Retries > 0 {
				record.Message = fmt.Sprintf("重试%d次秒传成功", record.Retries)
			}
			if !rapid {
				record.Status = "uploaded"
				record.Message = "传输成功"
			}
			if err := a.rapid115Finish(record); err != nil {
				return err
			}
			if job.SuccessAction == "delete" {
				if err := a.rapid115RemoveSource(ctx, job, storage, revision, root, scope, ancestors, name, fi, f, sha); err != nil {
					return err
				}
			}
			return nil
		}()
		record = a.rapid115RecordProgress(record)
		if uploadErr != nil {
			record.Message = "处理失败；源文件保留，请检查目标后再手动处理"
			if record.Status == "failed" {
				record.Message = "秒传未成功，源文件保留；请检查存储授权、目标重名或秒传命中情况"
			}
		}
		if err := a.store.update(func(st *State) error {
			delete(st.Rapid115Retries, attempt)
			if missed && monitor && a.rapid115CurrentSnapshot(*st, job, storage, revision) {
				retries := job.FailureAction == "upload" || job.FailureAction == "delete"
				if retries && record.Retries < 3 && len(st.Rapid115Retries) < 100000 {
					if st.Rapid115Retries == nil {
						st.Rapid115Retries = map[string]rapid115Retry{}
					}
					delay := []time.Duration{10 * time.Minute, 30 * time.Minute, 60 * time.Minute}[record.Retries]
					record.NextRetry = a.rapid115Time().Add(delay)
					st.Rapid115Retries[attempt] = rapid115Retry{WatchID: key, Revision: revision, RecordID: record.ID, Count: record.Retries + 1, Next: record.NextRetry, Digest: sourceDigest}
					record.Status = "retrying"
					record.Message = fmt.Sprintf("秒传未成功，%d分钟后进行第%d次重试", int(delay/time.Minute), record.Retries+1)
					delete(st.Rapid115Attempts, attempt)
				} else if retries && record.Retries < 3 {
					record.Message = "待重试记录已达100000项上限，源文件保留，请核对后处理"
				} else if cron && job.FailureAction == "skip" {
					delete(st.Rapid115Attempts, attempt)
				}
			}
			for i := range st.Rapid115Records {
				if st.Rapid115Records[i].ID == record.ID {
					st.Rapid115Records[i] = record
					break
				}
			}
			return nil
		}); err != nil {
			return err
		}
		level := "info"
		if record.Status == "failed" || record.Status == "deleted" {
			level = "error"
		} else if record.Status == "retrying" {
			level = "warn"
		}
		a.store.event(level, "tasks", fmt.Sprintf("115秒传：%s；存储池 %s；%s；%s", label, storage.Name, record.Name, record.Message))
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	})
}

func (a *App) rapid115RemoveSource(ctx context.Context, job Rapid115Job, storage Storage, revision string, root *os.Root, scope os.FileInfo, ancestors map[string]os.FileInfo, name string, identity os.FileInfo, file *os.File, sha string) error {
	latest, err := os.Stat(job.Source)
	if err != nil || !os.SameFile(scope, latest) || !a.rapid115Current(job, storage, revision) || !rapid115Unchanged(root, name, identity) {
		return errors.New("源文件或目录已变化，已保留源文件")
	}
	nowSHA, err := rapid115Digest(ctx, file)
	if err != nil || nowSHA != sha || !rapid115Unchanged(root, name, identity) {
		return errors.New("源文件内容已变化，已保留")
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Join(job.Source, name)); err != nil || resolved != filepath.Join(job.Source, name) {
		return errors.New("源路径已变化，已保留")
	}
	for parent := filepath.Dir(name); ; parent = filepath.Dir(parent) {
		current, err := root.Lstat(parent)
		old := ancestors[parent]
		if err != nil || old == nil || !os.SameFile(current, old) || current.Mode()&os.ModeSymlink != 0 {
			return errors.New("源父目录已变化，已保留")
		}
		if parent == "." {
			break
		}
	}
	if ctx.Err() != nil || !a.rapid115Current(job, storage, revision) {
		return errors.New("秒传已取消或配置已变化，源文件保留")
	}
	if err := root.Remove(name); err != nil {
		return errors.New("源文件清理失败，未继续删除")
	}
	return nil
}

func (a *App) rapid115CurrentSnapshot(st State, job Rapid115Job, storage Storage, revision string) bool {
	if rapid115Stamp(st.Rapid115) != revision {
		return false
	}
	for _, s := range st.Storages {
		if s.ID == storage.ID {
			return s.Enabled && s.Type == "115" && !storageHealthPaused(s) && reflect.DeepEqual(s.Config, storage.Config)
		}
	}
	return false
}

func (a *App) upload115RapidOnly(ctx context.Context, s Storage, parent, name string, f *os.File, size int64, allowUpload bool) (bool, string, error) {
	c, err := client115(ctx, s)
	if err != nil {
		return false, "", &rapid115KnownFailure{err}
	}
	if _, err = c.UploadAvailable(); err != nil {
		return false, "", &rapid115KnownFailure{err}
	}
	if size > c.UploadMetaInfo.SizeLimit {
		return false, "", errors.New("文件超过115上传限制")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return false, "", err
	}
	hashCtx := context.WithValue(ctx, rapid115ReadStageKey{}, rapid115ReadStage{"计算秒传摘要", size})
	digest, err := c.GetDigestResult(upload115Reader{hashCtx, f})
	if err != nil {
		return false, "", err
	}
	if err = a.waitAPI(ctx, s.ID); err != nil {
		return false, "", &rapid115KnownFailure{err}
	}
	rapid115Report(ctx, "提交秒传", 0, 0)
	init, err := c.RapidUpload(size, name, parent, digest.PreID, digest.QuickID, upload115Reader{ctx, f})
	if err != nil {
		return false, "", errors.New("115秒传提交未确认，未自动重试")
	}
	if init == nil {
		return false, "", errors.New("115秒传响应为空，未自动重试")
	}
	rapid, err := init.Ok()
	if err != nil {
		return false, "", errors.New("115秒传返回异常，未自动重试")
	}
	if !rapid {
		if !allowUpload {
			return false, "", nil
		}
		token, err := c.GetOSSToken()
		if err != nil {
			return false, "", err
		}
		if err = upload115OSS(ctx, f, size, &init.UploadOSSParams, token); err != nil {
			return false, "", errors.New("115本地上传未确认，请检查目标")
		}
	}
	defer a.cache.clear()
	rapid115Report(ctx, "校验云端结果", 0, 0)
	for i := 0; i < 5; i++ {
		files, err := a.simulcast.list(ctx, s, c, parent)
		if err != nil {
			return rapid, "", err
		}
		matches := []string{}
		for _, file := range files {
			if file.Name == name && !file.IsDirectory && file.Size == size && strings.EqualFold(file.Sha1, digest.QuickID) && file.PickCode != "" {
				matches = append(matches, file.PickCode)
			}
		}
		if len(matches) == 1 {
			return rapid, matches[0], nil
		}
		if len(matches) > 1 {
			return rapid, "", errors.New("115返回多个同名结果，已保留源文件")
		}
		select {
		case <-ctx.Done():
			return rapid, "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return rapid, "", errors.New("无法校验目标文件，已保留源文件")
}

// Native filesystem events are debounced; reconciliation catches newly created
// subdirectories and missed notifications. Unchanged files keep their durable intent.
func (a *App) rapid115Monitor() {
	defer a.wg.Done()
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		a.store.event("error", "tasks", "115文件监控启动失败")
		return
	}
	defer watcher.Close()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	revision := ""
	watched := map[string]bool{}
	pending := map[string]time.Time{}
	next := map[string]time.Time{}
	lastReconcile := time.Time{}
	refresh := func(st State, now time.Time) {
		for dir := range watched {
			_ = watcher.Remove(dir)
		}
		watched = map[string]bool{}
		for _, w := range st.Rapid115.Watches {
			if !w.Enabled || w.Mode != "watch" {
				continue
			}
			entries := 0
			filters, filterErr := compileBackupFilters(w.Filters)
			if filterErr != nil {
				continue
			}
			_ = filepath.WalkDir(w.Source, func(name string, d fs.DirEntry, e error) error {
				if err := a.ctx.Err(); err != nil {
					return err
				}
				entries++
				if entries > 100000 {
					return errors.New("监控目录超过100000项")
				}
				if e != nil {
					return fs.SkipDir
				}
				relative, _ := filepath.Rel(w.Source, name)
				if relative != "." && backupNameExcluded(filters, File{Name: d.Name(), IsDir: d.IsDir()}, filepath.ToSlash(relative)) {
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
				if len(watched) >= 10000 {
					return fs.SkipDir
				}
				if d.Type()&os.ModeSymlink != 0 {
					return nil
				}
				if d.IsDir() {
					if !watched[name] && watcher.Add(name) == nil {
						watched[name] = true
					}
				}
				return nil
			})
		}
		lastReconcile = now
	}
	for {
		select {
		case <-a.ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
				st := a.store.snapshotWithLogLimit(0)
				for _, w := range st.Rapid115.Watches {
					rel, e := filepath.Rel(w.Source, event.Name)
					if w.Enabled && w.Mode == "watch" && e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
						filters, err := compileBackupFilters(w.Filters)
						if err != nil {
							continue
						}
						excluded := false
						for p := rel; p != "."; p = filepath.Dir(p) {
							if backupNameExcluded(filters, File{Name: filepath.Base(p), IsDir: p != rel}, filepath.ToSlash(p)) {
								excluded = true
								break
							}
						}
						if excluded {
							continue
						}
						pending[w.ID] = time.Now().Add(2 * time.Second)
					}
				}
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
			lastReconcile = time.Time{}
		case now := <-ticker.C:
			st := a.store.snapshotWithLogLimit(0)
			current := rapid115Stamp(st.Rapid115)
			if current != revision {
				revision = current
				pending = map[string]time.Time{}
				next = map[string]time.Time{}
				for _, w := range st.Rapid115.Watches {
					if !w.Enabled {
						continue
					}
					if w.Mode == "cron" && (w.FailureAction == "upload" || w.FailureAction == "delete") {
						if err := validateRapid115RetryCron(w.Cron); err != nil {
							a.store.event("warn", "tasks", "115监控 "+w.Name+" 已暂停："+err.Error())
							continue
						}
					}
					if w.Mode == "watch" {
						pending[w.ID] = now.Add(2 * time.Second)
					} else if schedule, e := cronParser.Parse(w.Cron); e == nil {
						next[w.ID] = schedule.Next(now)
					}
				}
				refresh(st, now)
			}
			if now.Sub(lastReconcile) >= 30*time.Second {
				refresh(st, now)
				for _, w := range st.Rapid115.Watches {
					if w.Enabled && w.Mode == "watch" {
						if _, ok := pending[w.ID]; !ok {
							pending[w.ID] = now
						}
					}
				}
			}
			ordered := append([]Rapid115Watch(nil), st.Rapid115.Watches...)
			retryDue := map[string]time.Time{}
			for _, retry := range st.Rapid115Retries {
				if retry.Revision != revision {
					continue
				}
				old := retryDue[retry.WatchID]
				if old.IsZero() || retry.Next.Before(old) {
					retryDue[retry.WatchID] = retry.Next
				}
			}
			sort.SliceStable(ordered, func(i, j int) bool { return pending[ordered[i].ID].Before(pending[ordered[j].ID]) })
			for _, w := range ordered {
				if !w.Enabled {
					continue
				}
				if w.Mode == "cron" && (w.FailureAction == "upload" || w.FailureAction == "delete") && validateRapid115RetryCron(w.Cron) != nil {
					continue
				}
				due := pending[w.ID]
				if w.Mode == "cron" {
					due = next[w.ID]
				}
				if retry := retryDue[w.ID]; !retry.IsZero() && (due.IsZero() || retry.Before(due)) {
					due = retry
				}
				if due.IsZero() || now.Before(due) {
					continue
				}
				if a.startRapid115(normalizeRapid115Job(w.Rapid115Job), w.ID, "监控 · "+w.Name, revision) {
					delete(pending, w.ID)
					if schedule, e := cronParser.Parse(w.Cron); w.Mode == "cron" && e == nil {
						next[w.ID] = schedule.Next(now)
					}
				}
			}
		}
	}
}
