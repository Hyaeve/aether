package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"time"
)

type videoProbeStream struct {
	Index         int    `json:"index"`
	Type          string `json:"codec_type"`
	Codec         string `json:"codec_name"`
	Profile       string `json:"profile"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	FrameRate     string `json:"avg_frame_rate"`
	SampleRate    string `json:"sample_rate"`
	Channels      int    `json:"channels"`
	ChannelLayout string `json:"channel_layout"`
	BitRate       string `json:"bit_rate"`
	Transfer      string `json:"color_transfer"`
	Tags          struct {
		Language string `json:"language"`
		Title    string `json:"title"`
	} `json:"tags"`
}

type videoProbeInfo struct {
	Streams []videoProbeStream `json:"streams"`
	Format  struct {
		Name     string `json:"format_name"`
		Duration string `json:"duration"`
		BitRate  string `json:"bit_rate"`
	} `json:"format"`
}

// Share the subtitle reader's authorization, byte budget and two-slot gate.
// Return technical fields only, never source URLs or arbitrary format tags.
func (a *App) videoInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	for key, values := range r.URL.Query() {
		if len(values) != 1 || (key != "storage" && key != "parent" && key != "id") {
			fail(w, 400, errors.New("视频信息参数无效"))
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	select {
	case a.thumbnails.slots <- struct{}{}:
		defer func() { <-a.thumbnails.slots }()
	case <-ctx.Done():
		fail(w, 408, errors.New("视频信息请求已超时"))
		return
	}
	s, f, parent, err := a.thumbnails.source(ctx, r, false)
	if err != nil || thumbnailKind(f.Name) != "video" {
		http.NotFound(w, r)
		return
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		fail(w, 503, errors.New("服务端未安装FFprobe"))
		return
	}
	source, closeSource, err := a.subtitleSource(ctx, s, f, parent)
	if err != nil {
		fail(w, 503, errors.New("无法准备视频信息读取"))
		return
	}
	defer closeSource()
	output, err := runSubtitleTool(ctx, probe, "-v", "error", "-protocol_whitelist", "http,tcp", "-format_whitelist", "mov,matroska,webm,avi,mpegts,mpeg", "-probesize", "8388608", "-analyzeduration", "10000000", "-show_entries", "stream=index,codec_type,codec_name,profile,width,height,avg_frame_rate,sample_rate,channels,channel_layout,bit_rate,color_transfer:stream_tags=language,title:format=format_name,duration,bit_rate", "-of", "json", source)
	if err != nil {
		fail(w, 400, errors.New("视频信息读取失败或超过限额"))
		return
	}
	var data videoProbeInfo
	if json.Unmarshal(output, &data) != nil || len(data.Streams) > 64 {
		fail(w, 400, errors.New("视频信息响应无效"))
		return
	}
	if err := a.cloudStorageCurrent(s); err != nil {
		fail(w, 409, errors.New("存储配置或状态已变化"))
		return
	}
	jsonResponse(w, 200, map[string]any{"size": f.Size, "streams": data.Streams, "format": data.Format})
}
