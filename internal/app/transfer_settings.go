package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type TransferSettings struct {
	TempDirectory     string   `json:"tempDirectory,omitempty"`
	Version           int      `json:"version"`
	DownloadSpeed     int64    `json:"downloadSpeed"`
	DownloadUnit      string   `json:"downloadUnit"`
	UploadSpeed       int64    `json:"uploadSpeed"`
	UploadUnit        string   `json:"uploadUnit"`
	MaxPreprocess     int      `json:"maxPreprocess"`
	MaxUploads        int      `json:"maxUploads"`
	UploadDelay       int      `json:"uploadDelay"`
	CrossCloudTemp    bool     `json:"crossCloudTemp"`
	IgnoredExtensions []string `json:"ignoredExtensions"`
}

type BackupResources struct {
	Version            int  `json:"version"`
	PauseBidirectional bool `json:"pauseBidirectional"`
	QueueUpper         int  `json:"queueUpper"`
	QueueLower         int  `json:"queueLower"`
	MaxConcurrentScan  int  `json:"maxConcurrentScan"`
}

func normalizedTransferSettings(s TransferSettings) TransferSettings {
	if s.Version == 0 {
		return TransferSettings{Version: 1, DownloadUnit: "KB/s", UploadUnit: "KB/s", MaxPreprocess: 5, MaxUploads: 5, UploadDelay: 10, CrossCloudTemp: true, IgnoredExtensions: []string{"clfstmp", "parts", "tmp", "downloading", "aria2", "temp", "temp_video", "~#0"}}
	}
	s.IgnoredExtensions = append([]string{}, s.IgnoredExtensions...)
	return s
}
func normalizedBackupResources(s BackupResources) BackupResources {
	if s.Version == 0 {
		return BackupResources{Version: 1, QueueUpper: 10000, MaxConcurrentScan: 1}
	}
	return s
}
func speedBytes(value int64, unit string) int64 {
	if unit == "MB/s" {
		return value * 1024 * 1024
	}
	return value * 1024
}
func validateTransferSettings(s *TransferSettings) error {
	s.TempDirectory = strings.TrimSpace(s.TempDirectory)
	if s.TempDirectory != "" && (!filepath.IsAbs(s.TempDirectory) || strings.ContainsAny(s.TempDirectory, "\x00\r\n") || len(s.TempDirectory) > 4096) {
		return errors.New("临时文件位置须为容器内绝对目录")
	}
	if s.Version != 1 || s.DownloadSpeed < 0 || s.UploadSpeed < 0 || s.DownloadSpeed > 1000000 || s.UploadSpeed > 1000000 || (s.DownloadUnit != "KB/s" && s.DownloadUnit != "MB/s") || (s.UploadUnit != "KB/s" && s.UploadUnit != "MB/s") || s.MaxPreprocess < 1 || s.MaxPreprocess > 64 || s.MaxUploads < 1 || s.MaxUploads > 64 || s.UploadDelay < 0 || s.UploadDelay > 86400 || len(s.IgnoredExtensions) > 128 {
		return errors.New("传输设置无效：速度须为非负整数，并发须为1至64，延迟须为0至86400秒")
	}
	next, seen := []string{}, map[string]bool{}
	for _, ext := range s.IgnoredExtensions {
		ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
		if ext == "" {
			continue
		}
		if len(ext) > 64 || strings.ContainsAny(ext, "/\\;\x00\r\n ") {
			return errors.New("忽略的扩展名无效")
		}
		if !seen[ext] {
			next = append(next, ext)
			seen[ext] = true
		}
	}
	s.IgnoredExtensions = next
	return nil
}
func validateBackupResources(s BackupResources) error {
	if s.Version != 1 || s.QueueUpper < 0 || s.QueueUpper > 100000 || s.QueueLower < 0 || s.QueueUpper == 0 && s.QueueLower != 0 || s.QueueUpper > 0 && s.QueueLower >= s.QueueUpper || s.MaxConcurrentScan < 1 || s.MaxConcurrentScan > 16 {
		return errors.New("备份资源设置无效：队列下限须小于上限，并发扫描须为1至16")
	}
	return nil
}
func (a *App) transferConfig() TransferSettings {
	if a.store == nil {
		return normalizedTransferSettings(TransferSettings{})
	}
	a.store.mu.RLock()
	s := normalizedTransferSettings(a.store.state.TransferSettings)
	a.store.mu.RUnlock()
	return s
}
func (a *App) backupResourceConfig() BackupResources {
	if a.store == nil {
		return normalizedBackupResources(BackupResources{})
	}
	a.store.mu.RLock()
	s := normalizedBackupResources(a.store.state.BackupResources)
	a.store.mu.RUnlock()
	return s
}
func (a *App) transferSettingsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		jsonResponse(w, 200, a.transferConfig())
		return
	}
	if r.Method != "PUT" {
		w.WriteHeader(405)
		return
	}
	var s TransferSettings
	if !decode(w, r, &s) {
		return
	}
	s.Version = 1
	if err := validateTransferSettings(&s); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.store.update(func(st *State) error { st.TransferSettings = s; st.TransferVersion = 1; return nil }); err != nil {
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 200, s)
}
func (a *App) backupSettingsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		jsonResponse(w, 200, a.backupResourceConfig())
		return
	}
	if r.Method != "PUT" {
		w.WriteHeader(405)
		return
	}
	var s BackupResources
	if !decode(w, r, &s) {
		return
	}
	s.Version = 1
	if err := validateBackupResources(s); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.store.update(func(st *State) error { st.BackupResources = s; st.TransferVersion = 1; return nil }); err != nil {
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 200, s)
}

type transferResources struct {
	mu       sync.Mutex
	active   map[string]int
	limiters map[string]*rate.Limiter
	rates    map[string]int64
}

func waitResource(ctx context.Context, duration time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Limits are re-read while queued. A changed limit does not terminate an active IO.
func (a *App) acquireTransferResource(ctx context.Context, kind string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		limit := a.transferConfig().MaxUploads
		if kind == "preprocess" {
			limit = a.transferConfig().MaxPreprocess
		}
		if kind == "scan" {
			limit = a.backupResourceConfig().MaxConcurrentScan
		}
		if kind == "publish" {
			limit = 1
		}
		if kind == "feature-hash" {
			limit = 2
		}
		if limit < 1 {
			return nil, errors.New("资源并发设置无效")
		}
		a.resources.mu.Lock()
		if a.resources.active == nil {
			a.resources.active = map[string]int{}
		}
		if a.resources.active[kind] < limit {
			a.resources.active[kind]++
			a.resources.mu.Unlock()
			var once sync.Once
			return func() { once.Do(func() { a.resources.mu.Lock(); a.resources.active[kind]--; a.resources.mu.Unlock() }) }, nil
		}
		a.resources.mu.Unlock()
		if err := waitResource(ctx, 25*time.Millisecond); err != nil {
			return nil, err
		}
	}
}

// A shared token bucket caps aggregate bytes across all tracked streams, not per file.
func (a *App) limitTransferBytes(ctx context.Context, kind string, n int) error {
	if n <= 0 || kind != "download" && kind != "upload" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s := a.transferConfig()
	bps := speedBytes(s.DownloadSpeed, s.DownloadUnit)
	if kind == "upload" {
		bps = speedBytes(s.UploadSpeed, s.UploadUnit)
	}
	if bps < 0 {
		return errors.New("传输速度设置无效")
	}
	if bps == 0 {
		return ctx.Err()
	}
	const burst = 32 * 1024
	a.resources.mu.Lock()
	if a.resources.limiters == nil {
		a.resources.limiters = map[string]*rate.Limiter{}
		a.resources.rates = map[string]int64{}
	}
	l := a.resources.limiters[kind]
	if l == nil {
		l = rate.NewLimiter(rate.Limit(bps), burst)
		a.resources.limiters[kind] = l
	} else if a.resources.rates[kind] != bps {
		l.SetLimit(rate.Limit(bps))
	}
	a.resources.rates[kind] = bps
	a.resources.mu.Unlock()
	for n > 0 {
		count := min(n, burst)
		if err := l.WaitN(ctx, count); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		n -= count
	}
	return nil
}

func (a *App) transferIgnored(name string) bool {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	for _, item := range a.transferConfig().IgnoredExtensions {
		if ext == item {
			return true
		}
	}
	return false
}

type resourceReader struct {
	ctx    context.Context
	app    *App
	kind   string
	reader io.Reader
}

type transferProgressResourceKey struct{}

func (r resourceReader) Read(b []byte) (int, error) {
	if progress, _ := r.ctx.Value(transferProgressResourceKey{}).(*transferProgress); progress != nil {
		if err := progress.waitContext(r.ctx); err != nil {
			return 0, err
		}
	}
	b = b[:min(len(b), 32*1024)]
	n, err := r.reader.Read(b)
	if e := r.app.limitTransferBytes(r.ctx, r.kind, n); e != nil {
		return 0, e
	}
	return n, err
}

var errBackupPaused = errors.New("双向同步已暂停，操作计划已记录；未发布或删除文件")

func (a *App) checkBackupMutation(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg, ok := ctx.Value(backupTransferSnapshotKey{}).(TransferSettings); ok {
		current := a.transferConfig()
		if strings.Join(cfg.IgnoredExtensions, "\x00") != strings.Join(current.IgnoredExtensions, "\x00") {
			return errors.New("传输过滤配置已变更，请重新扫描备份")
		}
	}
	if sync, _ := ctx.Value(backupSyncResourceKey{}).(bool); sync && a.backupResourceConfig().PauseBidirectional {
		return errBackupPaused
	}
	return nil
}

type backupSyncResourceKey struct{}
type backupTransferSnapshotKey struct{}

func (a *App) backupIgnored(ctx context.Context, name string) bool {
	cfg, ok := ctx.Value(backupTransferSnapshotKey{}).(TransferSettings)
	if !ok {
		cfg = a.transferConfig()
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	for _, item := range cfg.IgnoredExtensions {
		if ext == item {
			return true
		}
	}
	return false
}

func validateResourceState(st *State, allowLegacyPIN ...bool) error {
	for _, storage := range st.Storages {
		if nativeMobile(storage) {
			if err := validateMobileClient(storage); err != nil {
				return err
			}
		}
	}
	legacySchedule := len(allowLegacyPIN) > 0 && allowLegacyPIN[0]
	if !legacySchedule {
		expressions := []string{}
		for _, task := range st.Tasks {
			if task.Kind != "cache" {
				expressions = append(expressions, task.Cron)
			}
		}
		for _, rule := range st.BackupRules {
			expressions = append(expressions, rule.Cron)
		}
		for _, rule := range st.Automations {
			if rule.Trigger == "cron" {
				expressions = append(expressions, rule.Cron)
			}
		}
		for _, expression := range expressions {
			if strings.TrimSpace(expression) != "" {
				if _, err := cronParser.Parse(expression); err != nil {
					return err
				}
			}
		}
	}
	if err := validateFolderCleanup(st.FolderCleanup, *st, legacySchedule); err != nil {
		return err
	}
	if err := validateQuarkLibrary(*st, legacySchedule); err != nil {
		return err
	}
	if err := validatePluginRepositories(st.PluginRepositories); err != nil {
		return err
	}
	if err := validateNotifications(st.Notifications, *st); err != nil {
		return err
	}
	for kind, options := range st.CloudShareOptions {
		if err := validateCloudShareOptions(kind, options); err != nil {
			return err
		}
	}
	legacyPIN := len(allowLegacyPIN) > 0 && allowLegacyPIN[0]
	if err := validateRecycleCleanup(st.RecycleCleanup, *st, legacyPIN, legacySchedule); err != nil {
		return err
	}
	if value := st.CASExportDirectory; value != "" && (!filepath.IsAbs(value) || filepath.Clean(value) != value || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n")) {
		return errors.New("CAS导出目录须为容器内绝对路径")
	}
	for _, cfg := range st.Simulcast {
		if cfg.RetentionHours < 0 || cfg.RetentionHours > 8760 {
			return errors.New("复制保活须为1至8760 h，旧配置未设置时默认为12 h")
		}
	}
	if st.Rapid115Version > 1 {
		return errors.New("115秒传配置版本无效")
	}
	st.Rapid115 = normalizedRapid115(st.Rapid115)
	// Preserve readable legacy schedules at startup; the monitor pauses unsafe
	// intervals. New edits and imported configurations still enforce the limit.
	if err := validateRapid115(st.Rapid115, *st, false, legacySchedule); err != nil {
		return err
	}
	if st.ED2KPoolVersion > 1 {
		return errors.New("ED2K 绑定池配置版本无效")
	}
	pool := normalizedED2KPool(st.ED2KPool)
	if err := validateED2KPoolShape(pool); err != nil {
		return err
	}
	st.ED2KPool = pool
	if st.Settings.STRMURL != "" {
		format := st.Settings.STRM115Format
		if format == "" {
			format = "pickcode_name"
		}
		if err := validSTRMSettings(strmSettings{Address: st.Settings.STRMURL, Format: format, OverwriteMode: st.Settings.STRMOverwriteMode}); err != nil {
			return err
		}
	} else if st.Settings.STRM115Format != "" && st.Settings.STRM115Format != "pickcode_name" && st.Settings.STRM115Format != "pickcode" {
		return errors.New("115 STRM 格式无效")
	}
	if !validSTRMOverwriteMode(st.Settings.STRMOverwriteMode) {
		return errors.New("STRM覆盖模式无效")
	}
	if st.TransferVersion != 0 && (st.TransferVersion != 1 || st.TransferSettings.Version != 1 || st.BackupResources.Version != 1) {
		return errors.New("传输资源配置版本无效")
	}
	transfer := normalizedTransferSettings(st.TransferSettings)
	if err := validateTransferSettings(&transfer); err != nil {
		return err
	}
	backup := normalizedBackupResources(st.BackupResources)
	if err := validateBackupResources(backup); err != nil {
		return err
	}
	st.TransferSettings, st.BackupResources = transfer, backup
	return nil
}

type backupScanLeaseKey struct{}
type backupStageResourceKey struct{}
type backupStageResource struct {
	name, digest string
	size         int64
}

func (s *backupStageResource) clear() {
	if s.name != "" {
		_ = os.Remove(s.name)
		*s = backupStageResource{}
	}
}

func (a *App) stageBackupSource(ctx context.Context, source Storage, f File, dir string, limit int64) (stage backupStageResource, resultErr error) {
	phase := "创建本机暂存文件"
	defer func() { resultErr = diagnoseFileTransfer(ctx, phase, resultErr) }()
	if f.Size < 0 || f.Size > limit {
		return stage, rejectFileTransfer(ctx, "核对单文件大小", "暂存单文件上限 "+uploadLimitLabel(limit))
	}
	out, err := os.CreateTemp(dir, "backup-*")
	if err != nil {
		return stage, err
	}
	defer func() {
		out.Close()
		if resultErr != nil {
			_ = os.Remove(out.Name())
		}
	}()
	var input io.ReadCloser
	var progress *transferProgress
	if source.Type == "local" {
		phase = "打开本地来源文件"
		root, e := os.OpenRoot(source.Config["root"])
		if e != nil {
			return stage, e
		}
		rel, e := relative(f.ID)
		if e == nil {
			input, e = root.Open(rel)
		}
		root.Close()
		if e != nil {
			return stage, e
		}
	} else {
		phase = "获取来源下载链接"
		d, e := a.download(ctx, source, f.ID, f.PickCode)
		if e != nil {
			return stage, e
		}
		progress = a.beginTransfer(ctx, "download", source, f.Name, f.Size,
			transferLocations{From: a.transferFileLocation(source, f.ID), To: transferLocalLocation("本机临时目录", dir)})
		reader := &davFile{ctx: ctx, info: davInfo{f}, download: d, progress: progress}
		if progress != nil {
			reader.ctx = progress.ctx
		}
		if source.Type == "115" {
			reader.refreshDownload = func() (Download, error) {
				info, e := download115API(ctx, source, f.PickCode, pan115ReadUA, true)
				if e != nil {
					return Download{}, e
				}
				return Download{URL: info.Url.Url, Headers: info.Header}, nil
			}
		}
		input = reader
	}
	defer input.Close()
	defer func() { progress.finish(diagnoseFileTransfer(ctx, phase, resultErr)) }()
	h := sha256.New()
	phase = "下载来源文件到本机暂存"
	writers := []io.Writer{out, h}
	if progress, ok := ctx.Value(fileTransferProgressKey{}).(*transferProgress); ok {
		writers = append(writers, fileTransferCountWriter{progress})
	}
	n, err := io.Copy(io.MultiWriter(writers...), io.LimitReader(&cancelReader{ctx, input}, limit+1))
	if err != nil {
		return stage, err
	}
	phase = "核对来源文件完整性"
	if n != f.Size || n > limit {
		return stage, rejectFileTransfer(ctx, phase, "源文件内容不完整或读取期间大小变化")
	}
	phase = "关闭本机暂存文件"
	if err = out.Close(); err != nil {
		return stage, err
	}
	return backupStageResource{out.Name(), hex.EncodeToString(h.Sum(nil)), n}, nil
}

func (a *App) savePausedBackupPlan(rule *BackupRule, operations []syncOperation, dirs map[string]bool) error {
	plan := struct {
		At         time.Time `json:"at"`
		Rule       string    `json:"rule"`
		Operations []struct {
			Name   string `json:"name"`
			From   int    `json:"from"`
			To     int    `json:"to"`
			Delete bool   `json:"delete"`
		} `json:"operations"`
		Directories map[string]bool `json:"directories"`
	}{At: time.Now(), Rule: rule.ID, Directories: dirs}
	for _, op := range operations {
		plan.Operations = append(plan.Operations, struct {
			Name   string `json:"name"`
			From   int    `json:"from"`
			To     int    `json:"to"`
			Delete bool   `json:"delete"`
		}{op.name, op.from, op.to, op.remove})
	}
	raw, err := a.store.encodeJSONConfig("runtime/backup-plan/"+rule.ID, plan)
	if err != nil {
		return err
	}
	if err := writeBackupPlan(a, rule.ID, raw); err != nil {
		return err
	}
	rule.Total = len(operations)
	rule.Phase = "paused"
	if err := a.backupProgress(rule, "双向同步已暂停，操作计划已记录"); err != nil {
		return err
	}
	return errBackupPaused
}

func writeBackupPlan(a *App, id string, raw []byte) error {
	dir := filepath.Join(a.dataDir, "runtime", "backup-plans")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(id))
	return atomicWrite(filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), raw)
}

type transferUploadResourceKey struct{}
type transferAppResourceKey struct{}
type transferPreprocessResourceKey struct{}

// The parent uploader calls this once around its staged upload, before any
// cloud mutation. Local receive requests already own the same upload lease.
func (a *App) controlCloudUpload(ctx context.Context, name, local string) (context.Context, func(), error) {
	if err := a.checkBackupMutation(ctx); err != nil {
		return ctx, nil, err
	}
	if owned, _ := ctx.Value(transferUploadResourceKey{}).(bool); !owned {
		release, err := a.acquireTransferResource(ctx, "upload")
		if err != nil {
			return ctx, nil, err
		}
		ctx = context.WithValue(ctx, transferUploadResourceKey{}, true)
		ctx = context.WithValue(ctx, transferAppResourceKey{}, a)
		if err := a.checkBackupMutation(ctx); err != nil {
			release()
			return ctx, nil, err
		}
		return ctx, release, nil
	}
	if err := a.checkBackupMutation(ctx); err != nil {
		return ctx, nil, err
	}
	return context.WithValue(ctx, transferAppResourceKey{}, a), func() {}, nil
}

// Applied before receiving a file body, not after a temporary file was built.
func (a *App) transferRequestControls(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/files/upload" || r.Method != "POST" || !a.authenticated(r) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Origin") != "" && !a.acceptRequestOrigin(r, linkTrustedProxies()) {
			next.ServeHTTP(w, r)
			return
		}
		if a.transferIgnored(r.URL.Query().Get("name")) {
			fail(w, 400, errors.New("此扩展名已被全局传输设置忽略"))
			return
		}
		uploadRelease, err := a.acquireTransferResource(r.Context(), "upload")
		if err != nil {
			fail(w, 408, err)
			return
		}
		defer uploadRelease()
		release, err := a.acquireTransferResource(r.Context(), "preprocess")
		if err != nil {
			fail(w, 408, err)
			return
		}
		defer release()
		ctx := context.WithValue(r.Context(), transferUploadResourceKey{}, true)
		ctx = context.WithValue(ctx, transferAppResourceKey{}, a)
		ctx = context.WithValue(ctx, transferPreprocessResourceKey{}, true)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *App) controlPreprocess(ctx context.Context) (func(), error) {
	if owned, _ := ctx.Value(transferPreprocessResourceKey{}).(bool); owned {
		return func() {}, nil
	}
	return a.acquireTransferResource(ctx, "preprocess")
}

// Only pending local writes are delayed; verified backup snapshots are not.
func (a *App) waitLocalUpload(ctx context.Context, local string) error {
	base := filepath.Base(local)
	if !strings.HasPrefix(base, "pending-") && !strings.HasPrefix(base, "upload-") {
		return ctx.Err()
	}
	info, err := os.Stat(local)
	if err != nil {
		return err
	}
	delay := time.Duration(a.transferConfig().UploadDelay)*time.Second - time.Since(info.ModTime())
	if delay <= 0 {
		return ctx.Err()
	}
	return waitResource(ctx, delay)
}

func controlledUploadReader(ctx context.Context, reader io.Reader) io.Reader {
	if a, ok := ctx.Value(transferAppResourceKey{}).(*App); ok {
		return resourceReader{ctx, a, "upload", reader}
	}
	return reader
}
