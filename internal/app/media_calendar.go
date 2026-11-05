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
			items = append(items, calendarEpisode{fmt.Sprintf("%s:%d", card.Key, e.Number), card.Title, e.Date, card.Poster, card.ID, card.Season, e.Number})
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
