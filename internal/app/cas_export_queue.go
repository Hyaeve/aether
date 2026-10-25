package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type casExportJob struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Processed int       `json:"processed"`
	Total     int       `json:"total"`
	Message   string    `json:"message"`
	Name      string    `json:"name,omitempty"`
	Path      string    `json:"path,omitempty"`
	Created   time.Time `json:"created"`
	Download  bool      `json:"download"`
	request   casExportRequest
	output    string
	revision  string
}
type casExportProgressKey struct{}

func casExportProgress(ctx context.Context, processed, total int) {
	if update, ok := ctx.Value(casExportProgressKey{}).(func(int, int)); ok {
		update(processed, total)
	}
}

func (a *App) casExportJobAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	var v casExportRequest
	if !decode(w, r, &v) {
		return
	}
	s, err := a.store.storage(v.StorageID)
	if err != nil || !s.Enabled || s.Type != "local" && s.Type != "mobile" || !v.Confirm || len(v.IDs) == 0 || len(v.IDs) > 10000 {
		fail(w, 400, errors.New("请确认有效CAS导出范围"))
		return
	}
	if err = a.casExportDirectory(v.Directory); err != nil {
		fail(w, 400, err)
		return
	}
	if _, err = normalizeExtensions(v.Extensions); err != nil {
		fail(w, 400, err)
		return
	}
	a.casExportMu.Lock()
	if a.casExportJobs == nil {
		a.casExportJobs = map[string]*casExportJob{}
	}
	for key, job := range a.casExportJobs {
		if time.Since(job.Created) > 24*time.Hour && job.Status != "queued" && job.Status != "running" {
			if job.output != "" {
				_ = os.Remove(job.output)
			}
			delete(a.casExportJobs, key)
		}
	}
	if len(a.casExportQueue) >= 16 || len(a.casExportJobs) >= 100 {
		a.casExportMu.Unlock()
		fail(w, 409, errors.New("导出队列已满，请稍后再试"))
		return
	}
	job := &casExportJob{ID: id(), Status: "queued", Message: "排队等待导出", Created: time.Now(), Download: v.Directory == "", request: v, revision: rapid115Stamp(s.Config)}
	a.casExportJobs[job.ID] = job
	a.casExportQueue = append(a.casExportQueue, job)
	start := !a.casExportRunning
	a.casExportRunning = true
	result := *job
	a.casExportMu.Unlock()
	a.store.event("info", "files", "CAS导出加入队列："+job.ID+"；存储池 "+s.Name)
	if start {
		go a.runCASExports()
	}
	jsonResponse(w, 202, result)
}
func (a *App) casExportJobStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a.casExportMu.Lock()
	job := a.casExportJobs[r.PathValue("id")]
	if job == nil {
		a.casExportMu.Unlock()
		fail(w, 404, errors.New("导出任务不存在或已过期"))
		return
	}
	value := *job
	a.casExportMu.Unlock()
	jsonResponse(w, 200, value)
}
func (a *App) casExportJobDownload(w http.ResponseWriter, r *http.Request) {
	a.casExportMu.Lock()
	job := a.casExportJobs[r.PathValue("id")]
	if job == nil || job.Status != "success" || !job.Download || time.Since(job.Created) > 24*time.Hour {
		a.casExportMu.Unlock()
		fail(w, 404, errors.New("导出文件尚未完成或已过期"))
		return
	}
	name, output := job.Name, job.output
	a.casExportMu.Unlock()
	file, err := os.Open(output)
	if err != nil {
		fail(w, 404, errors.New("导出文件不存在"))
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fail(w, 500, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	http.ServeContent(w, r, name, info.ModTime(), file)
}

type casExportWriter struct {
	header http.Header
	code   int
	file   *os.File
	err    error
}

func (w *casExportWriter) Header() http.Header { return w.header }
func (w *casExportWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}
func (w *casExportWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = 200
	}
	n, err := w.file.Write(b)
	if err != nil {
		w.err = err
	}
	return n, err
}
func (a *App) runCASExports() {
	for {
		a.casExportMu.Lock()
		if len(a.casExportQueue) == 0 {
			a.casExportRunning = false
			a.casExportMu.Unlock()
			return
		}
		job := a.casExportQueue[0]
		a.casExportQueue = a.casExportQueue[1:]
		job.Status = "running"
		job.Message = "扫描导出范围"
		a.casExportMu.Unlock()
		a.executeCASExport(job)
	}
}
func (a *App) executeCASExport(job *casExportJob) {
	var output string
	var err error
	defer func() {
		a.casExportMu.Lock()
		if err != nil {
			job.Status = "error"
			job.Message = "导出失败，请查看运行日志"
		} else {
			job.Status = "success"
			job.Message = "导出完成"
		}
		a.casExportMu.Unlock()
		if err != nil {
			if output != "" {
				_ = os.Remove(output)
			}
			a.store.event("error", "files", "CAS导出失败："+job.ID+"；"+err.Error())
		} else {
			a.store.event("info", "files", "CAS导出任务完成："+job.ID)
		}
	}()
	s, e := a.store.storage(job.request.StorageID)
	if e != nil || !s.Enabled || rapid115Stamp(s.Config) != job.revision {
		err = errors.New("源存储配置已变化，排队导出已停止")
		return
	}
	root := filepath.Join(a.dataDir, "cache", "cas-export-results")
	if err = os.MkdirAll(root, 0700); err != nil {
		return
	}
	file, e := os.CreateTemp(root, "cas-export-"+job.ID+"-")
	if e != nil {
		err = e
		return
	}
	output = file.Name()
	defer file.Close()
	raw, _ := json.Marshal(job.request)
	req, e := http.NewRequest(http.MethodPost, "/api/files/cas-export", bytes.NewReader(raw))
	if e != nil {
		err = e
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
	defer cancel()
	ctx = context.WithValue(ctx, casExportProgressKey{}, func(processed, total int) {
		a.casExportMu.Lock()
		job.Processed = processed
		job.Total = total
		job.Message = "正在生成CAS"
		a.casExportMu.Unlock()
	})
	req = req.WithContext(ctx)
	writer := &casExportWriter{header: http.Header{}, file: file}
	a.casExport(writer, req)
	if writer.err != nil {
		err = errors.New("导出结果暂存写入失败")
		return
	}
	if e = file.Sync(); e != nil {
		err = e
		return
	}
	if writer.code != 200 {
		err = errors.New("导出处理失败（日志记录任务阶段；无完整产物）")
		return
	}
	if _, e = file.Seek(0, io.SeekStart); e != nil {
		err = e
		return
	}
	a.casExportMu.Lock()
	defer a.casExportMu.Unlock()
	if job.Download {
		_, params, _ := mime.ParseMediaType(writer.header.Get("Content-Disposition"))
		job.Name = params["filename"]
		job.output = output
	} else {
		var result struct {
			Path string `json:"path"`
		}
		if e = json.NewDecoder(file).Decode(&result); e != nil {
			err = e
			return
		}
		job.Path = result.Path
		_ = file.Close()
		_ = os.Remove(output)
	}
}

func (a *App) cleanupCASExportResults() {
	a.casExportMu.Lock()
	defer a.casExportMu.Unlock()
	active := map[string]bool{}
	for key, job := range a.casExportJobs {
		if job.Status == "running" || job.Status == "queued" || time.Since(job.Created) <= 24*time.Hour {
			active[job.output] = true
			continue
		}
		if job.output != "" {
			_ = os.Remove(job.output)
		}
		delete(a.casExportJobs, key)
	}
	root := filepath.Join(a.dataDir, "cache", "cas-export-results")
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(entry.Name(), "cas-export-") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err == nil && !active[path] && info.Mode().IsRegular() && time.Since(info.ModTime()) > 24*time.Hour {
			_ = os.Remove(path)
		}
	}
}

func (a *App) casExportResultMonitor() {
	a.cleanupCASExportResults()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.cleanupCASExportResults()
		}
	}
}
