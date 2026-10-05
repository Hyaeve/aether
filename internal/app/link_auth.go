package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"aether/internal/linkcore/upstream"
)

// Before using configured service credentials or a cached direct link, ask the
// upstream to authorize this player's own media request. Never inject our API key.
func authorizeLinkPlayer(provider upstream.Provider, next http.Handler) http.Handler {
	client := &http.Client{Timeout: 15 * time.Second, Transport: provider.Transport(), CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ref, media := provider.Match(r)
		if !media {
			next.ServeHTTP(w, r)
			return
		}
		target := *provider.BaseURL()
		q := url.Values{}
		for _, key := range []string{"api_key", "token", "X-Emby-Token", "userId", "UserId", "MediaSourceId"} {
			if value := r.URL.Query().Get(key); value != "" {
				q.Set(key, value)
			}
		}
		if provider.Type() == "audiobookshelf" {
			if ref.ItemID == "" {
				target.Path = strings.TrimRight(target.Path, "/") + "/api/session/" + url.PathEscape(ref.SessionID)
			} else {
				target.Path = strings.TrimRight(target.Path, "/") + "/api/items/" + url.PathEscape(ref.ItemID)
			}
		} else {
			base := strings.TrimRight(target.Path, "/")
			if provider.Type() == "fnos" && !strings.HasSuffix(base, "/emby") {
				base += "/emby"
			}
			target.Path = base + "/Items/" + url.PathEscape(ref.ItemID) + "/PlaybackInfo"
		}
		target.RawQuery = q.Encode()
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", target.String(), nil)
		for _, name := range []string{"Authorization", "X-Emby-Authorization", "X-Emby-Token", "Cookie", "User-Agent"} {
			req.Header.Set(name, r.Header.Get(name))
		}
		if provider.Type() == "audiobookshelf" && req.Header.Get("Authorization") == "" && q.Get("token") != "" {
			req.Header.Set("Authorization", "Bearer "+q.Get("token"))
		}
		response, err := client.Do(req)
		if err != nil {
			http.Error(w, "Media authorization unavailable", 502)
			return
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			status := response.StatusCode
			if status != 401 && status != 403 && status != 404 {
				status = 502
			}
			http.Error(w, "Media authorization denied", status)
			return
		}
		var payload map[string]json.RawMessage
		if json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload) != nil || len(payload) == 0 || (provider.Type() != "audiobookshelf" && (payload["ErrorCode"] != nil && string(payload["ErrorCode"]) != `""` && string(payload["ErrorCode"]) != "null")) {
			http.Error(w, "Media authorization response invalid", 403)
			return
		}
		if provider.Type() != "audiobookshelf" {
			var sources []json.RawMessage
			if json.Unmarshal(payload["MediaSources"], &sources) != nil || len(sources) == 0 {
				http.Error(w, "No authorized media source", 403)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
