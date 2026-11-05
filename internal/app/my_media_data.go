package app

import (
	"aether/internal/linkcore/upstream"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

var mediaItemID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,160}$`)

type videoMediaStream struct {
	Type   string `json:"Type"`
	Codec  string `json:"Codec"`
	Height int    `json:"Height"`
}
type videoMediaItem struct {
	ID          string            `json:"Id"`
	Name        string            `json:"Name"`
	Type        string            `json:"Type"`
	Overview    string            `json:"Overview"`
	SeriesID    string            `json:"SeriesId"`
	SeriesName  string            `json:"SeriesName"`
	Year        int               `json:"ProductionYear"`
	Season      int               `json:"ParentIndexNumber"`
	Episode     int               `json:"IndexNumber"`
	EpisodeEnd  int               `json:"IndexNumberEnd"`
	Date        string            `json:"DateCreated"`
	Runtime     int64             `json:"RunTimeTicks"`
	ProviderIDs map[string]string `json:"ProviderIds"`
	UserData    struct {
		PlayCount *int `json:"PlayCount"`
	} `json:"UserData"`
	Streams []videoMediaStream `json:"MediaStreams"`
	Sources []struct {
		Size    int64              `json:"Size"`
		Streams []videoMediaStream `json:"MediaStreams"`
	} `json:"MediaSources"`
}
type videoMediaPage struct {
	Items []videoMediaItem `json:"Items"`
	Total *int             `json:"TotalRecordCount"`
}

func videoPoster(l MediaLink, c *mediaDashboardClient, v videoMediaItem, library string) mediaPoster {
	imageID, title := v.ID, v.Name
	subtitle := ""
	if v.Year > 0 {
		subtitle = strconv.Itoa(v.Year)
	}
	if v.Type == "Episode" && v.SeriesID != "" {
		imageID = v.SeriesID
		title = fmt.Sprintf("%s · S%02dE%02d", v.SeriesName, v.Season, v.Episode)
	}
	return mediaPoster{ID: v.ID, Title: title, Subtitle: subtitle, Overview: v.Overview, Kind: v.Type, Poster: mediaImageURL(l, c.Stamp, imageID, "Primary"), Backdrop: mediaImageURL(l, c.Stamp, imageID, "Backdrop"), LibraryID: library, Date: v.Date, Duration: float64(v.Runtime) / 1e7}
}
func finishRecent(d *mediaDashboardData) {
	sort.SliceStable(d.Recent, func(i, j int) bool { return d.Recent[i].Date > d.Recent[j].Date })
	if len(d.Recent) > 24 {
		d.Recent = d.Recent[:24]
	}
}
func (a *App) loadVideoDashboard(ctx context.Context, st State, l MediaLink, c *mediaDashboardClient, d *mediaDashboardData) error {
	totals := map[string]int{"Movie": 0, "Series": 0, "Episode": 0}
	known := map[string]bool{"Movie": true, "Series": true, "Episode": true}
	for _, lib := range st.MediaDisplay.Libraries[l.ID] {
		var recent videoMediaPage
		if err := upstream.DashboardRead(ctx, c.Provider, "/Items", url.Values{"ParentId": {lib.ID}, "Recursive": {"true"}, "IncludeItemTypes": {"Movie,Episode"}, "SortBy": {"DateCreated"}, "SortOrder": {"Descending"}, "Limit": {"24"}, "Fields": {"Overview,DateCreated,SeriesName"}}, &recent); err != nil {
			dashboardWarning(d, lib.Name+"：最近入库读取失败")
		} else {
			for _, v := range recent.Items {
				d.Recent = append(d.Recent, videoPoster(l, c, v, lib.ID))
			}
		}
		for kind := range totals {
			var count videoMediaPage
			if err := upstream.DashboardRead(ctx, c.Provider, "/Items", url.Values{"ParentId": {lib.ID}, "Recursive": {"true"}, "IncludeItemTypes": {kind}, "Limit": {"1"}}, &count); err != nil || count.Total == nil {
				known[kind] = false
				dashboardWarning(d, lib.Name+"：部分数量统计不可用")
			} else {
				totals[kind] += *count.Total
			}
		}
	}
	for _, kind := range []string{"Movie", "Series", "Episode"} {
		if known[kind] {
			d.Stats = append(d.Stats, mediaMetric{map[string]string{"Movie": "影片", "Series": "剧集", "Episode": "总集数"}[kind], float64(totals[kind]), ""})
		}
	}
	finishRecent(d)
	return ctx.Err()
}

type absMediaItem struct {
	ID        string  `json:"id"`
	LibraryID string  `json:"libraryId"`
	Added     float64 `json:"addedAt"`
	MediaType string  `json:"mediaType"`
	Media     struct {
		Duration float64 `json:"duration"`
		Metadata struct {
			Title       string `json:"title"`
			Author      string `json:"authorName"`
			Narrator    string `json:"narratorName"`
			Description string `json:"description"`
		} `json:"metadata"`
	} `json:"media"`
}

func absPoster(l MediaLink, c *mediaDashboardClient, v absMediaItem) mediaPoster {
	return mediaPoster{ID: v.ID, Title: v.Media.Metadata.Title, Subtitle: strings.Trim(strings.Join([]string{v.Media.Metadata.Author, v.Media.Metadata.Narrator}, " · "), " ·"), Overview: v.Media.Metadata.Description, Kind: v.MediaType, Poster: mediaImageURL(l, c.Stamp, v.ID, "Primary"), Backdrop: mediaImageURL(l, c.Stamp, v.ID, "Backdrop"), LibraryID: v.LibraryID, Date: fmt.Sprintf("%020.0f", v.Added), Duration: v.Media.Duration}
}
func (a *App) loadABSDashboard(ctx context.Context, st State, l MediaLink, c *mediaDashboardClient, d *mediaDashboardData) error {
	totals := map[string]float64{}
	known := true
	for _, lib := range st.MediaDisplay.Libraries[l.ID] {
		var stats struct {
			Items    *float64 `json:"totalItems"`
			Tracks   float64  `json:"numAudioTrack"`
			Duration float64  `json:"totalDuration"`
			Size     float64  `json:"totalSize"`
		}
		if err := upstream.DashboardRead(ctx, c.Provider, "/api/libraries/"+lib.ID+"/stats", nil, &stats); err != nil || stats.Items == nil {
			known = false
			dashboardWarning(d, lib.Name+"：音频统计不可用")
		} else {
			totals["items"] += *stats.Items
			totals["tracks"] += stats.Tracks
			totals["duration"] += stats.Duration
			totals["size"] += stats.Size
		}
		var recent struct {
			Results []absMediaItem `json:"results"`
		}
		if err := upstream.DashboardRead(ctx, c.Provider, "/api/libraries/"+lib.ID+"/items", url.Values{"sort": {"addedAt"}, "desc": {"1"}, "limit": {"24"}, "page": {"0"}, "minified": {"0"}}, &recent); err != nil {
			dashboardWarning(d, lib.Name+"：最近入库读取失败")
		} else {
			for _, item := range recent.Results {
				if item.LibraryID == "" {
					item.LibraryID = lib.ID
				}
				d.Recent = append(d.Recent, absPoster(l, c, item))
			}
		}
	}
	if known {
		d.Stats = []mediaMetric{{"音频条目", totals["items"], ""}, {"音轨", totals["tracks"], ""}, {"总时长", totals["duration"] / 3600, "h"}, {"媒体体积", totals["size"] / (1 << 30), "GiB"}}
	}
	finishRecent(d)
	return ctx.Err()
}
func (a *App) loadMediaSessions(ctx context.Context, st State, l MediaLink, c *mediaDashboardClient) ([]mediaPlaying, error) {
	values := []mediaPlaying{}
	if len(st.MediaDisplay.Libraries[l.ID]) == 0 {
		return values, nil
	}
	if l.Type == "audiobookshelf" {
		var raw struct {
			Users []struct {
				ID   string `json:"id"`
				Name string `json:"username"`
			} `json:"usersOnline"`
			Sessions []struct {
				ID        string  `json:"id"`
				UserID    string  `json:"userId"`
				LibraryID string  `json:"libraryId"`
				ItemID    string  `json:"libraryItemId"`
				Position  float64 `json:"currentTime"`
				Duration  float64 `json:"duration"`
				Metadata  struct {
					Title  string `json:"title"`
					Author string `json:"authorName"`
				} `json:"mediaMetadata"`
			} `json:"openSessions"`
		}
		if err := upstream.DashboardRead(ctx, c.Provider, "/api/users/online", nil, &raw); err != nil {
			return nil, mediaError("播放状态")
		}
		for _, s := range raw.Sessions {
			if !selectedLibrary(st, l, s.LibraryID) {
				continue
			}
			name := "正在收听"
			for _, u := range raw.Users {
				if u.ID == s.UserID {
					name = u.Name
				}
			}
			values = append(values, mediaPlaying{s.ID, name, mediaPoster{ID: s.ItemID, Title: s.Metadata.Title, Subtitle: s.Metadata.Author, Poster: mediaImageURL(l, c.Stamp, s.ItemID, "Primary")}, s.Position, s.Duration, false})
		}
	} else {
		var raw []struct {
			ID    string         `json:"Id"`
			User  string         `json:"UserName"`
			Item  videoMediaItem `json:"NowPlayingItem"`
			State struct {
				Position int64 `json:"PositionTicks"`
				Paused   bool  `json:"IsPaused"`
			} `json:"PlayState"`
		}
		if err := upstream.DashboardRead(ctx, c.Provider, "/Sessions", nil, &raw); err != nil {
			return nil, mediaError("播放状态")
		}
		for _, s := range raw {
			if s.Item.ID == "" {
				continue
			}
			allowed := false
			for _, lib := range st.MediaDisplay.Libraries[l.ID] {
				var page videoMediaPage
				if err := upstream.DashboardRead(ctx, c.Provider, "/Items", url.Values{"Ids": {s.Item.ID}, "ParentId": {lib.ID}, "Recursive": {"true"}, "Limit": {"1"}}, &page); err == nil && slices.ContainsFunc(page.Items, func(i videoMediaItem) bool { return i.ID == s.Item.ID }) {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
			values = append(values, mediaPlaying{s.ID, s.User, videoPoster(l, c, s.Item, ""), float64(s.State.Position) / 1e7, float64(s.Item.Runtime) / 1e7, s.State.Paused})
		}
	}
	return values, nil
}
func (a *App) allVideoItems(ctx context.Context, c *mediaDashboardClient, endpoint string, q url.Values) ([]videoMediaItem, error) {
	values := []videoMediaItem{}
	seen := map[string]bool{}
	for offset := 0; ; {
		q.Set("StartIndex", strconv.Itoa(offset))
		q.Set("Limit", "500")
		var page videoMediaPage
		if err := upstream.DashboardRead(ctx, c.Provider, endpoint, q, &page); err != nil {
			return nil, err
		}
		if page.Total == nil {
			return nil, mediaError("完整媒体清单")
		}
		if *page.Total > 100000 {
			return nil, fmt.Errorf("媒体清单超过10万条，请缩小展示媒体库范围")
		}
		if len(page.Items) == 0 {
			if offset < *page.Total {
				return nil, fmt.Errorf("媒体分页不完整，请重试")
			}
			break
		}
		for _, v := range page.Items {
			if !mediaItemID.MatchString(v.ID) || seen[v.ID] {
				return nil, fmt.Errorf("媒体身份重复或无效")
			}
			seen[v.ID] = true
			values = append(values, v)
		}
		offset += len(page.Items)
		if offset >= *page.Total {
			break
		}
		if offset > 100000 {
			return nil, fmt.Errorf("媒体清单超出读取范围")
		}
	}
	return values, nil
}
func (a *App) loadMediaRanking(ctx context.Context, st State, l MediaLink, c *mediaDashboardClient) ([]mediaRank, string, error) {
	result := []mediaRank{}
	ranks := map[string]*mediaRank{}
	if len(st.MediaDisplay.Libraries[l.ID]) == 0 {
		return result, "次", nil
	}
	if l.Type == "audiobookshelf" {
		var users struct {
			Users []struct {
				ID string `json:"id"`
			} `json:"users"`
		}
		if err := upstream.DashboardRead(ctx, c.Provider, "/api/users", nil, &users); err != nil {
			return nil, "", mediaError("收听排行")
		}
		if len(users.Users) > 200 {
			return nil, "", fmt.Errorf("用户数超过统计范围")
		}
		for _, u := range users.Users {
			if !mediaItemID.MatchString(u.ID) {
				continue
			}
			var stats struct {
				Items map[string]struct {
					Time float64 `json:"timeListening"`
				} `json:"items"`
			}
			if err := upstream.DashboardRead(ctx, c.Provider, "/api/users/"+u.ID+"/listening-stats", nil, &stats); err != nil {
				return nil, "", mediaError("收听排行")
			}
			for id, v := range stats.Items {
				if !mediaItemID.MatchString(id) || v.Time <= 0 {
					continue
				}
				rank := ranks[id]
				if rank == nil {
					var item absMediaItem
					if err := upstream.DashboardRead(ctx, c.Provider, "/api/items/"+id, url.Values{"expanded": {"1"}}, &item); err != nil {
						return nil, "", mediaError("收听作品")
					}
					if !selectedLibrary(st, l, item.LibraryID) {
						continue
					}
					rank = &mediaRank{Item: absPoster(l, c, item)}
					ranks[id] = rank
				}
				rank.Value += v.Time / 3600
				if len(ranks) > 10000 {
					return nil, "", fmt.Errorf("收听作品超过统计范围")
				}
			}
		}
	} else {
		var users []struct {
			ID string `json:"Id"`
		}
		if err := upstream.DashboardRead(ctx, c.Provider, "/Users", nil, &users); err != nil {
			return nil, "", mediaError("播放排行")
		}
		if len(users) > 200 {
			return nil, "", fmt.Errorf("用户数超过统计范围")
		}
		for _, u := range users {
			if !mediaItemID.MatchString(u.ID) {
				continue
			}
			for _, lib := range st.MediaDisplay.Libraries[l.ID] {
				items, err := a.allVideoItems(ctx, c, "/Users/"+u.ID+"/Items", url.Values{"ParentId": {lib.ID}, "Recursive": {"true"}, "IncludeItemTypes": {"Movie,Episode"}, "Filters": {"IsPlayed"}, "Fields": {"UserData,SeriesName"}, "EnableUserData": {"true"}})
				if err != nil {
					return nil, "", mediaError("播放排行")
				}
				for _, item := range items {
					if item.UserData.PlayCount == nil {
						return nil, "", fmt.Errorf("媒体接口未提供播放次数")
					}
					if *item.UserData.PlayCount <= 0 {
						continue
					}
					id := item.ID
					if item.Type == "Episode" && item.SeriesID != "" {
						id = item.SeriesID
					}
					rank := ranks[id]
					if rank == nil {
						poster := videoPoster(l, c, item, lib.ID)
						if id != item.ID {
							poster.ID, poster.Title = id, item.SeriesName
						}
						rank = &mediaRank{Item: poster}
						ranks[id] = rank
					}
					rank.Value += float64(*item.UserData.PlayCount)
				}
			}
		}
	}
	for _, v := range ranks {
		result = append(result, *v)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Value != result[j].Value {
			return result[i].Value > result[j].Value
		}
		return result[i].Item.ID < result[j].Item.ID
	})
	if len(result) > 100 {
		result = result[:100]
	}
	unit := "次"
	if l.Type == "audiobookshelf" {
		unit = "h"
	}
	return result, unit, nil
}
