package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const subtitleWindow = 300

var subtitleTimestamp = regexp.MustCompile(`(?m)^((?:\d{2,}:)?\d{2}:\d{2}\.\d{3}) --> ((?:\d{2,}:)?\d{2}:\d{2}\.\d{3})(.*)$`)

func subtitleSeconds(value string) float64 {
	parts := strings.Split(value, ":")
	var seconds float64
	for _, part := range parts {
		number, _ := strconv.ParseFloat(part, 64)
		seconds = seconds*60 + number
	}
	return seconds
}

func subtitleTime(seconds float64) string {
	ms := int64(max(0, seconds)*1000 + .5)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

func subtitleWindowVTT(data []byte, start int) []byte {
	blocks := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n\n")
	out := []string{"WEBVTT"}
	for _, block := range blocks {
		match := subtitleTimestamp.FindStringSubmatch(block)
		if match == nil {
			continue
		}
		from, to := subtitleSeconds(match[1]), subtitleSeconds(match[2])
		if to <= float64(start) || from >= float64(start+subtitleWindow) {
			continue
		}
		replacement := subtitleTime(max(0, from-float64(start))) + " --> " + subtitleTime(min(float64(subtitleWindow), to-float64(start))) + match[3]
		out = append(out, subtitleTimestamp.ReplaceAllStringFunc(block, func(string) string { return replacement }))
	}
	return []byte(strings.Join(out, "\n\n") + "\n\n")
}

type videoSubtitleTrack struct {
	Index     int    `json:"index"`
	Codec     string `json:"codec"`
	Language  string `json:"language"`
	Title     string `json:"title"`
	Supported bool   `json:"supported"`
}

type subtitleOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (b *subtitleOutput) Write(data []byte) (int, error) {
	if b.buffer.Len()+len(data) > b.limit {
		return 0, errors.New("subtitle output limit")
	}
	return b.buffer.Write(data)
}

func runSubtitleTool(ctx context.Context, tool string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.WaitDelay = time.Second
	out := &subtitleOutput{limit: 4 << 20}
	cmd.Stdout = out
	// FFmpeg errors may include source URLs; do not persist or return stderr.
	if err := cmd.Run(); err != nil {
		return nil, errors.New("字幕处理失败或超过读取限额")
	}
	return out.buffer.Bytes(), nil
}

// FFmpeg only sees a token-protected loopback source. The shared storage reader
// handles credentials/redirects; media bytes and seek requests are bounded.
func (a *App) subtitleSource(ctx context.Context, s Storage, f File, parent string) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	token := id()
	var mu sync.Mutex
	var requests int
	var read int64
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+token || (r.Method != "GET" && r.Method != "HEAD") {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		requests++
		allowed := requests <= 128
		mu.Unlock()
		if !allowed {
			w.WriteHeader(429)
			return
		}
		current, err := a.store.storage(s.ID)
		if err != nil || shareConfig(current) != shareConfig(s) {
			w.WriteHeader(409)
			return
		}
		owned := context.WithValue(ctx, transferOwnedKey{}, true)
		file, err := (mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: parent, ReadOnly: true}}).OpenFile(owned, "/"+f.Name, os.O_RDONLY, 0)
		if err != nil {
			w.WriteHeader(404)
			return
		}
		defer file.Close()
		http.ServeContent(w, r.WithContext(ctx), f.Name, f.Modified, subtitleReader{ctx: ctx, reader: file, consume: func(n int) error {
			mu.Lock()
			read += int64(n)
			allowed := read <= 256<<20
			mu.Unlock()
			if !allowed {
				return errors.New("subtitle source limit")
			}
			return a.limitTransferBytes(ctx, "download", n)
		}})
	})}
	go server.Serve(listener)
	return "http://" + listener.Addr().String() + "/" + token, func() { server.Close() }, nil
}

type subtitleReader struct {
	ctx     context.Context
	reader  io.ReadSeeker
	consume func(int) error
}

func (r subtitleReader) Seek(off int64, whence int) (int64, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Seek(off, whence)
}
func (r subtitleReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(b[:min(len(b), 32<<10)])
	if e := r.consume(n); e != nil {
		return 0, e
	}
	return n, err
}

func (a *App) videoSubtitles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	// Reuse the existing two-slot bounded media inspection gate.
	select {
	case a.thumbnails.slots <- struct{}{}:
		defer func() { <-a.thumbnails.slots }()
	case <-ctx.Done():
		fail(w, 408, errors.New("字幕请求已超时"))
		return
	}
	q := r.URL.Query()
	for key, values := range q {
		if len(values) != 1 || (key != "storage" && key != "parent" && key != "id" && key != "track" && key != "start") {
			fail(w, 400, errors.New("字幕请求参数无效"))
			return
		}
	}
	track, start := -1, 0
	var err error
	if q.Has("track") {
		track, err = strconv.Atoi(q.Get("track"))
		if err != nil || track < 0 || track > 1024 {
			fail(w, 400, errors.New("字幕轨道无效"))
			return
		}
	}
	if q.Has("start") {
		start, err = strconv.Atoi(q.Get("start"))
		if err != nil || start < 0 || start > 7*24*3600 || start%subtitleWindow != 0 {
			fail(w, 400, errors.New("字幕时间范围无效"))
			return
		}
	}
	request := r.Clone(ctx)
	query := request.URL.Query()
	query.Del("track")
	query.Del("start")
	copiedURL := *request.URL
	copiedURL.RawQuery = query.Encode()
	request.URL = &copiedURL
	s, f, parent, err := a.thumbnails.source(ctx, request, false)
	if err != nil || thumbnailKind(f.Name) != "video" {
		http.NotFound(w, r)
		return
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		fail(w, 503, errors.New("服务端未安装FFprobe，无法读取内置字幕"))
		return
	}
	source, closeSource, err := a.subtitleSource(ctx, s, f, parent)
	if err != nil {
		fail(w, 503, errors.New("无法准备字幕读取"))
		return
	}
	defer closeSource()
	output, err := runSubtitleTool(ctx, probe, "-v", "error", "-protocol_whitelist", "http,tcp", "-format_whitelist", "mov,matroska,webm,avi,mpegts,mpeg", "-probesize", "8388608", "-analyzeduration", "10000000", "-select_streams", "s", "-show_entries", "stream=index,codec_name:stream_tags=language,title", "-of", "json", source)
	if err != nil {
		fail(w, 400, err)
		return
	}
	var data struct {
		Streams []struct {
			Index int    `json:"index"`
			Codec string `json:"codec_name"`
			Tags  struct {
				Language string `json:"language"`
				Title    string `json:"title"`
			} `json:"tags"`
		} `json:"streams"`
	}
	if json.Unmarshal(output, &data) != nil || len(data.Streams) > 64 {
		fail(w, 400, errors.New("字幕轨道信息无效"))
		return
	}
	tracks := make([]videoSubtitleTrack, 0, len(data.Streams))
	for _, stream := range data.Streams {
		supported := false
		switch stream.Codec {
		case "subrip", "ass", "ssa", "webvtt", "mov_text", "text":
			supported = true
		}
		tracks = append(tracks, videoSubtitleTrack{stream.Index, stream.Codec, stream.Tags.Language, stream.Tags.Title, supported})
	}
	if track < 0 {
		jsonResponse(w, 200, map[string]any{"tracks": tracks})
		return
	}
	found := false
	for _, item := range tracks {
		if item.Index == track && item.Supported {
			found = true
		}
	}
	if !found {
		fail(w, 400, errors.New("此字幕轨道不支持文本显示"))
		return
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		fail(w, 503, errors.New("服务端未安装FFmpeg"))
		return
	}
	output, err = runSubtitleTool(ctx, ffmpeg, "-nostdin", "-v", "error", "-protocol_whitelist", "http,tcp", "-format_whitelist", "mov,matroska,webm,avi,mpegts,mpeg", "-ss", strconv.Itoa(start), "-copyts", "-i", source, "-t", strconv.Itoa(start+subtitleWindow), "-map", fmt.Sprintf("0:%d", track), "-c:s", "webvtt", "-f", "webvtt", "pipe:1")
	if err != nil {
		fail(w, 400, err)
		return
	}
	if !strings.HasPrefix(string(output), "WEBVTT") {
		fail(w, 400, errors.New("字幕转换结果无效"))
		return
	}
	current, e := a.store.storage(s.ID)
	if e != nil || shareConfig(current) != shareConfig(s) {
		fail(w, 409, errors.New("存储配置已变化"))
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Write(subtitleWindowVTT(output, start))
}
