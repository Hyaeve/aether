package app

import (
	"errors"
	"math"
	"net/http"
	"slices"
	"strings"
)

type upgradeGroup struct {
	ID      string   `json:"id"`
	Enabled bool     `json:"enabled"`
	Values  []string `json:"values"`
}
type upgradeDefinition struct {
	Groups []upgradeGroup `json:"groups"`
}
type upgradeCatalogGroup struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Defaults []string `json:"defaults"`
	Extras   []string `json:"extras"`
}

var upgradeWeights = [...]float64{10, 8, 6, 5, 4, 3, 2, 2, 1, 0.6}

func upgradeCatalogGroups() []upgradeCatalogGroup {
	return []upgradeCatalogGroup{
		{"resolution", "分辨率", strings.Split("2160p,4K,1440p,1080p,720p,480p", ","), strings.Split("8K,1080P,1080I,720P,SD", ",")},
		{"source", "片源类型", strings.Split("REMUX,BluRay,WEB-DL,HDTV,DVDRip", ","), strings.Split("UHD-BLURAY-REMUX,UHD-BLURAY,BLURAY-REMUX,BLURAY,WEBRIP,UHDTV,DVD,DVDRIP,HDRIP,BDMV,SCR,HDTC,TC,TELESYNC,HDCAM,CAM", ",")},
		{"video_codec", "视频编码", strings.Split("H.265,HEVC,AV1,H.264,AVC,VP9", ","), strings.Split("MPEG2,VC1,XVID,DIVX,AVS3", ",")},
		{"audio_codec", "音频编码", strings.Split("TrueHD,DTS-HD MA,DTS,EAC3,AC3,FLAC,AAC,MP3", ","), strings.Split("TRUEHD7.1,TRUEHD5.1,TRUEHD,DTS-HDMA7.1,DTS-HDMA,DTS-X,DDP5.1,DDP,AAC2.0,LPCM,OPUS,ATMOS", ",")},
		{"hdr", "HDR类型", strings.Split("Dolby Vision,HDR10+,HDR Vivid,HDR10,HLG,HDR,SDR", ","), strings.Split("DV,HDR-VIVID,EDR,3D,IMAX,REPACK,PROPER,RERIP,DIY,HQ", ",")},
		{"color_depth", "色深", strings.Split("12-bit,10-bit,8-bit", ","), strings.Split("12BIT,10BIT,8BIT", ",")},
		{"frame_rate", "帧率", strings.Split("120fps,60fps,50fps,30fps", ","), strings.Split("120FPS,60FPS,50FPS,30FPS,24FPS", ",")},
		{"subtitle", "字幕", strings.Split("中文字幕,有字幕,无字幕", ","), strings.Split("CHS&CHT,CHS&ENG,CHS,CHT,MULTISUB,MIXED-SUB,EXT-SUB,HARDSUB,RAW,NO-SUB,NO-CHS", ",")},
		{"file_extension", "文件格式", strings.Split("MKV,MP4,AVI,TS,WMV", ","), strings.Split("MOV,FLV,RMVB,ISO,M4V,MPG,MPEG,3GP,WEBM,RM", ",")},
		{"file_size", "文件大小", []string{}, []string{}},
	}
}
func defaultUpgrade(p *upgradeDefinition) upgradeDefinition {
	if p != nil {
		c := upgradeDefinition{Groups: make([]upgradeGroup, len(p.Groups))}
		for i, g := range p.Groups {
			c.Groups[i] = g
			c.Groups[i].Values = append([]string{}, g.Values...)
		}
		return c
	}
	c := upgradeDefinition{Groups: []upgradeGroup{}}
	for _, g := range upgradeCatalogGroups() {
		c.Groups = append(c.Groups, upgradeGroup{g.ID, true, append([]string{}, g.Defaults...)})
	}
	return c
}
func validateUpgrade(c upgradeDefinition) error {
	catalog := upgradeCatalogGroups()
	if len(c.Groups) != len(catalog) {
		return errors.New("洗版策略须包含全部10个评分分组")
	}
	seen := map[string]bool{}
	for _, g := range c.Groups {
		idx := slices.IndexFunc(catalog, func(v upgradeCatalogGroup) bool { return v.ID == g.ID })
		if idx < 0 || seen[g.ID] {
			return errors.New("洗版评分分组无效或重复")
		}
		seen[g.ID] = true
		options := append(slices.Clone(catalog[idx].Defaults), catalog[idx].Extras...)
		values := map[string]bool{}
		for _, v := range g.Values {
			if !slices.Contains(options, v) || values[v] {
				return errors.New("洗版标签无效或重复")
			}
			values[v] = true
		}
	}
	return nil
}
func upgradeCanonical(group, value string) string {
	if group == "file_extension" {
		value = strings.ToLower(value)
		for _, suffix := range []string{".cas.strm", ".share.strm", ".ed2k.strm", ".strm"} {
			if strings.HasSuffix(value, suffix) {
				value = strings.TrimSuffix(value, suffix)
				break
			}
		}
	}
	v := strings.ToUpper(value)
	v = strings.NewReplacer(" ", "", ".", "", "-", "", "_", "").Replace(v)
	aliases := map[string]map[string]string{
		"resolution":  {"4K": "2160P", "8K": "4320P", "SD": "480P"},
		"video_codec": {"H265": "HEVC", "X265": "HEVC", "H264": "AVC", "X264": "AVC"},
		"audio_codec": {"DDP": "EAC3", "DDP51": "EAC351", "DTSHDMA": "DTSHDMA"},
		"hdr":         {"DV": "DOLBYVISION"},
		"color_depth": {"12BIT": "12BIT", "10BIT": "10BIT", "8BIT": "8BIT"},
		"subtitle":    {"NOSUB": "无字幕", "RAW": "无字幕"},
	}
	if alias := aliases[group][v]; alias != "" {
		return alias
	}
	return v
}

type upgradeVersion struct {
	Facts map[string][]string `json:"facts"`
	Size  int64               `json:"size"`
}
type upgradeScore struct {
	ID       string  `json:"id"`
	Weight   float64 `json:"weight"`
	Existing float64 `json:"existing"`
	Incoming float64 `json:"incoming"`
}
type upgradeComparison struct {
	Winner      string         `json:"winner"`
	Existing    float64        `json:"existing"`
	Incoming    float64        `json:"incoming"`
	Groups      []upgradeScore `json:"groups"`
	LoserAction string         `json:"loserAction"`
	Directory   string         `json:"directory,omitempty"`
}

func upgradePreference(g upgradeGroup, facts []string) float64 {
	// Aliases share the best position: spelling alone never makes a version win.
	for i, v := range g.Values {
		key := upgradeCanonical(g.ID, v)
		for _, actual := range facts {
			match := upgradeCanonical(g.ID, actual) == key
			if g.ID == "subtitle" {
				a := upgradeCanonical(g.ID, actual)
				chinese := slices.Contains([]string{"CHS&CHT", "CHS&ENG", "CHS", "CHT", "中文字幕"}, a)
				if v == "中文字幕" {
					match = chinese
				}
				if v == "有字幕" {
					match = chinese || slices.Contains([]string{"有字幕", "MULTISUB", "MIXEDSUB", "EXTSUB", "HARDSUB", "NOCHS"}, a)
				}
			}
			if g.ID == "audio_codec" {
				a := upgradeCanonical(g.ID, actual)
				if key == "TRUEHD" || key == "DTSHDMA" || key == "EAC3" || key == "AAC" {
					suffix := strings.TrimPrefix(a, key)
					match = match || strings.HasPrefix(a, key) && suffix != "" && strings.Trim(suffix, "0123456789") == ""
				}
			}
			if match {
				return 100 * float64(len(g.Values)-i) / float64(len(g.Values))
			}
		}
	}
	return 0
}

// This is a pure decision. The organizing executor must first establish that
// both candidates are the same work/episode before calling it or moving a loser.
func compareUpgrade(c upgradeDefinition, old, next upgradeVersion, task organizeTaskDefinition) (upgradeComparison, error) {
	if err := validateUpgrade(c); err != nil {
		return upgradeComparison{}, err
	}
	if old.Size < 0 || next.Size < 0 {
		return upgradeComparison{}, errors.New("文件大小不能为负数")
	}
	index := 0
	r := upgradeComparison{Winner: "existing", LoserAction: "keep", Groups: []upgradeScore{}}
	for _, g := range c.Groups {
		if !g.Enabled {
			continue
		}
		weight := upgradeWeights[index]
		index++
		a, b := upgradePreference(g, old.Facts[g.ID]), upgradePreference(g, next.Facts[g.ID])
		if g.ID == "file_size" {
			largest := max(old.Size, next.Size)
			if largest > 0 {
				a = 100 * float64(old.Size) / float64(largest)
				b = 100 * float64(next.Size) / float64(largest)
			}
		}
		a, b = a*weight/100, b*weight/100
		r.Groups = append(r.Groups, upgradeScore{g.ID, weight, a, b})
		r.Existing += a
		r.Incoming += b
	}
	if r.Incoming-r.Existing > 1e-9 {
		r.Winner = "incoming"
	}
	if math.Abs(r.Incoming-r.Existing) <= 1e-9 {
		r.Winner = "tie"
	}
	if task.FailureDirectory != "" {
		r.LoserAction = "move"
		r.Directory = task.FailureDirectory
	}
	if r.Winner == "tie" {
		r.LoserAction = "keep"
		r.Directory = ""
	}
	return r, nil
}
func (a *App) upgradeCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]any{"groups": upgradeCatalogGroups(), "defaults": defaultUpgrade(nil), "weightMode": "priority", "weights": upgradeWeights})
}
func (a *App) upgradePreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Config   upgradeDefinition `json:"config"`
		Existing upgradeVersion    `json:"existing"`
		Incoming upgradeVersion    `json:"incoming"`
	}
	if !decode(w, r, &input) {
		return
	}
	result, err := compareUpgrade(input.Config, input.Existing, input.Incoming, organizeTaskDefinition{})
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, result)
}
