package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
)

type scrapeRequest struct {
	ArchiveID      string   `json:"archiveId"`
	TaskID         string   `json:"taskId"`
	Path           string   `json:"path"`
	TMDB           int      `json:"tmdb"`
	Kind           string   `json:"kind"`
	Group          bool     `json:"group"`
	Confirmed      bool     `json:"confirmed"`
	Scope          string   `json:"scope"`
	Scopes         []string `json:"scopes"`
	ExcludedScopes []string `json:"excludedScopes"`
	Paths          []string `json:"paths"`
	DeleteFiles    bool     `json:"deleteFiles"`
	Scrape         bool     `json:"scrape"`
	Reidentify     bool     `json:"reidentify"`
}
type queuedScrapeMatch struct {
	request     scrapeRequest
	root, stamp string
	identity    os.FileInfo
	epoch       uint64
}
type queuedScrapeKey struct{}

func (a *App) scrapeQueueStamp(taskID string) string {
	st := a.store.snapshotWithLogLimit(0)
	stamp := fmt.Sprintf("%#v", a.scrapeSourceConfig(taskID))
	for _, t := range st.Tasks {
		if t.ID == taskID {
			s, _ := a.store.storage(t.StorageID)
			stamp += syncQueueStamp(t) + storageRevision(s)
		}
	}
	if card, ok := shareScrapeCard(st, taskID); ok {
		stamp += shareScrapeRevision(card, shareScrapeResult(st, taskID, card))
	}
	return archiveStamp(stamp)
}

// scrapeMu is held. Never edit the index of a currently running scrape.
func (a *App) queueScrapeMatch(in scrapeRequest) error {
	if len(a.scrapeQueue) >= 100 {
		return errors.New("手动识别队列已满，请等待当前队列完成")
	}
	if in.TMDB < 1 || (in.Kind != "movie" && in.Kind != "tv") || in.Path == "" && in.Scope == "" {
		return errors.New("请输入有效的作品、TMDB ID 和媒体类型")
	}
	if in.ArchiveID != "" || in.Scope != "" && (!fs.ValidPath(in.Scope) || in.Scope == "." || in.Path != "") || len(in.Scopes)+len(in.ExcludedScopes)+len(in.Paths) > 0 || in.DeleteFiles {
		return errors.New("手动匹配排队只允许选择单个作品")
	}
	root, err := a.scrapeRoot(in.TaskID)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return errors.New("生成目录不存在，请先生成STRM")
	}
	index := a.loadScrapeIndex(in.TaskID, root)
	found := false
	for _, item := range index.Items {
		if item.Path == in.Path || in.Scope != "" && strings.HasPrefix(item.Path, in.Scope+"/") {
			found = true
			break
		}
	}
	if !found {
		return errors.New("作品已变化，请刷新后重新选择")
	}
	for _, q := range a.scrapeQueue {
		if q.request.TaskID == in.TaskID && q.request.Path == in.Path && q.request.Scope == in.Scope {
			return errors.New("该作品已在手动识别队列中")
		}
	}
	a.scrapeQueue = append(a.scrapeQueue, queuedScrapeMatch{in, root, a.scrapeQueueStamp(in.TaskID), info, a.scrapeQueueEpoch})
	return nil
}

type scrapeQueueResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *scrapeQueueResponse) Header() http.Header    { return w.header }
func (w *scrapeQueueResponse) WriteHeader(status int) { w.status = status }
func (w *scrapeQueueResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(p)
}

// Reserve the dispatch gap, then reuse the full match validation and execution
// path. The current worker remains alive until the next worker is registered.
func (a *App) drainScrapeQueue() {
	for {
		a.scrapeMu.Lock()
		if a.ctx.Err() != nil {
			a.scrapeQueue = nil
		}
		if a.scrapeProgress.Running || a.scrapeDispatch || len(a.scrapeQueue) == 0 {
			a.scrapeMu.Unlock()
			return
		}
		q := a.scrapeQueue[0]
		a.scrapeQueue = a.scrapeQueue[1:]
		a.scrapeDispatch = true
		a.scrapeMu.Unlock()
		body, _ := json.Marshal(q.request)
		r, _ := http.NewRequestWithContext(context.WithValue(a.ctx, queuedScrapeKey{}, q), "POST", "http://localhost/api/strm-scrape/match", bytes.NewReader(body))
		r.SetPathValue("action", "match")
		w := &scrapeQueueResponse{header: http.Header{}}
		a.strmScrape(w, r)
		a.scrapeMu.Lock()
		a.scrapeDispatch = false
		a.scrapeMu.Unlock()
		if w.status >= 400 {
			a.store.event("warn", "tasks", "排队的手动识别未执行："+w.body.String())
		}
	}
}
