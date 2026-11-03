package app

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
)

type namingDefinition struct {
	TV    string `json:"tv"`
	Movie string `json:"movie"`
}
type namingVariable struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Group  string `json:"group"`
	TVOnly bool   `json:"tvOnly,omitempty"`
}
type namingToken struct{ kind, value string }

func namingVariables() []namingVariable {
	return []namingVariable{
		{"title", "媒体标题", "基础信息", false}, {"original_title", "原语言标题", "基础信息", false}, {"year", "发行年份", "基础信息", false},
		{"tmdb_tag", "TMDB识别标签", "媒体识别", false}, {"tmdb_id", "TMDB数字ID", "媒体识别", false},
		{"season_episode", "季集组合", "剧集专用", true},
		{"resolution", "分辨率", "技术标签", false}, {"frame_rate", "帧率", "技术标签", false}, {"color_depth", "色深", "技术标签", false},
		{"web_source", "流媒体平台", "技术标签", false}, {"resource_type", "片源类型", "技术标签", false}, {"revision", "修订版本", "技术标签", false},
		{"resource_effect", "资源效果", "技术标签", false}, {"video_codec", "视频编码", "技术标签", false}, {"audio_codec", "音频编码", "技术标签", false},
		{"customization", "自定义匹配值", "技术标签", false}, {"release_group", "制作/发布组", "技术标签", false},
	}
}
func defaultNaming(p *namingDefinition) namingDefinition {
	if p != nil {
		return *p
	}
	file := "{title} - {original_title}.{year}.{resolution}.{frame_rate}.{color_depth}.{web_source}.{resource_type}.{revision}.{resource_effect}.{video_codec}.{audio_codec}.{customization}-{release_group}"
	return namingDefinition{TV: "{title} ({year}) -{tmdb_tag}/{title}.{season_episode}.{resolution}.{web_source}.{resource_type}.{video_codec}.{audio_codec}-{release_group}", Movie: "{title} ({year}) -{tmdb_tag}/" + file}
}

// Only literal path tokens may create directories. Metadata is always one component.
func parseNaming(source, mediaType string) ([]namingToken, error) {
	if mediaType != "tv" && mediaType != "movie" {
		return nil, errors.New("命名类型仅支持剧集和电影")
	}
	if len(source) == 0 || len(source) > 8192 || strings.ContainsAny(source, "\\") || strings.IndexFunc(source, unicode.IsControl) >= 0 {
		return nil, errors.New("命名模板为空、过长或含非法字符")
	}
	allowed := map[string]bool{}
	for _, v := range namingVariables() {
		if !v.TVOnly || mediaType == "tv" {
			allowed[v.ID] = true
		}
	}
	tokens := []namingToken{}
	seen := map[string]bool{}
	count, segments := 0, 1
	for len(source) > 0 {
		switch source[0] {
		case '/':
			if count == 0 || segments >= 8 {
				return nil, errors.New("路径前后须有命名变量，且最多8层")
			}
			tokens = append(tokens, namingToken{"path", "/"})
			source = source[1:]
			seen = map[string]bool{}
			count = 0
			segments++
		case '{':
			end := strings.IndexByte(source, '}')
			if end < 0 {
				return nil, errors.New("命名变量缺少右花括号")
			}
			key := source[1:end]
			if !allowed[key] {
				return nil, fmt.Errorf("不支持的命名变量：%s", key)
			}
			if seen[key] {
				return nil, fmt.Errorf("同一路径区段不能重复使用变量：%s", key)
			}
			seen[key] = true
			count++
			tokens = append(tokens, namingToken{"variable", key})
			source = source[end+1:]
		default:
			end := strings.IndexAny(source, "/{")
			if end < 0 {
				end = len(source)
			}
			text := source[:end]
			if strings.ContainsAny(text, "<>:\"|?*") {
				return nil, errors.New("文本标签含文件名称不支持的字符")
			}
			if strings.Contains(text, "}") {
				return nil, errors.New("文本标签不能含花括号")
			}
			tokens = append(tokens, namingToken{"text", text})
			source = source[end:]
		}
		if len(tokens) > 256 {
			return nil, errors.New("模板最多256个标签")
		}
	}
	if count == 0 {
		return nil, errors.New("每个路径区段须有命名变量")
	}
	return tokens, nil
}
func validateNaming(c namingDefinition) error {
	for _, v := range []struct{ source, kind string }{{c.TV, "tv"}, {c.Movie, "movie"}} {
		if _, err := parseNaming(v.source, v.kind); err != nil {
			return err
		}
	}
	return nil
}
func namingSample(kind string) map[string]string {
	f := map[string]string{"title": "肖申克的救赎", "original_title": "The Shawshank Redemption", "year": "1994", "tmdb_id": "278", "tmdb_tag": "{tmdbid=278}", "resolution": "1080p", "frame_rate": "24FPS", "color_depth": "10BIT", "web_source": "NF", "resource_type": "BLURAY", "revision": "V2", "resource_effect": "HDR10", "video_codec": "HEVC", "audio_codec": "AAC", "customization": "DIY", "release_group": "CMCT"}
	if kind == "tv" {
		f["title"] = "三体"
		f["original_title"] = "Three-Body"
		f["year"] = "2023"
		f["tmdb_id"] = "204541"
		f["tmdb_tag"] = "{tmdbid=204541}"
		f["season_episode"] = "S01E01"
	}
	return f
}
func namingComponent(value string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if strings.ContainsRune("/\\<>:\"|?*", r) || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, value))
}

type namingResult struct {
	Directory string `json:"directory"`
	Filename  string `json:"filename"`
}

func renderNaming(source, kind, original string, facts map[string]string) (namingResult, error) {
	tokens, err := parseNaming(source, kind)
	if err != nil {
		return namingResult{}, err
	}
	parts := []string{}
	start := 0
	for end := 0; end <= len(tokens); end++ {
		if end < len(tokens) && tokens[end].kind != "path" {
			continue
		}
		segment := tokens[start:end]
		first, last := -1, -1
		for i, t := range segment {
			if t.kind == "variable" {
				if first < 0 {
					first = i
				}
				last = i
			}
		}
		var b strings.Builder
		for i, t := range segment {
			if t.kind == "variable" {
				b.WriteString(namingComponent(facts[t.value]))
			} else if i > first && i < last {
				b.WriteString(t.value)
			}
		}
		part := strings.TrimSpace(b.String())
		if part == "" || part == "." || part == ".." || len(part) > 240 {
			return namingResult{}, errors.New("命名结果为空、过长或不是安全的目录名称")
		}
		parts = append(parts, part)
		start = end + 1
	}
	suffix := recognitionFilenameSuffix(original)
	if suffix == "" {
		return namingResult{}, errors.New("原文件须有有效扩展名")
	}
	filename := parts[len(parts)-1] + suffix
	if len(filename) > 255 {
		return namingResult{}, errors.New("文件名称超过255字节")
	}
	return namingResult{Directory: strings.Join(parts[:len(parts)-1], "/"), Filename: filename}, nil
}
func (a *App) namingCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]any{"variables": namingVariables(), "defaults": defaultNaming(nil)})
}
func (a *App) namingPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Source    string `json:"source"`
		MediaType string `json:"mediaType"`
	}
	if !decode(w, r, &input) {
		return
	}
	result, err := renderNaming(input.Source, input.MediaType, "example.mkv", namingSample(input.MediaType))
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, result)
}
