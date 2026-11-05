package app

import (
	"aether/internal/linkcore/upstream"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func mediaFixture(t *testing.T, hooks ...func(http.ResponseWriter, *http.Request) bool) (*App, http.Handler, *http.Cookie, *atomic.Int32) {
	t.Helper()
	a := testApp(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, hook := range hooks {
			if hook(w, r) {
				return
			}
		}
		requests.Add(1)
		p := r.URL.Path
		q := r.URL.Query()
		if strings.HasPrefix(p, "/3/") {
			if r.Header.Get("Authorization") != "Bearer tmdb-test" {
				t.Error("missing TMDB authorization")
			}
			if p == "/3/tv/42" {
				jsonResponse(w, 200, map[string]any{"id": 42, "name": "剧集示例", "seasons": []any{map[string]any{"season_number": 1, "episode_count": 3}}})
				return
			}
			if p == "/3/tv/42/season/1" {
				jsonResponse(w, 200, map[string]any{"episodes": []any{map[string]any{"episode_number": 1, "season_number": 1, "air_date": "2026-01-01"}, map[string]any{"episode_number": 2, "season_number": 1, "air_date": "2026-01-02"}, map[string]any{"episode_number": 3, "season_number": 1, "air_date": "2099-01-01"}}})
				return
			}
		}
		if strings.HasPrefix(p, "/api/") {
			if r.Header.Get("Authorization") != "Bearer media-key" {
				t.Error("ABS credentials not reused")
			}
		} else if r.Header.Get("X-Emby-Token") != "media-key" {
			t.Error("Emby credentials not reused", p)
		}
		switch p {
		case "/Library/VirtualFolders":
			jsonResponse(w, 200, []any{map[string]any{"Name": "电影", "ItemId": "lib", "CollectionType": "movies"}, map[string]any{"Name": "私藏", "ItemId": "excluded", "CollectionType": "tvshows"}})
		case "/Items", "/Users/u1/Items":
			var items []any
			if q.Get("IncludeItemTypes") == "Series" {
				items = []any{map[string]any{"Id": "show", "Name": "剧集示例", "Type": "Series", "ProviderIds": map[string]string{"Tmdb": "42"}}}
			} else {
				items = []any{map[string]any{"Id": "film", "Name": "影片", "Type": "Movie", "RunTimeTicks": 1200000000, "DateCreated": "2026-01-01T00:00:00Z", "UserData": map[string]any{"PlayCount": 4}, "MediaSources": []any{map[string]any{"Size": 1024, "MediaStreams": []any{map[string]any{"Type": "Video", "Height": 2160, "Codec": "hevc"}}}}}}
			}
			jsonResponse(w, 200, map[string]any{"Items": items, "TotalRecordCount": len(items)})
		case "/Shows/show/Episodes":
			jsonResponse(w, 200, map[string]any{"Items": []any{map[string]any{"Id": "e1", "Name": "第一集", "Type": "Episode", "ParentIndexNumber": 1, "IndexNumber": 1}}, "TotalRecordCount": 1})
		case "/Users":
			jsonResponse(w, 200, []any{map[string]any{"Id": "u1"}})
		case "/Sessions":
			jsonResponse(w, 200, []any{map[string]any{"Id": "s1", "UserName": "用户", "NowPlayingItem": map[string]any{"Id": "film", "Name": "影片", "Type": "Movie", "RunTimeTicks": 1200000000}, "PlayState": map[string]any{"PositionTicks": 600000000, "IsPaused": false}}})
		case "/Items/film/Images/Primary":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("png-image-test"))
		case "/api/libraries":
			jsonResponse(w, 200, map[string]any{"libraries": []any{map[string]any{"id": "books", "name": "有声书", "mediaType": "book"}}})
		case "/api/libraries/books/stats":
			jsonResponse(w, 200, map[string]any{"totalItems": 3, "numAudioTrack": 9, "totalDuration": 7200, "totalSize": 1073741824})
		case "/api/libraries/books/items", "/api/items/book":
			item := map[string]any{"id": "book", "libraryId": "books", "mediaType": "book", "addedAt": 12345, "media": map[string]any{"duration": 3600, "metadata": map[string]any{"title": "有声书", "authorName": "作者", "narratorName": "播讲"}}}
			if p == "/api/items/book" {
				jsonResponse(w, 200, item)
			} else {
				jsonResponse(w, 200, map[string]any{"results": []any{item}})
			}
		case "/api/users":
			jsonResponse(w, 200, map[string]any{"users": []any{map[string]any{"id": "u1"}}})
		case "/api/users/u1/listening-stats":
			jsonResponse(w, 200, map[string]any{"items": map[string]any{"book": map[string]any{"timeListening": 7200}}})
		case "/api/users/online":
			jsonResponse(w, 200, map[string]any{"usersOnline": []any{map[string]any{"id": "u1", "username": "读者"}}, "openSessions": []any{map[string]any{"id": "s1", "userId": "u1", "libraryId": "books", "libraryItemId": "book", "currentTime": 1800, "duration": 3600, "mediaMetadata": map[string]any{"title": "有声书"}}}})
		default:
			t.Errorf("unexpected upstream %s %s", r.Method, p)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	err := a.store.update(func(st *State) error {
		st.Links = []MediaLink{{ID: "emby", Name: "影视", Type: "emby", Address: server.URL, Port: 15990, APIKey: "media-key", Mode: "always", Enabled: false}, {ID: "abs", Name: "听书", Type: "audiobookshelf", Address: server.URL, Port: 15991, APIKey: "media-key", Mode: "always", Enabled: false}}
		if st.Plugins == nil {
			st.Plugins = map[string]PluginConfig{}
		}
		st.Plugins["tmdb"] = PluginConfig{Enabled: true, APIURL: server.URL, APIKey: "tmdb-test", RequestInterval: 1, Language: "zh-CN"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	return a, h, setupTest(t, a, h), &requests
}

func TestMyMediaIncompleteRevokedAndCancelledScansKeepLastResult(t *testing.T) {
	for _, kind := range []string{"incomplete", "revoked", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			entered, release := make(chan struct{}, 1), make(chan struct{})
			defer close(release)
			a, h, cookie, _ := mediaFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path != "/Items" {
					return false
				}
				if r.URL.Query().Get("ParentId") != "lib" {
					t.Error("unselected library read")
				}
				if kind == "incomplete" {
					if r.URL.Query().Get("StartIndex") == "0" {
						jsonResponse(w, 200, map[string]any{"Items": []any{map[string]any{"Id": "one", "Name": "one"}}, "TotalRecordCount": 2})
					} else {
						jsonResponse(w, 200, map[string]any{"Items": []any{}, "TotalRecordCount": 2})
					}
					return true
				}
				select {
				case entered <- struct{}{}:
				default:
				}
				select {
				case <-release:
				case <-r.Context().Done():
					return true
				}
				jsonResponse(w, 200, map[string]any{"Items": []any{map[string]any{"Id": "one", "Name": "one"}}, "TotalRecordCount": 1})
				return true
			})
			setMediaLibraries(t, a)
			if err := a.store.update(func(st *State) error {
				st.MediaQuality = map[string]mediaQualityResult{"emby": {Items: 77}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if w := request(t, h, "POST", "/api/my-media/emby/scan", map[string]any{"kind": "quality"}, cookie); w.Code != 202 {
				t.Fatal(w.Code)
			}
			if kind != "incomplete" {
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("scan did not start")
				}
				if w := request(t, h, "POST", "/api/my-media/emby/scan", map[string]any{"kind": "quality"}, cookie); w.Code != 409 {
					t.Fatal("duplicate scan accepted")
				}
				if kind == "revoked" {
					if err := a.store.update(func(st *State) error { st.MediaDisplay.Libraries["emby"] = nil; return nil }); err != nil {
						t.Fatal(err)
					}
					release <- struct{}{}
				} else {
					request(t, h, "POST", "/api/my-media/emby/scan", map[string]any{"kind": "stop"}, cookie)
				}
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				a.mediaMu.Lock()
				running, message := a.mediaScans["emby"].Running, a.mediaScans["emby"].Message
				a.mediaMu.Unlock()
				if !running {
					if message == "扫描完成" || kind == "cancelled" && message != "扫描已停止" {
						t.Fatal("failure hidden", message)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("scan not stopped")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if a.store.snapshot().MediaQuality["emby"].Items != 77 {
				t.Fatal("failed scan replaced prior results")
			}
		})
	}
}
func TestMyMediaConfigPersistsOrderDisabledAndCanonicalLibraries(t *testing.T) {
	a, h, cookie, _ := mediaFixture(t)
	w := request(t, h, "GET", "/api/my-media/config", nil, cookie)
	if w.Code != 200 || strings.Contains(w.Body.String(), "media-key") {
		t.Fatal(w.Code, w.Body.String())
	}
	var cfg struct {
		Items []struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		}
		Selected string
	}
	json.Unmarshal(w.Body.Bytes(), &cfg)
	if len(cfg.Items) != 2 || cfg.Items[0].ID != "emby" || cfg.Items[0].Enabled {
		t.Fatal("disabled card or ordering lost", cfg)
	}
	for _, body := range []any{map[string]any{"selected": "abs"}, map[string]any{"missingSelected": "emby"}, map[string]any{"linkId": "emby", "libraries": []any{map[string]any{"id": "lib", "name": "forged"}}}} {
		w = request(t, h, "PUT", "/api/my-media/config", body, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	stored := a.store.snapshot().MediaDisplay
	if stored.Selected != "abs" || stored.MissingSelected != "emby" || stored.Libraries["emby"][0].Name != "电影" {
		t.Fatal("selection not canonical", stored)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || reopened.snapshot().MediaDisplay.Selected != "abs" || len(reopened.snapshot().MediaDisplay.Libraries["emby"]) != 1 {
		t.Fatal("media selection not persisted", err)
	}
	for _, body := range []any{map[string]any{"selected": "missing"}, map[string]any{"missingSelected": "abs"}, map[string]any{"linkId": "emby", "libraries": []any{map[string]any{"id": "unknown"}}}} {
		if w = request(t, h, "PUT", "/api/my-media/config", body, cookie); w.Code < 400 {
			t.Fatal("invalid media config accepted")
		}
	}
	if w = request(t, h, "GET", "/api/my-media/config", nil, nil); w.Code != 401 {
		t.Fatal("media config unauthenticated")
	}
	if err := a.store.update(func(st *State) error {
		next := st.Links[0]
		next.ID = "second"
		next.Port = 15992
		st.Links = append(st.Links[1:], next)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w = request(t, h, "GET", "/api/my-media/config", nil, cookie)
	var restored struct {
		Selected string `json:"selected"`
		Missing  string `json:"missingSelected"`
	}
	json.Unmarshal(w.Body.Bytes(), &restored)
	if w.Code != 200 || restored.Selected != "abs" || restored.Missing != "second" {
		t.Fatal("deleted Emby selection did not fall back", w.Body.String())
	}
}
func setMediaLibraries(t *testing.T, a *App) {
	t.Helper()
	if err := a.store.update(func(st *State) error {
		st.MediaDisplay.Libraries = map[string][]upstream.Library{"emby": {{ID: "lib", Name: "电影"}}, "abs": {{ID: "books", Name: "有声书"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestMyMediaDashboardSessionsRankImagesAndScope(t *testing.T) {
	a, h, cookie, calls := mediaFixture(t)
	setMediaLibraries(t, a)
	for _, key := range []string{"emby", "abs"} {
		w := request(t, h, "GET", "/api/my-media/"+key+"/dashboard", nil, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var data mediaDashboardData
		json.Unmarshal(w.Body.Bytes(), &data)
		if len(data.Recent) != 1 || len(data.Stats) < 3 || len(data.Warnings) > 0 {
			t.Fatal(data)
		}
		n := calls.Load()
		if request(t, h, "GET", "/api/my-media/"+key+"/dashboard", nil, cookie).Code != 200 || calls.Load() != n {
			t.Fatal("dashboard cache missed")
		}
		w = request(t, h, "GET", "/api/my-media/"+key+"/sessions", nil, cookie)
		var sessions []mediaPlaying
		json.Unmarshal(w.Body.Bytes(), &sessions)
		if w.Code != 200 || len(sessions) != 1 || sessions[0].Position/sessions[0].Duration != .5 {
			t.Fatal(w.Code, w.Body.String())
		}
		w = request(t, h, "GET", "/api/my-media/"+key+"/ranking", nil, cookie)
		var ranks struct {
			Items []mediaRank
			Unit  string
		}
		json.Unmarshal(w.Body.Bytes(), &ranks)
		if w.Code != 200 || len(ranks.Items) != 1 {
			t.Fatal(w.Code, w.Body.String())
		}
		if key == "emby" && ranks.Items[0].Value != 4 || key == "abs" && (ranks.Items[0].Value != 2 || ranks.Unit != "h") {
			t.Fatal(ranks)
		}
		if strings.Contains(w.Body.String(), "media-key") {
			t.Fatal("credentials leaked")
		}
	}
	st := a.store.snapshot()
	l := st.Links[0]
	stamp := mediaDisplayStamp(st, l)
	w := request(t, h, "GET", mediaImageURL(l, stamp, "film", "Primary"), nil, cookie)
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := a.store.update(func(st *State) error { st.Links[0].APIKey = "changed"; return nil }); err != nil {
		t.Fatal(err)
	}
	if w = request(t, h, "GET", mediaImageURL(l, stamp, "film", "Primary"), nil, cookie); w.Code != 409 {
		t.Fatal("old image revision accepted")
	}
	if w = request(t, h, "GET", "/api/my-media/emby/arbitrary", nil, cookie); w.Code != 404 {
		t.Fatal("unknown media action accepted")
	}
}
func TestMyMediaMissingAndQualityScansPersistWithoutRemoteWrites(t *testing.T) {
	a, h, cookie, _ := mediaFixture(t)
	setMediaLibraries(t, a)
	for _, kind := range []string{"missing", "quality"} {
		w := request(t, h, "POST", "/api/my-media/emby/scan", map[string]any{"kind": kind}, cookie)
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			w = request(t, h, "GET", "/api/my-media/emby/scan", nil, cookie)
			var status struct{ Progress mediaScan }
			json.Unmarshal(w.Body.Bytes(), &status)
			if !status.Progress.Running {
				if status.Progress.Message != "扫描完成" {
					t.Fatal(status.Progress.Message)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("scan did not finish")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	st := a.store.snapshot()
	missing := st.MediaMissing["emby"]
	if missing.Scanned != 1 || missing.Unknown != 0 || len(missing.Items) != 1 || strings.Join(missing.Items[0].Missing, ",") != "S01E02" {
		t.Fatal("future episodes counted or local missed", missing)
	}
	q := st.MediaQuality["emby"]
	if q.Items != 1 || q.Size != 1024 || q.Resolutions["4K"] != 1 || q.Codecs["HEVC"] != 1 {
		t.Fatal(q)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if reopened.snapshot().MediaMissing["emby"].Scanned != 1 {
		t.Fatal("missing results did not survive runtime reopen")
	}
	if w := request(t, h, "POST", "/api/my-media/abs/scan", map[string]any{"kind": "missing"}, cookie); w.Code != 400 {
		t.Fatal("ABS missing scan accepted")
	}
	st.MediaDisplay.Libraries["emby"] = nil
	l, _ := mediaLink(st, "emby")
	if mediaScopeCurrent(st, l, missing.Stamp) {
		t.Fatal("changed display range accepted old results")
	}
}
func TestMediaCalendarOnlySubscribedAiringAndReleaseNotes(t *testing.T) {
	a, h, cookie, _ := mediaFixture(t)
	if err := a.store.update(func(st *State) error {
		st.MediaSubscriptions = []mediaSubscription{{discoveryMedia: discoveryMedia{ID: 42, Kind: "tv", Title: "剧集示例"}, mediaSubscriptionOptions: mediaSubscriptionOptions{Season: 1, StartEpisode: 1}, Key: "subscription"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w := request(t, h, "GET", "/api/media-calendar?month=2026-01", nil, cookie)
	var got struct {
		Items    []calendarEpisode
		Warnings []string
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || len(got.Items) != 2 || got.Items[1].Episode != 2 || len(got.Warnings) > 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(t, h, "GET", "/api/media-calendar?month=invalid", nil, cookie); w.Code != 400 {
		t.Fatal("invalid calendar month accepted")
	}
	if w = request(t, h, "GET", "/api/version/notes", nil, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), Version) || !strings.Contains(w.Body.String(), "body") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(t, h, "GET", "/api/version/notes", nil, nil); w.Code != 401 {
		t.Fatal("release notes not protected")
	}
}
func TestOrganizeProcessConcurrencyDefaultsAndValidation(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	in := organizeTaskDefinition{Name: "并发", StorageID: s.ID, Source: "/in", TargetStorageID: s.ID, Target: "/out"}
	w := request(t, h, "POST", "/api/organize-tasks", in, cookie)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var got organizeTaskDefinition
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.ProcessConcurrency != 1 {
		t.Fatal("default concurrency not one")
	}
	got.ProcessConcurrency = 3
	if w = request(t, h, "PUT", "/api/organize-tasks/"+got.ID, got, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || reopened.snapshot().OrganizeTasks[0].ProcessConcurrency != 3 {
		t.Fatal(err)
	}
	for _, n := range []int{-1, 4} {
		got.ProcessConcurrency = n
		if w = request(t, h, "PUT", "/api/organize-tasks/"+got.ID, got, cookie); w.Code != 400 {
			t.Fatal("invalid concurrency accepted", w.Code)
		}
	}
	if len(a.running) != 0 {
		t.Fatal("configuration started organizing")
	}
}
