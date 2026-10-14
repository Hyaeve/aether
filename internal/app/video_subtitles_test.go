package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestVideoSubtitleLimitsAndWindowNormalization(t *testing.T) {
	out := &subtitleOutput{limit: 10}
	if _, err := io.Copy(out, bytes.NewReader(bytes.Repeat([]byte("x"), 100))); err == nil || out.buffer.Len() > 10 {
		t.Fatal("stdout limit bypassed", err, out.buffer.Len())
	}
	data := []byte("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nold\n\n00:04:59.000 --> 00:05:02.000\noverlap\n\n00:05:01.200 --> 00:05:03.400\ncurrent\n\n00:10:00.000 --> 00:10:02.000\nlater\n\n")
	text := string(subtitleWindowVTT(data, 300))
	if strings.Contains(text, "old") || strings.Contains(text, "later") || !strings.Contains(text, "00:00:00.000 --> 00:00:02.000") || !strings.Contains(text, "00:00:01.200 --> 00:00:03.400") {
		t.Fatal(text)
	}
}

func TestVideoSubtitlesAuthenticationValidationAndTextExtraction(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	if w := request(t, h, "GET", "/api/files/video-subtitles", nil, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	query := url.Values{"storage": {s.ID}, "parent": {"/"}, "id": {"/video.mkv"}}
	for _, extra := range []string{"&track=-1", "&start=1", "&start=99999999999999999", "&id=/other", "&url=http://example.com"} {
		w := request(t, h, "GET", "/api/files/video-subtitles?"+query.Encode()+extra, nil, cookie)
		if w.Code != 400 {
			t.Fatal(extra, w.Code, w.Body.String())
		}
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe unavailable")
	}
	writeTest(t, filepath.Join(s.Config["root"], "subtitles.srt"), "1\n00:00:00,200 --> 00:00:01,800\nHello embedded subtitles\n\n2\n00:05:01,200 --> 00:05:01,800\nSecond subtitle window\n")
	output, err := exec.Command(ffmpeg, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=64x64:r=1/30:d=302", "-i", filepath.Join(s.Config["root"], "subtitles.srt"), "-c:v", "libx264", "-c:s", "srt", "-metadata:s:s:0", "language=zho", "-metadata:s:s:0", "title=中文", filepath.Join(s.Config["root"], "video.mkv")).CombinedOutput()
	if err != nil {
		t.Fatal(err, string(output))
	}
	w := request(t, h, "GET", "/api/files/video-subtitles?"+query.Encode(), nil, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var data struct {
		Tracks []videoSubtitleTrack `json:"tracks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil || len(data.Tracks) != 1 || !data.Tracks[0].Supported || data.Tracks[0].Title != "中文" {
		t.Fatal(data, err)
	}
	query.Set("track", "1")
	query.Set("start", "0")
	w = request(t, h, "GET", "/api/files/video-subtitles?"+query.Encode(), nil, cookie)
	if w.Code != 200 || !strings.HasPrefix(w.Body.String(), "WEBVTT") || !strings.Contains(w.Body.String(), "Hello embedded subtitles") || w.Header().Get("Content-Type") != "text/vtt; charset=utf-8" {
		t.Fatal(w.Code, w.Body.String())
	}
	query.Set("start", "300")
	w = request(t, h, "GET", "/api/files/video-subtitles?"+query.Encode(), nil, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Second subtitle window") || !strings.Contains(w.Body.String(), "00:00:01.200") || strings.Contains(w.Body.String(), "Hello embedded subtitles") {
		t.Fatal("window timestamp", w.Code, w.Body.String())
	}
	query.Set("track", "0")
	w = request(t, h, "GET", "/api/files/video-subtitles?"+query.Encode(), nil, cookie)
	if w.Code != 400 {
		t.Fatal("video stream admitted", w.Code, w.Body.String())
	}
	var external atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { external.Add(1) }))
	defer remote.Close()
	writeTest(t, filepath.Join(s.Config["root"], "playlist.mkv"), "#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:10,\n"+remote.URL+"/private.ts\n#EXT-X-ENDLIST\n")
	query.Del("track")
	query.Del("start")
	query.Set("id", "/playlist.mkv")
	a.cache.clear()
	w = request(t, h, "GET", "/api/files/video-subtitles?"+query.Encode(), nil, cookie)
	if w.Code != 400 || external.Load() != 0 {
		t.Fatal("playlist contacted external source", w.Code, external.Load())
	}
}
