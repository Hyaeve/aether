package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"golang.org/x/net/webdav"
)

type transferSourceKey struct{}
type transferOwnedKey struct{}
type transferEntry struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Source  string    `json:"source"`
	Storage string    `json:"storage"`
	Name    string    `json:"name"`
	Status  string    `json:"status"`
	Done    int64     `json:"done"`
	Total   int64     `json:"total"`
	Started time.Time `json:"started"`
	Updated time.Time `json:"updated"`
	Message string    `json:"message,omitempty"`
}
type transferLog struct {
	mu    sync.Mutex
	items map[string]transferEntry
	file  string
}

func (l *transferLog) restore(file string) {
	l.file = file
	l.items = map[string]transferEntry{}
	b, err := os.ReadFile(file)
	if err != nil || len(b) > 4<<20 {
		return
	}
	var items []transferEntry
	if json.Unmarshal(b, &items) != nil {
		return
	}
	for _, item := range items {
		if item.Status == "failed" && time.Since(item.Updated) <= 72*time.Hour && len(l.items) < 500 {
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
		if item.Status == "failed" && time.Since(item.Updated) <= 72*time.Hour {
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
	meter *trafficMeter
	kind  string
	log   *transferLog
	id    string
	once  sync.Once
}

func (a *App) beginTransfer(ctx context.Context, kind string, s Storage, name string, total int64) *transferProgress {
	if owned, _ := ctx.Value(transferOwnedKey{}).(bool); owned {
		return nil
	}
	source, _ := ctx.Value(transferSourceKey{}).(string)
	if source == "" {
		source = "浏览器"
	}
	now := time.Now()
	entry := transferEntry{ID: id(), Kind: kind, Source: source, Storage: s.Name, Name: name, Status: "running", Total: total, Started: now, Updated: now}
	l := &a.transfers
	l.mu.Lock()
	if l.items == nil {
		l.items = map[string]transferEntry{}
	}
	if len(l.items) >= 500 {
		oldest := ""
		for key, item := range l.items {
			if item.Status != "running" && (oldest == "" || item.Updated.Before(l.items[oldest].Updated)) {
				oldest = key
			}
		}
		if oldest != "" {
			delete(l.items, oldest)
		} else {
			l.mu.Unlock()
			return &transferProgress{meter: &a.traffic, kind: kind}
		}
	}
	l.items[entry.ID] = entry
	l.mu.Unlock()
	return &transferProgress{log: l, id: entry.ID, meter: &a.traffic, kind: kind}
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
	if !ok || entry.Status != "running" {
		return
	}
	entry.Done += int64(n)
	entry.Updated = time.Now()
	p.log.items[p.id] = entry
}
func (p *transferProgress) finish(err error) {
	if p == nil || p.log == nil {
		return
	}
	p.once.Do(func() {
		p.log.mu.Lock()
		defer p.log.mu.Unlock()
		entry, ok := p.log.items[p.id]
		if !ok {
			return
		}
		entry.Status = "completed"
		entry.Updated = time.Now()
		if err != nil {
			entry.Status = "failed"
			entry.Message = "操作失败，请检查服务日志及存储权限"
		}
		p.log.items[p.id] = entry
		if err != nil {
			if saveErr := p.log.saveFailures(); saveErr != nil {
				log.Printf("传输失败记录保存失败：%v", saveErr)
			}
		}
	})
}
func (a *App) transferList(w http.ResponseWriter, r *http.Request) {
	a.transfers.mu.Lock()
	changed := false
	items := make([]transferEntry, 0, len(a.transfers.items))
	for _, item := range a.transfers.items {
		if item.Status == "completed" || (item.Status == "failed" && time.Since(item.Updated) > 72*time.Hour) {
			delete(a.transfers.items, item.ID)
			changed = true
			continue
		}
		if r.Method == "DELETE" && item.Status == "failed" {
			delete(a.transfers.items, item.ID)
			changed = true
			continue
		}
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
	sort.Slice(items, func(i, j int) bool { return items[i].Started.After(items[j].Started) })
	jsonResponse(w, 200, items)
}

type transferReader struct {
	io.Reader
	progress *transferProgress
}

type transferResponse struct {
	http.ResponseWriter
	progress *transferProgress
	failure  error
}

func (w *transferResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *transferResponse) WriteHeader(status int) {
	if status >= 400 {
		w.failure = errors.New("download failed")
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *transferResponse) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.progress.add(n)
	if err != nil {
		w.failure = err
	}
	return n, err
}

func (r transferReader) Read(b []byte) (int, error) {
	n, err := r.Reader.Read(b)
	r.progress.add(n)
	return n, err
}

type transferWriter struct {
	io.Writer
	progress *transferProgress
}

func (w transferWriter) Write(b []byte) (int, error) {
	n, err := w.Writer.Write(b)
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
	n, err := f.File.Read(b)
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
	n, err := f.File.Write(b)
	f.progress.add(n)
	if err != nil {
		f.failure = err
		f.progress.finish(err)
	}
	return n, err
}
func (f *transferFile) Close() error {
	err := f.File.Close()
	if err == nil {
		err = f.failure
	}
	f.progress.finish(err)
	return err
}
func (a *App) trackedFile(ctx context.Context, s Storage, name string, file webdav.File, flag int) webdav.File {
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return file
	}
	kind := "download"
	write := flag&os.O_WRONLY != 0 || flag&os.O_RDWR != 0
	if write {
		kind = "upload"
	}
	return &transferFile{File: file, write: write, progress: a.beginTransfer(ctx, kind, s, name, info.Size())}
}
