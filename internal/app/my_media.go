package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"aether/internal/linkcore/upstream"
)

type mediaDisplaySettings struct {
	Selected        string                        `json:"selected"`
	MissingSelected string                        `json:"missingSelected"`
	Libraries       map[string][]upstream.Library `json:"libraries"`
}
type mediaDashboardClient struct {
	Provider upstream.Provider
	Stamp    string
	mu       sync.Mutex
	Data     *mediaDashboardData
	At       time.Time
}
type mediaPoster struct {
	Path        string  `json:"path,omitempty"`
	LibraryName string  `json:"libraryName,omitempty"`
	TMDB        int     `json:"tmdb,omitempty"`
	Episodes    *int    `json:"episodes,omitempty"`
	WebURL      string  `json:"webURL,omitempty"`
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Subtitle    string  `json:"subtitle"`
	Overview    string  `json:"overview"`
	Kind        string  `json:"kind"`
	Poster      string  `json:"poster"`
	Backdrop    string  `json:"backdrop"`
	LibraryID   string  `json:"libraryId"`
	Date        string  `json:"date"`
	Duration    float64 `json:"duration"`
}
type mediaMetric struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
}
type mediaDashboardData struct {
	Featured      []mediaPoster `json:"featured"`
	FeaturedUntil time.Time     `json:"featuredUntil,omitzero"`
	Recent        []mediaPoster `json:"recent"`
	Stats         []mediaMetric `json:"stats"`
	Warnings      []string      `json:"warnings"`
	Updated       time.Time     `json:"updated"`
}
type mediaFeaturedResult struct {
	Stamp string        `json:"stamp"`
	Until time.Time     `json:"until"`
	Items []mediaPoster `json:"items"`
}
type mediaPlaying struct {
	ID       string      `json:"id"`
	User     string      `json:"user"`
	Item     mediaPoster `json:"item"`
	Position float64     `json:"position"`
	Duration float64     `json:"duration"`
	Paused   bool        `json:"paused"`
}
type mediaRank struct {
	Item  mediaPoster `json:"item"`
	Value float64     `json:"value"`
}

func mediaLink(st State, key string) (MediaLink, bool) {
	for _, l := range st.Links {
		if l.ID == key {
			return l, true
		}
	}
	return MediaLink{}, false
}
func mediaDisplayStamp(st State, l MediaLink) string {
	return telegramDigest(struct {
		Link      MediaLink
		Libraries []upstream.Library
		Owner     string
	}{l, st.MediaDisplay.Libraries[l.ID], st.Username})
}
func validateMediaDisplay(c mediaDisplaySettings) error {
	if len(c.Selected) > 200 || len(c.MissingSelected) > 200 || len(c.Libraries) > 200 {
		return errors.New("媒体展示配置无效")
	}
	for id, ls := range c.Libraries {
		if !shareCodePattern.MatchString(id) || len(ls) > 100 {
			return errors.New("媒体库展示范围无效")
		}
		seen := map[string]bool{}
		for _, l := range ls {
			if !mediaItemID.MatchString(l.ID) || len(l.Name) > 500 || seen[l.ID] {
				return errors.New("媒体库标识或名称无效")
			}
			seen[l.ID] = true
		}
	}
	return nil
}
func (a *App) dashboardClient(st State, l MediaLink) (*mediaDashboardClient, error) {
	stamp := mediaDisplayStamp(st, l)
	a.mediaMu.Lock()
	defer a.mediaMu.Unlock()
	if a.mediaClients == nil {
		a.mediaClients = map[string]*mediaDashboardClient{}
		context.AfterFunc(a.ctx, func() {
			a.mediaMu.Lock()
			defer a.mediaMu.Unlock()
			for _, c := range a.mediaClients {
				if t, ok := c.Provider.Transport().(*http.Transport); ok {
					t.CloseIdleConnections()
				}
			}
		})
	}
	if c := a.mediaClients[l.ID]; c != nil && c.Stamp == stamp {
		return c, nil
	}
	if c := a.mediaClients[l.ID]; c != nil {
		if t, ok := c.Provider.Transport().(*http.Transport); ok {
			t.CloseIdleConnections()
		}
	}
	if len(a.mediaClients) >= 32 {
		for id, c := range a.mediaClients {
			if t, ok := c.Provider.Transport().(*http.Transport); ok {
				t.CloseIdleConnections()
			}
			delete(a.mediaClients, id)
			break
		}
	}
	p, err := upstream.NewDashboard(a.linkUpstream(l))
	if err != nil {
		return nil, errors.New("以链地址或授权配置无效")
	}
	c := &mediaDashboardClient{Provider: p, Stamp: stamp}
	a.mediaClients[l.ID] = c
	return c, nil
}
func (a *App) mediaConfigAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == "PUT" {
		var in struct {
			Selected        *string             `json:"selected"`
			MissingSelected *string             `json:"missingSelected"`
			LinkID          string              `json:"linkId"`
			Libraries       *[]upstream.Library `json:"libraries"`
		}
		if !decode(w, r, &in) {
			return
		}
		before := a.store.snapshotWithLogLimit(0)
		var canonical []upstream.Library
		var sourceStamp string
		if in.Libraries != nil {
			l, ok := mediaLink(before, in.LinkID)
			if !ok {
				fail(w, 400, errors.New("以链不存在"))
				return
			}
			c, err := a.dashboardClient(before, l)
			if err != nil {
				fail(w, 400, err)
				return
			}
			sourceStamp = mediaDisplayStamp(before, l)
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()
			ls, err := c.Provider.Libraries(ctx)
			if err != nil {
				fail(w, 502, errors.New("媒体库读取失败，请检查以链授权"))
				return
			}
			canonical = []upstream.Library{}
			for _, wanted := range *in.Libraries {
				n := slices.IndexFunc(ls, func(v upstream.Library) bool { return v.ID == wanted.ID })
				if n < 0 {
					fail(w, 409, errors.New("媒体库已移除或无权访问，请重新获取"))
					return
				}
				canonical = append(canonical, ls[n])
			}
		}
		err := a.store.update(func(st *State) error {
			if in.Selected != nil {
				if _, ok := mediaLink(*st, *in.Selected); !ok && *in.Selected != "" {
					return errors.New("所选媒体已移除")
				}
				st.MediaDisplay.Selected = *in.Selected
			}
			if in.MissingSelected != nil {
				l, ok := mediaLink(*st, *in.MissingSelected)
				if *in.MissingSelected != "" && (!ok || l.Type != "emby") {
					return errors.New("缺集管理仅支持Emby")
				}
				st.MediaDisplay.MissingSelected = *in.MissingSelected
			}
			if in.Libraries != nil {
				l, ok := mediaLink(*st, in.LinkID)
				if !ok || mediaDisplayStamp(*st, l) != sourceStamp {
					return errors.New("以链配置已改变，请重试")
				}
				if st.MediaDisplay.Libraries == nil {
					st.MediaDisplay.Libraries = map[string][]upstream.Library{}
				}
				st.MediaDisplay.Libraries[in.LinkID] = canonical
			}
			return validateMediaDisplay(st.MediaDisplay)
		})
		if err != nil {
			fail(w, 409, err)
			return
		}
	}
	st := a.store.snapshotWithLogLimit(0)
	rows := []map[string]any{}
	for _, l := range st.Links {
		u, _ := url.Parse(l.Address)
		address := ""
		if u != nil {
			u.User = nil
			u.RawQuery = ""
			u.Fragment = ""
			address = u.String()
		}
		rows = append(rows, map[string]any{"id": l.ID, "name": l.Name, "type": l.Type, "address": address, "enabled": l.Enabled, "libraries": st.MediaDisplay.Libraries[l.ID]})
	}
	selected := st.MediaDisplay.Selected
	if _, ok := mediaLink(st, selected); !ok {
		selected = ""
		if len(st.Links) > 0 {
			selected = st.Links[0].ID
		}
	}
	missingSelected := st.MediaDisplay.MissingSelected
	if link, ok := mediaLink(st, missingSelected); !ok || link.Type != "emby" {
		missingSelected = ""
		for _, link := range st.Links {
			if link.Type == "emby" {
				missingSelected = link.ID
				break
			}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]any{"items": rows, "selected": selected, "missingSelected": missingSelected})
}
func (a *App) mediaLibrariesAPI(w http.ResponseWriter, r *http.Request) {
	st := a.store.snapshotWithLogLimit(0)
	l, ok := mediaLink(st, r.PathValue("id"))
	if !ok {
		fail(w, 404, errors.New("以链不存在"))
		return
	}
	c, err := a.dashboardClient(st, l)
	if err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ls, err := c.Provider.Libraries(ctx)
	if err != nil {
		fail(w, 502, errors.New("媒体库读取失败，请检查以链授权与服务支持"))
		return
	}
	jsonResponse(w, 200, ls)
}
func mediaImageURL(l MediaLink, stamp, id, kind string) string {
	if !mediaItemID.MatchString(id) {
		return ""
	}
	return "/api/my-media/" + url.PathEscape(l.ID) + "/image/" + url.PathEscape(id) + "?kind=" + kind + "&revision=" + stamp
}
func (a *App) mediaImageAPI(w http.ResponseWriter, r *http.Request) {
	st := a.store.snapshotWithLogLimit(0)
	l, ok := mediaLink(st, r.PathValue("id"))
	if !ok {
		w.WriteHeader(404)
		return
	}
	stamp := mediaDisplayStamp(st, l)
	if r.URL.Query().Get("revision") != stamp {
		w.WriteHeader(409)
		return
	}
	c, err := a.dashboardClient(st, l)
	if err != nil {
		w.WriteHeader(502)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	b, ct, err := upstream.DashboardImage(ctx, c.Provider, r.PathValue("item"), r.URL.Query().Get("kind"))
	if err != nil {
		w.WriteHeader(502)
		return
	}
	now := a.store.snapshotWithLogLimit(0)
	current, ok := mediaLink(now, l.ID)
	if !ok || mediaDisplayStamp(now, current) != stamp {
		w.WriteHeader(409)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private,max-age=60")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(b)
}
func (a *App) mediaDashboardAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	action := r.PathValue("action")
	if action != "dashboard" && action != "sessions" && action != "ranking" {
		fail(w, 404, errors.New("媒体页面不存在"))
		return
	}
	st := a.store.snapshotWithLogLimit(0)
	l, ok := mediaLink(st, r.PathValue("id"))
	if !ok {
		fail(w, 404, errors.New("以链不存在"))
		return
	}
	c, err := a.dashboardClient(st, l)
	if err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if action == "ranking" {
		rank, unit, err := a.loadMediaRanking(ctx, st, l, c)
		if err != nil {
			fail(w, 502, err)
			return
		}
		if !mediaScopeCurrent(a.store.snapshotWithLogLimit(0), l, c.Stamp) {
			fail(w, 409, errors.New("媒体配置已改变"))
			return
		}
		jsonResponse(w, 200, map[string]any{"items": rank, "unit": unit})
		return
	}
	if action == "sessions" {
		sessions, err := a.loadMediaSessions(ctx, st, l, c)
		if err != nil {
			fail(w, 502, err)
			return
		}
		if !mediaScopeCurrent(a.store.snapshotWithLogLimit(0), l, c.Stamp) {
			fail(w, 409, errors.New("媒体配置已改变"))
			return
		}
		jsonResponse(w, 200, sessions)
		return
	}
	data, err := a.mediaDashboard(ctx, l, c, time.Now(), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		if errors.Is(err, errMediaScopeChanged) {
			fail(w, 409, err)
			return
		}
		fail(w, 502, err)
		return
	}
	jsonResponse(w, 200, data)
}

var errMediaScopeChanged = errors.New("媒体展示配置已改变")

func nextMediaFeaturedRefresh(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
}

func (a *App) mediaDashboard(ctx context.Context, l MediaLink, c *mediaDashboardClient, now time.Time, refresh bool) (*mediaDashboardData, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := a.store.snapshotWithLogLimit(0)
	if !mediaScopeCurrent(st, l, c.Stamp) {
		return nil, errMediaScopeChanged
	}
	next := nextMediaFeaturedRefresh(now)
	if c.Data != nil && !refresh && now.Sub(c.At) < time.Minute && next.Equal(nextMediaFeaturedRefresh(c.At)) &&
		(c.Data.FeaturedUntil.IsZero() || now.Before(c.Data.FeaturedUntil)) {
		return c.Data, nil
	}
	data := &mediaDashboardData{Featured: []mediaPoster{}, Recent: []mediaPoster{}, Stats: []mediaMetric{}, Warnings: []string{}, Updated: now}
	if len(st.MediaDisplay.Libraries[l.ID]) == 0 {
		return data, nil
	}
	featured := st.MediaFeatured[l.ID]
	if featured.Stamp != c.Stamp {
		featured = mediaFeaturedResult{}
	}
	if featured.Items != nil {
		data.Featured = featured.Items
	}
	data.FeaturedUntil = featured.Until
	var err error
	recentComplete := true
	if l.Type == "audiobookshelf" {
		recentComplete, err = a.loadABSDashboard(ctx, st, l, c, data)
	} else {
		err = a.loadVideoDashboard(ctx, st, l, c, data)
	}
	if err != nil {
		return nil, err
	}
	if featured.Until.Before(next) {
		items := data.Recent[:min(5, len(data.Recent))]
		if l.Type != "audiobookshelf" {
			var videoItems []mediaPoster
			videoItems, err = a.loadVideoFeatured(ctx, st, l, c)
			if len(videoItems) > 0 {
				items = videoItems
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil && recentComplete {
			batch := mediaFeaturedResult{Stamp: c.Stamp, Until: next, Items: items}
			err = a.store.update(func(current *State) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if !mediaScopeCurrent(*current, l, c.Stamp) {
					return errMediaScopeChanged
				}
				if saved := current.MediaFeatured[l.ID]; saved.Stamp == c.Stamp && !saved.Until.Before(next) {
					batch = saved
					return nil
				}
				if current.MediaFeatured == nil {
					current.MediaFeatured = map[string]mediaFeaturedResult{}
				}
				for key := range current.MediaFeatured {
					if _, exists := mediaLink(*current, key); !exists {
						delete(current.MediaFeatured, key)
					}
				}
				current.MediaFeatured[l.ID] = batch
				return nil
			})
			if err == nil {
				data.Featured, data.FeaturedUntil = batch.Items, batch.Until
			}
		}
		if err != nil || !recentComplete {
			dashboardWarning(data, "横幅海报暂未更新")
			data.FeaturedUntil = now.Add(time.Minute)
			if next.Before(data.FeaturedUntil) {
				data.FeaturedUntil = next
			}
		}
	}
	if !mediaScopeCurrent(a.store.snapshotWithLogLimit(0), l, c.Stamp) {
		return nil, errMediaScopeChanged
	}
	c.Data, c.At = data, now
	return data, nil
}

func selectedLibrary(st State, l MediaLink, id string) bool {
	return slices.ContainsFunc(st.MediaDisplay.Libraries[l.ID], func(v upstream.Library) bool { return v.ID == id })
}
func dashboardWarning(data *mediaDashboardData, what string) {
	if !slices.Contains(data.Warnings, what) {
		data.Warnings = append(data.Warnings, what)
	}
}
func mediaError(what string) error {
	return fmt.Errorf("%s读取失败，请检查媒体服务授权及接口支持", strings.TrimSpace(what))
}
