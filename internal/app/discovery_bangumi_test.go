package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestBangumiDiscoveryAndSubscriptionsWithoutTMDB(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	old := apiClient.Transport
	t.Cleanup(func() { apiClient.Transport = old })
	var order, keyword, offset string
	episodePages := 0
	const anime = `{"id":42,"type":2,"name":"Original","name_cn":"番剧示例","platform":"TV","summary":"简介","date":"2026-11-10","eps":12,"images":{"large":"http://lain.bgm.tv/pic/cover/l/anime.jpg"},"rating":{"score":8.1}}`
	const film = `{"id":43,"type":2,"name":"Film","name_cn":"剧场版示例","platform":"剧场版","images":{"large":"https://elsewhere.test/pic/cover/l/wrong.jpg"}}`
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.bgm.tv" || !strings.Contains(r.UserAgent(), "Hyaeve/Aether/") || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected Bangumi request: %s", r.URL)
		}
		body := ""
		switch r.URL.Path {
		case "/calendar":
			body = `[{"weekday":{"id":1},"items":[` + anime + `,` + anime + `,{"id":99,"type":1,"name":"书籍"}]}]`
		case "/v0/search/subjects":
			var input struct {
				Keyword string
				Sort    string
				Filter  struct {
					Type []int
					NSFW *bool
				}
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || r.Method != http.MethodPost ||
				len(input.Filter.Type) != 1 || input.Filter.Type[0] != 2 || input.Filter.NSFW == nil || *input.Filter.NSFW {
				t.Errorf("unexpected search body: %#v, %v", input, err)
			}
			order, keyword, offset = input.Sort, input.Keyword, r.URL.Query().Get("offset")
			body = `{"total":40,"data":[` + anime + `,` + film + `,{"id":99,"type":2,"name":"无效条目","nsfw":true}]}`
		case "/v0/subjects/42":
			body = anime
		case "/v0/subjects/43":
			body = film
		case "/v0/subjects/42/characters":
			body = `[{"name":"角色","actors":[{"name":"声优","images":{"large":"https://lain.bgm.tv/pic/crt/l/voice.jpg?r=123"}}]}]`
		case "/v0/episodes":
			episodePages++
			if r.URL.Query().Get("subject_id") != "42" || r.URL.Query().Get("type") != "0" {
				t.Error("wrong Bangumi calendar request", r.URL)
			}
			switch r.URL.Query().Get("offset") {
			case "0":
				body = `{"total":2,"data":[{"id":15,"subject_id":42,"ep":1,"airdate":"2026-11-10"}]}`
			case "1":
				body = `{"total":2,"data":[{"id":16,"subject_id":42,"ep":2,"airdate":"2026-11-17"}]}`
			}
		}
		if body == "" {
			t.Error("unexpected Bangumi endpoint", r.URL)
			body = `{}`
		}
		return jsonHTTPResponse(body), nil
	})
	read := func(path string, out any) {
		t.Helper()
		w := request(t, h, "GET", path, nil, cookie)
		if w.Code != 200 {
			t.Fatalf("%s: HTTP %d: %s", path, w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatal(err)
		}
	}
	var feed struct{ Sections []discoverySection }
	read("/api/discovery?category=anime", &feed)
	if len(feed.Sections) != 2 || feed.Sections[0].ID != "bangumi-calendar" || len(feed.Sections[0].Items) != 1 ||
		feed.Sections[1].ID != "bangumi-popular" || len(feed.Sections[1].Items) != 2 || order != "heat" || keyword != "" || offset != "0" {
		t.Fatal("unexpected Bangumi feed", feed, order, keyword, offset)
	}
	if item := feed.Sections[0].Items[0]; item.Source != "bangumi" || item.Year != "2026" || item.Poster != "https://lain.bgm.tv/pic/cover/l/anime.jpg" {
		t.Fatal("wrong calendar item", item)
	}
	if item := feed.Sections[1].Items[1]; item.Kind != "movie" || item.Poster != "" {
		t.Fatal("wrong film or unauthorized image", item)
	}
	read("/api/discovery?category=anime&q=anime&page=2", &feed)
	if len(feed.Sections) != 1 || feed.Sections[0].ID != "search" || feed.Sections[0].Pages != 2 || order != "match" || keyword != "anime" || offset != "20" {
		t.Fatal("wrong anime search", feed, order, keyword, offset)
	}
	var details discoveryDetail
	read("/api/discovery/anime/42", &details)
	if len(details.Seasons) != 1 || details.Seasons[0].Episodes != 12 || len(details.People) != 1 ||
		details.People[0].Photo != "https://lain.bgm.tv/pic/crt/l/voice.jpg" || details.Overview != "简介" || details.Source != "bangumi" {
		t.Fatal("wrong usable anime details", details)
	}
	if err := a.store.update(func(st *State) error {
		st.MediaSubscriptions = []mediaSubscription{{Key: "tmdb-existing", discoveryMedia: discoveryMedia{ID: 42, Kind: "tv", Title: "同号TMDB剧集"},
			mediaSubscriptionOptions: mediaSubscriptionOptions{Season: 1, StartEpisode: 1}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"source": "bangumi", "id": 42, "kind": "tv", "season": 1}
	w := request(t, h, "POST", "/api/media-subscriptions", payload, cookie)
	if w.Code != 201 {
		t.Fatal("Bangumi and TMDB IDs were not independent", w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", "/api/media-subscriptions", payload, cookie); w.Code != 409 {
		t.Fatal("duplicate Bangumi subscription was accepted", w.Code)
	}
	payload["id"] = 43
	if w := request(t, h, "POST", "/api/media-subscriptions", payload, cookie); w.Code != 400 {
		t.Fatal("anime film subscribed as tv", w.Code)
	}
	payload["kind"] = "movie"
	if w := request(t, h, "POST", "/api/media-subscriptions", payload, cookie); w.Code != 201 {
		t.Fatal("anime film subscription rejected", w.Code, w.Body.String())
	}
	var calendar struct{ Items []calendarEpisode }
	read("/api/media-calendar?month=2026-11", &calendar)
	if episodePages != 2 || len(calendar.Items) != 2 || calendar.Items[0].Bangumi != 42 || calendar.Items[0].TMDB != 0 ||
		calendar.Items[1].Episode != 2 {
		t.Fatal("invalid Bangumi calendar paging or identity", calendar, episodePages)
	}
}
