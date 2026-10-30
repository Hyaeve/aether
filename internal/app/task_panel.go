package app

import (
	"net/http"
	"time"
)

type taskPanelEntry struct {
	ID         string    `json:"id"`
	TaskID     string    `json:"taskId,omitempty"`
	Kind       string    `json:"kind"`
	Source     string    `json:"source"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Message    string    `json:"message"`
	StorageID  string    `json:"storageId,omitempty"`
	Processed  int       `json:"processed"`
	Total      int       `json:"total"`
	TotalKnown bool      `json:"totalKnown"`
	Fraction   float64   `json:"fraction,omitempty"`
	Unit       string    `json:"unit,omitempty"`
	StartedAt  time.Time `json:"startedAt,omitempty"`
}

func (a *App) taskPanel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	st := a.store.snapshotWithLogLimit(0)
	items := make([]taskPanelEntry, 0)
	names := make(map[string]string, len(st.Tasks))
	for _, task := range st.Tasks {
		names[task.ID] = task.Name
		if task.Status != "running" {
			continue
		}
		unit := "项"
		if task.Kind == "cache" {
			unit = "目录"
		}
		items = append(items, taskPanelEntry{ID: "task:" + task.ID, TaskID: task.ID, Kind: task.Kind, Source: "task",
			Name: task.Name, Status: task.Status, Message: task.Message, StorageID: task.StorageID,
			Processed: task.ProgressDone, Total: task.ProgressTotal, TotalKnown: task.ProgressKnown, Fraction: task.ProgressFraction, Unit: unit, StartedAt: task.LastRun})
	}
	for _, job := range st.GenerationJobs {
		if job.Status == "running" || job.Status == "queued" {
			items = append(items, taskPanelEntry{ID: "generation:" + job.ID, Kind: job.Kind, Source: "generation",
				Name: job.Name, Status: job.Status, Message: job.Message, Processed: job.Processed,
				Total: job.Total, TotalKnown: job.Total > 0, Unit: "项", StartedAt: job.Created})
		}
	}
	a.scrapeMu.Lock()
	progress := a.scrapeProgress
	a.scrapeMu.Unlock()
	if progress.Running {
		name := names[progress.TaskID]
		if name == "" {
			name = "STRM 刮削"
		}
		items = append(items, taskPanelEntry{ID: "scrape:" + progress.TaskID, TaskID: progress.TaskID, Kind: "scrape", Source: "scrape",
			Name: name, Status: "running", Message: progress.Message, Processed: progress.Done,
			Total: progress.Total, TotalKnown: progress.Total > 0, Unit: "项"})
	}
	for _, rule := range st.Automations {
		if rule.Status == "running" {
			items = append(items, taskPanelEntry{ID: "automation:" + rule.ID, TaskID: rule.ID, Kind: "automation", Source: "automation",
				Name: rule.Name, Status: rule.Status, Message: rule.Message, StartedAt: rule.LastRun, Processed: rule.ProgressDone,
				Total: len(rule.Steps), TotalKnown: len(rule.Steps) > 0, Unit: "动作"})
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, map[string]any{"items": items})
}
