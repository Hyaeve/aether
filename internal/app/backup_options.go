package app

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

type BackupLocation struct {
	StorageID string `json:"storageId"`
	Path      string `json:"path"`
	Label     string `json:"label"`
}

type BackupFilter struct {
	Type      string `json:"type"`
	Mode      string `json:"mode"`
	Value     string `json:"value"`
	MatchDir  bool   `json:"matchDir"`
	MatchFile bool   `json:"matchFile"`
	MinSize   int64  `json:"minSize"`
	MaxSize   int64  `json:"maxSize"`
	Unit      string `json:"unit"`
}

func backupLocations(r BackupRule) ([]BackupLocation, []BackupLocation) {
	sources, targets := r.Sources, r.Targets
	if len(sources) == 0 && r.SourceID != "" {
		sources = []BackupLocation{{r.SourceID, r.Source, r.SourceLabel}}
	}
	if len(targets) == 0 && r.TargetID != "" {
		targets = []BackupLocation{{r.TargetID, r.Target, r.TargetLabel}}
	}
	return sources, targets
}

func backupPair(r BackupRule, source, target BackupLocation) BackupRule {
	r.Sources, r.Targets = nil, nil
	r.SourceID, r.Source, r.SourceLabel = source.StorageID, source.Path, source.Label
	r.TargetID, r.Target, r.TargetLabel = target.StorageID, target.Path, target.Label
	return r
}

func backupUnit(unit string) int64 {
	switch unit {
	case "B", "":
		return 1
	case "KB":
		return 1024
	case "MB":
		return 1024 * 1024
	case "GB":
		return 1024 * 1024 * 1024
	}
	return 0
}

func validateBackupOptions(r *BackupRule, st State) error {
	if r.SyncMode == "" {
		r.SyncMode = "one_way"
	}
	if r.SyncMode != "one_way" && r.SyncMode != "two_way" {
		return errors.New("同步模式无效")
	}
	if r.DeletionRule == "" {
		r.DeletionRule = "keep"
	}
	if r.DeletionRule != "keep" && r.DeletionRule != "trash" && r.DeletionRule != "delete" {
		return errors.New("删除规则无效")
	}
	if r.ConflictRule == "" {
		r.ConflictRule = "keep_both"
	}
	if r.ConflictRule != "keep_both" && r.ConflictRule != "source" && r.ConflictRule != "newest" {
		return errors.New("冲突规则无效")
	}
	if r.ConflictMarker == "" {
		r.ConflictMarker = "conflict copy"
	}
	if !safeName(r.ConflictMarker) || len(r.ConflictMarker) > 100 {
		return errors.New("冲突副本标记无效")
	}
	if r.TargetOnly == "" {
		r.TargetOnly = "copy"
	}
	if r.TargetOnly != "copy" && r.TargetOnly != "keep" {
		return errors.New("目标独有文件规则无效")
	}
	if r.HistoryDays < 0 || r.HistoryDays > 36500 || r.DeleteLimit < 0 || r.DeleteLimit > 100 || r.FullScanEvery < 0 || r.FullScanEvery > 100000 || r.FullScanHours < 0 || r.FullScanHours > 876000 {
		return errors.New("双向同步高级设置无效")
	}
	if r.SyncMode == "two_way" {
		r.CompletionRule = "keep"
	}
	if r.CompletionRule == "" {
		r.CompletionRule = "keep"
	}
	if r.CompletionRule != "keep" && r.CompletionRule != "delete_source" && r.CompletionRule != "delete_source_dir" {
		return errors.New("无效的备份完成操作")
	}
	if r.ScanInterval < 0 || r.ScanInterval > 31536000 || r.ScanInterval > 0 && r.ScanInterval < 60 {
		return errors.New("扫描间隔须为0或60至31536000秒")
	}
	if len(r.Filters) > 32 {
		return errors.New("最多32条筛选规则")
	}
	for i := range r.Filters {
		f := &r.Filters[i]
		f.Value = strings.TrimSpace(f.Value)
		if len(f.Value) > 2048 || f.Mode != "include" && f.Mode != "exclude" || !f.MatchDir && !f.MatchFile {
			return fmt.Errorf("筛选规则%d的范围或模式无效", i+1)
		}
		switch f.Type {
		case "name", "regex":
			if f.Value == "" {
				return fmt.Errorf("请填写筛选规则%d的匹配内容", i+1)
			}
			if f.Type == "regex" {
				if _, err := regexp.Compile(f.Value); err != nil {
					return fmt.Errorf("筛选规则%d的正则表达式无效", i+1)
				}
			}
			if f.Type == "name" {
				for _, word := range strings.Split(f.Value, ";") {
					if !strings.ContainsAny(word, "*?") {
						continue
					}
					if _, err := path.Match(strings.TrimSpace(word), ""); err != nil {
						return fmt.Errorf("筛选规则%d的名称通配符无效", i+1)
					}
				}
			}
		case "extension":
			value, err := normalizeExtensions(f.Value)
			if err != nil || value == "" {
				return fmt.Errorf("筛选规则%d的扩展名无效", i+1)
			}
			f.Value = value
		case "size":
			factor := backupUnit(f.Unit)
			if factor == 0 || f.MinSize < 0 || f.MaxSize < 0 || f.MaxSize > 0 && f.MaxSize < f.MinSize || f.MinSize > (1<<63-1)/factor || f.MaxSize > (1<<63-1)/factor {
				return fmt.Errorf("筛选规则%d的大小范围无效", i+1)
			}
		default:
			return errors.New("不支持的筛选类型")
		}
		if (f.Type == "extension" || f.Type == "size") && !f.MatchFile {
			return errors.New("扩展名和大小筛选必须作用于文件")
		}
	}
	sources, targets := backupLocations(*r)
	if r.MonitorEnabled || r.CompletionRule == "delete_source_dir" {
		monitored := sources
		if r.SyncMode == "two_way" {
			monitored = append(append([]BackupLocation{}, sources...), targets...)
		}
		for _, loc := range monitored {
			s, err := backupStorage(st, loc.StorageID)
			if err != nil {
				return err
			}
			if s.Type != "local" {
				return errors.New("文件系统监听及空目录安全清理仅支持本地源目录；云盘可使用定时扫描和删除源文件")
			}
		}
	}
	if len(sources) == 0 || len(targets) == 0 || len(sources) > 16 || len(targets) > 16 {
		return errors.New("请选择源和目标目录，每类最多16个")
	}
	for _, list := range [][]BackupLocation{sources, targets} {
		seen := map[string]bool{}
		for _, loc := range list {
			if _, err := backupStorage(st, loc.StorageID); err != nil {
				return err
			}
			if loc.Path == "" || len(loc.Path) > 4096 || len(loc.Label) > 4096 {
				return errors.New("备份目录无效")
			}
			key := loc.StorageID + "\x00" + loc.Path
			if seen[key] {
				return errors.New("备份目录不能重复添加")
			}
			seen[key] = true
		}
	}
	r.SourceID, r.Source, r.SourceLabel = sources[0].StorageID, sources[0].Path, sources[0].Label
	r.TargetID, r.Target, r.TargetLabel = targets[0].StorageID, targets[0].Path, targets[0].Label
	return nil
}

type compiledBackupFilter struct {
	BackupFilter
	expression *regexp.Regexp
}

func compileBackupFilters(filters []BackupFilter) ([]compiledBackupFilter, error) {
	result := make([]compiledBackupFilter, 0, len(filters))
	for _, f := range filters {
		item := compiledBackupFilter{BackupFilter: f}
		if f.Type == "regex" {
			var err error
			item.expression, err = regexp.Compile(f.Value)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func backupFilterAccept(filters []compiledBackupFilter, f File, relative string) bool {
	hasInclude, included := false, false
	for _, filter := range filters {
		if f.IsDir && (!filter.MatchDir || filter.Type == "extension" || filter.Type == "size") || !f.IsDir && !filter.MatchFile {
			continue
		}
		matched := false
		switch filter.Type {
		case "extension":
			matched = excludedType(f.Name, filter.Value)
		case "size":
			factor := backupUnit(filter.Unit)
			matched = f.Size >= filter.MinSize*factor && (filter.MaxSize == 0 || f.Size <= filter.MaxSize*factor)
		case "regex":
			matched = filter.expression.MatchString(f.Name) || filter.expression.MatchString(relative)
		case "name":
			for _, word := range strings.Split(filter.Value, ";") {
				word = strings.ToLower(strings.TrimSpace(word))
				if word == "" {
					continue
				}
				if strings.ContainsAny(word, "*?") {
					a, _ := path.Match(word, strings.ToLower(f.Name))
					b, _ := path.Match(word, strings.ToLower(relative))
					matched = matched || a || b
				} else {
					matched = matched || strings.Contains(strings.ToLower(f.Name), word)
				}
			}
		}
		if filter.Mode == "exclude" && matched {
			return false
		}
		if filter.Mode == "include" {
			hasInclude, included = true, included || matched
		}
	}
	return !hasInclude || included
}
