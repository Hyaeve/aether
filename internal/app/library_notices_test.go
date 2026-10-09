package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLibraryNoticeAggregation(t *testing.T) {
	st := State{}
	season, other := 1, 2
	now := time.Now()
	for _, episode := range []int{1, 3, 4, 5, 7} {
		if !mergeLibraryNotice(&st, LibraryNotice{ID: fmt.Sprint(episode), Name: "Episode", MediaType: "episode", Series: "Show", SeriesID: "show", Season: &season, Episodes: []int{episode}, ItemIDs: []string{fmt.Sprint(episode)}, Time: now}) {
			t.Fatal("new episode ignored")
		}
	}
	if len(st.LibraryNotices) != 1 || len(st.LibraryNotices[0].Episodes) != 5 {
		t.Fatal(st.LibraryNotices)
	}
	duplicate := LibraryNotice{Name: "Episode", MediaType: "episode", Series: "Show", SeriesID: "show", Season: &season, Episodes: []int{7}, ItemIDs: []string{"7"}, Time: now}
	if got := libraryNoticeDescription(st.LibraryNotices[0]); got != "电视剧 · Show · 第1季 · 1、3-5、7集" {
		t.Fatal(got)
	}
	if mergeLibraryNotice(&st, duplicate) {
		t.Fatal("duplicate accepted")
	}
	duplicate.ItemIDs = []string{"season2"}
	duplicate.Season = &other
	mergeLibraryNotice(&st, duplicate)
	duplicate.ItemIDs = []string{"server2"}
	duplicate.ServerID = "other"
	mergeLibraryNotice(&st, duplicate)
	duplicate.ItemIDs = []string{"late"}
	duplicate.Time = now.Add(11 * time.Minute)
	mergeLibraryNotice(&st, duplicate)
	if len(st.LibraryNotices) != 4 {
		t.Fatal("merged separate season/server/window")
	}
}

func TestEmbyEpisodeWebhookMetadata(t *testing.T) {
	a := testApp(t)
	a.store.update(func(st *State) error {
		st.Plugins = map[string]PluginConfig{"emby": {Enabled: true, Token: "test"}}
		return nil
	})
	h := a.Handler(t.TempDir())
	body := map[string]any{"Event": "library.new", "Item": map[string]any{"Id": "ep", "Type": "Episode", "Name": "Pilot", "SeriesName": "Show", "SeriesId": "series", "ParentIndexNumber": 1, "IndexNumber": 2, "IndexNumberEnd": 5}}
	send := func() *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/api/emby/webhook?token=test", bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body["Library"] = map[string]string{"Name": "剧集库"}
	body["Server"] = map[string]string{"Id": "home", "Name": "家庭影院"}
	w := send()
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	n := a.store.snapshot().LibraryNotices[0]
	if n.LibraryName != "剧集库" || n.ServerName != "家庭影院" {
		t.Fatal(n)
	}
	if n.Series != "Show" || n.Season == nil || *n.Season != 1 || len(n.Episodes) != 4 || n.Episodes[3] != 5 {
		t.Fatal(n)
	}
	body["Item"].(map[string]any)["IndexNumberEnd"] = 1000000
	if w = send(); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestEmbyOtherEventsRemainDistinct(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.Plugins = map[string]PluginConfig{"emby": {Enabled: true, Token: "events"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	for _, event := range []string{"library.new", "playback.start", "playback.stop", "system.notificationtest"} {
		data, _ := json.Marshal(map[string]any{"Event": event, "Title": "测试通知", "Item": map[string]any{"Id": "same", "Name": "电影"}})
		r := httptest.NewRequest("POST", "/api/emby/webhook?token=events", bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	notices := a.store.snapshot().LibraryNotices
	if len(notices) != 4 || notices[1].Event != "playback.start" {
		t.Fatal(notices)
	}
}
