package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"
)

type calendarEpisode struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Date    string `json:"date"`
	Poster  string `json:"poster"`
	TMDB    int    `json:"tmdb"`
	Bangumi int    `json:"bangumi,omitempty"`
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
}

type tmdbSeason struct {
	Episodes []struct {
		Name   string `json:"name"`
		Date   string `json:"air_date"`
		Season int    `json:"season_number"`
		Number int    `json:"episode_number"`
	} `json:"episodes"`
}

func (a *App) bangumiCalendarEpisodes(ctx context.Context, card mediaSubscription, month string) ([]calendarEpisode, error) {
	const limit = 200
	items := []calendarEpisode{}
	for offset := 0; offset < 100_000; {
		q := url.Values{"subject_id": {fmt.Sprint(card.ID)}, "type": {"0"}, "limit": {fmt.Sprint(limit)}, "offset": {fmt.Sprint(offset)}}
		var page struct {
			Total int `json:"total"`
			Data  []struct {
				ID      int     `json:"id"`
				Subject int     `json:"subject_id"`
				Number  float64 `json:"ep"`
				Date    string  `json:"airdate"`
			} `json:"data"`
		}
		if err := a.bangumiJSON(ctx, http.MethodGet, "/v0/episodes", q, nil, &page); err != nil {
			return nil, err
		}
		for _, e := range page.Data {
			if e.Subject != card.ID || e.Number < 1 || e.Number > 100_000 || e.Number != float64(int(e.Number)) {
				continue
			}
			date, err := time.Parse("2006-01-02", e.Date)
			if err != nil || date.Format("2006-01") != month {
				continue
			}
			items = append(items, calendarEpisode{ID: fmt.Sprintf("%s:%d", card.Key, e.ID), Title: card.Title, Date: e.Date,
				Poster: card.Poster, Bangumi: card.ID, Season: card.Season, Episode: int(e.Number)})
		}
		offset += len(page.Data)
		if offset >= page.Total {
			return items, nil
		}
		if len(page.Data) == 0 {
			break
		}
	}
	return nil, fmt.Errorf("Bangumi 集数列表不完整")
}

func (a *App) mediaCalendar(w http.ResponseWriter, r *http.Request) {
	month, err := time.Parse("2006-01", r.URL.Query().Get("month"))
	if err != nil {
		fail(w, 400, fmt.Errorf("月份格式须为YYYY-MM"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	items := []calendarEpisode{}
	warnings := []string{}
	for _, card := range a.store.snapshotWithLogLimit(0).MediaSubscriptions {
		if card.Kind != "tv" {
			continue
		}
		if card.Source == bangumiSource {
			episodes, err := a.bangumiCalendarEpisodes(ctx, card, month.Format("2006-01"))
			if err != nil {
				warnings = append(warnings, card.Title+"：Bangumi 播出日期读取失败")
				if ctx.Err() != nil {
					break
				}
			} else {
				items = append(items, episodes...)
			}
			continue
		}
		var data tmdbSeason
		if err := a.discoveryJSON(ctx, fmt.Sprintf("tv/%d/season/%d", card.ID, card.Season), url.Values{}, &data); err != nil {
			warnings = append(warnings, card.Title+"：播出日期读取失败")
			if ctx.Err() != nil {
				break
			}
			continue
		}
		for _, e := range data.Episodes {
			if len(e.Date) != 10 || e.Date[:7] != month.Format("2006-01") {
				continue
			}
			if _, err := time.Parse("2006-01-02", e.Date); err != nil {
				continue
			}
			items = append(items, calendarEpisode{ID: fmt.Sprintf("%s:%d", card.Key, e.Number), Title: card.Title, Date: e.Date,
				Poster: card.Poster, TMDB: card.ID, Season: card.Season, Episode: e.Number})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Date != items[j].Date {
			return items[i].Date < items[j].Date
		}
		if items[i].Title != items[j].Title {
			return items[i].Title < items[j].Title
		}
		return items[i].Episode < items[j].Episode
	})
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]any{"items": items, "warnings": warnings, "month": month.Format("2006-01")})
}
