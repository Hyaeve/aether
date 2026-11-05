package proxy

import (
	"aether/internal/linkcore/config"
	"aether/internal/linkcore/upstream"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbyHLSPlaybackInfoUsesPlaylistContainer(t *testing.T) {
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"MediaSources": []any{map[string]any{"Id": "source", "Path": "http://aether.test/139-share/code/Show.mkv", "Container": "hls", "Protocol": "Http", "SupportsDirectPlay": true, "DirectStreamUrl": "/Videos/item/stream.hls"}}})
	}))
	defer media.Close()
	provider, err := upstream.New(config.Upstream{Name: "test", Type: config.UpstreamEmby, BaseURL: media.URL, APIKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/Items/item/PlaybackInfo", nil)
	response, err := http.Get(media.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	rewriter, ok := provider.(interface {
		RewriteResponse(string, *http.Response) (int, error)
	})
	if !ok {
		t.Fatal("rewriter missing")
	}
	if n, err := rewriter.RewriteResponse(req.URL.Path, response); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var body struct {
		Sources []map[string]any `json:"MediaSources"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	source := body.Sources[0]
	if source["Container"] != "m3u8" || source["MimeType"] != "application/vnd.apple.mpegurl" || !strings.Contains(source["DirectStreamUrl"].(string), "/stream.m3u8?") || source["SupportsDirectStream"] != true || source["SupportsDirectPlay"] != false {
		t.Fatal(source)
	}
}
