package app

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode"
)

var organizeSuffix = regexp.MustCompile(`^[a-z0-9_-]{1,32}(\.[a-z0-9_-]{1,32}){0,4}$`)

func organizeAIAvailable(st State) bool {
	p := pluginDefaults("ai", st.Plugins["ai"])
	return p.Enabled && strings.TrimSpace(p.APIURL) != "" && strings.TrimSpace(p.Model) != "" && validatePlugin("ai", p) == nil
}

func normalizeOrganizeOptions(t *organizeTaskDefinition, st State) {
	if excludes, err := normalizeOrganizeExclusions(t.ExcludeList); err == nil {
		t.ExcludeList = excludes
	}
	if t.OptionsVersion == 0 {
		t.OptionsVersion = 1
		t.MetadataExtensions = "srt;ass;ssa"
		t.TMDBScrape, t.FFprobe = true, true
	}
	for kind, selected := range map[string]*string{"naming": &t.NamingRuleID, "categories": &t.MediaCategoryRuleID} {
		if *selected != "" {
			continue
		}
		for _, rule := range st.RulePresets {
			if rule.Kind == kind {
				*selected = rule.ID
				break
			}
		}
	}
}

func validateOrganizeOptions(t *organizeTaskDefinition, st State, available bool) error {
	normalizeOrganizeOptions(t, st)
	if t.OptionsVersion != 1 {
		return errors.New("整理归档配置版本无效")
	}
	for kind, selected := range map[string]string{"naming": t.NamingRuleID, "categories": t.MediaCategoryRuleID, "upgrade": t.UpgradeRuleID} {
		if selected == "" {
			continue
		}
		if len(selected) > 200 || !shareCodePattern.MatchString(selected) {
			return errors.New("整理规则ID无效")
		}
		found := false
		for _, rule := range st.RulePresets {
			if rule.ID != selected {
				continue
			}
			if rule.Kind != kind {
				return errors.New("整理规则类型不匹配，请重新选择")
			}
			found = true
			break
		}
		if available && !found {
			return errors.New("所选整理规则已移除，请重新选择")
		}
	}
	if t.AIEnhance && available && !organizeAIAvailable(st) {
		return errors.New("请先配置并启用AI辅助识别")
	}
	ext, err := normalizeExtensions(t.MetadataExtensions)
	if err != nil {
		return errors.New("元数据归档扩展名无效，请用英文分号分隔")
	}
	t.MetadataExtensions = ext
	t.ExcludeList, err = normalizeOrganizeExclusions(t.ExcludeList)
	return err
}

func normalizeOrganizeExclusions(input string) (string, error) {
	if len(input) > 16384 || strings.IndexFunc(input, unicode.IsControl) >= 0 || strings.Contains(input, "；") {
		return "", errors.New("排除名单无效，请用英文分号分隔")
	}
	parts, seen := []string{}, map[string]bool{}
	for _, raw := range strings.Split(input, ";") {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if value == "/" {
			return "", errors.New("排除目录前缀后须填写关键词")
		}
		if strings.HasPrefix(value, ".") || strings.HasPrefix(value, "*") {
			// Read legacy star-prefixed extensions as the canonical dot syntax.
			suffix := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(value, "*"), "."))
			if !organizeSuffix.MatchString(suffix) {
				return "", errors.New("排除类型请使用.加扩展名，例如.mkv或.cas.strm")
			}
			value = "." + suffix
		}
		if !seen[value] {
			parts = append(parts, value)
			seen[value] = true
		}
	}
	if len(parts) > 512 {
		return "", errors.New("排除名单最多512项")
	}
	return strings.Join(parts, ";"), nil
}

func (a *App) organizeOptionsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	st := a.store.snapshotWithLogLimit(0)
	rules := map[string][]rulePreset{"naming": {}, "categories": {}, "upgrade": {}}
	for _, rule := range st.RulePresets {
		if validRulePresetKind(rule.Kind) {
			rules[rule.Kind] = append(rules[rule.Kind], rule)
		}
	}
	jsonResponse(w, 200, map[string]any{"rules": rules, "aiAvailable": organizeAIAvailable(st)})
}
