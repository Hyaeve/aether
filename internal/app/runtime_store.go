package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

var runtimeFields = map[string][]string{
	"tasks":       {"status", "message", "processed", "lastRun", "nextRun", "casGenerated", "casFailed"},
	"automations": {"status", "message", "lastRun", "nextRun", "lastResult"},
	"backupRules": {"status", "message", "scanned", "copied", "skipped", "deleted", "phase", "total", "processed", "lastRun", "nextRun", "syncPending"},
	"storages":    {"status", "lastError", "health", "usage"},
	"mounts":      {"status", "lastError"},
}
var runtimeModule = map[string]string{"tasks": "task/", "automations": "task/automation", "backupRules": "transfer/backup", "storages": "storage/storage", "mounts": "file/mount"}
var runtimeRootFields = []string{"libraryNotices", "shareNotices", "casTemporary", "casStats", "renameHistory", "ed2kTemporary", "ed2kStats", "generationJobs", "fileFavorites", "fileVisits", "rapid115Records", "rapid115Attempts", "rapid115Retries", "simulcastTemporary", "cloudCASRecords", "telegramRuntime", "quarkLibraryRuntime", "folderCleanupReceipts"}

func stripRuntimeRecords(value any, fields []string) any {
	raw, _ := json.Marshal(value)
	tree, _ := configTree(raw)
	records, _ := tree.([]any)
	for _, value := range records {
		record := value.(map[string]any)
		for _, field := range fields {
			delete(record, field)
		}
	}
	return records
}
func stripRuntimeConfig(values map[string]any) {
	for kind, prefix := range runtimeModule {
		for module, value := range values {
			if module == prefix || kind == "tasks" && (module == "task/strm" || module == "task/cas" || module == "task/ed2k" || module == "task/cache" || module == "task/other") {
				values[module] = stripRuntimeRecords(value, runtimeFields[kind])
			}
		}
	}
	raw, _ := json.Marshal(values["state"])
	tree, _ := configTree(raw)
	root := tree.(map[string]any)
	for _, field := range append([]string{"logs"}, runtimeRootFields...) {
		delete(root, field)
	}
	values["state"] = root
}

func (s *Store) runtimeValues() map[string]any {
	raw, _ := json.Marshal(s.state)
	tree, _ := configTree(raw)
	state := tree.(map[string]any)
	result := map[string]any{}
	for kind, fields := range runtimeFields {
		records := map[string]any{}
		list, _ := state[kind].([]any)
		for _, value := range list {
			record := value.(map[string]any)
			runtime := map[string]any{}
			for _, field := range fields {
				runtime[field] = record[field]
			}
			records[record["id"].(string)] = runtime
		}
		result[kind] = records
	}
	for _, field := range runtimeRootFields {
		result[field] = state[field]
	}
	return result
}

func (s *Store) saveRuntimeLocked() error {
	if s.runtimeDir == "" {
		return nil
	}
	values := s.runtimeValues()
	raw, err := json.Marshal(values)
	if err != nil {
		return err
	}
	if bytes.Equal(raw, s.runtimeCache) {
		return nil
	}
	encoded, err := s.encodeJSONConfig("runtime/state", values)
	if err != nil {
		return err
	}
	if err = durableConfigWrite(filepath.Join(s.runtimeDir, "state.json"), encoded); err != nil {
		return err
	}
	s.runtimeCache = raw
	return nil
}

func (s *Store) initRuntime(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	s.runtimeDir = dir
	raw, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err == nil && !s.state.RuntimeSeparated {
		// Configuration import resets task results, not the administrator's file records.
		var records struct {
			QuarkLibrary          quarkLibraryRuntime             `json:"quarkLibraryRuntime"`
			Telegram              telegramRuntime                 `json:"telegramRuntime"`
			Favorites             []FileFavorite                  `json:"fileFavorites"`
			Visits                []FileVisit                     `json:"fileVisits"`
			RapidAttempts         map[string]bool                 `json:"rapid115Attempts"`
			RapidRecords          []Rapid115Record                `json:"rapid115Records"`
			RapidRetries          map[string]rapid115Retry        `json:"rapid115Retries"`
			SimulcastTemporary    []SimulcastTemporary            `json:"simulcastTemporary"`
			CloudCASRecords       []cloudCASRecord                `json:"cloudCASRecords"`
			FolderCleanupReceipts map[string]folderCleanupReceipt `json:"folderCleanupReceipts"`
		}
		if err := s.decodeJSONConfig("runtime/state", raw, &records); err != nil {
			return err
		}
		if records.Favorites != nil {
			s.state.FileFavorites = records.Favorites
		}
		if records.Visits != nil {
			s.state.FileVisits = records.Visits
		}
		s.state.Rapid115Attempts, s.state.Rapid115Records, s.state.SimulcastTemporary = records.RapidAttempts, records.RapidRecords, records.SimulcastTemporary
		s.state.Rapid115Retries = records.RapidRetries
		s.state.CloudCASRecords = records.CloudCASRecords
		s.state.FolderCleanupReceipts = records.FolderCleanupReceipts
		s.state.TelegramRuntime = records.Telegram
		s.state.QuarkLibraryRuntime = records.QuarkLibrary
	}
	if err == nil && s.state.RuntimeSeparated {
		var plain json.RawMessage
		if err := s.decodeJSONConfig("runtime/state", raw, &plain); err != nil {
			return err
		}
		tree, err := configTree(plain)
		if err != nil {
			return err
		}
		runtime, ok := tree.(map[string]any)
		if !ok {
			return errors.New("运行态格式无效")
		}
		current, _ := json.Marshal(s.state)
		tree, _ = configTree(current)
		state := tree.(map[string]any)
		for kind, fields := range runtimeFields {
			records, _ := runtime[kind].(map[string]any)
			list, _ := state[kind].([]any)
			for _, value := range list {
				record := value.(map[string]any)
				data, _ := records[record["id"].(string)].(map[string]any)
				for _, field := range fields {
					if value, ok := data[field]; ok {
						record[field] = value
					}
				}
			}
		}
		for _, field := range runtimeRootFields {
			if value, exists := runtime[field]; exists {
				if field == "fileFavorites" && len(s.state.FileFavorites) > 0 {
					var saved []FileFavorite
					raw, _ := json.Marshal(value)
					if err := json.Unmarshal(raw, &saved); err != nil {
						return err
					}
					for _, legacy := range s.state.FileFavorites {
						found := false
						for _, item := range saved {
							if item.Storage == legacy.Storage && item.ID == legacy.ID {
								found = true
								break
							}
						}
						if !found && len(saved) < 2000 {
							saved = append(saved, legacy)
						}
					}
					value = saved
				}
				state[field] = value
			}
		}
		merged, _ := json.Marshal(state)
		if err := json.Unmarshal(merged, &s.state); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	// Missing runtime data can be rebuilt; corrupt authenticated data is not ignored.
	for key, result := range s.state.QuarkLibraryRuntime.Results {
		if result.Status == "running" {
			result.Status, result.Message = "interrupted", "服务重启，生成已中断，已生成文件保留"
			s.state.QuarkLibraryRuntime.Results[key] = result
		}
	}
	for i := range s.state.TelegramRuntime.Jobs {
		job := &s.state.TelegramRuntime.Jobs[i]
		if (job.Status == "choosing" || job.Status == "confirming") && job.MessageID > 0 {
			job.CleanupPending = true
		}
		if telegramActive(job.Status) {
			job.Status = "interrupted"
			job.Link = ""
		}
	}
	for i := range s.state.CloudCASRecords {
		if s.state.CloudCASRecords[i].Status == "running" {
			s.state.CloudCASRecords[i].Status = "interrupted"
		}
	}
	for i := range s.state.Rapid115Records {
		if s.state.Rapid115Records[i].Status == "running" {
			s.state.Rapid115Records[i].Status = "failed"
			s.state.Rapid115Records[i].Message = "服务重启，执行结果未确认；源文件保留，请检查网盘"
		}
	}
	revision, legacyRevision := rapid115Stamp(s.state.Rapid115), rapid115RawStamp(s.state.Rapid115)
	for key, retry := range s.state.Rapid115Retries {
		// Only an exact old configuration digest can migrate. In-flight and
		// unrelated receipts must never become replayable after an upgrade.
		if retry.Revision == legacyRevision {
			retry.Revision = revision
			s.state.Rapid115Retries[key] = retry
		}
		if s.state.Rapid115Attempts[key] || retry.Revision != revision {
			delete(s.state.Rapid115Retries, key)
		}
	}
	for i := range s.state.GenerationJobs {
		if s.state.GenerationJobs[i].Status == "running" || s.state.GenerationJobs[i].Status == "queued" {
			s.state.GenerationJobs[i].Status, s.state.GenerationJobs[i].Message = "interrupted", "服务重启，生成已中断，请重新解析"
			if generationHasTemporary(s.state.GenerationJobs[i]) {
				s.state.GenerationJobs[i].Message = "服务重启，临时转存或清理结果未确认；请核对网盘临时目录，勿直接重复提交"
			}
		}
	}
	for i := range s.state.Tasks {
		t := &s.state.Tasks[i]
		if t.Status == "running" {
			t.Status, t.Message = "interrupted", "服务重启，任务已中断"
		}
		if t.Kind != "cache" && t.Cron == "" {
			t.NextRun = time.Time{}
		}
	}
	for i := range s.state.Automations {
		if s.state.Automations[i].Status == "running" {
			s.state.Automations[i].Status, s.state.Automations[i].Message = "interrupted", "服务重启，联动已中断"
		}
	}
	for i := range s.state.BackupRules {
		if s.state.BackupRules[i].Status == "running" {
			s.state.BackupRules[i].Status, s.state.BackupRules[i].Message = "interrupted", "服务重启，备份已中断"
		}
		s.state.BackupRules[i].NextRun = backupNext(s.state.BackupRules[i], time.Now())
	}
	if err := s.saveRuntimeLocked(); err != nil {
		return err
	}
	s.state.RuntimeSeparated = true
	s.jsonCache = nil
	if !s.modular {
		return errors.New("运行态分离需要模块配置")
	}
	return s.saveJSONModules()
}
