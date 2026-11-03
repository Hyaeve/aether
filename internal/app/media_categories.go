package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type mediaCategoryDirectory struct {
	ID        string   `json:"id"`
	MediaType string   `json:"mediaType"`
	Name      string   `json:"name"`
	Path      []string `json:"path"`
	Enabled   bool     `json:"enabled"`
}

type mediaCategoryCondition struct {
	Kind     string                   `json:"kind"`
	Field    string                   `json:"field,omitempty"`
	Operator string                   `json:"operator,omitempty"`
	Value    json.RawMessage          `json:"value,omitempty"`
	Children []mediaCategoryCondition `json:"children,omitempty"`
}

type mediaCategoryRule struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	MediaType   string                 `json:"mediaType"`
	Enabled     bool                   `json:"enabled"`
	DirectoryID string                 `json:"directoryId"`
	When        mediaCategoryCondition `json:"when"`
}

type mediaCategoryDefinition struct {
	Directories []mediaCategoryDirectory `json:"directories"`
	Rules       []mediaCategoryRule      `json:"rules"`
	Fallbacks   map[string]string        `json:"fallbacks"`
}

type mediaCategoryField struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Type      string   `json:"type"`
	Operators []string `json:"operators"`
	Example   string   `json:"example"`
}

func mediaCategoryFields() []mediaCategoryField {
	text := []string{"equals", "not_equals", "contains", "starts_with", "ends_with", "in", "not_in", "exists", "not_exists"}
	number := []string{"equals", "not_equals", "gt", "gte", "lt", "lte", "between", "in", "not_in", "exists", "not_exists"}
	list := []string{"contains_any", "contains_all", "contains_none", "exists", "not_exists"}
	return []mediaCategoryField{
		{"title", "名称", "string", text, "蜘蛛侠"}, {"originalTitle", "原始名称", "string", text, "Spider-Man"},
		{"year", "年份", "integer", number, "2021"}, {"genres", "类型／题材", "list", list, "动画;纪录片"},
		{"genreIds", "类型ID", "integer_list", list, "16;99"}, {"countries", "国家／地区", "list", list, "CN;JP"},
		{"language", "原始语言", "string", text, "zh;ja"}, {"rating", "评分", "number", number, "8"},
		{"keywords", "关键词", "list", list, "超级英雄;漫威"}, {"source", "识别来源", "string", text, "tmdb"},
		{"adult", "成人内容", "boolean", []string{"is_true", "is_false", "exists", "not_exists"}, ""},
	}
}

func defaultMediaCategory(value *mediaCategoryDefinition) mediaCategoryDefinition {
	if value != nil {
		cfg := *value
		cfg.Directories = append([]mediaCategoryDirectory{}, value.Directories...)
		for i := range cfg.Directories {
			if cfg.Directories[i].Path == nil {
				cfg.Directories[i].Path = []string{}
			}
		}
		cfg.Rules = append([]mediaCategoryRule{}, value.Rules...)
		return cfg
	}
	return mediaCategoryDefinition{
		Directories: []mediaCategoryDirectory{{"tv-root", "tv", "剧集", []string{}, true}, {"movie-root", "movie", "电影", []string{}, true}},
		Rules:       []mediaCategoryRule{}, Fallbacks: map[string]string{"tv": "tv-root", "movie": "movie-root"},
	}
}

func validMediaCategoryType(kind string) bool { return kind == "tv" || kind == "movie" }

func validateMediaCategory(cfg mediaCategoryDefinition) error {
	if len(cfg.Directories) < 2 || len(cfg.Directories) > 512 || len(cfg.Rules) > 512 || len(cfg.Fallbacks) != 2 {
		return errors.New("媒体分类须保留剧集、电影根目录；目录和规则各最多512项")
	}
	dirs, paths := map[string]mediaCategoryDirectory{}, map[string]bool{}
	for _, d := range cfg.Directories {
		if !shareCodePattern.MatchString(d.ID) || len(d.ID) > 200 || !validMediaCategoryType(d.MediaType) || d.Name == "" || utf8.RuneCountInString(d.Name) > 100 || strings.IndexFunc(d.Name, unicode.IsControl) >= 0 {
			return errors.New("媒体分类目录名称、ID或媒体类型无效")
		}
		if _, ok := dirs[d.ID]; ok {
			return errors.New("媒体分类目录ID重复")
		}
		if len(d.Path) == 0 {
			wantName := map[string]string{"tv": "剧集", "movie": "电影"}[d.MediaType]
			if d.ID != d.MediaType+"-root" || d.Name != wantName || !d.Enabled {
				return errors.New("剧集和电影根目录不可改名、停用或替换")
			}
		}
		if len(d.Path) > 8 || len(strings.Join(d.Path, "/")) > 512 {
			return errors.New("分类目录最多8层、512字节")
		}
		for _, segment := range d.Path {
			if !validLocalDirectoryName(segment) || len(segment) > 128 {
				return errors.New("分类目录须为安全的相对路径，不允许空段或上级路径")
			}
		}
		pathKey := d.MediaType + ":" + strings.ToLower(strings.Join(d.Path, "/"))
		if paths[pathKey] {
			return errors.New("同一媒体大类中目录路径重复")
		}
		paths[pathKey], dirs[d.ID] = true, d
	}
	for _, kind := range []string{"tv", "movie"} {
		root, ok := dirs[kind+"-root"]
		if !ok || root.MediaType != kind || len(root.Path) != 0 {
			return errors.New("缺少剧集或电影根目录")
		}
		fallback, ok := dirs[cfg.Fallbacks[kind]]
		if !ok || fallback.MediaType != kind || !fallback.Enabled {
			return errors.New("媒体分类兜底目录须为同大类的已启用目录")
		}
	}
	ids := map[string]bool{}
	for i, rule := range cfg.Rules {
		if ids[rule.ID] || !shareCodePattern.MatchString(rule.ID) || len(rule.ID) > 200 || strings.TrimSpace(rule.Name) == "" || utf8.RuneCountInString(rule.Name) > 100 || strings.IndexFunc(rule.Name, unicode.IsControl) >= 0 || !validMediaCategoryType(rule.MediaType) {
			return fmt.Errorf("媒体分类第%d条规则名称、ID或媒体类型无效", i+1)
		}
		ids[rule.ID] = true
		dir, ok := dirs[rule.DirectoryID]
		if !ok || dir.MediaType != rule.MediaType || rule.Enabled && !dir.Enabled {
			return fmt.Errorf("媒体分类第%d条规则须选择同大类且可用的目录", i+1)
		}
		nodes := 0
		if err := validateMediaCategoryCondition(rule.When, 0, &nodes); err != nil {
			return fmt.Errorf("媒体分类第%d条规则：%w", i+1, err)
		}
	}
	return nil
}

func validateMediaCategoryCondition(c mediaCategoryCondition, depth int, count *int) error {
	*count++
	if depth > 6 || *count > 128 {
		return errors.New("条件组最多嵌套6层、共128项")
	}
	if c.Kind != "condition" {
		if c.Kind != "all" && c.Kind != "any" && c.Kind != "not" || c.Field != "" || c.Operator != "" || len(c.Value) > 0 || len(c.Children) == 0 || c.Kind == "not" && len(c.Children) != 1 {
			return errors.New("条件组须为全部满足、任一满足或单项取反，且不能为空")
		}
		for _, child := range c.Children {
			if err := validateMediaCategoryCondition(child, depth+1, count); err != nil {
				return err
			}
		}
		return nil
	}
	if len(c.Children) > 0 {
		return errors.New("普通条件不能包含子条件")
	}
	var field *mediaCategoryField
	for _, f := range mediaCategoryFields() {
		if f.ID == c.Field {
			copy := f
			field = &copy
			break
		}
	}
	if field == nil || !slices.Contains(field.Operators, c.Operator) {
		return errors.New("分类字段或比较方式不支持")
	}
	if c.Operator == "exists" || c.Operator == "not_exists" || c.Operator == "is_true" || c.Operator == "is_false" {
		if len(c.Value) > 0 && string(c.Value) != "null" {
			return errors.New("存在性和布尔条件不需要填写值")
		}
		return nil
	}
	if len(c.Value) > 8192 {
		return errors.New("条件值过长")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(c.Value))
	if err := decoder.Decode(&value); err != nil {
		return errors.New("请填写有效条件值")
	}
	list := field.Type == "list" || field.Type == "integer_list" || c.Operator == "in" || c.Operator == "not_in" || c.Operator == "between"
	if list {
		values, ok := value.([]any)
		if !ok || len(values) == 0 || len(values) > 100 || c.Operator == "between" && len(values) != 2 {
			return errors.New("条件须填写非空值列表，区间须填写起止两个值")
		}
		for _, item := range values {
			if !validMediaCategoryValue(item, field.Type) {
				return errors.New("条件列表值类型无效")
			}
		}
		if c.Operator == "between" && values[0].(float64) > values[1].(float64) {
			return errors.New("条件区间起点不能大于终点")
		}
	} else if !validMediaCategoryValue(value, field.Type) {
		return errors.New("条件值类型无效")
	}
	return nil
}

func validMediaCategoryValue(v any, kind string) bool {
	if kind == "number" || kind == "integer" || kind == "integer_list" {
		n, ok := v.(float64)
		return ok && !math.IsInf(n, 0) && !math.IsNaN(n) && (kind == "number" || math.Trunc(n) == n)
	}
	s, ok := v.(string)
	return ok && s != "" && len(s) <= 1024 && utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0
}

// Deterministic classification of already identified facts; no I/O or organizing actions.
func evaluateMediaCategory(cfg mediaCategoryDefinition, mediaType string, facts map[string]any) (mediaCategoryDirectory, string, error) {
	if err := validateMediaCategory(cfg); err != nil {
		return mediaCategoryDirectory{}, "", err
	}
	if !validMediaCategoryType(mediaType) {
		return mediaCategoryDirectory{}, "", errors.New("媒体分类仅支持剧集和电影")
	}
	facts, err := normalizedMediaCategoryFacts(facts)
	if err != nil {
		return mediaCategoryDirectory{}, "", err
	}
	target, ruleID := cfg.Fallbacks[mediaType], ""
	for _, rule := range cfg.Rules {
		if rule.Enabled && rule.MediaType == mediaType && matchMediaCategoryCondition(rule.When, facts) {
			target, ruleID = rule.DirectoryID, rule.ID
			break
		}
	}
	for _, dir := range cfg.Directories {
		if dir.ID == target {
			return dir, ruleID, nil
		}
	}
	return mediaCategoryDirectory{}, "", errors.New("分类目标目录不存在")
}

func normalizedMediaCategoryFacts(facts map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(facts)
	if err != nil || len(raw) > 64<<10 || len(facts) > 64 {
		return nil, errors.New("分类事实无效或过大")
	}
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, err
	}
	for _, f := range mediaCategoryFields() {
		value, found := normalized[f.ID]
		if !found || value == nil {
			continue
		}
		valid := true
		switch f.Type {
		case "list", "integer_list":
			items, ok := value.([]any)
			valid = ok && len(items) <= 100
			for _, item := range items {
				valid = valid && validMediaCategoryValue(item, f.Type)
			}
		case "boolean":
			_, valid = value.(bool)
		case "string":
			text, ok := value.(string)
			valid = ok && (text == "" || validMediaCategoryValue(text, "string"))
		default:
			valid = validMediaCategoryValue(value, f.Type)
		}
		if !valid {
			return nil, fmt.Errorf("分类字段%s的数据类型无效", f.Label)
		}
	}
	return normalized, nil
}

func matchMediaCategoryCondition(c mediaCategoryCondition, facts map[string]any) bool {
	if c.Kind == "not" {
		return !matchMediaCategoryCondition(c.Children[0], facts)
	}
	if c.Kind == "all" || c.Kind == "any" {
		for _, child := range c.Children {
			hit := matchMediaCategoryCondition(child, facts)
			if c.Kind == "all" && !hit {
				return false
			}
			if c.Kind == "any" && hit {
				return true
			}
		}
		return c.Kind == "all"
	}
	actual, exists := facts[c.Field]
	exists = exists && actual != nil
	if text, ok := actual.(string); ok {
		exists = exists && text != ""
	}
	if list, ok := actual.([]any); ok {
		exists = exists && len(list) > 0
	}
	if c.Operator == "exists" {
		return exists
	}
	if c.Operator == "not_exists" {
		return !exists
	}
	if !exists {
		return false
	}
	if c.Operator == "is_true" || c.Operator == "is_false" {
		v, ok := actual.(bool)
		return ok && v == (c.Operator == "is_true")
	}
	var want any
	_ = json.Unmarshal(c.Value, &want)
	if options, ok := want.([]any); ok {
		if c.Operator == "between" {
			n, ok := actual.(float64)
			return ok && n >= options[0].(float64) && n <= options[1].(float64)
		}
		values, ok := actual.([]any)
		if !ok {
			values = []any{actual}
		}
		hits := 0
		for _, expected := range options {
			if slices.ContainsFunc(values, func(v any) bool { return categoryScalarEqual(v, expected) }) {
				hits++
			}
		}
		switch c.Operator {
		case "in", "contains_any":
			return hits > 0
		case "not_in", "contains_none":
			return hits == 0
		case "contains_all":
			return hits == len(options)
		}
	}
	switch c.Operator {
	case "equals":
		return categoryScalarEqual(actual, want)
	case "not_equals":
		return !categoryScalarEqual(actual, want)
	case "contains", "starts_with", "ends_with":
		a, ok := actual.(string)
		b, valid := want.(string)
		if !ok || !valid {
			return false
		}
		if c.Operator == "contains" {
			return strings.Contains(a, b)
		}
		if c.Operator == "starts_with" {
			return strings.HasPrefix(a, b)
		}
		return strings.HasSuffix(a, b)
	case "gt", "gte", "lt", "lte":
		a, ok := actual.(float64)
		b, valid := want.(float64)
		if !ok || !valid {
			return false
		}
		switch c.Operator {
		case "gt":
			return a > b
		case "gte":
			return a >= b
		case "lt":
			return a < b
		case "lte":
			return a <= b
		}
	}
	return false
}

func categoryScalarEqual(a, b any) bool {
	switch actual := a.(type) {
	case string:
		expected, ok := b.(string)
		return ok && actual == expected
	case float64:
		expected, ok := b.(float64)
		return ok && actual == expected
	case bool:
		expected, ok := b.(bool)
		return ok && actual == expected
	}
	return false
}

func (a *App) mediaCategoryCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]any{"fields": mediaCategoryFields(), "defaults": defaultMediaCategory(nil)})
}

func (a *App) mediaCategoryPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Config    mediaCategoryDefinition `json:"config"`
		MediaType string                  `json:"mediaType"`
		Facts     map[string]any          `json:"facts"`
	}
	if !decode(w, r, &input) {
		return
	}
	dir, rule, err := evaluateMediaCategory(input.Config, input.MediaType, input.Facts)
	if err != nil {
		fail(w, 400, err)
		return
	}
	root := map[string]string{"tv": "剧集", "movie": "电影"}[input.MediaType]
	jsonResponse(w, 200, map[string]any{"directoryId": dir.ID, "path": path.Join(append([]string{root}, dir.Path...)...), "ruleId": rule})
}
