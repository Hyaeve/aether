package app

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
	ID       string    `json:"id"`
	Time     time.Time `json:"time"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	PickCode string    `json:"pickcode"`
	Status   string    `json:"status"`
	Source   string    `json:"source"`
	Message  string    `json:"message,omitempty"`
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
	if j.SuccessAction != "delete" && j.SuccessAction != "keep" || j.FailureAction != "skip" && j.FailureAction != "upload" {
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
			if _, err := cronParser.Parse(w.Cron); err != nil {
				return errors.New("Cron表达式无效")
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
		err := a.store.update(func(st *State) error {
			if err := validateRapid115(value, *st); err != nil {
				return err
			}
			st.Rapid115 = value
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
func (a *App) rapid115Records(w http.ResponseWriter, r *http.Request) {
	a.rapid115Mu.Lock()
	running := a.rapid115Running
	a.rapid115Mu.Unlock()
	st := a.store.snapshotWithLogLimit(0)
	jsonResponse(w, 200, map[string]any{"items": st.Rapid115Records, "running": running})
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
	if a.rapid115Running || a.ctx.Err() != nil {
		return false
	}
	a.rapid115Running = true
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer func() { a.rapid115Mu.Lock(); a.rapid115Running = false; a.rapid115Mu.Unlock() }()
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
	if _, err := io.Copy(h, upload115Reader{ctx, f}); err != nil {
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
		record := Rapid115Record{ID: id(), Time: time.Now(), Name: filepath.Base(name), Size: fi.Size(), Status: "running", Source: label}
		attempt := rapid115Stamp([]any{key, job, storage.Config, name, fi.Size(), fi.ModTime().UnixNano()})
		fresh, err := a.rapid115Intent(attempt, record)
		if err != nil {
			return err
		}
		if !fresh {
			return nil
		}
		record.Status = "failed"
		uploadErr := func() error {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
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
			rapid, pick, err := call(ctx, storage, parent, base, f, fi.Size(), job.FailureAction == "upload")
			if err != nil {
				return err
			}
			if !rapid && pick == "" {
				return errors.New("未命中秒传，已跳过并保留源文件")
			}
			if pick == "" {
				return errors.New("115未返回可校验文件，已保留源文件")
			}
			record.PickCode = pick
			record.Status = "success"
			if !rapid {
				record.Status = "uploaded"
			}
			if err := a.rapid115Finish(record); err != nil {
				return err
			}
			if job.SuccessAction == "delete" {
				latest, err := os.Stat(job.Source)
				if err != nil || !os.SameFile(scope, latest) || !a.rapid115Current(job, storage, revision) || !rapid115Unchanged(root, name, fi) {
					return errors.New("源文件或目录已变化，已保留源文件")
				}
				nowSHA, err := rapid115Digest(ctx, f)
				if err != nil || nowSHA != sha || !rapid115Unchanged(root, name, fi) {
					return errors.New("源文件内容已变化，已保留")
				}
				// Check every ancestor before cleanup, rejecting substituted paths.
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
				if err := root.Remove(name); err != nil {
					return errors.New("目标已生成，源文件清理失败")
				}
			}
			return nil
		}()
		if uploadErr != nil {
			record.Message = "处理失败；源文件保留，请检查目标后再手动处理"
			if record.Status == "failed" {
				record.Message = "秒传未成功，源文件保留；请检查存储授权、目标重名或秒传命中情况"
			}
		}
		if err := a.rapid115Finish(record); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	})
}

func (a *App) upload115RapidOnly(ctx context.Context, s Storage, parent, name string, f *os.File, size int64, allowUpload bool) (bool, string, error) {
	c, err := client115(ctx, s)
	if err != nil {
		return false, "", err
	}
	if _, err = c.UploadAvailable(); err != nil {
		return false, "", err
	}
	if size > c.UploadMetaInfo.SizeLimit {
		return false, "", errors.New("文件超过115上传限制")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return false, "", err
	}
	digest, err := c.GetDigestResult(upload115Reader{ctx, f})
	if err != nil {
		return false, "", err
	}
	if err = a.waitAPI(ctx, s.ID); err != nil {
		return false, "", err
	}
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
			sort.SliceStable(ordered, func(i, j int) bool { return pending[ordered[i].ID].Before(pending[ordered[j].ID]) })
			for _, w := range ordered {
				if !w.Enabled {
					continue
				}
				due := pending[w.ID]
				if w.Mode == "cron" {
					due = next[w.ID]
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
