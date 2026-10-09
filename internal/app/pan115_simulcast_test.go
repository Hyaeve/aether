package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func simulcastFixture(t *testing.T, mode string) (*pan115Simulcast, Storage, *int) {
	t.Helper()
	a := testApp(t)
	s := Storage{ID: "sim", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	if err := a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, s)
		st.Simulcast = map[string]SimulcastConfig{s.ID: {Enabled: true, Directory: "99"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	count := 0
	names := map[string]string{}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	file := func(fid, name string) map[string]any {
		parent, pick, sha := "99", "copy"+fid, strings.Repeat("a", 40)
		if fid == "1" {
			parent, pick = "0", "original"
		}
		if mode == "hash" && fid != "1" {
			sha = strings.Repeat("b", 40)
		}
		return map[string]any{"fid": fid, "cid": parent, "n": name, "s": "42", "pc": pick, "sha": sha}
	}
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		var body any
		switch r.URL.Path {
		case "/files/get_info":
			fid := r.URL.Query().Get("file_id")
			name := names[fid]
			if fid == "1" {
				name = "movie.mkv"
			}
			body = map[string]any{"state": true, "data": []any{file(fid, name)}}
		case "/files":
			if r.URL.Query().Get("cid") != "99" {
				t.Errorf("incorrect target: %s", r.URL)
			}
			items := []any{}
			for fid, name := range names {
				items = append(items, file(fid, name))
			}
			if mode == "collision" {
				items = append(items, file("77", "movie.mkv"))
			}
			body = map[string]any{"state": true, "cid": "99", "count": len(items), "offset": 0, "data": items}
		case "/files/copy":
			_ = r.ParseForm()
			if r.Form.Get("pid") != "99" || r.Form.Get("fid[0]") != "1" {
				t.Errorf("wrong copy form: %v", r.Form)
			}
			count++
			names[fmt.Sprint(100+count)] = "movie.mkv"
			if mode == "ambiguous" {
				names["900"] = "movie.mkv"
			}
			if mode == "uncertain" {
				return nil, fmt.Errorf("lost response")
			}
			body = map[string]any{"state": true}
		case "/files/batch_rename":
			_ = r.ParseForm()
			fid := r.Form.Get("fid")
			if fid == "1" || names[fid] == "" {
				t.Errorf("renamed unverified file %q", fid)
			}
			if mode != "rename" {
				names[fid] = r.Form.Get("file_name")
			}
			body = map[string]any{"state": true}
		default:
			t.Errorf("unexpected HTTP request: %s", r.URL)
			body = map[string]any{"state": false}
		}
		data, _ := json.Marshal(body)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(data))), Request: r}, nil
	})}
	return newPan115Simulcast(a, []string{"10.0.0.0/8"}), s, &count
}

func TestPan115SimulcastHTTPPlaybackAndDownloadBypass(t *testing.T) {
	a := testApp(t)
	if a.simulcast == nil {
		t.Fatal("App did not initialize simulcast")
	}
	s := Storage{ID: "http115", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{s}
		// Model an invalid imported/legacy configuration, not a valid API PUT.
		st.Simulcast = map[string]SimulcastConfig{s.ID: {Enabled: true, Directory: "invalid"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f := File{ID: "123", PickCode: "cq391yoe47hu5dd6u", Name: "movie.mkv"}
	readable, err := a.publicSTRMURL(s, f)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Path != "/app/chrome/downurl" || r.UserAgent() != "Simulcast-HTTP/1" {
			t.Errorf("unexpected upstream request: %s UA=%q", r.URL, r.UserAgent())
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"state":false,"errno":10008,"error":"mock-download-rejected"}`)), Request: r}, nil
	})}
	server := httptest.NewServer(a.Handler(t.TempDir()))
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	do := func(method, address string, status int, message string, wantCalls int32) {
		t.Helper()
		u, err := url.Parse(address)
		if err != nil {
			t.Fatal(err)
		}
		r, err := http.NewRequest(method, server.URL+u.RequestURI(), nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("User-Agent", "Simulcast-HTTP/1")
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != status || (method != "HEAD" && !strings.Contains(string(body), message)) || calls.Load() != wantCalls {
			t.Fatalf("%s %s: status=%d body=%s calls=%d err=%v", method, u.Path, res.StatusCode, body, calls.Load(), err)
		}
	}
	signed := a.streamURL(s.ID, f.ID, f.PickCode)
	for _, address := range []string{signed, readable} {
		for _, method := range []string{"GET", "HEAD"} {
			do(method, address, 502, "同播文件或目录无效", 0)
		}
	}
	bad, _ := url.Parse(signed)
	q := bad.Query()
	q.Set("sign", "invalid")
	bad.RawQuery = q.Encode()
	do("GET", bad.String(), 403, "无效的播放签名", 0)
	do("GET", bad.String()+"&download=1", 403, "无效的播放签名", 0)
	do("GET", "/d/unregistered123.mkv", 404, "404", 0)
	// Both remain 502 because the real driver rejects our mock API response;
	// reaching it (and not the hook error) proves the HTTP download bypass.
	do("GET", signed+"&download=1", 502, "115 获取下载链接失败", 1)
	do("HEAD", signed+"&download=1", 502, "", 2)
	if len(a.simulcast.sessions) != 0 {
		t.Fatal("invalid playback or download created sessions")
	}
	if err := a.store.update(func(st *State) error { st.Simulcast = map[string]SimulcastConfig{}; return nil }); err != nil {
		t.Fatal(err)
	}
	do("GET", readable, 502, "115 获取下载链接失败", 3)
}

func TestPan115SimulcastRegisteredSettingsAndToolsRestore(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "persist115", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	configs := map[string]SimulcastConfig{s.ID: {Enabled: true, Directory: "99", DirectoryLabel: "Copies"}}
	for _, method := range []string{"GET", "PUT"} {
		if w := request(t, h, method, "/api/115-simulcast", configs, nil); w.Code != 401 {
			t.Fatal("route lacks auth", method, w.Code)
		}
	}
	if w := request(t, h, "PUT", "/api/115-simulcast", configs, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.state.ModuleVersion != 3 {
		t.Fatal("configuration missing from JSON tool module")
	}
	if err = reopened.initTools(filepath.Join(a.dataDir, "tools")); err != nil {
		t.Fatal(err)
	}
	if got := reopened.snapshot().Simulcast[s.ID]; got != configs[s.ID] {
		t.Fatal("tool snapshot lost simulcast", got)
	}
	w := request(t, h, "GET", "/api/115-simulcast", nil, cookie)
	var got map[string]SimulcastConfig
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(w.Body.Bytes(), &got) != nil || got[s.ID] != configs[s.ID] {
		t.Fatal(w.Code, w.Body.String())
	}
}

func simulcastRequest(ip, ua string) *http.Request {
	r := httptest.NewRequest("GET", "http://localhost/d/movie", nil)
	r.RemoteAddr = "10.0.0.2:54321"
	r.Header.Set("X-Forwarded-For", ip)
	r.Header.Set("User-Agent", ua)
	return r
}

func TestPan115SimulcastCopyReuseAndConcurrency(t *testing.T) {
	p, s, count := simulcastFixture(t, "")
	source := File{ID: "1", PickCode: "original"}
	first, err := p.PlayFile(simulcastRequest("192.0.2.1", "Player"), s, source)
	if err != nil || first.ID != "1" || *count != 0 {
		t.Fatal(first, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, e := p.PlayFile(simulcastRequest("192.0.2.2", "Player"), s, source)
			if e != nil || f.ID != "101" || f.Name != "movie (1).mkv" || f.PickCode != "copy101" {
				t.Errorf("copy: %+v %v", f, e)
			}
		}()
	}
	wg.Wait()
	if *count != 1 {
		t.Fatal("duplicate copy", *count)
	}
	f, err := p.PlayFile(simulcastRequest("192.0.2.2", "OtherPlayer"), s, source)
	if err != nil || f.ID != "102" || f.Name != "movie (2).mkv" || *count != 2 {
		t.Fatal(f, err, *count)
	}
}

func TestPan115SimulcastRejectUncertainResults(t *testing.T) {
	for _, mode := range []string{"hash", "collision", "ambiguous", "uncertain", "rename"} {
		t.Run(mode, func(t *testing.T) {
			p, s, count := simulcastFixture(t, mode)
			source := File{ID: "1", PickCode: "original"}
			_, _ = p.PlayFile(simulcastRequest("192.0.2.1", "Player"), s, source)
			if _, err := p.PlayFile(simulcastRequest("192.0.2.2", "Player"), s, source); err == nil {
				t.Fatal("unsafe copy accepted")
			}
			before := *count
			if _, err := p.PlayFile(simulcastRequest("192.0.2.2", "Player"), s, source); err == nil || *count != before {
				t.Fatal("failed mutation replayed", err, *count)
			}
		})
	}
}

func TestPan115SimulcastExpiryBoundsAndBypass(t *testing.T) {
	p, s, count := simulcastFixture(t, "")
	now := time.Now()
	p.now = func() time.Time { return now }
	source := File{ID: "1", PickCode: "original"}
	_, _ = p.PlayFile(simulcastRequest("192.0.2.1", "Player"), s, source)
	now = now.Add(simulcastWindow)
	f, err := p.PlayFile(simulcastRequest("192.0.2.2", "Player"), s, source)
	if err != nil || f.ID != "1" || *count != 0 {
		t.Fatal("expiry", f, err)
	}
	r := simulcastRequest("192.0.2.3", "Player")
	r.URL.RawQuery = "download=1"
	if f, err = p.PlayFile(r, s, source); err != nil || f.ID != "1" || *count != 0 {
		t.Fatal("download was intercepted")
	}
	for _, clients := range p.sessions {
		for i := len(clients); i < simulcastClients; i++ {
			clients[simulcastHash(i)] = &simulcastClient{seen: now, file: source}
		}
	}
	if _, err = p.PlayFile(simulcastRequest("192.0.2.4", "Player"), s, source); err == nil {
		t.Fatal("client bound ignored")
	}
	if err = p.app.store.update(func(st *State) error { delete(st.Simulcast, s.ID); return nil }); err != nil {
		t.Fatal(err)
	}
	if f, err = p.PlayFile(simulcastRequest("192.0.2.5", "Player"), s, source); err != nil || f.ID != "1" {
		t.Fatal("disabled hook", err)
	}
}

func TestPan115SimulcastSettings(t *testing.T) {
	p, s, _ := simulcastFixture(t, "")
	a := p.app
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	api := http.HandlerFunc(a.pan115SimulcastSettings)
	if request(t, api, "GET", "/api/115-simulcast", nil, nil).Code != 401 {
		t.Fatal("unauthenticated settings")
	}
	for _, value := range []any{map[string]SimulcastConfig{"unknown": {Directory: "99"}}, map[string]SimulcastConfig{s.ID: {Directory: "../x"}}, nil} {
		if request(t, api, "PUT", "/api/115-simulcast", value, cookie).Code != 400 {
			t.Fatal("invalid settings accepted")
		}
	}
	res := request(t, api, "PUT", "/api/115-simulcast", map[string]SimulcastConfig{s.ID: {Enabled: true, Directory: "/"}}, cookie)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"directory":"0"`) {
		t.Fatal(res.Body.String())
	}
	if request(t, api, "PUT", "/api/115-simulcast", map[string]SimulcastConfig{}, cookie).Code != 200 || len(a.store.snapshot().Simulcast) != 0 {
		t.Fatal("delete failed")
	}
}

func TestPan115SimulcastClientTrustAndCancellation(t *testing.T) {
	p, s, count := simulcastFixture(t, "")
	source := File{ID: "1", PickCode: "original"}
	r := simulcastRequest("192.0.2.1", "Player")
	r.RemoteAddr = "198.51.100.1:1000"
	_, _ = p.PlayFile(r, s, source)
	r.Header.Set("X-Forwarded-For", "192.0.2.200")
	r.RemoteAddr = "198.51.100.1:2000"
	f, err := p.PlayFile(r, s, source)
	if err != nil || f.ID != "1" || *count != 0 {
		t.Fatal("untrusted forwarding or port changed identity", f, err)
	}
	p.gate <- struct{}{}
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	if _, err = p.PlayFile(r.WithContext(ctx), s, source); err != context.Canceled {
		t.Fatal("cancel ignored", err)
	}
	<-p.gate
	changed := s
	changed.Config = map[string]string{"cookie": "different-account"}
	if _, err = p.PlayFile(r, changed, source); err == nil {
		t.Fatal("stale account accepted")
	}
}

func TestPan115SimulcastMemoryCaps(t *testing.T) {
	for _, totalLimit := range []bool{false, true} {
		t.Run(fmt.Sprint(totalLimit), func(t *testing.T) {
			p, s, count := simulcastFixture(t, "")
			now := time.Now()
			p.now = func() time.Time { return now }
			for i := 0; i < simulcastFiles; i++ {
				clients := map[[32]byte]*simulcastClient{}
				limit := 1
				if totalLimit {
					limit = simulcastTotal / simulcastFiles
				}
				for j := 0; j < limit; j++ {
					clients[simulcastHash(j)] = &simulcastClient{seen: now}
				}
				p.sessions[simulcastHash(i)] = clients
			}
			source := File{ID: "1", PickCode: "original"}
			if _, err := p.PlayFile(simulcastRequest("192.0.2.1", "Player"), s, source); err == nil || *count != 0 {
				t.Fatal("capacity ignored")
			}
			now = now.Add(simulcastWindow)
			if f, err := p.PlayFile(simulcastRequest("192.0.2.1", "Player"), s, source); err != nil || f.ID != "1" || len(p.sessions) != 1 {
				t.Fatal("expired capacity retained", err)
			}
		})
	}
}
