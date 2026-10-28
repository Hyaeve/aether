package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const archivePageSize = 30

type archiveEntry struct {
	ID         string    `json:"id"`
	Category   string    `json:"category"`
	TaskID     string    `json:"taskId"`
	TaskName   string    `json:"taskName"`
	TaskKind   string    `json:"taskKind,omitempty"`
	Trigger    string    `json:"trigger"`
	Action     string    `json:"action"`
	Status     string    `json:"status"`
	Message    string    `json:"message"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
	Processed  int       `json:"processed,omitempty"`
	Path       string    `json:"path,omitempty"`
	Filename   string    `json:"filename,omitempty"`
	Folder     string    `json:"folder,omitempty"`
	Title      string    `json:"title,omitempty"`
	Kind       string    `json:"kind,omitempty"`
	TMDB       int       `json:"tmdb,omitempty"`
	Poster     string    `json:"poster,omitempty"`
}

type archiveRecord struct {
	archiveEntry
	RootStamp   string `json:"rootStamp,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	GroupID     string `json:"groupId,omitempty"`
}

type executionArchives struct {
	mu      sync.RWMutex
	store   *Store
	file    string
	records []archiveRecord
}

func openExecutionArchives(store *Store, dataDir string) (*executionArchives, error) {
	h := &executionArchives{store: store, file: filepath.Join(dataDir, "runtime", "taiyi-archives.json")}
	f, err := os.Open(h.file)
	if os.IsNotExist(err) {
		return h, nil
	}
	if err != nil {
		return nil, errors.New("无法读取档案中心")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	f.Close()
	if err != nil || len(raw) > 32<<20 {
		return nil, errors.New("档案中心文件过大或读取失败")
	}
	if err := store.decodeJSONConfig("runtime/taiyi-archives", raw, &h.records); err != nil {
		return nil, errors.New("档案中心完整性校验失败")
	}
	changed := false
	for i := range h.records {
		if h.records[i].Status == "running" {
			h.records[i].Status = "interrupted"
			h.records[i].Message = "服务重启，执行结果未确认"
			h.records[i].FinishedAt = time.Now()
			changed = true
		}
	}
	if changed {
		if err := h.save(); err != nil {
			return nil, err
		}
	}
	return h, nil
}

func (h *executionArchives) save() error {
	raw, err := h.store.encodeJSONConfig("runtime/taiyi-archives", h.records)
	if err != nil {
		return err
	}
	if len(raw) > 32<<20 {
		return errors.New("档案中心容量已达上限")
	}
	return durableConfigWrite(h.file, raw)
}

func (h *executionArchives) put(record archiveRecord) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	before := append([]archiveRecord(nil), h.records...)
	found := false
	for i := range h.records {
		if h.records[i].ID == record.ID {
			h.records[i] = record
			found = true
			break
		}
	}
	if !found {
		h.records = append(h.records, record)
	}
	sort.SliceStable(h.records, func(i, j int) bool {
		if h.records[i].StartedAt.Equal(h.records[j].StartedAt) {
			return h.records[i].ID < h.records[j].ID
		}
		return h.records[i].StartedAt.After(h.records[j].StartedAt)
	})
	counts := map[string]int{}
	kept := h.records[:0]
	for _, item := range h.records {
		limit := 10000
		if item.Category == "tasks" {
			limit = 2000
		}
		counts[item.Category]++
		if counts[item.Category] <= limit || item.Status == "running" {
			kept = append(kept, item)
		}
	}
	h.records = kept
	if err := h.save(); err != nil {
		h.records = before
		return err
	}
	return nil
}

func (h *executionArchives) get(id string) (archiveRecord, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, item := range h.records {
		if item.ID == id {
			return item, true
		}
	}
	return archiveRecord{}, false
}

func archiveStamp(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func archiveFingerprint(root *os.Root, name string) (string, error) {
	if !fs.ValidPath(name) || !strings.EqualFold(path.Ext(name), ".strm") {
		return "", errors.New("STRM 路径无效")
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil || info.Mode()&os.ModeSymlink != 0 || i < len(parts)-1 && !info.IsDir() || i == len(parts)-1 && !info.Mode().IsRegular() {
			return "", errors.New("STRM 文件或所在目录的类型已变化")
		}
	}
	f, err := root.Open(name)
	if err != nil {
		return "", errors.New("STRM 文件已不存在或不可读取")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return "", errors.New("STRM 文件类型或大小已变化")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return "", errors.New("STRM 文件读取失败")
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (a *App) newScrapeArchive(taskID, rootName, action string, root *os.Root, item scrapeItem, expectedFingerprint string) (archiveRecord, error) {
	fingerprint, err := archiveFingerprint(root, item.Path)
	if err != nil {
		return archiveRecord{}, err
	}
	if expectedFingerprint != "" && fingerprint != expectedFingerprint {
		return archiveRecord{}, errors.New("原 STRM 内容已变化")
	}
	rootStamp := archiveStamp(filepath.Clean(rootName))
	record := archiveRecord{
		archiveEntry: archiveEntry{
			ID: archiveStamp(taskID, rootStamp, item.Path), Category: "scrape",
			TaskID: taskID, Trigger: "manual", Action: action, Status: "running",
			StartedAt: time.Now(), Path: item.Path, Filename: path.Base(item.Path),
			Folder: scrapeWorkDir(item.Path), Title: item.Title, Kind: item.Kind,
		},
		RootStamp: rootStamp, Fingerprint: fingerprint,
		GroupID: archiveStamp(taskID, rootStamp, scrapeWorkKey(item)),
	}
	a.store.mu.RLock()
	for _, task := range a.store.state.Tasks {
		if task.ID == taskID {
			record.TaskName = task.Name
			break
		}
	}
	a.store.mu.RUnlock()
	return record, a.archives.put(record)
}

func (a *App) finishScrapeArchive(record archiveRecord, item scrapeItem, err error) error {
	record.Title, record.Kind, record.TMDB = item.Title, item.Kind, item.TMDB
	if item.MatchedTitle != "" {
		record.Title = item.MatchedTitle
	}
	record.FinishedAt = time.Now()
	switch {
	case errors.Is(err, context.Canceled):
		record.Status, record.Message = "cancelled", "识别刮削已取消"
	case err != nil || item.Status == "error" || item.Status == "miss":
		record.Status, record.Message = "failed", "识别或刮削失败，请查看运行日志"
		if item.Status == "miss" {
			record.Message = "未找到唯一匹配，请手动确认"
		}
	case item.Status == "doubt":
		record.Status, record.Message = "doubt", "存在多个同名匹配，请手动确认"
	case item.TMDB > 0:
		record.Status, record.Message, record.Poster = "success", "识别成功", item.Poster
		if item.Status == "ok" {
			record.Message = "元数据已写入"
		}
	default:
		record.Status, record.Message = "failed", "未取得有效识别结果"
	}
	if err := a.archives.put(record); err != nil {
		return err
	}
	level := "info"
	if record.Status != "success" {
		level = "warn"
	}
	a.store.event(level, "tasks", fmt.Sprintf("档案中心：STRM %s，任务 %q，原文件 %q：%s", record.Action, record.TaskName, record.Path, record.Message))
	return nil
}

type archiveGroup struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Kind     string `json:"kind"`
	TMDB     int    `json:"tmdb,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Folder   string `json:"folder"`
	TaskName string `json:"taskName"`
	Count    int    `json:"count"`
	Status   string `json:"status"`
}

func archivePriority(status string) int {
	return map[string]int{"success": 0, "doubt": 1, "failed": 2, "cancelled": 2, "interrupted": 2, "running": 3}[status]
}

func (a *App) archiveList(w http.ResponseWriter, r *http.Request) {
	category := r.PathValue("category")
	if category != "scrape" && category != "tasks" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	query := r.URL.Query()
	search, status, trigger := strings.ToLower(strings.TrimSpace(query.Get("q"))), query.Get("status"), query.Get("trigger")
	if len(search) > 300 {
		fail(w, 400, errors.New("搜索内容过长"))
		return
	}
	records := []archiveRecord{}
	a.archives.mu.RLock()
	for _, record := range a.archives.records {
		if record.Category != category || status != "" && record.Status != status || trigger != "" && record.Trigger != trigger {
			continue
		}
		if group := query.Get("group"); group != "" && record.GroupID != group {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(strings.Join([]string{record.TaskName, record.Title, record.Path}, " ")), search) {
			continue
		}
		records = append(records, record)
	}
	a.archives.mu.RUnlock()
	page, _ := strconv.Atoi(query.Get("page"))
	total := len(records)
	groups := []archiveGroup{}
	grouped := category == "scrape" && query.Get("mode") == "group"
	if grouped {
		indices := map[string]int{}
		for _, record := range records {
			if i, found := indices[record.GroupID]; found {
				groups[i].Count++
				if archivePriority(record.Status) > archivePriority(groups[i].Status) {
					groups[i].Status = record.Status
				}
				if groups[i].TMDB == 0 && record.Status == "success" && record.TMDB > 0 {
					groups[i].Title, groups[i].TMDB, groups[i].Poster = record.Title, record.TMDB, record.Poster
				}
			} else {
				indices[record.GroupID] = len(groups)
				groups = append(groups, archiveGroup{ID: record.GroupID, Title: record.Title, Kind: record.Kind, TMDB: record.TMDB, Poster: record.Poster, Folder: record.Folder, TaskName: record.TaskName, Count: 1, Status: record.Status})
			}
		}
		total = len(groups)
	}
	pages := max(1, (total+archivePageSize-1)/archivePageSize)
	page = min(max(1, page), pages)
	start, end := min(total, (page-1)*archivePageSize), min(total, page*archivePageSize)
	var items any
	if grouped {
		items = groups[start:end]
	} else {
		entries := make([]archiveEntry, 0, end-start)
		for _, record := range records[start:end] {
			entries = append(entries, record.archiveEntry)
		}
		items = entries
	}
	jsonResponse(w, 200, map[string]any{"items": items, "total": total, "page": page, "pages": pages, "pageSize": archivePageSize})
}

func (a *App) archiveRetry(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirmed bool `json:"confirmed"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Confirmed {
		fail(w, 400, errors.New("请确认重新识别刮削"))
		return
	}
	raw, _ := json.Marshal(map[string]any{"archiveId": r.PathValue("id"), "confirmed": true})
	request := r.Clone(r.Context())
	request.Body = io.NopCloser(bytes.NewReader(raw))
	request.ContentLength = int64(len(raw))
	request.SetPathValue("action", "identify")
	a.strmScrape(w, request)
}

// A historical path is not authority to scrape a new file or another output root.
func (a *App) validateArchiveRetry(id string) (archiveRecord, error) {
	record, found := a.archives.get(id)
	if !found || record.Category != "scrape" {
		return archiveRecord{}, errors.New("刮削档案已不存在")
	}
	rootName, err := a.scrapeRoot(record.TaskID)
	if err != nil || archiveStamp(filepath.Clean(rootName)) != record.RootStamp {
		return archiveRecord{}, errors.New("任务或生成位置已变化，请从刮削平台重新选择")
	}
	root, err := os.OpenRoot(rootName)
	if err != nil {
		return archiveRecord{}, errors.New("生成目录已不可读取")
	}
	defer root.Close()
	fingerprint, err := archiveFingerprint(root, record.Path)
	if err != nil || fingerprint != record.Fingerprint {
		return archiveRecord{}, errors.New("原 STRM 文件已变化，请从刮削平台重新识别")
	}
	return record, nil
}
