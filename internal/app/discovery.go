package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type discoveryCacheEntry struct {
	Data  []byte
	Until time.Time
}
type discoveryMedia struct {
	ID            int     `json:"id"`
	Kind          string  `json:"kind"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"originalTitle"`
	Year          string  `json:"year"`
	Overview      string  `json:"overview"`
	Poster        string  `json:"poster"`
	Backdrop      string  `json:"backdrop"`
	Score         float64 `json:"score"`
}
type discoverySeason struct {
	Number   int    `json:"number"`
	Name     string `json:"name"`
	Episodes int    `json:"episodes"`
}
type discoveryDetail struct {
	discoveryMedia
	Genres    []string          `json:"genres"`
	Runtime   int               `json:"runtime"`
	Status    string            `json:"status"`
	Language  string            `json:"language"`
	IMDb      string            `json:"imdb"`
	Countries []string          `json:"countries"`
	Companies []string          `json:"companies"`
	Cast      []string          `json:"cast"`
	Seasons   []discoverySeason `json:"seasons"`
}
type tmdbDiscovery struct {
	ID             int     `json:"id"`
	MediaType      string  `json:"media_type"`
	Title          string  `json:"title"`
	Name           string  `json:"name"`
	OriginalTitle  string  `json:"original_title"`
	OriginalName   string  `json:"original_name"`
	Date           string  `json:"release_date"`
	FirstDate      string  `json:"first_air_date"`
	Overview       string  `json:"overview"`
	Poster         string  `json:"poster_path"`
	Backdrop       string  `json:"backdrop_path"`
	Score          float64 `json:"vote_average"`
	Runtime        int     `json:"runtime"`
	EpisodeRuntime []int   `json:"episode_run_time"`
	Status         string  `json:"status"`
	Language       string  `json:"original_language"`
	IMDb           string  `json:"imdb_id"`
	Genres         []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Countries []struct {
		Name string `json:"name"`
	} `json:"production_countries"`
	Companies []struct {
		Name string `json:"name"`
	} `json:"production_companies"`
	Seasons []struct {
		Number   int    `json:"season_number"`
		Name     string `json:"name"`
		Episodes int    `json:"episode_count"`
	} `json:"seasons"`
	Credits struct {
		Cast []struct {
			Name string `json:"name"`
		} `json:"cast"`
	} `json:"credits"`
}

func discoveryImage(p, size string) string {
	if p == "" || !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "?#\\\x00\r\n") || strings.Contains(p, "..") {
		return ""
	}
	return "https://image.tmdb.org/t/p/" + size + p
}
func (v tmdbDiscovery) media(kind string) discoveryMedia {
	if v.MediaType != "" {
		kind = v.MediaType
	}
	title, original, date := v.Title, v.OriginalTitle, v.Date
	if kind == "tv" {
		title, original, date = v.Name, v.OriginalName, v.FirstDate
	}
	if len(date) > 4 {
		date = date[:4]
	}
	return discoveryMedia{v.ID, kind, title, original, date, v.Overview, discoveryImage(v.Poster, "w500"), discoveryImage(v.Backdrop, "w1280"), v.Score}
}
func (a *App) discoveryJSON(ctx context.Context, endpoint string, q url.Values, out any) error {
	st := a.store.snapshotWithLogLimit(0)
	cfg := pluginDefaults("tmdb", st.Plugins["tmdb"])
	if !cfg.Enabled || cfg.APIKey == "" {
		return errors.New("请先在以太识律的辅助配置中配置并启用TMDB")
	}
	key := archiveStamp(telegramDigest(cfg), telegramDigest(st.Plugins["proxy"]), endpoint, q.Encode())
	a.discoveryMu.Lock()
	cached, ok := a.discoveryCache[key]
	a.discoveryMu.Unlock()
	if ok && time.Now().Before(cached.Until) {
		return json.Unmarshal(cached.Data, out)
	}
	client, closeIdle, err := pluginClient(st.Plugins["proxy"])
	if err != nil {
		return err
	}
	defer closeIdle()
	var raw json.RawMessage
	if err := tmdbGet(ctx, client, cfg, endpoint, q, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.New("TMDB数据格式无效")
	}
	if len(raw) <= 1<<20 {
		a.discoveryMu.Lock()
		if a.discoveryCache == nil {
			a.discoveryCache = map[string]discoveryCacheEntry{}
		}
		if len(a.discoveryCache) >= 32 {
			for k, v := range a.discoveryCache {
				if time.Now().After(v.Until) {
					delete(a.discoveryCache, k)
				}
			}
			if len(a.discoveryCache) >= 32 {
				for k := range a.discoveryCache {
					delete(a.discoveryCache, k)
					break
				}
			}
		}
		a.discoveryCache[key] = discoveryCacheEntry{append([]byte(nil), raw...), time.Now().Add(15 * time.Minute)}
		a.discoveryMu.Unlock()
	}
	return nil
}

type discoveryFeedSpec struct {
	ID, Title, Endpoint, Kind string
	Anime                     bool
}

var discoveryFeeds = map[string][]discoveryFeedSpec{
	"all":      {{"trending", "流行趋势", "trending/all/day", "", false}, {"playing", "正在热映", "movie/now_playing", "movie", false}, {"airing", "正在播出", "tv/on_the_air", "tv", false}},
	"tv":       {{"popular-tv", "热门剧集", "tv/popular", "tv", false}, {"airing", "正在播出", "tv/on_the_air", "tv", false}, {"top-tv", "高分剧集", "tv/top_rated", "tv", false}},
	"movie":    {{"playing", "正在热映", "movie/now_playing", "movie", false}, {"popular-movie", "热门电影", "movie/popular", "movie", false}, {"upcoming", "即将上映", "movie/upcoming", "movie", false}},
	"anime":    {{"anime-tv", "热门动漫剧集", "discover/tv", "tv", true}, {"anime-movie", "热门动画电影", "discover/movie", "movie", true}},
	"rankings": {{"top-movie", "电影高分榜", "movie/top_rated", "movie", false}, {"top-tv", "剧集高分榜", "tv/top_rated", "tv", false}},
}

func (a *App) discoveryFeed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	category := r.URL.Query().Get("category")
	if category == "" {
		category = "all"
	}
	specs, ok := discoveryFeeds[category]
	page := 1
	if s := r.URL.Query().Get("page"); s != "" {
		n, e := strconv.Atoi(s)
		if e != nil || n < 1 || n > 500 {
			fail(w, 400, errors.New("页码须为1至500的整数"))
			return
		}
		page = n
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if !ok || len(query) > 300 {
		fail(w, 400, errors.New("推荐分类或搜索内容无效"))
		return
	}
	if query != "" {
		specs = []discoveryFeedSpec{{"search", "搜索结果", "search/multi", "", false}}
	}
	if group := r.URL.Query().Get("group"); group != "" && query == "" {
		found := false
		for _, s := range specs {
			if s.ID == group {
				specs = []discoveryFeedSpec{s}
				found = true
				break
			}
		}
		if !found {
			fail(w, 400, errors.New("推荐榜单无效"))
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	sections := []map[string]any{}
	for _, s := range specs {
		q := url.Values{"page": {strconv.Itoa(page)}, "include_adult": {"false"}}
		if s.Anime {
			q.Set("with_genres", "16")
			q.Set("sort_by", "popularity.desc")
		}
		if query != "" {
			q.Set("query", query)
		}
		var raw struct {
			Results []tmdbDiscovery `json:"results"`
			Pages   int             `json:"total_pages"`
		}
		if err := a.discoveryJSON(ctx, s.Endpoint, q, &raw); err != nil {
			fail(w, 502, err)
			return
		}
		items := []discoveryMedia{}
		for _, v := range raw.Results {
			m := v.media(s.Kind)
			if m.ID > 0 && (m.Kind == "movie" || m.Kind == "tv") && m.Title != "" {
				items = append(items, m)
			}
		}
		pages := min(500, max(1, raw.Pages))
		sections = append(sections, map[string]any{"id": s.ID, "title": s.Title, "items": items, "page": page, "pages": pages})
	}
	jsonResponse(w, 200, map[string]any{"sections": sections})
}
func (a *App) discoveryMediaDetail(ctx context.Context, kind string, id int) (discoveryDetail, error) {
	if (kind != "movie" && kind != "tv") || id < 1 || id > 1_000_000_000 {
		return discoveryDetail{}, errors.New("媒体类型或TMDB编号无效")
	}
	var v tmdbDiscovery
	if err := a.discoveryJSON(ctx, fmt.Sprintf("%s/%d", kind, id), url.Values{"append_to_response": {"credits"}}, &v); err != nil {
		return discoveryDetail{}, err
	}
	if v.ID != id {
		return discoveryDetail{}, errors.New("TMDB返回的媒体编号不符")
	}
	d := discoveryDetail{discoveryMedia: v.media(kind), Runtime: v.Runtime, Status: v.Status, Language: v.Language, IMDb: v.IMDb, Genres: []string{}, Countries: []string{}, Companies: []string{}, Cast: []string{}, Seasons: []discoverySeason{}}
	if d.Runtime == 0 && len(v.EpisodeRuntime) > 0 {
		d.Runtime = v.EpisodeRuntime[0]
	}
	for _, x := range v.Genres {
		d.Genres = append(d.Genres, x.Name)
	}
	for _, x := range v.Countries {
		d.Countries = append(d.Countries, x.Name)
	}
	for _, x := range v.Companies {
		d.Companies = append(d.Companies, x.Name)
	}
	for i, x := range v.Credits.Cast {
		if i >= 12 {
			break
		}
		d.Cast = append(d.Cast, x.Name)
	}
	for _, x := range v.Seasons {
		d.Seasons = append(d.Seasons, discoverySeason{x.Number, x.Name, x.Episodes})
	}
	return d, nil
}
func (a *App) discoveryDetailAPI(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		fail(w, 400, errors.New("TMDB编号无效"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	value, err := a.discoveryMediaDetail(ctx, r.PathValue("kind"), id)
	if err != nil {
		fail(w, 502, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, value)
}
