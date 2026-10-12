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
	Type          string `json:"type"`
	Mode          string `json:"mode"`
	Value         string `json:"value"`
	MatchDir      bool   `json:"matchDir"`
	MatchFile     bool   `json:"matchFile"`
	MinSize       int64  `json:"minSize"`
	MaxSize       int64  `json:"maxSize"`
	Unit          string `json:"unit"`
	MinUnit       string `json:"minUnit,omitempty"`
	MaxUnit       string `json:"maxUnit,omitempty"`
	CaseSensitive bool   `json:"caseSensitive,omitempty"`
}

func backupFilterPath(value string) (string, error) {
	value = strings.ReplaceAll(value, `\`, "/")
	if value == "" || strings.HasPrefix(value, "/") || strings.ContainsAny(value, ":\x00\r\n") {
		return "", errors.New("筛选路径须为源目录内的相对路径")
	}
	value = strings.TrimSuffix(value, "/")
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("筛选路径不能包含空目录段、.或..")
		}
	}
	return value, nil
}

func backupFilterExtensions(value string, sensitive bool) (string, error) {
	if !sensitive {
		return normalizeExtensions(value)
	}
	tokens, seen := []string{}, map[string]bool{}
	for _, token := range strings.Split(value, ";") {
		token = strings.TrimPrefix(strings.TrimSpace(token), ".")
		if token == "" {
			continue
		}
		if !extensionToken.MatchString(strings.ToLower(token)) {
			return "", errors.New("扩展名无效")
		}
		if !seen[token] {
			tokens, seen[token] = append(tokens, token), true
		}
	}
	return strings.Join(tokens, ";"), nil
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
		if (f.Type == "path_prefix" || f.Type == "path_regex") && !f.MatchDir && !f.MatchFile {
			f.MatchDir, f.MatchFile = true, true
		}
		// Bind the rule option before either execution path compiles filters. Keeping
		// it in the filter JSON also invalidates sync baselines when matching changes.
		f.CaseSensitive = r.CaseSensitive
		f.Value = strings.TrimSpace(f.Value)
		if len(f.Value) > 2048 || f.Mode != "include" && f.Mode != "exclude" || !f.MatchDir && !f.MatchFile {
			return fmt.Errorf("筛选规则%d的范围或模式无效", i+1)
		}
		switch f.Type {
		case "name", "regex", "path_regex":
			if f.Value == "" {
				return fmt.Errorf("请填写筛选规则%d的匹配内容", i+1)
			}
			if f.Type == "regex" || f.Type == "path_regex" {
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
			value, err := backupFilterExtensions(f.Value, r.CaseSensitive)
			if err != nil || value == "" {
				return fmt.Errorf("筛选规则%d的扩展名无效", i+1)
			}
			f.Value = value
		case "path_prefix":
			var prefixes []string
			for _, line := range strings.Split(strings.ReplaceAll(f.Value, "\r\n", "\n"), "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				value, err := backupFilterPath(strings.TrimSpace(line))
				if err != nil {
					return fmt.Errorf("筛选规则%d的路径前缀无效：%w", i+1, err)
				}
				prefixes = append(prefixes, value)
			}
			if len(prefixes) == 0 {
				return errors.New("请填写路径前缀")
			}
			f.Value = strings.Join(prefixes, "\n")
		case "size":
			minFactor, maxFactor := backupSizeFactors(*f)
			if minFactor == 0 || maxFactor == 0 || f.MinSize < 0 || f.MaxSize < 0 || f.MinSize > (1<<63-1)/minFactor || f.MaxSize > (1<<63-1)/maxFactor || f.MaxSize > 0 && f.MaxSize*maxFactor < f.MinSize*minFactor {
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

func backupSizeFactors(f BackupFilter) (int64, int64) {
	minUnit, maxUnit := f.MinUnit, f.MaxUnit
	if minUnit == "" {
		minUnit = f.Unit
	}
	if maxUnit == "" {
		maxUnit = f.Unit
	}
	return backupUnit(minUnit), backupUnit(maxUnit)
}

type compiledBackupFilter struct {
	BackupFilter
	expression *regexp.Regexp
}

func compileBackupFilters(filters []BackupFilter) ([]compiledBackupFilter, error) {
	result := make([]compiledBackupFilter, 0, len(filters))
	for _, f := range filters {
		item := compiledBackupFilter{BackupFilter: f}
		if f.Type == "path_prefix" {
			var values []string
			for _, line := range strings.Split(f.Value, "\n") {
				value, err := backupFilterPath(strings.TrimSpace(line))
				if err != nil {
					return nil, err
				}
				values = append(values, value)
			}
			item.Value = strings.Join(values, "\n")
		}
		if f.Type == "regex" || f.Type == "path_regex" {
			var err error
			expression := f.Value
			if !f.CaseSensitive {
				expression = "(?i:" + expression + ")"
			}
			item.expression, err = regexp.Compile(expression)
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
		name, candidate := f.Name, relative
		if !filter.CaseSensitive {
			name, candidate = strings.ToLower(name), strings.ToLower(candidate)
		}
		switch filter.Type {
		case "extension":
			extension := strings.TrimPrefix(path.Ext(name), ".")
			for _, token := range strings.Split(filter.Value, ";") {
				if !filter.CaseSensitive {
					token = strings.ToLower(token)
				}
				matched = matched || extension != "" && extension == token
			}
		case "size":
			minFactor, maxFactor := backupSizeFactors(filter.BackupFilter)
			matched = f.Size >= filter.MinSize*minFactor && (filter.MaxSize == 0 || f.Size <= filter.MaxSize*maxFactor)
		case "regex":
			matched = filter.expression.MatchString(f.Name) || filter.expression.MatchString(relative)
		case "path_prefix", "path_regex":
			value, err := backupFilterPath(relative)
			if err != nil {
				return false
			}
			if filter.Type == "path_regex" {
				matched = filter.expression.MatchString(value)
				if filter.Mode == "include" && f.IsDir {
					matched = true
				}
				break
			}
			for _, prefix := range strings.Split(filter.Value, "\n") {
				if !filter.CaseSensitive {
					value, prefix = strings.ToLower(value), strings.ToLower(prefix)
				}
				matched = matched || value == prefix || strings.HasPrefix(value, prefix+"/")
				if filter.Mode == "include" && f.IsDir && strings.HasPrefix(prefix, value+"/") {
					matched = true
				}
			}
		case "name":
			for _, word := range strings.Split(filter.Value, ";") {
				word = strings.TrimSpace(word)
				if !filter.CaseSensitive {
					word = strings.ToLower(word)
				}
				if word == "" {
					continue
				}
				if strings.ContainsAny(word, "*?") {
					a, _ := path.Match(word, name)
					b, _ := path.Match(word, candidate)
					matched = matched || a || b
				} else {
					matched = matched || strings.Contains(name, word)
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
