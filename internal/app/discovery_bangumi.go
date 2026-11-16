package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const bangumiSource = "bangumi"
const bangumiAnime = 2
const bangumiPageSize = 20

type bangumiImages struct {
	Large  string `json:"large"`
	Medium string `json:"medium"`
	Common string `json:"common"`
}

func (i bangumiImages) image() string {
	for _, raw := range []string{i.Large, i.Common, i.Medium} {
		if image := bangumiImage(raw); image != "" {
			return image
		}
	}
	return ""
}

func bangumiImage(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host != "lain.bgm.tv" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(!strings.HasPrefix(u.Path, "/pic/") && !strings.HasPrefix(u.Path, "/r/")) || strings.ContainsAny(u.Path, "\\\x00\r\n") || strings.Contains(u.Path, "..") {
		return ""
	}
	u.Scheme, u.RawQuery, u.Fragment, u.RawFragment, u.ForceQuery = "https", "", "", "", false
	return u.String()
}

type bangumiSubject struct {
	ID       int           `json:"id"`
	Type     int           `json:"type"`
	Name     string        `json:"name"`
	NameCN   string        `json:"name_cn"`
	Platform string        `json:"platform"`
	Summary  string        `json:"summary"`
	Date     string        `json:"date"`
	AirDate  string        `json:"air_date"`
	Episodes int           `json:"eps"`
	NSFW     bool          `json:"nsfw"`
	Images   bangumiImages `json:"images"`
	Rating   struct {
		Score float64 `json:"score"`
	} `json:"rating"`
}

func (s bangumiSubject) media() discoveryMedia {
	title := strings.TrimSpace(s.NameCN)
	if title == "" {
		title = strings.TrimSpace(s.Name)
	}
	if s.ID < 1 || s.ID > 1_000_000_000 || s.Type != bangumiAnime || s.NSFW || title == "" {
		return discoveryMedia{}
	}
	kind := "tv"
	if s.Platform == "剧场版" || strings.EqualFold(s.Platform, "movie") {
		kind = "movie"
	}
	year := ""
	if date, err := time.Parse("2006-01-02", s.release()); err == nil {
		year = date.Format("2006")
	}
	return discoveryMedia{ID: s.ID, Kind: kind, Source: bangumiSource, Title: title, OriginalTitle: s.Name,
		Year: year, Overview: s.Summary, Poster: s.Images.image(), Score: s.Rating.Score}
}

func (s bangumiSubject) release() string {
	if s.Date != "" {
		return s.Date
	}
	return s.AirDate
}

func (a *App) bangumiJSON(ctx context.Context, method, endpoint string, query url.Values, body any, out any) error {
	client, closeIdle, err := pluginClient(a.store.snapshotWithLogLimit(0).Plugins["proxy"])
	if err != nil {
		return err
	}
	defer closeIdle()
	address := &url.URL{Scheme: "https", Host: "api.bgm.tv", Path: endpoint, RawQuery: query.Encode()}
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, address.String(), payload)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Hyaeve/Aether/"+Version+" (https://github.com/Hyaeve/aether)")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("无法连接 Bangumi，请检查网络或代理")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("Bangumi 请求失败（HTTP %d）", res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out); err != nil {
		return errors.New("Bangumi 返回的数据无效")
	}
	return nil
}

func (a *App) bangumiFeed(w http.ResponseWriter, ctx context.Context, query, group string, page int) {
	var sections []discoverySection
	if query == "" && group != "" && group != "bangumi-calendar" && group != "bangumi-popular" {
		fail(w, 400, errors.New("推荐榜单无效"))
		return
	}
	if query == "" && group != "bangumi-popular" {
		calendar, err := a.bangumiCalendarShelf(ctx, page)
		if err != nil {
			fail(w, 502, err)
			return
		}
		sections = append(sections, calendar)
	}
	if query != "" || group != "bangumi-calendar" {
		list, err := a.bangumiSearchShelf(ctx, query, page)
		if err != nil {
			fail(w, 502, err)
			return
		}
		sections = append(sections, list)
	}
	jsonResponse(w, http.StatusOK, map[string]any{"sections": sections})
}

func (a *App) bangumiCalendarShelf(ctx context.Context, page int) (discoverySection, error) {
	var week []struct {
		Weekday struct {
			ID int `json:"id"`
		} `json:"weekday"`
		Items []bangumiSubject `json:"items"`
	}
	if err := a.bangumiJSON(ctx, http.MethodGet, "/calendar", nil, nil, &week); err != nil {
		return discoverySection{}, err
	}
	today := int(time.Now().In(time.FixedZone("Bangumi", 8*60*60)).Weekday())
	items, seen := []discoveryMedia{}, map[int]bool{}
	for day := range 7 {
		for _, entry := range week {
			if entry.Weekday.ID < 1 || entry.Weekday.ID > 7 || entry.Weekday.ID%7 != (today+day)%7 {
				continue
			}
			for _, subject := range entry.Items {
				m := subject.media()
				if m.ID > 0 && !seen[m.ID] {
					items = append(items, m)
					seen[m.ID] = true
				}
			}
		}
	}
	pages := max(1, (len(items)+bangumiPageSize-1)/bangumiPageSize)
	page = min(page, pages)
	start := (page - 1) * bangumiPageSize
	return discoverySection{"bangumi-calendar", "Bangumi 每日放送", items[start:min(len(items), start+bangumiPageSize)], page, pages}, nil
}

func (a *App) bangumiSearchShelf(ctx context.Context, query string, page int) (discoverySection, error) {
	limitPages := 1000 / bangumiPageSize
	page = min(page, limitPages)
	id, title, order := "bangumi-popular", "Bangumi 热门动漫", "heat"
	if query != "" {
		id, title, order = "search", "搜索结果", "match"
	}
	q := url.Values{"limit": {strconv.Itoa(bangumiPageSize)}, "offset": {strconv.Itoa((page - 1) * bangumiPageSize)}}
	body := map[string]any{"keyword": query, "sort": order, "filter": map[string]any{"type": []int{bangumiAnime}, "nsfw": false}}
	var result struct {
		Data  []bangumiSubject `json:"data"`
		Total int              `json:"total"`
	}
	if err := a.bangumiJSON(ctx, http.MethodPost, "/v0/search/subjects", q, body, &result); err != nil {
		return discoverySection{}, err
	}
	items, seen := []discoveryMedia{}, map[int]bool{}
	for _, subject := range result.Data {
		m := subject.media()
		if m.ID > 0 && !seen[m.ID] {
			items = append(items, m)
			seen[m.ID] = true
		}
	}
	pages := max(1, (min(result.Total, 1000)+bangumiPageSize-1)/bangumiPageSize)
	return discoverySection{id, title, items, page, pages}, nil
}

func (a *App) bangumiGetSubject(ctx context.Context, id int) (bangumiSubject, error) {
	if id < 1 || id > 1_000_000_000 {
		return bangumiSubject{}, errors.New("Bangumi 编号无效")
	}
	var subject bangumiSubject
	if err := a.bangumiJSON(ctx, http.MethodGet, fmt.Sprintf("/v0/subjects/%d", id), nil, nil, &subject); err != nil {
		return bangumiSubject{}, err
	}
	if subject.ID != id || subject.media().ID == 0 {
		return bangumiSubject{}, errors.New("Bangumi 条目不存在或不是可用的动画")
	}
	return subject, nil
}

func (a *App) bangumiDetail(ctx context.Context, id int) (discoveryDetail, error) {
	subject, err := a.bangumiGetSubject(ctx, id)
	if err != nil {
		return discoveryDetail{}, err
	}
	d := discoveryDetail{discoveryMedia: subject.media(), Release: subject.release(), Genres: []string{"动画"}, Seasons: []discoverySeason{}}
	if d.Kind == "tv" {
		d.Seasons = append(d.Seasons, discoverySeason{Number: 1, Name: subject.Platform, Episodes: max(0, subject.Episodes)})
	}
	var characters []struct {
		Name   string `json:"name"`
		Actors []struct {
			Name   string        `json:"name"`
			Images bangumiImages `json:"images"`
		} `json:"actors"`
	}
	creditsCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if a.bangumiJSON(creditsCtx, http.MethodGet, fmt.Sprintf("/v0/subjects/%d/characters", id), nil, nil, &characters) == nil {
		for _, character := range characters {
			for _, actor := range character.Actors {
				if len(d.People) == 12 {
					return d, nil
				}
				if actor.Name != "" {
					d.People = append(d.People, discoveryPerson{Name: actor.Name, Role: character.Name, Photo: actor.Images.image()})
				}
			}
		}
	}
	return d, nil
}
