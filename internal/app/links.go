package app

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	lc "aether/internal/linkcore/config"
	"aether/internal/linkcore/logx"
	"aether/internal/linkcore/proxy"
	"aether/internal/linkcore/resolver"
	"aether/internal/linkcore/stats"
	"aether/internal/linkcore/upstream"
)

type MediaLink struct {
	MediaRoot    string `json:"mediaRoot,omitempty"`
	UpstreamRoot string `json:"upstreamRoot,omitempty"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Address      string `json:"address"`
	Port         int    `json:"port"`
	APIKey       string `json:"apiKey"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	Mode         string `json:"mode"`
	BlockedUA    string `json:"blockedUA"`
	Enabled      bool   `json:"enabled"`
}

type linkService struct {
	server   *http.Server
	resolver *resolver.Resolver
}
type linkRuntime struct {
	mu       sync.Mutex
	services map[string]*linkService
	stats    *stats.Collector
	audio    *proxy.AudioCache
	active   bool
}

func (a *App) startLinks() {
	a.links.mu.Lock()
	defer a.links.mu.Unlock()
	a.links.active = true
	logx.SetSink(func(level, message string) {
		a.store.event(level, "links", strings.ReplaceAll(message, "AetherLink", "Aether"))
	})
	a.links.audio = proxy.NewAudioCache(a.ctx, filepath.Join(a.dataDir, "cache", "link", "audio"))
	for _, link := range a.store.snapshot().Links {
		if !link.Enabled {
			continue
		}
		service, err := a.buildLink(link)
		if err != nil {
			a.store.event("error", "links", fmt.Sprintf("%s 启动失败：%v", link.Name, err))
			continue
		}
		a.links.services[link.ID] = service
	}
}

func (a *App) closeLinks() {
	a.links.mu.Lock()
	defer a.links.mu.Unlock()
	for _, service := range a.links.services {
		service.server.Close()
		service.resolver.Close()
	}
	a.links.services = map[string]*linkService{}
	logx.SetSink(nil)
}

func (a *App) buildLink(link MediaLink) (*linkService, error) {
	if err := os.MkdirAll(filepath.Join(a.dataDir, "cache", "link"), 0700); err != nil {
		return nil, err
	}
	cfg := lc.Default()
	cfg.Redirect.Mode = lc.RedirectMode(link.Mode)
	cfg.Redirect.TrustedProxyCIDRs = linkTrustedProxies()
	cfg.Redirect.FollowUpstreamRedirects = true
	cfg.Redirect.BlockClientUserAgent = lc.Bool(strings.TrimSpace(link.BlockedUA) != "")
	cfg.Redirect.BlockedUserAgents = strings.Split(link.BlockedUA, "\n")
	cfg.Redirect.StreamTimeout = 2 * time.Hour
	cfg.Redirect.FallbackUserAgent = "Aether"
	provider, err := upstream.New(a.linkUpstream(link))
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(link.Port)))
	if err != nil {
		return nil, fmt.Errorf("反代端口不可用：%w", err)
	}
	// Old snapshots may contain unprobed STRM URLs instead of resolved download URLs.
	r := resolver.NewWithPersistence(cfg.Cache, cfg.Redirect, filepath.Join(a.dataDir, "cache", "link", link.ID+"-direct-v2.json"))
	handler := proxy.NewWithAudioCache(provider, r, a.links.stats, cfg.Redirect, a.links.audio)
	authorized := authorizeLinkPlayer(provider, handler)
	metered := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, media := provider.Match(request)
		if media && request.Method == http.MethodGet {
			w = &transferResponse{ResponseWriter: w, successOnly: true, progress: &transferProgress{meter: &a.traffic, kind: "download"}}
		}
		authorized.ServeHTTP(w, request)
	})
	service := &linkService{resolver: r, server: &http.Server{Handler: metered, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}}
	go func() {
		if err := service.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.store.event("error", "links", "以链监听异常："+link.Name)
		}
	}()
	return service, nil
}

func validateLink(link *MediaLink, links []MediaLink) error {
	link.MediaRoot = strings.TrimSpace(link.MediaRoot)
	link.UpstreamRoot = strings.TrimSpace(link.UpstreamRoot)
	if link.UpstreamRoot != "" && link.MediaRoot == "" {
		return errors.New("填写上游媒体目录时，需同时填写 Aether 媒体目录")
	}
	for _, root := range []string{link.MediaRoot, link.UpstreamRoot} {
		if root != "" && ((!filepath.IsAbs(root) && !path.IsAbs(root)) || path.Clean(root) == "/" || filepath.Clean(root) == string(filepath.Separator) || strings.ContainsRune(root, '\x00')) {
			return errors.New("媒体目录须为非根目录的绝对路径")
		}
	}
	link.Name = strings.TrimSpace(link.Name)
	if link.Name == "" || len(link.Name) > 180 {
		return errors.New("请输入以链名称")
	}
	switch link.Type {
	case "audiobookshelf", "emby", "fnos":
	default:
		return errors.New("不支持的媒体类型")
	}
	u, err := url.Parse(link.Address)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("服务地址应为HTTP(S)地址，不含凭据或参数")
	}
	if link.Port < 1024 || link.Port > 65535 {
		return errors.New("反代端口应为1024–65535")
	}
	if u.Port() == strconv.Itoa(link.Port) {
		local := strings.EqualFold(u.Hostname(), "localhost")
		ip := net.ParseIP(u.Hostname())
		if ip != nil {
			local = ip.IsLoopback() || ip.IsUnspecified()
			addresses, _ := net.InterfaceAddrs()
			for _, address := range addresses {
				if network, ok := address.(*net.IPNet); ok && network.IP.Equal(ip) {
					local = true
				}
			}
		}
		if local {
			return errors.New("服务地址不能指向本机反代端口")
		}
	}
	addr, _ := listenAddress()
	_, port, _ := net.SplitHostPort(addr)
	if strconv.Itoa(link.Port) == port {
		return errors.New("不能占用管理后台端口")
	}
	switch link.Mode {
	case "always", "public", "private", "never":
	default:
		return errors.New("跳转模式无效")
	}
	if link.Type == "fnos" {
		if link.Username == "" || link.Password == "" {
			return errors.New("请输入飞牛账号密码")
		}
	} else if link.APIKey == "" {
		return errors.New("请输入API Key")
	}
	if len(link.BlockedUA) > 16384 {
		return errors.New("屏蔽UA列表过长")
	}
	for _, other := range links {
		if other.ID != link.ID && (other.Port == link.Port || strings.EqualFold(other.Name, link.Name)) {
			return errors.New("以链名称或端口已存在")
		}
	}
	return nil
}

func (a *App) linkUpstream(link MediaLink) lc.Upstream {
	u := lc.Upstream{Name: link.ID, Type: lc.UpstreamType(link.Type), BaseURL: link.Address, APIKey: link.APIKey, Username: link.Username, Password: link.Password, ListenPort: link.Port,
		StrmRoots: []string{a.dataDir, "/mnt", "/audiobooks", "/media", "/strm", "/NetDisk"}}
	if link.MediaRoot != "" {
		u.StrmRoots = append(u.StrmRoots, link.MediaRoot)
		if link.UpstreamRoot != "" {
			u.PathMappings = []lc.PathMapping{{From: link.UpstreamRoot, To: link.MediaRoot}}
		}
	}
	return u
}

func (a *App) reorderLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var move struct {
		ID     string `json:"id"`
		Target string `json:"target"`
	}
	if !decode(w, r, &move) {
		return
	}
	a.links.mu.Lock()
	defer a.links.mu.Unlock()
	err := a.store.update(func(st *State) error {
		from, to := -1, -1
		for i, link := range st.Links {
			if link.ID == move.ID {
				from = i
			}
			if link.ID == move.Target {
				to = i
			}
		}
		if from < 0 || to < 0 {
			return errors.New("以链不存在，请刷新后重试")
		}
		item := st.Links[from]
		if from < to {
			copy(st.Links[from:to], st.Links[from+1:to+1])
		}
		if from > to {
			copy(st.Links[to+1:from+1], st.Links[to:from])
		}
		st.Links[to] = item
		return nil
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	a.store.event("info", "links", "调整以链排序")
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *App) mediaLinks(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		a.store.mu.RLock()
		links := append([]MediaLink(nil), a.store.state.Links...)
		a.store.mu.RUnlock()
		jsonResponse(w, 200, redactedMediaLinks(links))
		return
	}
	if r.Method != "POST" && r.Method != "PUT" && r.Method != "DELETE" {
		w.WriteHeader(405)
		return
	}
	a.links.mu.Lock()
	defer a.links.mu.Unlock()
	var incoming MediaLink
	if r.Method != "DELETE" && !decode(w, r, &incoming) {
		return
	}
	sid := r.PathValue("id")
	existing := a.store.snapshot().Links
	index := -1
	for i, link := range existing {
		if link.ID == sid {
			index = i
		}
	}
	if r.Method != "POST" && index < 0 {
		fail(w, 404, errors.New("以链不存在"))
		return
	}
	if r.Method == "POST" {
		incoming.ID = id()
	} else {
		incoming.ID = sid
	}
	if index >= 0 {
		if incoming.APIKey == "********" {
			incoming.APIKey = existing[index].APIKey
		}
		if incoming.Password == "********" {
			incoming.Password = existing[index].Password
		}
	}
	if r.Method != "DELETE" {
		if err := validateLink(&incoming, existing); err != nil {
			fail(w, 400, err)
			return
		}
	}
	// Stop the old listener before rebinding its port; restore it if activation or persistence fails.
	old := a.links.services[sid]
	if old != nil {
		old.server.Close()
		old.resolver.Close()
		delete(a.links.services, sid)
	}
	restore := func() {
		if old != nil {
			if service, err := a.buildLink(existing[index]); err == nil {
				a.links.services[sid] = service
			} else {
				a.store.event("error", "links", "恢复以链失败："+err.Error())
			}
		}
	}
	var service *linkService
	var err error
	if r.Method != "DELETE" && incoming.Enabled && a.links.active {
		service, err = a.buildLink(incoming)
		if err != nil {
			restore()
			fail(w, 400, err)
			return
		}
	}
	err = a.store.update(func(st *State) error {
		if r.Method == "DELETE" {
			st.Links = append(st.Links[:index], st.Links[index+1:]...)
		} else if index >= 0 {
			st.Links[index] = incoming
		} else {
			st.Links = append(st.Links, incoming)
		}
		return nil
	})
	if err != nil {
		if service != nil {
			service.server.Close()
			service.resolver.Close()
		}
		restore()
		fail(w, 500, err)
		return
	}
	if service != nil {
		a.links.services[incoming.ID] = service
	}
	a.store.event("info", "links", "以链配置已更新")
	jsonResponse(w, 200, map[string]string{"id": incoming.ID})
}

func redactedMediaLinks(links []MediaLink) []MediaLink {
	result := append([]MediaLink{}, links...)
	for i := range result {
		if result[i].APIKey != "" {
			result[i].APIKey = "********"
		}
		if result[i].Password != "" {
			result[i].Password = "********"
		}
	}
	return result
}

func (a *App) linkPlayback(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	snapshot := a.links.stats.Snapshot(2000)
	for i := range snapshot.RecentEvents {
		e := &snapshot.RecentEvents[i]
		e.Target = logx.Redact(e.Target)
		e.Error = strings.ReplaceAll(logx.Redact(e.Error), "AetherLink", "Aether")
	}
	jsonResponse(w, 200, snapshot)
}
