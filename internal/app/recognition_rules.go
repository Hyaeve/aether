package app

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

type recognitionReplacement struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Regex bool   `json:"regex"`
}

type recognitionSettings struct {
	MatchRules    []recognitionReplacement `json:"matchRules"`
	NamingRules   []recognitionReplacement `json:"namingRules"`
	Blacklist     []string                 `json:"blacklist"`
	ReleaseGroups []string                 `json:"releaseGroups"`
	MinSizeMB     int64                    `json:"minSizeMB"`
}

type recognitionRuleError struct{ field, message string }

func (e *recognitionRuleError) Error() string { return e.message }

type compiledRecognition struct {
	settings recognitionSettings
	match    []compiledRenameRule
	naming   []compiledRenameRule
}

func recognitionDefaults(value *recognitionSettings) recognitionSettings {
	var cfg recognitionSettings
	if value != nil {
		cfg = *value
	}
	if cfg.MatchRules == nil {
		cfg.MatchRules = []recognitionReplacement{}
	}
	if cfg.NamingRules == nil {
		cfg.NamingRules = []recognitionReplacement{}
	}
	if cfg.Blacklist == nil {
		cfg.Blacklist = []string{}
	}
	if cfg.ReleaseGroups == nil {
		cfg.ReleaseGroups = []string{}
	}
	return cfg
}

func compileRecognitionSettings(cfg recognitionSettings) (compiledRecognition, error) {
	out := compiledRecognition{settings: recognitionDefaults(&cfg)}
	bad := func(field, message string) (compiledRecognition, error) {
		return compiledRecognition{}, &recognitionRuleError{field, message}
	}
	if cfg.MinSizeMB < 0 || cfg.MinSizeMB > 9007199254740991 {
		return bad("minSizeMB", "文件体积过滤须为非负安全整数")
	}
	for _, group := range []struct {
		key   string
		label string
		rules *[]recognitionReplacement
		code  *[]compiledRenameRule
	}{{"matchRules", "匹配替换", &out.settings.MatchRules, &out.match}, {"namingRules", "命名替换", &out.settings.NamingRules, &out.naming}} {
		if len(*group.rules) > 512 {
			return bad(group.key, group.label+"最多512条")
		}
		rows := make([]recognitionReplacement, 0, len(*group.rules))
		for i, row := range *group.rules {
			if row.From == "" && row.To == "" {
				continue
			}
			if strings.IndexFunc(row.From+row.To, unicode.IsControl) >= 0 {
				return bad(fmt.Sprintf("%s.%d.from", group.key, i), fmt.Sprintf("%s第%d条不能包含控制字符", group.label, i+1))
			}
			mode := "literal"
			if row.Regex {
				mode = "regex"
			}
			code, err := compileRenameRule(renameRule{Kind: "replace", FindType: mode, Find: row.From, Replace: row.To, CaseSensitive: true})
			if err != nil {
				return bad(fmt.Sprintf("%s.%d.from", group.key, i), fmt.Sprintf("%s第%d条：%s", group.label, i+1, err))
			}
			rows = append(rows, row)
			*group.code = append(*group.code, code)
		}
		*group.rules = rows
	}
	for _, group := range []struct {
		key  string
		list *[]string
	}{{"blacklist", &out.settings.Blacklist}, {"releaseGroups", &out.settings.ReleaseGroups}} {
		if len(*group.list) > 512 {
			return bad(group.key, "每个名单最多512条")
		}
		values, seen := []string{}, map[string]bool{}
		for i, raw := range *group.list {
			value := strings.TrimSpace(raw)
			if value == "" {
				continue
			}
			if !utf8.ValidString(value) || len(value) > 1024 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
				return bad(group.key, fmt.Sprintf("第%d条须为不含控制字符的单行文本（最多1024字节）", i+1))
			}
			if group.key == "blacklist" {
				if strings.HasPrefix(value, "/") && (value == "/" || strings.ContainsAny(value[1:], `/\`)) || strings.HasPrefix(value, ".") && !organizeSuffix.MatchString(strings.ToLower(value[1:])) {
					return bad(group.key, fmt.Sprintf("黑名单第%d条无效，请填写文件关键词、.扩展名或/目录关键词", i+1))
				}
			} else if !safeName(value) {
				return bad(group.key, "发布组不能包含路径分隔符")
			}
			if !seen[value] {
				seen[value] = true
				values = append(values, value)
			}
		}
		*group.list = values
	}
	return out, nil
}

func applyRecognitionReplacements(name string, rules []compiledRenameRule) (string, error) {
	if !utf8.ValidString(name) || len(name) > 4096 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errors.New("名称须为不含控制字符的文本，最多4096字节")
	}
	for _, rule := range rules {
		value, err := rule.apply(name, 0)
		if err != nil {
			return "", err
		}
		if len(value) > 4096 {
			return "", errors.New("替换后名称过长")
		}
		name = value
	}
	return name, nil
}

// Recognition uses a name copy; callers keep original names and storage identities.
func (c compiledRecognition) recognitionName(original string) (string, error) {
	return applyRecognitionReplacements(original, c.match)
}

// These decisions are consumed by the future organizing executor, never executed here.
func (c compiledRecognition) blacklistMatch(relative string, isDir bool) bool {
	parts := strings.Split(strings.Trim(relative, "/"), "/")
	file := parts[len(parts)-1]
	dirs := parts[:len(parts)-1]
	if isDir {
		dirs = parts
	}
	for _, entry := range c.settings.Blacklist {
		switch {
		case strings.HasPrefix(entry, "/"):
			for _, dir := range dirs {
				if strings.Contains(dir, entry[1:]) {
					return true
				}
			}
		case !isDir && strings.HasPrefix(entry, "."):
			if strings.HasSuffix(file, entry) {
				return true
			}
		case !isDir:
			if strings.Contains(file, entry) {
				return true
			}
		}
	}
	return false
}

func (c compiledRecognition) blacklistDestination(task organizeTaskDefinition) string {
	if task.FailureDirectory != "" {
		return "failure-directory"
	}
	return "recycle"
}

func (c compiledRecognition) tooSmall(size int64) bool {
	// Decimal MB, compared by division to avoid overflow at the allowed upper bound.
	return c.settings.MinSizeMB > 0 && size >= 0 && size/1_000_000 < c.settings.MinSizeMB
}

func recognitionFilenameSuffix(name string) string {
	end := path.Ext(name)
	if strings.EqualFold(end, ".strm") {
		base := strings.TrimSuffix(name, end)
		kind := path.Ext(base)
		if strings.EqualFold(kind, ".cas") || strings.EqualFold(kind, ".ed2k") || strings.EqualFold(kind, ".share") {
			end = kind + end
			base = strings.TrimSuffix(base, kind)
		}
		media := path.Ext(base)
		for _, ext := range strings.Split(taskVideoExtensions+";"+taskAudioExtensions+";srt;ass;ssa;sub;vtt;cbz;cbr", ";") {
			if strings.EqualFold(media, "."+ext) {
				end = media + end
				break
			}
		}
	}
	return end
}

func (c compiledRecognition) archiveName(original, generated string) (string, error) {
	name, err := applyRecognitionReplacements(generated, c.naming)
	if err != nil {
		return "", err
	}
	for _, team := range c.settings.ReleaseGroups {
		if strings.Contains(original, team) {
			suffix := recognitionFilenameSuffix(name)
			stem := strings.TrimSuffix(name, suffix)
			if !strings.HasSuffix(stem, "-"+team) {
				name = stem + "-" + team + suffix
			}
			break
		}
	}
	if !safeName(name) || len(name) > 4096 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errors.New("命名结果为空、过长或包含路径分隔符")
	}
	return name, nil
}

func recognitionFailure(w http.ResponseWriter, err error) {
	var rule *recognitionRuleError
	if errors.As(err, &rule) {
		jsonResponse(w, 400, map[string]string{"error": rule.message, "field": rule.field})
		return
	}
	fail(w, 400, err)
}

func (a *App) recognitionRulesAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		jsonResponse(w, 200, recognitionDefaults(a.store.snapshotWithLogLimit(0).RecognitionRules))
		return
	}
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var cfg recognitionSettings
	if !decode(w, r, &cfg) {
		return
	}
	compiled, err := compileRecognitionSettings(cfg)
	if err != nil {
		recognitionFailure(w, err)
		return
	}
	if err := a.store.update(func(st *State) error { st.RecognitionRules = &compiled.settings; return nil }); err != nil {
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 200, compiled.settings)
}

func (a *App) recognitionRulesPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Name  string                   `json:"name"`
		Rules []recognitionReplacement `json:"rules"`
	}
	if !decode(w, r, &input) {
		return
	}
	compiled, err := compileRecognitionSettings(recognitionSettings{MatchRules: input.Rules})
	if err != nil {
		recognitionFailure(w, err)
		return
	}
	result, err := compiled.recognitionName(input.Name)
	if err != nil {
		recognitionFailure(w, err)
		return
	}
	jsonResponse(w, 200, map[string]string{"result": result})
}
