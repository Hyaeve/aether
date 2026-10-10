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
	"tasks":       {"status", "message", "processed", "lastRun", "nextRun"},
	"automations": {"status", "message", "lastRun", "nextRun"},
	"backupRules": {"status", "message", "scanned", "copied", "skipped", "phase", "total", "processed", "lastRun", "nextRun"},
	"storages":    {"status", "lastError"},
	"mounts":      {"status", "lastError"},
}
var runtimeModule = map[string]string{"tasks": "task/", "automations": "task/automation", "backupRules": "transfer/backup", "storages": "storage/storage", "mounts": "file/mount"}

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
	for _, field := range []string{"logs", "libraryNotices", "casTemporary"} {
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
	for _, field := range []string{"libraryNotices", "casTemporary"} {
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
		for _, field := range []string{"libraryNotices", "casTemporary"} {
			state[field] = runtime[field]
		}
		merged, _ := json.Marshal(state)
		if err := json.Unmarshal(merged, &s.state); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	// Missing runtime data can be rebuilt; corrupt authenticated data is not ignored.
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
