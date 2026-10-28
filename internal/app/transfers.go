package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/webdav"
)

type transferSourceKey struct{}
type transferOwnedKey struct{}
type transferEntry struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Source   string            `json:"source"`
	Storage  string            `json:"storage"`
	From     *transferLocation `json:"from,omitempty"`
	To       *transferLocation `json:"to,omitempty"`
	Name     string            `json:"name"`
	Status   string            `json:"status"`
	Done     int64             `json:"done"`
	Total    int64             `json:"total"`
	Speed    int64             `json:"speed"`
	Controls transferControls  `json:"controls"`
	rate     *trafficMeter
	cancel   context.CancelFunc
	pausable bool
	phase    string
	retry    func(context.Context, *transferProgress) error
	Started  time.Time `json:"started"`
	Updated  time.Time `json:"updated"`
	Message  string    `json:"message,omitempty"`
}
type transferLog struct {
	mu             sync.Mutex
	items          map[string]transferEntry
	file           string
	paths          map[string]transferPathHint
	pathGeneration uint64
}

func (l *transferLog) restore(file string) {
	l.file = file
	l.items = map[string]transferEntry{}
	l.paths, l.pathGeneration = nil, 0
	b, err := os.ReadFile(file)
	if err != nil || len(b) > 4<<20 {
		return
	}
	var items []transferEntry
	if json.Unmarshal(b, &items) != nil {
		return
	}
	for _, item := range items {
		if transferFailed(item.Status) && time.Since(item.Updated) <= 72*time.Hour && len(l.items) < 500 {
			item.Controls = transferControls{Delete: true}
			l.items[item.ID] = item
		}
	}
}

// Caller holds mu; only failed records survive a restart.
func (l *transferLog) saveFailures() error {
	if l.file == "" {
		return nil
	}
	items := []transferEntry{}
	for _, item := range l.items {
		if transferFailed(item.Status) && time.Since(item.Updated) <= 72*time.Hour {
			items = append(items, item)
		}
	}
	b, err := json.Marshal(items)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(l.file), 0700); err != nil {
		return err
	}
	return atomicWrite(l.file, b)
}

type transferProgress struct {
	meter      *trafficMeter
	kind       string
	log        *transferLog
	id         string
	once       sync.Once
	bindOnce   sync.Once
	ctx        context.Context
	app        *App
	cancel     context.CancelFunc
	resourceMu sync.Mutex
	release    func()
	ignored    bool
	entry      transferEntry
}

func (a *App) transferEvent(level string, entry transferEntry, result string) {
	if a == nil || a.store == nil || entry.ID == "" {
		return
	}
	kind := map[string]string{"upload": "上传", "download": "下载", "copy": "复制"}[entry.Kind]
	if kind == "" {
		kind = "传输"
	}
	locations := ""
	if entry.From != nil && entry.To != nil {
		locations = fmt.Sprintf("，来源位置 %q / %q，目标位置 %q / %q", entry.From.Storage, entry.From.Directory, entry.To.Storage, entry.To.Directory)
	}
	a.store.event(level, "transfers", fmt.Sprintf("传输%s：任务 %q，来源 %q，存储池 %q，文件 %q%s，已处理 %d/%d 字节：%s", kind, entry.ID, entry.Source, entry.Storage, entry.Name, locations, entry.Done, entry.Total, result))
}

// Error text can contain signed URLs, credentials and upstream response bodies.
func transferFailureReason(err error) string {
	var diagnosed *fileTransferError
	if errors.As(err, &diagnosed) {
		return diagnosed.Error()
	}
	var auth *storageAuthError
	var request *storageRequestError
	var dns *net.DNSError
	var network net.Error
	switch {
	case errors.Is(err, errDAVPartialRead):
		return errDAVPartialRead.Error()
	case errors.Is(err, errTransferIgnored):
		return errTransferIgnored.Error()
	case errors.Is(err, errTransferSkipped):
		return errTransferSkipped.Error()
	case errors.Is(err, context.Canceled):
		return "已取消或连接中断"
	case errors.Is(err, context.DeadlineExceeded):
		return "读取或传输超时"
	case errors.As(err, &network) && network.Timeout():
		return "读取或传输超时"
	case errors.As(err, &dns):
		return "上游域名解析失败"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "上游拒绝连接"
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return "上游连接中断"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "上游响应提前结束，读取内容不完整"
	case errors.Is(err, io.ErrShortWrite):
		return "写入内容不完整"
	case errors.Is(err, syscall.ENOSPC):
		return "本机暂存或目标磁盘空间不足"
	case errors.Is(err, syscall.EROFS):
		return "目标文件系统只读"
	case errors.Is(err, os.ErrExist):
		return "目标文件或目录已存在，未覆盖"
	case errors.Is(err, syscall.ENOTDIR):
		return "路径中的某一项不是目录"
	case errors.As(err, &auth):
		return "存储授权失效或访问被拒绝"
	case errors.Is(err, os.ErrPermission):
		return "存储权限不足"
	case errors.Is(err, os.ErrNotExist):
		return "源文件或目标目录不存在"
	case errors.As(err, &request):
		return request.message
	default:
		return "操作失败，请检查存储状态、网络及对应模块日志"
	}
}

func (a *App) beginTransfer(ctx context.Context, kind string, s Storage, name string, total int64, locations ...transferLocations) *transferProgress {
	if owned, _ := ctx.Value(transferOwnedKey{}).(bool); owned {
		return nil
	}
	source, _ := ctx.Value(transferSourceKey{}).(string)
	if source == "" {
		source = "浏览器"
	}
	ctx, cancel := context.WithCancel(ctx)
	now := time.Now()
	entry := transferEntry{ID: id(), Kind: kind, Source: source, Storage: s.Name, Name: name, Status: "running", Total: total, Started: now, Updated: now}
	pair := transferLocations{From: a.transferFileLocation(s, name), To: transferClientLocation(ctx)}
	if kind == "upload" {
		pair.From, pair.To = pair.To, pair.From
	} else if kind == "copy" {
		pair.To = transferLocation{}
	}
	if len(locations) > 0 {
		pair = locations[0]
	}
	entry.From, entry.To = &pair.From, &pair.To
	entry.rate = &trafficMeter{}
	entry.cancel = cancel
	l := &a.transfers
	l.mu.Lock()
	if l.items == nil {
		l.items = map[string]transferEntry{}
	}
	if len(l.items) >= 500 {
		oldest := ""
		for key, item := range l.items {
			if !transferActive(item.Status) && (oldest == "" || item.Updated.Before(l.items[oldest].Updated)) {
				oldest = key
			}
		}
		if oldest != "" {
			delete(l.items, oldest)
		} else {
			l.mu.Unlock()
			a.transferEvent("info", entry, "开始（任务列表已达上限，仍记录执行结果）")
			return &transferProgress{meter: &a.traffic, kind: kind, app: a, ctx: ctx, cancel: cancel, entry: entry}
		}
	}
	l.items[entry.ID] = entry
	l.mu.Unlock()
	a.transferEvent("info", entry, "开始")
	p := &transferProgress{log: l, id: entry.ID, meter: &a.traffic, kind: kind, ctx: ctx, app: a, cancel: cancel, entry: entry, ignored: kind == "upload" && a.transferIgnored(name)}
	p.ctx = context.WithValue(ctx, transferProgressResourceKey{}, p)
	return p
}

// Pause at IO boundaries without buffering the remaining file in memory.
func (p *transferProgress) wait() error {
	if p == nil {
		return nil
	}
	return p.waitContext(p.ctx)
}

func (p *transferProgress) waitContext(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if p.ignored {
		return errTransferIgnored
	}
	if p.ctx != nil && p.ctx.Err() != nil {
		return p.ctx.Err()
	}
	if err := p.waitPaused(ctx); err != nil {
		return err
	}
	if p.app != nil && p.kind == "upload" {
		if owned, _ := p.ctx.Value(transferUploadResourceKey{}).(bool); !owned {
			p.resourceMu.Lock()
			if p.release == nil {
				var err error
				waitCtx := p.ctx
				if waitCtx == nil {
					waitCtx = context.Background()
				}
				waitCtx, cancel := context.WithCancel(waitCtx)
				stop := func() bool { return false }
				if ctx != nil {
					stop = context.AfterFunc(ctx, cancel)
				}
				p.release, err = p.app.acquireTransferResource(waitCtx, "upload")
				stop()
				cancel()
				if err != nil {
					p.resourceMu.Unlock()
					return err
				}
			}
			p.resourceMu.Unlock()
		}
	}
	return p.waitPaused(ctx)
}

func (p *transferProgress) waitPaused(ctx context.Context) error {
	if p == nil || p.log == nil {
		return nil
	}
	for {
		if p.ctx != nil && p.ctx.Err() != nil {
			return p.ctx.Err()
		}
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		p.log.mu.Lock()
		entry, ok := p.log.items[p.id]
		p.log.mu.Unlock()
		if !ok {
			return context.Canceled
		}
		if entry.Status != "paused" {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (a *App) transferAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if !decode(w, r, &in) {
		return
	}
	l := &a.transfers
	l.mu.Lock()
	var eventEntry transferEntry
	var result string
	level := "info"
	defer func() {
		l.mu.Unlock()
		if result != "" {
			a.transferEvent(level, eventEntry, result)
		}
	}()
	entry, ok := l.items[in.ID]
	if !ok {
		fail(w, 404, errors.New("传输任务不存在"))
		return
	}
	switch in.Action {
	case "pause", "resume":
		if entry.Kind == "copy" && !entry.pausable || !transferActive(entry.Status) {
			fail(w, 409, errors.New("此任务不支持暂停或继续，请重新发起失败的传输"))
			return
		}
		before := entry.Status
		entry.Status = "paused"
		if in.Action == "resume" {
			entry.Status = entry.phase
			if entry.Status == "" || entry.Status == "paused" {
				entry.Status = "running"
			}
		} else if before != "paused" {
			entry.phase = before
		}
		entry.Updated = time.Now()
		l.items[in.ID] = entry
		if before != entry.Status {
			eventEntry = entry
			result = "暂停"
			if in.Action == "resume" {
				result = "继续"
			}
		}
	case "delete":
		if entry.Kind == "copy" && !entry.pausable && transferActive(entry.Status) {
			fail(w, 409, errors.New("云端复制提交后不能中断，请等待结束"))
			return
		}
		if entry.cancel != nil {
			entry.cancel()
		}
		delete(l.items, in.ID)
		if err := l.saveFailures(); err != nil {
			fail(w, 500, errors.New("保存传输记录失败"))
			return
		}
		eventEntry = entry
		result = "清理传输记录"
		if transferActive(entry.Status) {
			level, result = "warn", "取消传输并清理记录"
		}
	case "retry":
		if entry.Status != "failed" || entry.retry == nil {
			fail(w, 409, errors.New("此记录没有可安全重建的传输源，请从原入口重新发起；未知云端写入不会重放"))
			return
		}
		replay := entry.retry
		before := entry
		ctx, cancel := context.WithCancel(a.ctx)
		entry.Status, entry.phase, entry.Message = "preparing", "preparing", ""
		entry.Done, entry.Speed, entry.rate, entry.cancel = 0, 0, &trafficMeter{}, cancel
		entry.Updated = time.Now()
		l.items[in.ID] = entry
		if err := l.saveFailures(); err != nil {
			l.items[in.ID] = before
			cancel()
			fail(w, 500, errors.New("保存传输记录失败"))
			return
		}
		p := &transferProgress{log: l, id: entry.ID, kind: entry.Kind, meter: &a.traffic, app: a, ctx: ctx, cancel: cancel, entry: entry}
		p.ctx = context.WithValue(ctx, transferProgressResourceKey{}, p)
		eventEntry, result = entry, "手动重试"
		go func() { p.finish(replay(p.ctx, p)); a.cache.clear() }()
	default:
		fail(w, 400, errors.New("无效的传输操作"))
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (p *transferProgress) add(n int) {
	if p != nil && p.meter != nil {
		p.meter.add(p.kind, n, time.Now())
	}
	if p == nil || p.log == nil || n <= 0 {
		return
	}
	p.log.mu.Lock()
	defer p.log.mu.Unlock()
	entry, ok := p.log.items[p.id]
	if !ok || !transferActive(entry.Status) {
		return
	}
	if entry.Status != "paused" {
		entry.Status, entry.phase = "running", "running"
	}
	entry.Done += int64(n)
	if entry.rate != nil {
		entry.rate.add("upload", n, time.Now())
	}
	entry.Updated = time.Now()
	p.log.items[p.id] = entry
}
func (p *transferProgress) setTotal(total int64) {
	if p == nil || p.log == nil || total < 0 {
		return
	}
	p.log.mu.Lock()
	defer p.log.mu.Unlock()
	if entry, ok := p.log.items[p.id]; ok {
		entry.Total = total
		p.log.items[p.id] = entry
	}
}
func (p *transferProgress) finish(err error) {
	if p == nil {
		return
	}
	p.once.Do(func() {
		if p.cancel != nil {
			p.cancel()
		}
		p.resourceMu.Lock()
		if p.release != nil {
			p.release()
			p.release = nil
		}
		p.resourceMu.Unlock()
		logResult := func(entry transferEntry) {
			level, result := "info", "完成"
			if err == nil && entry.Kind == "download" && entry.Total > 0 && entry.Done < entry.Total {
				result = "读取结束（本次按需读取，未读取整个文件）"
			}
			if err != nil {
				level, result = "error", transferFailureReason(err)
				if errors.Is(err, errTransferIgnored) || errors.Is(err, errTransferSkipped) || errors.Is(err, errDAVPartialRead) {
					level = "info"
				}
				if errors.Is(err, context.Canceled) {
					level = "warn"
				}
			}
			p.app.transferEvent(level, entry, result)
		}
		if p.log == nil {
			logResult(p.entry)
			return
		}
		p.log.mu.Lock()
		entry, ok := p.log.items[p.id]
		if !ok {
			p.log.mu.Unlock()
			return
		}
		entry.Status = "completed"
		if err == nil && entry.Total <= 0 {
			entry.Total = entry.Done
		}
		entry.Updated = time.Now()
		if err != nil {
			entry.Status = "failed"
			entry.Message = transferFailureReason(err)
			var diagnosed *fileTransferError
			if errors.As(err, &diagnosed) {
				entry.Message = transferFailureReason(err)
			}
			if errors.Is(err, context.Canceled) {
				entry.Status = "error"
				entry.Message = transferFailureReason(err)
			}
			if errors.Is(err, errTransferIgnored) {
				entry.Status, entry.Message = "ignored", errTransferIgnored.Error()
			} else if errors.Is(err, errTransferSkipped) {
				entry.Status, entry.Message = "skipped", errTransferSkipped.Error()
			} else if errors.Is(err, errDAVPartialRead) {
				entry.Status, entry.Message = "skipped", errDAVPartialRead.Error()
			}
		}
		p.log.items[p.id] = entry
		if err != nil {
			if saveErr := p.log.saveFailures(); saveErr != nil {
				log.Printf("传输失败记录保存失败：%v", saveErr)
			}
		}
		p.log.mu.Unlock()
		logResult(entry)
	})
}
func (a *App) transferList(w http.ResponseWriter, r *http.Request) {
	a.transfers.mu.Lock()
	changed := false
	items := make([]transferEntry, 0, len(a.transfers.items))
	cleared := 0
	for _, item := range a.transfers.items {
		if (!transferActive(item.Status) && !transferFailed(item.Status) && time.Since(item.Updated) >= time.Minute) || (transferFailed(item.Status) && time.Since(item.Updated) > 72*time.Hour) {
			delete(a.transfers.items, item.ID)
			changed = true
			continue
		}
		if r.Method == "DELETE" && transferFailed(item.Status) {
			delete(a.transfers.items, item.ID)
			changed = true
			cleared++
			continue
		}
		item.Speed = 0
		if item.Status == "running" && item.rate != nil {
			item.Speed = item.rate.rates(time.Now())["uploadRate"]
		}
		item.Controls = controlsForTransfer(item)
		items = append(items, item)
	}
	var saveErr error
	if changed {
		saveErr = a.transfers.saveFailures()
	}
	a.transfers.mu.Unlock()
	if saveErr != nil {
		fail(w, 500, errors.New("保存传输记录失败"))
		return
	}
	if cleared > 0 && a.store != nil {
		a.store.event("info", "transfers", fmt.Sprintf("清理失败传输记录：%d 项", cleared))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Started.After(items[j].Started) })
	jsonResponse(w, 200, items)
}

type transferReader struct {
	io.Reader
	progress *transferProgress
}

type transferResponse struct {
	http.ResponseWriter
	progress    *transferProgress
	failure     error
	successOnly bool
	status      int
}

func (w *transferResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *transferResponse) WriteHeader(status int) {
	w.status = status
	if status >= 200 && status < 300 {
		if total, err := strconv.ParseInt(w.Header().Get("Content-Length"), 10, 64); err == nil {
			w.progress.setTotal(total)
		}
	}
	if status >= 400 {
		w.failure = errors.New("download failed")
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *transferResponse) Write(b []byte) (int, error) {
	if w.status == 0 {
		if total, err := strconv.ParseInt(w.Header().Get("Content-Length"), 10, 64); err == nil {
			w.progress.setTotal(total)
		}
	}
	if err := w.progress.wait(); err != nil {
		w.failure = err
		return 0, err
	}
	n, err := writeResourceChunks(w.ResponseWriter, b, w.progress)
	if !w.successOnly || w.status == 0 || w.status >= 200 && w.status < 300 {
		w.progress.add(n)
	}
	if err != nil {
		w.failure = err
	}
	return n, err
}

func (r transferReader) Read(b []byte) (int, error) {
	if r.progress != nil {
		if closer, ok := r.Reader.(io.Closer); ok {
			r.progress.bindOnce.Do(func() { context.AfterFunc(r.progress.ctx, func() { _ = closer.Close() }) })
		}
	}
	if err := r.progress.wait(); err != nil {
		return 0, err
	}
	b = b[:min(len(b), 32*1024)]
	n, err := r.Reader.Read(b)
	if e := r.progress.limit(n); e != nil {
		return 0, e
	}
	r.progress.add(n)
	return n, err
}

type transferWriter struct {
	io.Writer
	progress *transferProgress
}

func (w transferWriter) Write(b []byte) (int, error) {
	if err := w.progress.wait(); err != nil {
		return 0, err
	}
	n, err := writeResourceChunks(w.Writer, b, w.progress)
	w.progress.add(n)
	return n, err
}

type transferFile struct {
	webdav.File
	progress *transferProgress
	write    bool
	failure  error
}

func (f *transferFile) Read(b []byte) (int, error) {
	if err := f.progress.wait(); err != nil {
		f.failure = err
		return 0, err
	}
	n, err := f.File.Read(b)
	if !f.write {
		if e := f.progress.limit(n); e != nil {
			f.failure = e
			return 0, e
		}
	}
	if !f.write {
		f.progress.add(n)
	}
	if err != nil && err != io.EOF {
		f.failure = err
		f.progress.finish(err)
	}
	return n, err
}
func (f *transferFile) Write(b []byte) (int, error) {
	if err := f.progress.wait(); err != nil {
		f.failure = err
		return 0, err
	}
	n, err := writeResourceChunks(f.File, b, f.progress)
	f.progress.add(n)
	if err != nil {
		f.failure = err
		f.progress.finish(err)
	}
	return n, err
}
func (f *transferFile) Close() error {
	if f.write {
		if err := f.progress.wait(); err != nil {
			f.failure = err
		}
		if staged, ok := f.File.(*cloudWriteFile); ok && f.failure != nil {
			staged.writeErr = f.failure
		}
		if staged, ok := f.File.(*cloudWriteFile); ok && f.progress != nil {
			staged.ctx = context.WithValue(staged.ctx, transferUploadResourceKey{}, true)
		}
	}
	err := f.File.Close()
	if err == nil {
		err = f.failure
	}
	f.progress.finish(err)
	return err
}

func (p *transferProgress) limit(n int) error {
	if p == nil || p.app == nil {
		return nil
	}
	return p.app.limitTransferBytes(p.ctx, p.kind, n)
}

func writeResourceChunks(out io.Writer, b []byte, p *transferProgress) (int, error) {
	total := 0
	for len(b) > 0 {
		part := b[:min(len(b), 32*1024)]
		if err := p.wait(); err != nil {
			return total, err
		}
		if err := p.limit(len(part)); err != nil {
			return total, err
		}
		n, err := out.Write(part)
		total += n
		b = b[n:]
		if err != nil {
			return total, err
		}
		if n != len(part) {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
func (a *App) trackedFile(ctx context.Context, s Storage, name string, file webdav.File, flag int, locations ...transferLocations) webdav.File {
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return file
	}
	kind := "download"
	write := flag&os.O_WRONLY != 0 || flag&os.O_RDWR != 0
	if write {
		kind = "upload"
	}
	total := info.Size()
	if write {
		total = 0
		if expected, ok := ctx.Value(mountPutLength{}).(int64); ok && expected >= 0 {
			total = expected
		}
	}
	progress := a.beginTransfer(ctx, kind, s, name, total, locations...)
	if progress != nil {
		switch f := file.(type) {
		case *cloudWriteFile:
			f.ctx = progress.ctx
		case *davFile:
			f.ctx = progress.ctx
		}
	}
	return &transferFile{File: file, write: write, progress: progress}
}
