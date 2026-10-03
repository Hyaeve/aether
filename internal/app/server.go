package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/webdav"
)

type App struct {
	versionMu      sync.Mutex
	versionExpiry  time.Time
	versionResult  map[string]any
	tianyiMu       sync.Mutex
	tianyiSessions map[string]tianyiSession
	casGate        chan struct{}
	casActive      map[string]int
	store          *Store
	cache          *Cache
	ctx            context.Context
	logger         *log.Logger
	outputDir      string
	dataDir        string
	wg             sync.WaitGroup
	runMu          sync.Mutex
	running        map[string]context.CancelFunc
	runningStorage map[string]string
	gateMu         sync.Mutex
	gates          map[string]time.Time
	intervals      map[string]int
	sessionMu      sync.Mutex
	sessions       map[string]time.Time
	loginMu        sync.Mutex
	loginAttempts  map[string][]time.Time
	downloaded     atomic.Int64
	uploaded       atomic.Int64
	started        time.Time
	dav            *webdav.Handler
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func listenAddress() (string, error) {
	if addr := os.Getenv("AETHER_ADDR"); addr != "" {
		return addr, nil
	}
	value := env("AETHER_PORT", "15151")
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("AETHER_PORT 必须为 1–65535 之间的整数")
	}
	return net.JoinHostPort("", strconv.Itoa(port)), nil
}

func New(ctx context.Context, dir, output string) (*App, error) {
	return newWithDirectories(ctx, dir, dir, output)
}

func newWithDirectories(ctx context.Context, configDir, dataDir, output string) (*App, error) {
	if err := prepareDirectories(configDir, dataDir); err != nil {
		return nil, err
	}
	store, err := NewStore(configDir)
	if err != nil {
		return nil, err
	}
	a := &App{store: store, cache: NewCache(), ctx: ctx, outputDir: output, dataDir: dataDir, logger: log.Default(), running: map[string]context.CancelFunc{}, runningStorage: map[string]string{},
		gates: map[string]time.Time{}, intervals: map[string]int{}, sessions: map[string]time.Time{}, loginAttempts: map[string][]time.Time{}, started: time.Now()}
	a.casGate = make(chan struct{}, 1)
	a.casActive = map[string]int{}
	a.tianyiSessions = map[string]tianyiSession{}
	a.cache.restore(dataDir, store.snapshot().Settings)
	a.dav = &webdav.Handler{Prefix: "/dav", FileSystem: davFS{a}, LockSystem: webdav.NewMemLS()}
	return a, nil
}

func Run(ctx context.Context) error {
	return RunWithDirectories(ctx, "config", "data")
}

func RunWithDirectories(ctx context.Context, configDir, dataDir string) error {
	addr, err := listenAddress()
	if err != nil {
		return err
	}
	a, err := newWithDirectories(ctx, configDir, dataDir, filepath.Join(dataDir, "strm"))
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	a.wg.Add(1)
	go a.scheduler()
	server := &http.Server{Handler: a.Handler(env("AETHER_WEB_DIR", "web/dist")), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 1 << 20}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	log.Printf("Aether listening on %s", addr)
	select {
	case err = <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}
	a.wg.Wait()
	if a.store.snapshot().Settings.CachePersist {
		return a.cache.persist(a.dataDir)
	}
	return nil
}

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func fail(w http.ResponseWriter, status int, err error) {
	jsonResponse(w, status, map[string]string{"error": err.Error()})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, errors.New("请求数据格式不正确"))
		return false
	}
	return true
}

func (a *App) Handler(webDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]string{"status": "ok", "name": "Aether"})
	})
	mux.HandleFunc("GET /api/auth/status", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{"initialized": a.store.snapshot().Password != "", "authenticated": a.authenticated(r)})
	})
	mux.HandleFunc("POST /api/auth/setup", a.setup)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("aether_session"); err == nil {
			a.sessionMu.Lock()
			delete(a.sessions, c.Value)
			a.sessionMu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: "aether_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
	mux.Handle("/api/state", a.protected(http.HandlerFunc(a.state)))
	mux.Handle("GET /api/cas/status", a.protected(http.HandlerFunc(a.casStatus)))
	mux.Handle("POST /api/cas/cleanup", a.protected(http.HandlerFunc(a.casCleanup)))
	mux.Handle("GET /api/version", a.protected(http.HandlerFunc(a.version)))
	mux.Handle("GET /api/version/check", a.protected(http.HandlerFunc(a.checkVersion)))
	mux.Handle("POST /api/authorization/{provider}/start", a.protected(http.HandlerFunc(a.startAuthorization)))
	mux.Handle("POST /api/authorization/{provider}/poll", a.protected(http.HandlerFunc(a.pollAuthorization)))
	mux.Handle("/api/storages", a.protected(http.HandlerFunc(a.storages)))
	mux.Handle("/api/storages/{id}", a.protected(http.HandlerFunc(a.storageItem)))
	mux.Handle("/api/storages/{id}/test", a.protected(http.HandlerFunc(a.testStorage)))
	mux.Handle("/api/files", a.protected(http.HandlerFunc(a.files)))
	mux.Handle("/api/tasks", a.protected(http.HandlerFunc(a.tasks)))
	mux.Handle("/api/tasks/{id}", a.protected(http.HandlerFunc(a.taskItem)))
	mux.Handle("/api/tasks/{id}/{action}", a.protected(http.HandlerFunc(a.taskAction)))
	mux.Handle("/api/settings", a.protected(http.HandlerFunc(a.settings)))
	mux.Handle("/api/account", a.protected(http.HandlerFunc(a.account)))
	mux.Handle("/api/webdav/users", a.protected(http.HandlerFunc(a.davUsers)))
	mux.Handle("/api/webdav/users/{id}", a.protected(http.HandlerFunc(a.davUsers)))
	mux.Handle("/api/cache/clear", a.protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		a.cache.clear()
		if err := os.Remove(filepath.Join(a.dataDir, "cache.json")); err != nil && !os.IsNotExist(err) {
			fail(w, 500, err)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})))
	mux.HandleFunc("/stream/{token}", a.stream)
	mux.HandleFunc("/dav", a.serveDAV)
	mux.HandleFunc("/dav/", a.serveDAV)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		p, err := relative(r.URL.Path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		name := filepath.Join(webDir, p)
		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			http.ServeFile(w, r, name)
			return
		}
		http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Origin") != "" {
				u, err := url.Parse(r.Header.Get("Origin"))
				if err != nil || u.Host != r.Host {
					fail(w, 403, errors.New("跨站请求已拒绝"))
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *App) authenticated(r *http.Request) bool {
	c, err := r.Cookie("aether_session")
	if err != nil {
		return false
	}
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	exp, ok := a.sessions[c.Value]
	if ok && time.Now().After(exp) {
		delete(a.sessions, c.Value)
		return false
	}
	return ok
}

func (a *App) protected(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authenticated(r) {
			fail(w, 401, errors.New("请先登录"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) setSession(w http.ResponseWriter, r *http.Request) {
	token := id()
	days := a.store.snapshot().Settings.SessionDays
	if days < 1 || days > 365 {
		days = 7
	}
	a.sessionMu.Lock()
	for k, v := range a.sessions {
		if time.Now().After(v) {
			delete(a.sessions, k)
		}
	}
	if len(a.sessions) >= 100 {
		for k := range a.sessions {
			delete(a.sessions, k)
			break
		}
	}
	a.sessions[token] = time.Now().Add(time.Duration(days) * 24 * time.Hour)
	a.sessionMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "aether_session", Value: token, Path: "/", MaxAge: days * 86400, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
}

func (a *App) allowLogin(r *http.Request) bool {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	now := time.Now()
	for key, times := range a.loginAttempts {
		valid := times[:0]
		for _, t := range times {
			if now.Sub(t) < time.Minute {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(a.loginAttempts, key)
		} else {
			a.loginAttempts[key] = valid
		}
	}
	if len(a.loginAttempts[ip]) >= 10 || len(a.loginAttempts) > 10000 {
		return false
	}
	a.loginAttempts[ip] = append(a.loginAttempts[ip], now)
	return true
}

type credentials struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Current     string `json:"current,omitempty"`
	SessionDays int    `json:"sessionDays,omitempty"`
}

func (a *App) setup(w http.ResponseWriter, r *http.Request) {
	if !a.allowLogin(r) {
		fail(w, 429, errors.New("请求过于频繁，请稍后重试"))
		return
	}
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	if len(c.Password) == 0 || len(c.Password) > 72 || strings.TrimSpace(c.Username) == "" {
		fail(w, 400, errors.New("请输入账号和非空密码，密码不能超过 72 字节"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(w, 500, err)
		return
	}
	err = a.store.update(func(st *State) error {
		if st.Password != "" {
			return errors.New("系统已初始化")
		}
		st.Username, st.Password = strings.TrimSpace(c.Username), string(hash)
		return nil
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	a.setSession(w, r)
	a.store.log("info", "管理员账号已创建")
	jsonResponse(w, 201, map[string]bool{"ok": true})
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !a.allowLogin(r) {
		fail(w, 429, errors.New("登录尝试过多，请一分钟后重试"))
		return
	}
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	st := a.store.snapshot()
	if bcrypt.CompareHashAndPassword([]byte(st.Password), []byte(c.Password)) != nil || subtle.ConstantTimeCompare([]byte(c.Username), []byte(st.Username)) != 1 {
		fail(w, 401, errors.New("账号或密码错误"))
		return
	}
	a.setSession(w, r)
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *App) state(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	st := a.store.snapshot()
	for i := range st.Storages {
		for _, key := range []string{"password", "token", "accessToken", "refreshToken", "cookie", "authorization"} {
			if st.Storages[i].Config[key] != "" {
				st.Storages[i].Config[key] = "********"
			}
		}
	}
	jsonResponse(w, 200, map[string]any{"storages": st.Storages, "tasks": st.Tasks, "settings": st.Settings, "logs": st.Logs, "username": st.Username,
		"cache": a.cache.stats(), "traffic": map[string]any{"uploaded": a.uploaded.Load(), "downloaded": a.downloaded.Load()}, "uptime": int(time.Since(a.started).Seconds()), "strmRoot": a.outputDir})
}

func validateStorage(s *Storage) error {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" || len(s.Name) > 150 {
		return errors.New("请输入有效的存储池名称")
	}
	if s.CacheTTL < 0 || s.CacheTTL > 525600 {
		return errors.New("缓存时间必须在 0–525600 分钟之间")
	}
	if s.Config == nil {
		s.Config = map[string]string{}
	}
	if s.Config["deleteMode"] == "" {
		s.Config["deleteMode"] = "trash"
	}
	if s.Config["deleteMode"] != "trash" && s.Config["deleteMode"] != "permanent" {
		return errors.New("无效的删除模式")
	}
	if nativeMobile(*s) {
		_, _, err := mobileAccount(*s)
		return err
	}
	required := []string{}
	switch s.Type {
	case "local":
		required = []string{"root"}
	case "115":
		required = []string{"accessToken"}
	case "quark":
		required = []string{"cookie"}
	case "tianyi":
		required = []string{"username", "password"}
		s.Config["username"] = strings.TrimSpace(s.Config["username"])
		s.Config["mode"] = "native"
		if s.Config["root"] == "" || s.Config["root"] == "/" {
			s.Config["root"] = "-11"
		}
		if _, err := strconv.ParseInt(s.Config["root"], 10, 64); err != nil {
			return errors.New("天翼根目录需填写数字 ID，个人云根目录为 -11")
		}
		delete(s.Config, "address")
		delete(s.Config, "token")
	case "openlist", "mobile", "webdav":
		required = []string{"address"}
		u, err := url.Parse(s.Config["address"])
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return errors.New("请输入有效的 HTTP(S) 服务地址")
		}
	default:
		return errors.New("无效的存储类型")
	}
	for _, key := range required {
		if strings.TrimSpace(s.Config[key]) == "" {
			return fmt.Errorf("缺少必填配置：%s", key)
		}
	}
	return nil
}

func (a *App) storages(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var s Storage
	if !decode(w, r, &s) {
		return
	}
	if err := validateStorage(&s); err != nil {
		fail(w, 400, err)
		return
	}
	s.ID, s.CreatedAt, s.Status, s.LastError = id(), time.Now(), "unchecked", ""
	err := a.store.update(func(st *State) error {
		for _, v := range st.Storages {
			if strings.EqualFold(v.Name, s.Name) {
				return errors.New("存储池名称已存在")
			}
		}
		st.Storages = append(st.Storages, s)
		return nil
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	a.store.log("info", "添加存储池："+s.Name)
	jsonResponse(w, 201, map[string]string{"id": s.ID})
}

func (a *App) storageItem(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	if r.Method != "PUT" && r.Method != "DELETE" {
		w.WriteHeader(405)
		return
	}
	var incoming Storage
	if r.Method == "PUT" && !decode(w, r, &incoming) {
		return
	}
	if err := a.acquireCAS(r.Context()); err != nil {
		fail(w, 408, err)
		return
	}
	defer func() { <-a.casGate }()
	a.runMu.Lock()
	defer a.runMu.Unlock()
	for _, running := range a.runningStorage {
		if running == sid {
			fail(w, 409, errors.New("请先停止该存储池的任务"))
			return
		}
	}
	err := a.store.update(func(st *State) error {
		for i, s := range st.Storages {
			if s.ID != sid {
				continue
			}
			for _, temporary := range st.CASTemporary {
				if temporary.StorageID == sid {
					if r.Method == "DELETE" || !incoming.Enabled || !casStorage(incoming) {
						return errors.New("该存储池仍有 CAS 临时文件，请等待过期清理后再删除、停用或切换接入方式")
					}
					if nativeTianyi(s) || incoming.Config["authorization"] != "********" {
						oldAccount, _ := casAccount(s)
						newAccount, err := casAccount(incoming)
						if err != nil || oldAccount != newAccount {
							return errors.New("CAS 临时文件清理前不能更换网盘账号，可更新同账号凭据")
						}
					}
				}
			}
			if r.Method == "DELETE" {
				for _, t := range st.Tasks {
					if t.StorageID == sid {
						return errors.New("请先删除关联任务")
					}
				}
				st.Storages = append(st.Storages[:i], st.Storages[i+1:]...)
				return nil
			}
			if incoming.Type != s.Type {
				return errors.New("不能修改存储类型")
			}
			for k, v := range incoming.Config {
				if v == "********" {
					incoming.Config[k] = s.Config[k]
				}
			}
			if err := validateStorage(&incoming); err != nil {
				return err
			}
			for _, other := range st.Storages {
				if other.ID != sid && strings.EqualFold(other.Name, incoming.Name) {
					return errors.New("存储池名称已存在")
				}
			}
			incoming.ID, incoming.CreatedAt, incoming.Status, incoming.LastError = sid, s.CreatedAt, "unchecked", ""
			st.Storages[i] = incoming
			return nil
		}
		return errors.New("存储池不存在")
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	a.cache.clear()
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *App) testStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	s, err := a.store.storage(r.PathValue("id"))
	if err != nil {
		fail(w, 404, err)
		return
	}
	_, testErr := a.listFiles(r.Context(), s, rootOf(s), 0, true)
	status, message := "connected", ""
	if testErr != nil {
		status, message = "error", testErr.Error()
	}
	err = a.store.update(func(st *State) error {
		for i := range st.Storages {
			if st.Storages[i].ID == s.ID {
				st.Storages[i].Status = status
				st.Storages[i].LastError = message
			}
		}
		return nil
	})
	if err != nil {
		fail(w, 500, err)
		return
	}
	if testErr != nil {
		fail(w, 400, testErr)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *App) files(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	s, err := a.store.storage(r.URL.Query().Get("storage"))
	if err != nil {
		fail(w, 404, err)
		return
	}
	files, err := a.listFiles(r.Context(), s, r.URL.Query().Get("path"), 0, r.URL.Query().Get("refresh") == "true")
	if err != nil {
		fail(w, 400, err)
		return
	}
	type fileLink struct {
		File
		URL string `json:"url,omitempty"`
	}
	out := []fileLink{}
	for _, f := range files {
		link := ""
		if !f.IsDir {
			link = a.streamURL(s.ID, f.ID, f.PickCode)
		}
		out = append(out, fileLink{f, link})
	}
	jsonResponse(w, 200, out)
}

func (a *App) validateTask(t *Task) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return errors.New("请输入任务名称")
	}
	if _, err := a.store.storage(t.StorageID); err != nil {
		return err
	}
	if t.Source == "" {
		t.Source = "/"
	}
	if t.APIInterval < 0 || t.APIInterval > 60000 {
		return errors.New("API 间隔必须在 0–60000ms 之间")
	}
	if t.CacheTTL < 0 || t.CacheTTL > 525600 {
		return errors.New("缓存时间无效")
	}
	switch t.Kind {
	case "strm", "cas":
		if t.Kind == "cas" {
			if t.RetentionHours == 0 {
				t.RetentionHours = 12
			}
			if t.RetentionHours < 1 || t.RetentionHours > 8760 {
				return errors.New("还原文件保留时间必须为 1–8760 小时")
			}
			s, _ := a.store.storage(t.StorageID)
			if !casStorage(s) {
				return errors.New("CAS 任务需要原生移动新版个人云或天翼个人云存储池")
			}
		}
		t.Target = strings.TrimSpace(t.Target)
		if _, err := a.outputRelative(t.Target); err != nil {
			return err
		}
		if t.Mode != "full" && t.Mode != "incremental" {
			return errors.New("无效的生成方式")
		}
		if t.Cron != "" {
			if _, err := cronParser.Parse(t.Cron); err != nil {
				return errors.New("Cron 格式错误，需要五个字段")
			}
		}
	case "cache":
		if t.Interval < 1 || t.Interval > 525600 {
			return errors.New("执行间隔必须为 1–525600 分钟")
		}
		if t.Depth < 0 || t.Depth > 128 {
			return errors.New("扫描层级必须在 0–128 之间，0 表示所有层级")
		}
	default:
		return errors.New("不支持的任务类型")
	}
	return nil
}

func (a *App) tasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var t Task
	if !decode(w, r, &t) {
		return
	}
	if err := a.validateTask(&t); err != nil {
		fail(w, 400, err)
		return
	}
	t.ID, t.Status, t.Message, t.Processed, t.LastRun = id(), "idle", "等待执行", 0, time.Time{}
	t.NextRun = nextRun(t, time.Now())
	if err := a.store.update(func(st *State) error { st.Tasks = append(st.Tasks, t); return nil }); err != nil {
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 201, t)
}

func (a *App) taskItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" && r.Method != "DELETE" {
		w.WriteHeader(405)
		return
	}
	tid := r.PathValue("id")
	var in Task
	if r.Method == "PUT" {
		if !decode(w, r, &in) {
			return
		}
		if err := a.validateTask(&in); err != nil {
			fail(w, 400, err)
			return
		}
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if _, ok := a.running[tid]; ok {
		fail(w, 409, errors.New("请先停止任务"))
		return
	}
	err := a.store.update(func(st *State) error {
		for i, t := range st.Tasks {
			if t.ID != tid {
				continue
			}
			if r.Method == "DELETE" {
				st.Tasks = append(st.Tasks[:i], st.Tasks[i+1:]...)
				return nil
			}
			in.ID, in.Status, in.Message, in.Processed, in.LastRun = tid, t.Status, t.Message, t.Processed, t.LastRun
			in.NextRun = nextRun(in, time.Now())
			st.Tasks[i] = in
			return nil
		}
		return errors.New("任务不存在")
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *App) taskAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	switch r.PathValue("action") {
	case "run":
		if err := a.startTask(r.PathValue("id")); err != nil {
			fail(w, 409, err)
			return
		}
	case "stop":
		a.runMu.Lock()
		if cancel, ok := a.running[r.PathValue("id")]; ok {
			cancel()
		}
		a.runMu.Unlock()
	default:
		http.NotFound(w, r)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		w.WriteHeader(405)
		return
	}
	var s Settings
	if !decode(w, r, &s) {
		return
	}
	// Session policy changes go through the account endpoint and rotate sessions.
	s.SessionDays = a.store.snapshot().Settings.SessionDays
	if s.CacheTTL < 1 || s.CacheMaxItems < 1 || s.CacheMaxItems > 1000000 || s.CacheMemoryMB < 1 || s.CacheMemoryMB > 4096 || s.SnapshotInterval < 1 {
		fail(w, 400, errors.New("缓存设置超出有效范围"))
		return
	}
	u, err := url.Parse(s.PublicURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		fail(w, 400, errors.New("外部访问地址应为 HTTP(S) 源站地址，不能包含子路径或参数"))
		return
	}
	if err := a.store.update(func(st *State) error { st.Settings = s; return nil }); err != nil {
		fail(w, 500, err)
		return
	}
	a.cache.clear()
	if err := os.Remove(filepath.Join(a.dataDir, "cache.json")); err != nil && !os.IsNotExist(err) {
		a.logger.Printf("remove snapshot: %v", err)
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *App) account(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		w.WriteHeader(405)
		return
	}
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	c.Username = strings.TrimSpace(c.Username)
	if c.SessionDays == 0 {
		c.SessionDays = 7
	}
	if c.SessionDays < 1 || c.SessionDays > 365 {
		fail(w, 400, errors.New("会话有效期必须为 1–365 天"))
		return
	}
	if len(c.Password) == 0 || len(c.Password) > 72 || c.Username == "" || len(c.Username) > 150 {
		fail(w, 400, errors.New("请输入账号和非空密码，密码不能超过 72 字节"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.store.update(func(st *State) error {
		for _, user := range st.DAVUsers {
			if strings.EqualFold(user.Username, c.Username) {
				return errors.New("账号与 WebDAV 用户重名")
			}
		}
		st.Username = c.Username
		st.Password = string(hash)
		st.Settings.SessionDays = c.SessionDays
		return nil
	}); err != nil {
		fail(w, 500, err)
		return
	}
	a.sessionMu.Lock()
	a.sessions = map[string]time.Time{}
	a.sessionMu.Unlock()
	a.setSession(w, r)
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

type countWriter struct {
	http.ResponseWriter
	count *atomic.Int64
}

func (w countWriter) Write(b []byte) (int, error) {
	n, e := w.ResponseWriter.Write(b)
	w.count.Add(int64(n))
	return n, e
}
func (w countWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (a *App) stream(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return
	}
	token := r.PathValue("token")
	mac := hmac.New(sha256.New, []byte(a.store.snapshot().SignKey))
	mac.Write([]byte(token))
	sig, err := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("sign"))
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		fail(w, 403, errors.New("无效的播放签名"))
		return
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	var claim streamClaim
	if err != nil || json.Unmarshal(b, &claim) != nil {
		w.WriteHeader(400)
		return
	}
	s, err := a.store.storage(claim.Storage)
	if err != nil {
		fail(w, 404, err)
		return
	}
	var d Download
	if claim.CAS != nil {
		var release func()
		d, release, err = a.casDownload(r.Context(), s, claim)
		defer release()
	} else {
		d, err = a.download(r.Context(), s, claim.File, claim.Pick)
	}
	if err != nil {
		fail(w, 502, err)
		return
	}
	cw := countWriter{w, &a.downloaded}
	if s.Type == "local" {
		root, err := os.OpenRoot(s.Config["root"])
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer root.Close()
		f, err := root.Open(d.Local)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(cw, r, info.Name(), info.ModTime(), f)
		return
	}
	target, _ := url.Parse(d.URL)
	proxy := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		pr.Out.URL = target
		pr.Out.Host = target.Host
		pr.Out.Header = d.Headers.Clone()
		for _, key := range []string{"Range", "If-Range", "If-Modified-Since", "If-None-Match"} {
			if v := pr.In.Header.Get(key); v != "" {
				pr.Out.Header.Set(key, v)
			}
		}
	}, ModifyResponse: func(res *http.Response) error {
		res.Header.Del("Set-Cookie")
		res.Header.Del("WWW-Authenticate")
		return nil
	}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, e error) {
		fail(w, 502, errors.New("上游媒体读取失败"))
	}}
	proxy.ServeHTTP(cw, r)
}

func (a *App) serveDAV(w http.ResponseWriter, r *http.Request) {
	st := a.store.snapshot()
	if !st.Settings.WebDAVEnabled {
		http.NotFound(w, r)
		return
	}
	user, pass, ok := r.BasicAuth()
	authorized := false
	if ok && len(pass) <= 72 && a.allowLogin(r) {
		if user == st.Username {
			authorized = bcrypt.CompareHashAndPassword([]byte(st.Password), []byte(pass)) == nil
		} else {
			for _, u := range st.DAVUsers {
				if u.Enabled && u.Username == user && bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(pass)) == nil {
					grants := append([]DAVGrant{}, u.Grants...)
					r = r.WithContext(context.WithValue(r.Context(), davGrantsKey{}, grants))
					authorized = true
					break
				}
			}
		}
		// Successful DAV requests are not login attempts; do not throttle playback.
		if authorized {
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)
			a.loginMu.Lock()
			delete(a.loginAttempts, ip)
			a.loginMu.Unlock()
		}
	}
	if !authorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="Aether WebDAV"`)
		w.WriteHeader(401)
		return
	}
	switch r.Method {
	case "GET", "HEAD", "PROPFIND", "OPTIONS":
		a.dav.ServeHTTP(countWriter{w, &a.downloaded}, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, PROPFIND, OPTIONS")
		w.WriteHeader(405)
	}
}
