package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (h *organizeRunner) probe(ctx context.Context, e organizeEntry, template string, facts map[string]string) error {
	needed := false
	for _, key := range []string{"resolution", "frame_rate", "resource_effect", "video_codec", "audio_codec", "color_depth"} {
		if strings.Contains(template, "{"+key+"}") && facts[key] == "" {
			needed = true
		}
	}
	if !needed {
		return nil
	}
	var key string
	if !strings.HasSuffix(strings.ToLower(e.file.Name), ".strm") {
		var err error
		key, err = h.digest(ctx, h.source, e.file)
		if err != nil {
			return err
		}
	}
	cache := ""
	if key != "" {
		sum := sha256.Sum256([]byte(key))
		cache = filepath.Join(h.a.dataDir, "runtime", "organize-probe", hex.EncodeToString(sum[:])+".json")
	}
	var info videoProbeInfo
	if cache != "" {
		if raw, err := os.ReadFile(cache); err == nil && len(raw) < 65536 {
			if h.a.store.decodeJSONConfig("organize-probe", raw, &info) != nil {
				info = videoProbeInfo{}
			}
		}
	}
	if len(info.Streams) == 0 {
		tool, err := exec.LookPath("ffprobe")
		if err != nil {
			return errors.New("FFprobe解析已开启，但服务端未安装FFprobe")
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		source, closeSource, err := h.probeSource(ctx, e)
		if err != nil {
			return err
		}
		defer closeSource()
		raw, err := runSubtitleTool(ctx, tool, "-v", "error", "-protocol_whitelist", "http,tcp", "-format_whitelist", "mov,matroska,webm,avi,mpegts,mpeg", "-probesize", "4194304", "-analyzeduration", "3000000", "-show_entries", "stream=codec_type,codec_name,width,height,avg_frame_rate,color_transfer,bits_per_raw_sample", "-of", "json", source)
		if err != nil {
			return errors.New("FFprobe读取失败或超过8MiB限额")
		}
		if json.Unmarshal(raw, &info) != nil || len(info.Streams) == 0 || len(info.Streams) > 64 {
			return errors.New("FFprobe响应无效")
		}
		if cache != "" {
			raw, err := h.a.store.encodeJSONConfig("organize-probe", info)
			if err != nil {
				return err
			}
			if err = os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
				return err
			}
			if err = atomicWrite(cache, raw); err != nil {
				return err
			}
		}
	}
	for _, s := range info.Streams {
		if s.Type == "video" {
			if facts["resolution"] == "" {
				switch {
				case s.Width >= 3800 || s.Height >= 2000:
					facts["resolution"] = "2160p"
				case s.Width >= 1900 || s.Height >= 1000:
					facts["resolution"] = "1080p"
				case s.Width >= 1200 || s.Height >= 700:
					facts["resolution"] = "720p"
				case s.Height > 0:
					facts["resolution"] = strconv.Itoa(s.Height) + "p"
				}
			}
			if facts["video_codec"] == "" {
				facts["video_codec"] = strings.ToUpper(s.Codec)
			}
			if facts["color_depth"] == "" {
				if bits, err := strconv.Atoi(s.BitDepth); err == nil && bits > 0 && bits <= 32 {
					facts["color_depth"] = strconv.Itoa(bits) + "BIT"
				}
			}
			if facts["resource_effect"] == "" {
				switch s.Transfer {
				case "smpte2084":
					facts["resource_effect"] = "HDR10"
				case "arib-std-b67":
					facts["resource_effect"] = "HLG"
				}
			}
			if facts["frame_rate"] == "" {
				p := strings.Split(s.FrameRate, "/")
				if len(p) == 2 {
					a, _ := strconv.ParseFloat(p[0], 64)
					b, _ := strconv.ParseFloat(p[1], 64)
					if b > 0 {
						facts["frame_rate"] = strconv.FormatFloat(a/b, 'f', 2, 64) + "FPS"
					}
				}
			}
		}
		if s.Type == "audio" && facts["audio_codec"] == "" {
			facts["audio_codec"] = strings.ToUpper(s.Codec)
		}
	}
	return nil
}

// Private bounded range proxy: ffprobe never receives cloud credentials or a
// signed upstream URL. The complete session may read at most 8 MiB.
func (h *organizeRunner) probeSource(ctx context.Context, e organizeEntry) (string, func(), error) {
	var download Download
	var err error
	client := &http.Client{Transport: apiClient.Transport, CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 || next.URL.User != nil || (next.URL.Scheme != "http" && next.URL.Scheme != "https") || via[len(via)-1].URL.Scheme == "https" && next.URL.Scheme != "https" {
			return http.ErrUseLastResponse
		}
		if next.URL.Host != via[0].URL.Host || next.URL.Scheme != via[0].URL.Scheme {
			next.Header.Del("Authorization")
			next.Header.Del("Cookie")
			next.Header.Del("Proxy-Authorization")
			redirect115ReadCookies(next, via)
		}
		return nil
	}}
	closeTransport := func() {}
	var local *os.File
	if strings.HasSuffix(strings.ToLower(e.file.Name), ".strm") {
		if e.file.Size > 65536 {
			return "", nil, errors.New("STRM指针过大")
		}
		raw, err := h.a.readSTRMMetadata(ctx, h.source, e.file)
		if err != nil {
			return "", nil, err
		}
		var trusted bool
		download, local, trusted, err = h.resolveProbePointer(ctx, strings.TrimSpace(string(raw)))
		if err != nil {
			return "", nil, err
		}
		if !trusted {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			transport.DialContext = organizeProbePublicDial
			client.Transport = transport
			closeTransport = transport.CloseIdleConnections
		}
	} else if h.source.Type == "local" {
		root, err := os.OpenRoot(h.source.Config["root"])
		if err != nil {
			return "", nil, err
		}
		rel, err := relative(e.file.ID)
		if err == nil {
			local, err = root.Open(rel)
		}
		root.Close()
		if err != nil {
			return "", nil, err
		}
	} else {
		download, err = h.a.download(ctx, h.source, e.file.ID, e.file.PickCode)
		if err != nil {
			return "", nil, err
		}
	}
	var localSize int64
	if local != nil {
		info, err := local.Stat()
		if err != nil || !info.Mode().IsRegular() {
			local.Close()
			return "", nil, errors.New("FFprobe仅支持普通媒体文件")
		}
		localSize = info.Size()
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		closeTransport()
		if local != nil {
			local.Close()
		}
		return "", nil, err
	}
	token := id()
	var mu sync.Mutex
	var read int64
	requests := 0
	consume := func(n int) error {
		mu.Lock()
		defer mu.Unlock()
		read += int64(n)
		if read > 8<<20 {
			return errors.New("FFprobe读取超过8MiB")
		}
		return nil
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+token || (r.Method != "GET" && r.Method != "HEAD") {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		requests++
		allowed := requests <= 24
		mu.Unlock()
		if !allowed {
			w.WriteHeader(429)
			return
		}
		if h.check(ctx) != nil {
			w.WriteHeader(409)
			return
		}
		if local != nil {
			reader := io.NewSectionReader(local, 0, localSize)
			http.ServeContent(w, r, e.file.Name, e.file.Modified, subtitleReader{ctx: ctx, reader: reader, consume: consume})
			return
		}
		req, err := http.NewRequestWithContext(ctx, r.Method, download.URL, nil)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		req.Header = download.Headers.Clone()
		if req.Header == nil {
			req.Header = http.Header{}
		}
		if value := r.Header.Get("Range"); value != "" {
			req.Header.Set("Range", value)
		}
		res, err := client.Do(req)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		defer res.Body.Close()
		for _, key := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "Content-Type"} {
			if val := res.Header.Get(key); val != "" {
				w.Header().Set(key, val)
			}
		}
		w.WriteHeader(res.StatusCode)
		if r.Method == "HEAD" {
			return
		}
		buffer := make([]byte, 32768)
		for {
			n, err := res.Body.Read(buffer)
			if n > 0 {
				if consume(n) != nil {
					return
				}
				if _, err = w.Write(buffer[:n]); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	})}
	go server.Serve(listener)
	return "http://" + listener.Addr().String() + "/" + token, func() {
		server.Close()
		closeTransport()
		if local != nil {
			local.Close()
		}
	}, nil
}

func organizeProbePublicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, resolved := range addresses {
		ip := resolved.IP
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if v := ip.To4(); v != nil && v[0] == 100 && v[1]&0xc0 == 64 {
			continue
		}
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	return nil, errors.New("STRM指针不允许访问内部网络地址")
}

// Resolve Aether's signed file references without following user-supplied
// internal HTTP addresses. Public URLs use a separate restricted transport.
func (h *organizeRunner) resolveProbePointer(ctx context.Context, address string) (Download, *os.File, bool, error) {
	if filepath.IsAbs(address) && !strings.ContainsAny(address, "\r\n\x00") {
		for _, storage := range h.st.Storages {
			if storage.Type != "local" || !storage.Enabled {
				continue
			}
			rootPath, err := filepath.Abs(storage.Config["root"])
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(rootPath, address)
			if err != nil || rel == "." || !filepath.IsLocal(rel) {
				continue
			}
			if err = h.a.cloudStorageCurrent(storage); err != nil {
				return Download{}, nil, false, err
			}
			if _, err = organizeLocalDir(storage, filepath.ToSlash(filepath.Dir(rel))); err != nil {
				return Download{}, nil, false, err
			}
			root, err := os.OpenRoot(rootPath)
			if err != nil {
				return Download{}, nil, false, err
			}
			defer root.Close()
			info, err := root.Lstat(rel)
			if err != nil || !info.Mode().IsRegular() {
				return Download{}, nil, false, errors.New("STRM本地指针不是普通文件")
			}
			file, err := root.Open(rel)
			return Download{}, file, true, err
		}
	}
	var ref strmReference
	u, err := url.Parse(address)
	if err != nil || u.User != nil || u.Fragment != "" || strings.ContainsAny(address, "\r\n\x00") {
		return Download{}, nil, false, errors.New("STRM指针格式无效")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case len(parts) == 2 && parts[0] == "stream" && u.Query().Has("sign"):
		mac := hmac.New(sha256.New, []byte(h.st.SignKey))
		mac.Write([]byte(parts[1]))
		sig, err := base64.RawURLEncoding.DecodeString(u.Query().Get("sign"))
		if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
			return Download{}, nil, false, errors.New("STRM播放签名无效")
		}
		raw, err := base64.RawURLEncoding.DecodeString(parts[1])
		var claim streamClaim
		if err != nil || json.Unmarshal(raw, &claim) != nil || claim.CAS != nil || claim.ED2K != nil {
			return Download{}, nil, false, errors.New("STRM媒体引用无效")
		}
		ref = strmReference{Storage: claim.Storage, File: claim.File, Pick: claim.Pick}
	case (len(parts) == 3 || len(parts) == 4) && parts[0] == "d" && pickcodePath.MatchString(parts[2]):
		if _, err := strconv.ParseUint(parts[1], 10, 64); err != nil {
			break
		}
		grant, err := h.a.readSTRMReference(parts[1] + "/" + parts[2])
		if err != nil {
			break
		}
		storage, err := h.a.store.storage(grant.Storage)
		account, accountErr := account115(storage)
		if err != nil || accountErr != nil || account != parts[1] || len(parts) == 4 && grant.Name != parts[3] {
			return Download{}, nil, false, errors.New("115 STRM存储引用无效")
		}
		ref = grant
	case len(parts) == 9 && strings.Join(parts[:3], "/") == "api/strm/play" && parts[5] == "t" && parts[7] == "n":
		if !hmac.Equal([]byte(parts[6]), []byte(h.a.quarkSTRMSign(parts[3], parts[4], parts[8]))) {
			return Download{}, nil, false, errors.New("夸克STRM签名无效")
		}
		fid, err := base64.RawURLEncoding.DecodeString(parts[4])
		if err != nil {
			return Download{}, nil, false, errors.New("夸克STRM文件标识无效")
		}
		ref = strmReference{Storage: parts[3], File: string(fid), Name: parts[8]}
	}
	if ref.Storage != "" && ref.File != "" {
		s, err := h.a.store.storage(ref.Storage)
		if err != nil {
			return Download{}, nil, false, err
		}
		d, err := h.a.download(ctx, s, ref.File, ref.Pick)
		if err != nil {
			return Download{}, nil, false, err
		}
		if s.Type == "local" {
			root, err := os.OpenRoot(s.Config["root"])
			if err != nil {
				return Download{}, nil, false, err
			}
			defer root.Close()
			f, err := root.Open(d.Local)
			return d, f, true, err
		}
		return d, nil, true, nil
	}
	if u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Download{}, nil, false, errors.New("此STRM指针不能进行FFprobe解析，可关闭此选项后整理")
	}
	return Download{URL: address}, nil, false, nil
}
