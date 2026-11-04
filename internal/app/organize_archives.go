package app

import (
	"errors"
	"path"
	"strconv"
	"time"
)

// recordOrganizeArchive is the organizer's durable receipt boundary. It records
// completed outcomes only; reading archives never starts an organization task.
// There is deliberately no browser endpoint for manufacturing these receipts.
func (a *App) recordOrganizeArchive(task Task, entry archiveEntry) error {
	if entry.SourcePath == "" || entry.TargetPath == "" || entry.Status == "" {
		return errors.New("整理档案缺少来源、归档位置或结果")
	}
	entry.ID = id()
	entry.Category = "organize"
	entry.TaskID = task.ID
	entry.TaskName = task.Name
	entry.TaskKind = "organize"
	if entry.StartedAt.IsZero() {
		entry.StartedAt = time.Now()
	}
	if entry.FinishedAt.IsZero() {
		entry.FinishedAt = time.Now()
	}
	if entry.Trigger == "" {
		entry.Trigger = "manual"
	}
	if entry.Filename == "" {
		entry.Filename = path.Base(entry.SourcePath)
	}
	entry.Path = entry.SourcePath
	entry.Folder = path.Dir(entry.TargetPath)
	key := entry.Kind + ":" + entry.Title + ":" + entry.Year
	if entry.Title == "" {
		key = entry.Folder
	}
	if entry.TMDB > 0 {
		key = entry.Kind + ":" + strconv.Itoa(entry.TMDB)
	}
	return a.archives.put(archiveRecord{archiveEntry: entry, GroupID: archiveStamp(task.ID, entry.TargetStorage, key)})
}
