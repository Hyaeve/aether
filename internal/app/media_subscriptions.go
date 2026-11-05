package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var subscriptionEpisode = regexp.MustCompile(`^S[0-9]{2,3}E[0-9]{2,5}$`)

type mediaSubscriptionOptions struct {
	MissingEpisodes []string `json:"missingEpisodes,omitempty"`
	Season          int      `json:"season"`
	Keyword         string   `json:"keyword"`
	TotalEpisodes   int      `json:"totalEpisodes"`
	StartEpisode    int      `json:"startEpisode"`
	Quality         string   `json:"quality"`
	Resolution      string   `json:"resolution"`
	Effect          string   `json:"effect"`
	SourceIDs       []string `json:"sourceIds"`
	TargetID        string   `json:"targetId"`
	SearchHours     *int     `json:"searchHours"`
	Include         string   `json:"include"`
	Exclude         string   `json:"exclude"`
	MediaCategory   string   `json:"mediaCategory"`
	CustomWords     string   `json:"customWords"`
}
type mediaSubscription struct {
	discoveryMedia
	mediaSubscriptionOptions
	Key     string    `json:"key"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

func validDiscoveryImage(value string) bool {
	if value == "" {
		return true
	}
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Host == "image.tmdb.org" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && strings.HasPrefix(u.Path, "/t/p/") && !strings.Contains(u.Path, "..")
}
func validateSubscriptionOptions(c mediaSubscriptionOptions) error {
	if len(c.MissingEpisodes) > 100000 {
		return errors.New("缺集范围过大")
	}
	seen := map[string]bool{}
	for _, value := range c.MissingEpisodes {
		if !subscriptionEpisode.MatchString(value) || seen[value] {
			return errors.New("缺集范围须为不重复的S01E01格式")
		}
		seen[value] = true
	}
	if c.Season < 0 || c.Season > 1000 || c.TotalEpisodes < 0 || c.TotalEpisodes > 100000 || c.StartEpisode < 1 || c.StartEpisode > 100000 {
		return errors.New("季号、总集数或开始集数超出范围")
	}
	if c.TotalEpisodes > 0 && c.StartEpisode > c.TotalEpisodes {
		return errors.New("开始集数不能大于总集数")
	}
	if c.SearchHours != nil && (*c.SearchHours < 1 || *c.SearchHours > 8760) {
		return errors.New("自定义搜索周期须为1至8760小时的整数")
	}
	if len(c.SourceIDs) > 0 {
		return errors.New("资源来源尚未开放，暂不能指定订阅站点")
	}
	for _, f := range []struct {
		value string
		limit int
	}{{c.Keyword, 300}, {c.TargetID, 128}, {c.Quality, 64}, {c.Resolution, 32}, {c.Effect, 64}, {c.Include, 1024}, {c.Exclude, 1024}, {c.MediaCategory, 255}, {c.CustomWords, 4096}} {
		if len(f.value) > f.limit || strings.ContainsRune(f.value, 0) {
			return errors.New("订阅字段过长或包含无效字符")
		}
	}
	return nil
}
func validateMediaSubscriptions(values []mediaSubscription) error {
	if len(values) > 500 {
		return errors.New("媒体订阅最多500项")
	}
	keys, media := map[string]bool{}, map[string]bool{}
	for _, v := range values {
		if !notificationID.MatchString(v.Key) || keys[v.Key] || v.ID < 1 || v.ID > 1_000_000_000 || (v.Kind != "tv" && v.Kind != "movie") || v.Title == "" || len(v.Title) > 500 || len(v.Overview) > 20000 || !validDiscoveryImage(v.Poster) || !validDiscoveryImage(v.Backdrop) {
			return errors.New("媒体订阅信息无效")
		}
		if v.Kind == "movie" && v.Season != 0 {
			return errors.New("电影订阅不能设置季号")
		}
		if err := validateSubscriptionOptions(v.mediaSubscriptionOptions); err != nil {
			return err
		}
		key := fmt.Sprintf("%s:%d:%d", v.Kind, v.ID, v.Season)
		if media[key] {
			return errors.New("该作品及季号已订阅")
		}
		media[key], keys[v.Key] = true, true
	}
	return nil
}
func subscriptionTargets(st State) []map[string]any {
	values := []map[string]any{}
	for _, target := range st.Notifications.Targets {
		for _, s := range st.Storages {
			if s.ID == target.StorageID && shareDestinationStorage(s) && (target.shares() || target.Provider == "115" && target.ED2KOffline) {
				values = append(values, map[string]any{"id": target.ID, "provider": s.Type, "name": s.Name, "path": target.Path, "shareTransfer": target.shares(), "offline": target.Provider == "115" && target.ED2KOffline})
			}
		}
	}
	return values
}
func subscriptionTargetValid(st State, key string) bool {
	if key == "" {
		return true
	}
	for _, target := range subscriptionTargets(st) {
		if target["id"] == key {
			return true
		}
	}
	return false
}
func (a *App) mediaSubscriptionsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		st := a.store.snapshotWithLogLimit(0)
		cards := st.MediaSubscriptions
		if cards == nil {
			cards = []mediaSubscription{}
		}
		jsonResponse(w, 200, map[string]any{"items": cards, "targets": subscriptionTargets(st), "sources": []any{}, "sourceReady": false})
		return
	}
	key := r.PathValue("id")
	if r.Method == http.MethodDelete {
		err := a.store.update(func(st *State) error {
			for i, c := range st.MediaSubscriptions {
				if c.Key == key {
					st.MediaSubscriptions = append(st.MediaSubscriptions[:i], st.MediaSubscriptions[i+1:]...)
					return nil
				}
			}
			return errors.New("订阅不存在")
		})
		if err != nil {
			fail(w, 404, err)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	var input struct {
		ID   int    `json:"id"`
		Kind string `json:"kind"`
		mediaSubscriptionOptions
	}
	input.StartEpisode = 1
	if !decode(w, r, &input) {
		return
	}
	if r.Method == http.MethodPost && input.Kind == "movie" {
		input.Season = 0
	}
	if err := validateSubscriptionOptions(input.mediaSubscriptionOptions); err != nil {
		fail(w, 400, err)
		return
	}
	var card mediaSubscription
	if r.Method == http.MethodPost {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		detail, err := a.discoveryMediaDetail(ctx, input.Kind, input.ID)
		if err != nil {
			fail(w, 400, err)
			return
		}
		card = mediaSubscription{discoveryMedia: detail.discoveryMedia, Key: id(), Created: time.Now()}
	}
	err := a.store.update(func(st *State) error {
		if !subscriptionTargetValid(*st, input.TargetID) {
			return errors.New("转存目录已移除、停用或不支持转存，请重新选择")
		}
		index := -1
		if r.Method == http.MethodPut {
			for i, c := range st.MediaSubscriptions {
				if c.Key == key {
					index = i
					card = c
					break
				}
			}
			if index < 0 {
				return errors.New("订阅不存在")
			}
			if card.Kind == "movie" {
				input.Season = 0
			}
		}
		card.mediaSubscriptionOptions = input.mediaSubscriptionOptions
		card.Updated = time.Now()
		if index < 0 {
			st.MediaSubscriptions = append(st.MediaSubscriptions, card)
		} else {
			st.MediaSubscriptions[index] = card
		}
		return validateMediaSubscriptions(st.MediaSubscriptions)
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	code := 200
	if r.Method == http.MethodPost {
		code = 201
	}
	jsonResponse(w, code, card)
}
