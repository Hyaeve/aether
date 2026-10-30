package app

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/width"
)

type renameRule struct {
	Kind           string   `json:"kind"`
	FindType       string   `json:"findType,omitempty"`
	Find           string   `json:"find,omitempty"`
	Replace        string   `json:"replace,omitempty"`
	CaseSensitive  bool     `json:"caseSensitive,omitempty"`
	FirstOnly      bool     `json:"firstOnly,omitempty"`
	Name           string   `json:"name,omitempty"`
	NumberPosition string   `json:"numberPosition,omitempty"`
	Start          int      `json:"start,omitempty"`
	Digits         int      `json:"digits,omitempty"`
	Separator      string   `json:"separator,omitempty"`
	Position       int      `json:"position,omitempty"`
	Text           string   `json:"text,omitempty"`
	DeleteTypes    []string `json:"deleteTypes,omitempty"`
	DeleteRange    bool     `json:"deleteRange,omitempty"`
	From           int      `json:"from,omitempty"`
	To             int      `json:"to,omitempty"`
	FromEnd        bool     `json:"fromEnd,omitempty"`
	ToEnd          bool     `json:"toEnd,omitempty"`
	CaseMode       string   `json:"caseMode,omitempty"`
	ChineseMode    string   `json:"chineseMode,omitempty"`
	WidthMode      string   `json:"widthMode,omitempty"`
}

type compiledRenameRule struct {
	wholeName bool
	apply     func(string, int) (string, error)
}
type compiledRenameRules []compiledRenameRule

var renameChineseConverters = map[string]func() (*renameChineseConverter, error){
	"t2s":  sync.OnceValues(func() (*renameChineseConverter, error) { return newRenameChineseConverter("t2s") }),
	"s2hk": sync.OnceValues(func() (*renameChineseConverter, error) { return newRenameChineseConverter("s2hk") }),
	"s2tw": sync.OnceValues(func() (*renameChineseConverter, error) { return newRenameChineseConverter("s2tw") }),
}

func renameOption(value string, allowed ...string) bool {
	if value == "" {
		return true
	}
	for _, option := range allowed {
		if value == option {
			return true
		}
	}
	return false
}

func renamePosition(value int) int {
	return max(1, value)
}

func compileRenameRules(rules []renameRule) (compiledRenameRules, error) {
	if len(rules) == 0 || len(rules) > 30 {
		return nil, errors.New("请添加 1–30 条规则")
	}
	out := make(compiledRenameRules, 0, len(rules))
	for i, rule := range rules {
		compiled, err := compileRenameRule(rule)
		if err != nil {
			return nil, fmt.Errorf("规则 %d：%w", i+1, err)
		}
		out = append(out, compiled)
	}
	return out, nil
}

func compileRenameRule(rule renameRule) (compiledRenameRule, error) {
	bad := func(message string) (compiledRenameRule, error) {
		return compiledRenameRule{}, errors.New(message)
	}
	for _, text := range []string{rule.Find, rule.Replace, rule.Name, rule.Text, rule.Separator} {
		if !utf8.ValidString(text) || len(text) > 1024 || strings.ContainsRune(text, 0) {
			return bad("规则文字最多 1024 字节且不能包含空字符")
		}
	}
	if utf8.RuneCountInString(rule.Text) > 255 || len(rule.Separator) > 32 {
		return bad("添加或删除字符最多 255 个，分隔符最多 32 字节")
	}
	for _, n := range []int{rule.Position, rule.From, rule.To} {
		if n < 0 || n > 4096 {
			return bad("字符位置须为 1–4096 的整数")
		}
	}
	result := compiledRenameRule{}
	switch rule.Kind {
	case "replace":
		if rule.Find == "" || !renameOption(rule.FindType, "literal", "regex") {
			return bad("查找内容不能为空，查找类型须为常规或正则")
		}
		pattern := regexp.QuoteMeta(rule.Find)
		if rule.FindType == "regex" {
			pattern = rule.Find
		}
		if !rule.CaseSensitive {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return bad("正则表达式无效")
		}
		result.wholeName = true
		result.apply = func(name string, _ int) (string, error) {
			matches := re.FindAllStringSubmatchIndex(name, -1)
			if rule.FirstOnly && len(matches) > 1 {
				matches = matches[:1]
			}
			var b strings.Builder
			start := 0
			for _, match := range matches {
				b.WriteString(name[start:match[0]])
				if rule.FindType == "regex" {
					b.Write(re.ExpandString(nil, rule.Replace, name, match))
				} else {
					b.WriteString(rule.Replace)
				}
				start = match[1]
				if b.Len() > 4096 {
					return "", errors.New("替换后名称过长")
				}
			}
			b.WriteString(name[start:])
			return b.String(), nil
		}
	case "number":
		if rule.Start < 0 || rule.Start > 1000000000 || rule.Digits < 0 || rule.Digits > 12 || !renameOption(rule.NumberPosition, "before", "after") {
			return bad("开始序号须为 1–1000000000，最少位数须为 1–12，序号位置无效")
		}
		result.apply = func(name string, index int) (string, error) {
			if rule.Name != "" {
				name = rule.Name
			}
			number := strconv.Itoa(renamePosition(rule.Start) + index)
			number = strings.Repeat("0", max(0, renamePosition(rule.Digits)-len(number))) + number
			if rule.NumberPosition == "after" {
				return name + rule.Separator + number, nil
			}
			return number + rule.Separator + name, nil
		}
	case "insert":
		if rule.Text == "" {
			return bad("请输入添加的字符")
		}
		result.apply = func(name string, _ int) (string, error) {
			runes := []rune(name)
			pos := min(renamePosition(rule.Position)-1, len(runes))
			return string(runes[:pos]) + rule.Text + string(runes[pos:]), nil
		}
	case "delete":
		types := map[string]bool{}
		for _, typ := range rule.DeleteTypes {
			if !renameOption(typ, "english", "digits", "special", "serial", "punctuation", "units", "arrows", "box") || typ == "" || types[typ] {
				return bad("删除字符类型无效或重复")
			}
			types[typ] = true
		}
		result.apply = func(name string, _ int) (string, error) {
			name = strings.Map(func(r rune) rune {
				if renameDeleteRune(r, types) {
					return -1
				}
				return r
			}, name)
			if rule.DeleteRange {
				runes := []rune(name)
				from, to := renamePosition(rule.From)-1, renamePosition(rule.To)-1
				if rule.FromEnd {
					from = len(runes) - renamePosition(rule.From)
				}
				if rule.ToEnd {
					to = len(runes) - renamePosition(rule.To)
				}
				if from > to {
					from, to = to, from
				}
				from, to = max(0, from), min(len(runes)-1, to)
				if from <= to {
					name = string(runes[:from]) + string(runes[to+1:])
				}
			}
			if rule.Text != "" {
				count := -1
				if rule.FirstOnly {
					count = 1
				}
				name = strings.Replace(name, rule.Text, "", count)
			}
			return name, nil
		}
	case "move":
		result.apply = func(name string, _ int) (string, error) {
			runes := []rune(name)
			from := renamePosition(rule.From) - 1
			if from >= len(runes) {
				return name, nil
			}
			moved := runes[from]
			runes = append(runes[:from], runes[from+1:]...)
			to := min(renamePosition(rule.To)-1, len(runes))
			return string(runes[:to]) + string(moved) + string(runes[to:]), nil
		}
	case "convert":
		if !renameOption(rule.CaseMode, "none", "lower", "upper", "title", "sentence") ||
			!renameOption(rule.ChineseMode, "none", "t2s", "s2hk", "s2tw") ||
			!renameOption(rule.WidthMode, "none", "narrow_cn", "narrow_en", "wide_cn", "wide_en") {
			return bad("字符转换选项无效")
		}
		var converter *renameChineseConverter
		if load, ok := renameChineseConverters[rule.ChineseMode]; ok {
			var err error
			converter, err = load()
			if err != nil {
				return bad("繁简转换词典加载失败")
			}
		}
		result.apply = func(name string, _ int) (string, error) {
			name = renameCase(name, rule.CaseMode)
			if converter != nil {
				name = converter.Convert(name)
			}
			return renameWidth(name, rule.WidthMode), nil
		}
	default:
		return bad("不支持的规则类型")
	}
	return result, nil
}

func (rules compiledRenameRules) apply(name string, index int, isDir bool) (string, error) {
	for _, rule := range rules {
		stem, extension := name, ""
		// Only explicit find/replace changes extensions; directories have no extension.
		if !rule.wholeName && !isDir {
			extension = path.Ext(name)
			if extension == name {
				extension = ""
			}
			stem = strings.TrimSuffix(name, extension)
		}
		changed, err := rule.apply(stem, index)
		if err != nil {
			return "", err
		}
		name = changed + extension
		if len(name) > 4096 {
			return "", errors.New("规则处理后名称过长")
		}
	}
	if !safeName(name) || len(name) > 255 {
		return "", errors.New("新名称为空、超过 255 字节或含非法字符")
	}
	return name, nil
}

func applyRenameRules(name string, rules []renameRule) (string, error) {
	compiled, err := compileRenameRules(rules)
	if err != nil {
		return "", err
	}
	return compiled.apply(name, 0, false)
}

func renameDeleteRune(r rune, types map[string]bool) bool {
	kind := ""
	switch {
	case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
		kind = "english"
	case unicode.Is(unicode.Nd, r):
		kind = "digits"
	case r >= 0x2460 && r <= 0x24ff || r >= 0x2160 && r <= 0x2188 || r >= 0x2776 && r <= 0x2793 || r >= 0x3220 && r <= 0x325f || r >= 0x3280 && r <= 0x32bf:
		kind = "serial"
	case r >= 0x2100 && r <= 0x214f || r >= 0x3300 && r <= 0x33ff || r == '°' || r == 'µ':
		kind = "units"
	case r >= 0x2190 && r <= 0x21ff || r >= 0x27f0 && r <= 0x27ff || r >= 0x2900 && r <= 0x297f || r >= 0x2b00 && r <= 0x2b11:
		kind = "arrows"
	case r >= 0x2500 && r <= 0x259f:
		kind = "box"
	case r == '_' || r == '~' || r == '`':
		kind = "special"
	case unicode.IsPunct(r):
		kind = "punctuation"
	case unicode.IsSymbol(r) || unicode.IsSpace(r):
		kind = "special"
	}
	return types[kind]
}

func renameCase(name, mode string) string {
	switch mode {
	case "lower":
		return strings.ToLower(name)
	case "upper":
		return strings.ToUpper(name)
	case "title", "sentence":
		first := true
		return strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) && !unicode.Is(unicode.Han, r) {
				if first {
					first = false
					return unicode.ToUpper(r)
				}
				return unicode.ToLower(r)
			}
			if mode == "title" {
				first = true
			}
			return r
		}, name)
	}
	return name
}

var renameChinesePunctuation = strings.NewReplacer(",", "，", ".", "。", "!", "！", "?", "？", ":", "：", ";", "；", "(", "（", ")", "）", "[", "【", "]", "】")
var renameEnglishPunctuation = strings.NewReplacer("，", ",", "。", ".", "！", "!", "？", "?", "：", ":", "；", ";", "（", "(", "）", ")", "【", "[", "】", "]", "“", `"`, "”", `"`, "‘", "'", "’", "'")

func renameWidth(name, mode string) string {
	switch mode {
	case "narrow_en":
		return width.Narrow.String(renameEnglishPunctuation.Replace(name))
	case "narrow_cn":
		return strings.Map(func(r rune) rune {
			narrow := width.Narrow.String(string(r))
			if unicode.IsPunct(r) {
				return r
			}
			converted, _ := utf8.DecodeRuneInString(narrow)
			return converted
		}, name)
	case "wide_en":
		return width.Widen.String(renameEnglishPunctuation.Replace(name))
	case "wide_cn":
		return width.Widen.String(renameChinesePunctuation.Replace(name))
	}
	return name
}
