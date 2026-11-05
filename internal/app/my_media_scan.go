package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type missingMediaWork struct {
	Item             mediaPoster `json:"item"`
	Missing          []string    `json:"missing"`
	LocalEpisodes    int         `json:"localEpisodes"`
	ExpectedEpisodes int         `json:"expectedEpisodes"`
}
type mediaMissingResult struct {
	Stamp   string             `json:"stamp"`
	Items   []missingMediaWork `json:"items"`
	Scanned int                `json:"scanned"`
	Unknown int                `json:"unknown"`
	Updated time.Time          `json:"updated"`
}
type mediaQualityResult struct {
	Stamp       string         `json:"stamp"`
	Items       int            `json:"items"`
	Size        int64          `json:"size"`
	SizeKnown   int            `json:"sizeKnown"`
	Resolutions map[string]int `json:"resolutions"`
	Codecs      map[string]int `json:"codecs"`
	Updated     time.Time      `json:"updated"`
}
type mediaScan struct {
	Running   bool               `json:"running"`
	Kind      string             `json:"kind"`
	Processed int                `json:"processed"`
	Total     int                `json:"total"`
	Message   string             `json:"message"`
	Cancel    context.CancelFunc `json:"-"`
}

func mediaScopeCurrent(st State, l MediaLink, stamp string) bool {
	current, ok := mediaLink(st, l.ID)
	return ok && mediaDisplayStamp(st, current) == stamp
}
func (a *App) mediaScanAPI(w http.ResponseWriter, r *http.Request) {
	st := a.store.snapshotWithLogLimit(0)
	l, ok := mediaLink(st, r.PathValue("id"))
	if !ok {
		fail(w, 404, errors.New("以链不存在"))
		return
	}
	stamp := mediaDisplayStamp(st, l)
	if r.Method == "POST" {
		var in struct {
			Kind string `json:"kind"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Kind == "stop" {
			a.mediaMu.Lock()
			if scan := a.mediaScans[l.ID]; scan != nil && scan.Running {
				scan.Cancel()
			}
			a.mediaMu.Unlock()
			jsonResponse(w, 200, map[string]bool{"ok": true})
			return
		}
		if (in.Kind != "missing" && in.Kind != "quality") || (in.Kind == "missing" && l.Type != "emby") || l.Type == "audiobookshelf" {
			fail(w, 400, errors.New("扫描类型不支持；缺集仅支持Emby"))
			return
		}
		if len(st.MediaDisplay.Libraries[l.ID]) == 0 {
			fail(w, 400, errors.New("请先在控制中心选择展示的媒体库"))
			return
		}
		c, err := a.dashboardClient(st, l)
		if err != nil {
			fail(w, 400, err)
			return
		}
		a.mediaMu.Lock()
		if a.mediaScans == nil {
			a.mediaScans = map[string]*mediaScan{}
		}
		running := 0
		for _, v := range a.mediaScans {
			if v.Running {
				running++
			}
		}
		if old := a.mediaScans[l.ID]; old != nil && old.Running || running >= 2 {
			a.mediaMu.Unlock()
			fail(w, 409, errors.New("媒体扫描正在执行，请等待或停止后重试"))
			return
		}
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Minute)
		scan := &mediaScan{Running: true, Kind: in.Kind, Message: "正在读取媒体清单", Cancel: cancel}
		a.mediaScans[l.ID] = scan
		a.mediaMu.Unlock()
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			defer cancel()
			var err error
			if in.Kind == "missing" {
				var result mediaMissingResult
				result, err = a.scanMediaMissing(ctx, st, l, c, scan)
				if err == nil {
					err = ctx.Err()
				}
				if err == nil {
					err = a.store.update(func(now *State) error {
						if !mediaScopeCurrent(*now, l, stamp) {
							return errors.New("媒体展示配置已改变，扫描结果未保存")
						}
						if now.MediaMissing == nil {
							now.MediaMissing = map[string]mediaMissingResult{}
						}
						now.MediaMissing[l.ID] = result
						return nil
					})
				}
			} else {
				var result mediaQualityResult
				result, err = a.scanMediaQuality(ctx, st, l, c, scan)
				if err == nil {
					err = ctx.Err()
				}
				if err == nil {
					err = a.store.update(func(now *State) error {
						if !mediaScopeCurrent(*now, l, stamp) {
							return errors.New("媒体展示配置已改变，扫描结果未保存")
						}
						if now.MediaQuality == nil {
							now.MediaQuality = map[string]mediaQualityResult{}
						}
						now.MediaQuality[l.ID] = result
						return nil
					})
				}
			}
			a.mediaMu.Lock()
			scan.Running = false
			scan.Message = "扫描完成"
			if err != nil {
				scan.Message = err.Error()
				if errors.Is(err, context.Canceled) {
					scan.Message = "扫描已停止"
				}
				if errors.Is(err, context.DeadlineExceeded) {
					scan.Message = "扫描超时，请缩小媒体库范围后重试"
				}
			}
			a.mediaMu.Unlock()
		}()
		jsonResponse(w, 202, map[string]bool{"running": true})
		return
	}
	a.mediaMu.Lock()
	progress := mediaScan{}
	if s := a.mediaScans[l.ID]; s != nil {
		progress = *s
	}
	a.mediaMu.Unlock()
	missing := st.MediaMissing[l.ID]
	if missing.Stamp != stamp {
		missing = mediaMissingResult{Items: []missingMediaWork{}}
	}
	quality := st.MediaQuality[l.ID]
	if quality.Stamp != stamp {
		quality = mediaQualityResult{}
	}
	jsonResponse(w, 200, map[string]any{"progress": progress, "missing": missing, "quality": quality})
}
func (a *App) mediaScanProgress(scan *mediaScan, done, total int, message string) {
	a.mediaMu.Lock()
	scan.Processed, scan.Total, scan.Message = done, total, message
	a.mediaMu.Unlock()
}
func (a *App) scanMediaMissing(ctx context.Context, st State, l MediaLink, c *mediaDashboardClient, scan *mediaScan) (mediaMissingResult, error) {
	result := mediaMissingResult{Stamp: c.Stamp, Items: []missingMediaWork{}, Updated: time.Now()}
	series := []videoMediaItem{}
	libraries := map[string]string{}
	for _, lib := range st.MediaDisplay.Libraries[l.ID] {
		items, err := a.allVideoItems(ctx, c, "/Items", url.Values{"ParentId": {lib.ID}, "Recursive": {"true"}, "IncludeItemTypes": {"Series"}, "Fields": {"ProviderIds,Overview"}})
		if err != nil {
			return result, err
		}
		for _, item := range items {
			series = append(series, item)
			libraries[item.ID] = lib.ID
		}
	}
	if len(series) > 10000 {
		return result, errors.New("剧集超过1万部，请缩小展示范围")
	}
	episodeBudget := 0
	for index, work := range series {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !mediaScopeCurrent(a.store.snapshotWithLogLimit(0), l, c.Stamp) {
			return result, errors.New("媒体展示配置已改变")
		}
		a.mediaScanProgress(scan, index, len(series), "核对 "+work.Name)
		tmdb := 0
		for k, v := range work.ProviderIDs {
			if strings.EqualFold(k, "tmdb") {
				tmdb, _ = strconv.Atoi(v)
			}
		}
		if tmdb < 1 {
			result.Unknown++
			continue
		}
		detail, err := a.discoveryMediaDetail(ctx, "tv", tmdb)
		if err != nil {
			result.Unknown++
			continue
		}
		local, err := a.allVideoItems(ctx, c, "/Shows/"+work.ID+"/Episodes", url.Values{"Fields": {"ProviderIds"}, "IsMissing": {"false"}})
		if err != nil {
			return result, err
		}
		present := map[string]bool{}
		for _, e := range local {
			if e.Season < 1 || e.Episode < 1 {
				continue
			}
			end := max(e.Episode, e.EpisodeEnd)
			if end-e.Episode > 1000 {
				return result, errors.New("本地季集编号范围异常")
			}
			for n := e.Episode; n <= end; n++ {
				present[fmt.Sprintf("S%02dE%02d", e.Season, n)] = true
			}
		}
		missing := []string{}
		expected := 0
		unknown := false
		for _, season := range detail.Seasons {
			if season.Number < 1 {
				continue
			}
			var raw tmdbSeason
			if err := a.discoveryJSON(ctx, fmt.Sprintf("tv/%d/season/%d", tmdb, season.Number), url.Values{}, &raw); err != nil {
				unknown = true
				continue
			}
			for _, e := range raw.Episodes {
				episodeBudget++
				if episodeBudget > 100000 {
					return result, errors.New("剧集超过10万集，请缩小展示范围")
				}
				date, err := time.Parse("2006-01-02", e.Date)
				if err != nil {
					unknown = true
					continue
				}
				if date.Format("2006-01-02") > time.Now().Format("2006-01-02") || e.Number < 1 {
					continue
				}
				expected++
				key := fmt.Sprintf("S%02dE%02d", season.Number, e.Number)
				if !present[key] {
					missing = append(missing, key)
				}
			}
		}
		if unknown {
			result.Unknown++
		}
		result.Scanned++
		if len(missing) > 0 {
			result.Items = append(result.Items, missingMediaWork{videoPoster(l, c, work, libraries[work.ID]), missing, len(present), expected})
		}
	}
	a.mediaScanProgress(scan, len(series), len(series), "扫描完成")
	return result, ctx.Err()
}
func (a *App) scanMediaQuality(ctx context.Context, st State, l MediaLink, c *mediaDashboardClient, scan *mediaScan) (mediaQualityResult, error) {
	result := mediaQualityResult{Stamp: c.Stamp, Resolutions: map[string]int{}, Codecs: map[string]int{}, Updated: time.Now()}
	for i, lib := range st.MediaDisplay.Libraries[l.ID] {
		if !mediaScopeCurrent(a.store.snapshotWithLogLimit(0), l, c.Stamp) {
			return result, errors.New("媒体展示配置已改变")
		}
		a.mediaScanProgress(scan, i, len(st.MediaDisplay.Libraries[l.ID]), "扫描 "+lib.Name)
		items, err := a.allVideoItems(ctx, c, "/Items", url.Values{"ParentId": {lib.ID}, "Recursive": {"true"}, "IncludeItemTypes": {"Movie,Episode"}, "Fields": {"MediaStreams,MediaSources"}})
		if err != nil {
			return result, err
		}
		if result.Items+len(items) > 100000 {
			return result, errors.New("媒体条目超过10万条，请缩小展示范围")
		}
		for _, v := range items {
			result.Items++
			streams := v.Streams
			if len(v.Sources) > 0 {
				if v.Sources[0].Size > 0 {
					result.Size += v.Sources[0].Size
					result.SizeKnown++
				}
				if len(streams) == 0 {
					streams = v.Sources[0].Streams
				}
			}
			res, codec := "未知", "未知"
			for _, s := range streams {
				if strings.EqualFold(s.Type, "Video") {
					if s.Height >= 2160 {
						res = "4K"
					} else if s.Height >= 1080 {
						res = "1080P"
					} else if s.Height >= 720 {
						res = "720P"
					} else if s.Height > 0 {
						res = "SD"
					}
					if s.Codec != "" {
						codec = strings.ToUpper(s.Codec)
					}
					break
				}
			}
			result.Resolutions[res]++
			result.Codecs[codec]++
		}
	}
	a.mediaScanProgress(scan, len(st.MediaDisplay.Libraries[l.ID]), len(st.MediaDisplay.Libraries[l.ID]), "统计完成")
	return result, ctx.Err()
}
