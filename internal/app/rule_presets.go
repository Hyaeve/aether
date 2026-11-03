package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type rulePreset struct {
	ID            string                   `json:"id"`
	Kind          string                   `json:"kind"`
	Name          string                   `json:"name"`
	Description   string                   `json:"description"`
	CreatedAt     time.Time                `json:"createdAt"`
	UpdatedAt     time.Time                `json:"updatedAt"`
	MediaCategory *mediaCategoryDefinition `json:"mediaCategory,omitempty"`
	Naming        *namingDefinition        `json:"naming,omitempty"`
	Upgrade       *upgradeDefinition       `json:"upgrade,omitempty"`
}

// These are saved task definitions. They are deliberately not registered with
// the task runner until the user supplies the organizing rules and workflow.
type organizeTaskDefinition struct {
	ID                  string           `json:"id"`
	Name                string           `json:"name"`
	StorageID           string           `json:"storageId"`
	Source              string           `json:"source"`
	SourcePath          string           `json:"sourcePath"`
	SourceLabel         string           `json:"sourceLabel"`
	SourceTrail         []DirectoryCrumb `json:"sourceTrail,omitempty"`
	TargetStorageID     string           `json:"targetStorageId"`
	Target              string           `json:"target"`
	TargetPath          string           `json:"targetPath"`
	TargetLabel         string           `json:"targetLabel"`
	TargetTrail         []DirectoryCrumb `json:"targetTrail,omitempty"`
	Method              string           `json:"method"`
	FailureDirectory    string           `json:"failureDirectory,omitempty"`
	FailurePath         string           `json:"failurePath,omitempty"`
	FailureLabel        string           `json:"failureLabel,omitempty"`
	FailureTrail        []DirectoryCrumb `json:"failureTrail,omitempty"`
	FileFilterMB        int64            `json:"fileFilterMB"`
	OptionsVersion      int              `json:"optionsVersion"`
	NamingRuleID        string           `json:"namingRuleId"`
	MediaCategoryRuleID string           `json:"mediaCategoryRuleId"`
	UpgradeRuleID       string           `json:"upgradeRuleId"`
	MetadataExtensions  string           `json:"metadataExtensions"`
	ExcludeList         string           `json:"excludeList"`
	AIEnhance           bool             `json:"aiEnhance"`
	TMDBScrape          bool             `json:"tmdbScrape"`
	FFprobe             bool             `json:"ffprobe"`
	SmartCleanup        bool             `json:"smartCleanup"`
	CreatedAt           time.Time        `json:"createdAt"`
	UpdatedAt           time.Time        `json:"updatedAt"`
}

func (t *organizeTaskDefinition) UnmarshalJSON(data []byte) error {
	type plain organizeTaskDefinition
	var value struct {
		plain
		LegacyCategoryRuleID *string `json:"categoryRuleId"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var presence map[string]json.RawMessage
	if err := json.Unmarshal(data, &presence); err != nil {
		return err
	}
	if legacy := value.LegacyCategoryRuleID; legacy != nil {
		if _, present := presence["mediaCategoryRuleId"]; present && value.MediaCategoryRuleID != *legacy {
			return errors.New("新旧媒体分类规则字段不一致")
		}
		value.MediaCategoryRuleID = *legacy
	}
	*t = organizeTaskDefinition(value.plain)
	return nil
}

func validRulePresetKind(kind string) bool {
	return kind == "naming" || kind == "categories" || kind == "upgrade"
}
func validatePreset(p *rulePreset) error {
	p.Name = strings.TrimSpace(p.Name)
	p.Description = strings.TrimSpace(p.Description)
	if !validRulePresetKind(p.Kind) {
		return errors.New("不支持的预设类型")
	}
	if p.Name == "" || utf8.RuneCountInString(p.Name) > 200 || strings.IndexFunc(p.Name, unicode.IsControl) >= 0 {
		return errors.New("预设名称须为1至200个字符")
	}
	if utf8.RuneCountInString(p.Description) > 2000 {
		return errors.New("预设说明最多2000个字符")
	}
	if p.Kind == "categories" {
		cfg := defaultMediaCategory(p.MediaCategory)
		if err := validateMediaCategory(cfg); err != nil {
			return err
		}
		p.MediaCategory = &cfg
	} else if p.MediaCategory != nil {
		return errors.New("媒体分类配置只能用于媒体分类预设")
	}
	if p.Kind == "naming" {
		cfg := defaultNaming(p.Naming)
		if err := validateNaming(cfg); err != nil {
			return err
		}
		p.Naming = &cfg
	} else if p.Naming != nil {
		return errors.New("命名配置只能用于命名预设")
	}
	if p.Kind == "upgrade" {
		cfg := defaultUpgrade(p.Upgrade)
		if err := validateUpgrade(cfg); err != nil {
			return err
		}
		p.Upgrade = &cfg
	} else if p.Upgrade != nil {
		return errors.New("洗版配置只能用于洗版预设")
	}
	return nil
}
func validateOrganizeDefinition(t *organizeTaskDefinition, st State, available bool) error {
	if err := validateOrganizeOptions(t, st, available); err != nil {
		return err
	}
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" || utf8.RuneCountInString(t.Name) > 200 || strings.IndexFunc(t.Name, unicode.IsControl) >= 0 {
		return errors.New("任务名称须为1至200个字符")
	}
	if t.StorageID == "" || t.TargetStorageID == "" || t.Source == "" || t.Target == "" {
		return errors.New("请选择待整理目录和归档目录")
	}
	if t.FileFilterMB < 0 || t.FileFilterMB > 9007199254740991 {
		return errors.New("文件过滤须为非负整数，单位MB")
	}
	if t.FailureDirectory == "" {
		t.FailurePath, t.FailureLabel, t.FailureTrail = "", "", nil
	}
	for _, value := range []string{t.Source, t.Target, t.SourcePath, t.TargetPath, t.SourceLabel, t.TargetLabel, t.FailureDirectory, t.FailurePath, t.FailureLabel} {
		if len(value) > 8192 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return errors.New("目录信息无效或过长")
		}
	}
	if len(t.SourceTrail) > 128 || len(t.TargetTrail) > 128 || len(t.FailureTrail) > 128 {
		return errors.New("目录层级最多128层")
	}
	for _, trail := range [][]DirectoryCrumb{t.SourceTrail, t.TargetTrail, t.FailureTrail} {
		for _, crumb := range trail {
			if len(crumb.ID) > 8192 || len(crumb.Name) > 8192 || strings.IndexFunc(crumb.ID+crumb.Name, unicode.IsControl) >= 0 {
				return errors.New("目录路径信息无效")
			}
		}
	}
	if t.Method == "" {
		t.Method = "move"
	}
	if t.Method != "move" && t.Method != "copy" && t.Method != "hardlink" && t.Method != "symlink" {
		return errors.New("整理方式无效")
	}
	var source, target *Storage
	for i := range st.Storages {
		s := &st.Storages[i]
		if s.ID == t.StorageID {
			source = s
		}
		if s.ID == t.TargetStorageID {
			target = s
		}
	}
	if available && (source == nil || target == nil || !source.Enabled || !target.Enabled) {
		return errors.New("请选择可用的存储池目录")
	}
	if source != nil && target != nil {
		if source.Type != target.Type {
			return errors.New("待整理目录和归档目录须为同类存储")
		}
		if source.Type != "local" && (t.Method == "hardlink" || t.Method == "symlink") {
			return errors.New("云存储仅支持移动或复制")
		}
		if source.ID == target.ID && backupDir(*source, t.Source) == backupDir(*target, t.Target) {
			return errors.New("归档目录不能与待整理目录相同")
		}
	}
	return nil
}
func validateRulesWorkspace(st State) error {
	if st.RecognitionRules != nil {
		if _, err := compileRecognitionSettings(*st.RecognitionRules); err != nil {
			return err
		}
	}
	if len(st.RulePresets) > 3000 || len(st.OrganizeTasks) > 1000 {
		return errors.New("规则预设或整理归档任务数量超限")
	}
	ids, names, counts := map[string]bool{}, map[string]bool{}, map[string]int{}
	for _, p := range st.RulePresets {
		if err := validatePreset(&p); err != nil {
			return err
		}
		if !shareCodePattern.MatchString(p.ID) || ids[p.ID] {
			return errors.New("规则预设ID无效或重复")
		}
		ids[p.ID] = true
		key := p.Kind + ":" + strings.ToLower(p.Name)
		if names[key] {
			return errors.New("同类预设名称已存在")
		}
		names[key] = true
		counts[p.Kind]++
		if counts[p.Kind] > 1000 {
			return errors.New("每类预设最多1000项")
		}
	}
	ids = map[string]bool{}
	for _, t := range st.OrganizeTasks {
		if !shareCodePattern.MatchString(t.ID) || ids[t.ID] {
			return errors.New("整理归档任务ID无效或重复")
		}
		ids[t.ID] = true
		if err := validateOrganizeDefinition(&t, st, false); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) rulePresetsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	key := r.PathValue("id")
	if r.Method == http.MethodGet && key == "" {
		kind := r.URL.Query().Get("kind")
		if kind != "" && !validRulePresetKind(kind) {
			fail(w, 400, errors.New("不支持的预设类型"))
			return
		}
		items := []rulePreset{}
		for _, p := range a.store.snapshotWithLogLimit(0).RulePresets {
			if kind == "" || kind == p.Kind {
				if p.Kind == "categories" {
					cfg := defaultMediaCategory(p.MediaCategory)
					p.MediaCategory = &cfg
				}
				if p.Kind == "naming" {
					cfg := defaultNaming(p.Naming)
					p.Naming = &cfg
				}
				if p.Kind == "upgrade" {
					cfg := defaultUpgrade(p.Upgrade)
					p.Upgrade = &cfg
				}
				items = append(items, p)
			}
		}
		jsonResponse(w, 200, items)
		return
	}
	if r.Method == http.MethodDelete && key != "" {
		found := false
		err := a.store.update(func(st *State) error {
			st.RulePresets = slices.DeleteFunc(st.RulePresets, func(p rulePreset) bool {
				if p.ID == key {
					found = true
					return true
				}
				return false
			})
			return nil
		})
		if err != nil {
			fail(w, 500, err)
		} else if !found {
			fail(w, 404, errors.New("预设不存在"))
		} else {
			jsonResponse(w, 200, map[string]bool{"ok": true})
		}
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut || r.Method == http.MethodPost && key != "" || r.Method == http.MethodPut && key == "" {
		w.WriteHeader(405)
		return
	}
	var input rulePreset
	if !decode(w, r, &input) {
		return
	}
	mediaProvided := input.MediaCategory != nil
	namingProvided, upgradeProvided := input.Naming != nil, input.Upgrade != nil
	if err := validatePreset(&input); err != nil {
		fail(w, 400, err)
		return
	}
	found := r.Method == http.MethodPost
	input.ID = key
	now := time.Now()
	input.UpdatedAt = now
	if found {
		input.ID = id()
		input.CreatedAt = now
	}
	err := a.store.update(func(st *State) error {
		if r.Method == http.MethodPost {
			st.RulePresets = append(st.RulePresets, input)
		} else {
			for i, p := range st.RulePresets {
				if p.ID == key {
					if p.Kind != input.Kind {
						return errors.New("不能更改预设类型")
					}
					input.CreatedAt = p.CreatedAt
					if p.Kind == "categories" && !mediaProvided && p.MediaCategory != nil {
						input.MediaCategory = p.MediaCategory
					}
					if p.Kind == "naming" && !namingProvided && p.Naming != nil {
						input.Naming = p.Naming
					}
					if p.Kind == "upgrade" && !upgradeProvided && p.Upgrade != nil {
						input.Upgrade = p.Upgrade
					}
					st.RulePresets[i] = input
					found = true
					break
				}
			}
		}
		return validateRulesWorkspace(*st)
	})
	if err != nil {
		fail(w, 409, err)
	} else if !found {
		fail(w, 404, errors.New("预设不存在"))
	} else {
		status := 200
		if r.Method == http.MethodPost {
			status = 201
		}
		jsonResponse(w, status, input)
	}
}
func (a *App) organizeTasksAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	key := r.PathValue("id")
	if r.Method == http.MethodGet && key == "" {
		st := a.store.snapshotWithLogLimit(0)
		items := st.OrganizeTasks
		for i := range items {
			normalizeOrganizeOptions(&items[i], st)
		}
		if items == nil {
			items = []organizeTaskDefinition{}
		}
		jsonResponse(w, 200, items)
		return
	}
	if r.Method == http.MethodDelete && key != "" {
		found := false
		err := a.store.update(func(st *State) error {
			st.OrganizeTasks = slices.DeleteFunc(st.OrganizeTasks, func(t organizeTaskDefinition) bool {
				if t.ID == key {
					found = true
					return true
				}
				return false
			})
			return nil
		})
		if err != nil {
			fail(w, 500, err)
		} else if !found {
			fail(w, 404, errors.New("任务不存在"))
		} else {
			jsonResponse(w, 200, map[string]bool{"ok": true})
		}
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut || r.Method == http.MethodPost && key != "" || r.Method == http.MethodPut && key == "" {
		w.WriteHeader(405)
		return
	}
	var input organizeTaskDefinition
	if !decode(w, r, &input) {
		return
	}
	if err := validateOrganizeDefinition(&input, a.store.snapshotWithLogLimit(0), true); err != nil {
		fail(w, 400, err)
		return
	}
	found := r.Method == http.MethodPost
	input.ID = key
	now := time.Now()
	input.UpdatedAt = now
	if found {
		input.ID = id()
		input.CreatedAt = now
	}
	err := a.store.update(func(st *State) error {
		if err := validateOrganizeDefinition(&input, *st, true); err != nil {
			return err
		}
		if r.Method == http.MethodPost {
			st.OrganizeTasks = append(st.OrganizeTasks, input)
		} else {
			for i, t := range st.OrganizeTasks {
				if t.ID == key {
					input.CreatedAt = t.CreatedAt
					st.OrganizeTasks[i] = input
					found = true
					break
				}
			}
		}
		return validateRulesWorkspace(*st)
	})
	if err != nil {
		fail(w, 409, err)
	} else if !found {
		fail(w, 404, errors.New("任务不存在"))
	} else {
		status := 200
		if r.Method == http.MethodPost {
			status = 201
		}
		jsonResponse(w, status, input)
	}
}
