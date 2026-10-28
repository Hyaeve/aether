package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	lc "aether/internal/linkcore/config"
	"aether/internal/linkcore/proxy"
	"aether/internal/linkcore/resolver"
	"aether/internal/linkcore/upstream"
)

func TestShareSTRMBodyPreservesOriginalReadableFilename(t *testing.T) {
	card := quarkLibraryCard{Secret: "https://pan.quark.cn/s/sample?pwd=aB12"}
	for _, name := range []string{"第1集 [Ygm] 测试.mkv", "[Ygm] Hyouken no Majutsushi [01].mkv", "字面%20 #1?.mkv"} {
		want := strings.NewReplacer("%", "%25", "#", "%23", "?", "%3F").Replace(name)
		for _, body := range []string{
			virtual115FilenameURL("http://example.test", "sample", "aB12", name),
			strings.TrimSpace(quarkLibraryURL(card, "folder/"+name+".strm", "http://example.test")),
		} {
			if !strings.HasSuffix(body, "/"+want) {
				t.Fatalf("body did not keep original readable text: %q", body)
			}
			parsed, err := url.Parse(body)
			if err != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.HasSuffix(parsed.Path, "/"+name) {
				t.Fatal("filename changed URL semantics", parsed, err)
			}
		}
	}
}

func TestShareFilenameEscapesOnlyURLSyntaxAndControls(t *testing.T) {
	for _, name := range []string{"原始 [名称] 01.mkv", "字面%20?#.mkv", "名称.mkv ", "名称.mkv  ", "控制\t\n.mkv", "删除\x7f.mkv"} {
		value := shareFilenamePath(name)
		if strings.ContainsAny(value, "\t\n\r\x7f") || strings.TrimSpace(value) != value {
			t.Fatal("STRM text boundary changed", value)
		}
		u, err := url.Parse("http://example.test/quark-share/code/" + value)
		if err != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/quark-share/code/"+name {
			t.Fatal("original filename cannot round trip", name, value, u, err)
		}
	}
}

func TestTianyiCASGoldSizePreflightAvoidsCloudWrites(t *testing.T) {
	a := testApp(t)
	pool := Storage{ID: "t", Type: "tianyi", Enabled: true, Config: map[string]string{"username": "account", "password": "secret"}}
	pool.Usage = &storageUsageState{Stamp: storageRevision(pool), storageUsage: storageUsage{Membership: membership("gold", "黄金会员", time.Now().Add(time.Hour))}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{pool}; return nil }); err != nil {
		t.Fatal(err)
	}
	original := apiClient
	t.Cleanup(func() { apiClient = original })
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	info := CASInfo{Name: "large.mkv", Size: 200_000_000_001, MD5: strings.Repeat("a", 32), SliceMD5: strings.Repeat("b", 32)}
	_, release, err := a.casDownload(context.Background(), pool, streamClaim{Storage: pool.ID, CAS: &info})
	release()
	if err == nil || calls != 0 || !strings.Contains(err.Error(), "200 GB") {
		t.Fatal("gold account submitted an oversized CAS", calls, err)
	}
	if err := a.restoreCASInto(a.ctx, pool, "target", info); err == nil || calls != 0 {
		t.Fatal("offline CAS bypassed the gold preflight", calls, err)
	}
}

func TestTianyiCASGoldAndPlatinumLimitsAndUnknownMetadata(t *testing.T) {
	a := testApp(t)
	for _, tier := range []string{"gold", "platinum"} {
		pool := Storage{ID: "t", Type: "tianyi", Config: map[string]string{"username": "account", "password": "secret"}}
		pool.Usage = &storageUsageState{Stamp: storageRevision(pool), storageUsage: storageUsage{Membership: membership(tier, tier, time.Now().Add(time.Hour))}}
		if err := a.checkTianyiCASSize(a.ctx, pool, tianyiGoldCASLimit); err != nil {
			t.Fatal(tier, err)
		}
		err := a.checkTianyiCASSize(a.ctx, pool, tianyiGoldCASLimit+1)
		if (err == nil) != (tier == "platinum") {
			t.Fatal("wrong tier limit", tier, err)
		}
		if tier == "platinum" {
			if err := a.checkTianyiCASSize(a.ctx, pool, 1<<40); err != nil {
				t.Fatal("platinum inherited the gold cap", err)
			}
		}
	}
	original := apiClient
	t.Cleanup(func() { apiClient = original })
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	pool := Storage{ID: "t", Type: "tianyi", Config: map[string]string{"accessToken": "test"}}
	if err := a.checkTianyiCASSize(a.ctx, pool, tianyiGoldCASLimit+1); err == nil {
		t.Fatal("unknown entitlement authorized a file over200 GB")
	}
	for _, expired := range []bool{true, false} {
		member := membership("platinum", "铂金会员", time.Now().Add(time.Hour))
		if expired {
			past := time.Now().Add(-time.Second)
			member.ExpiresAt = &past
		} else {
			member.CheckedAt = time.Now().Add(-6 * time.Minute)
		}
		pool.Usage = &storageUsageState{Stamp: storageRevision(pool), storageUsage: storageUsage{Membership: member}}
		if err := a.checkTianyiCASSize(a.ctx, pool, tianyiGoldCASLimit+1); err == nil {
			t.Fatal("expired or stale platinum entitlement was reused")
		}
	}
	ctx, cancel := context.WithCancel(a.ctx)
	cancel()
	if err := a.checkTianyiCASSize(ctx, pool, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled preflight continued", err)
	}
}

func TestQuarkShareProbeDoesNotCreateCompletedDownload(t *testing.T) {
	a := testApp(t)
	mock := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(a.ctx, card); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(strings.TrimSpace(quarkLibraryURL(card, mock.files[0].Name+".strm", "http://example.test")))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, u.EscapedPath(), nil)
	r.Header.Set("Range", "bytes=0-0")
	w := httptest.NewRecorder()
	a.Handler(t.TempDir()).ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent || w.Body.String() != "0" || mock.saves != 1 {
		t.Fatal("bounded share preparation failed", w.Code, w.Body.String(), mock.saves)
	}
	a.transfers.mu.Lock()
	defer a.transfers.mu.Unlock()
	if len(a.transfers.items) != 0 {
		t.Fatal("one-byte probe created a completed download task")
	}
}

func TestQuarkShareLinkPlaybackPreparesTVAfterReadOnlyHEAD(t *testing.T) {
	a := testApp(t)
	mock := mockQuarkLibrary(t, a)
	mock.files[0].Name = "第1集 [Ygm].mkv"
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(a.ctx, card); err != nil {
		t.Fatal(err)
	}
	binding := quarkTVDefaults(QuarkTVBinding{Enabled: true, AccessToken: "access", Device: "device"})
	binding.CookieHash = quarkCookieHash(a.store.snapshot().Storages[0])
	if err := a.store.update(func(st *State) error { st.QuarkTV = map[string]QuarkTVBinding{"q": binding}; return nil }); err != nil {
		t.Fatal(err)
	}
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Emby-Token") != "" {
			t.Error("player credentials leaked to CDN")
		}
		io.WriteString(w, "0123456789")
	}))
	defer cdn.Close()
	transport := apiClient.Transport
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "open-api-drive.quark.cn" {
			raw, _ := json.Marshal(map[string]any{"status": 200, "data": map[string]any{"video_info": []any{
				map[string]any{"url": cdn.URL + "/movie.mp4", "accessable": 1, "resolution": "super", "format": "mp4"},
			}}})
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
		}
		return transport.RoundTrip(r)
	})
	service := httptest.NewServer(a.Handler(t.TempDir()))
	defer service.Close()
	pointer := strings.TrimSpace(quarkLibraryURL(card, mock.files[0].Name+".strm", service.URL))
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"Id": "movie", "MediaSources": []any{
			map[string]any{"Id": "source", "Path": pointer, "Protocol": "Http", "Container": "strm"},
		}})
	}))
	defer media.Close()
	provider, err := upstream.New(a.linkUpstream(MediaLink{ID: "test", Name: "test", Type: "emby", Address: media.URL, APIKey: "key"}))
	if err != nil {
		t.Fatal(err)
	}
	cfg := lc.Default()
	cfg.Redirect.Mode, cfg.Redirect.FollowUpstreamRedirects = lc.RedirectAlways, true
	r := resolver.New(cfg.Cache, cfg.Redirect)
	defer r.Close()
	handler := proxy.New(provider, r, a.links.stats, cfg.Redirect)
	for _, method := range []string{http.MethodHead, http.MethodGet, http.MethodGet} {
		req := httptest.NewRequest(method, "/Videos/movie/stream.mp4", nil)
		req.RemoteAddr = "192.168.1.20:9000"
		req.Header.Set("User-Agent", "Hills/1.10.1 (android; 16)")
		req.Header.Set("X-Emby-Token", "client-token")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if method == http.MethodHead {
			if mock.saves != 0 {
				t.Fatal("read-only HEAD saved a cloud file")
			}
		} else if res.Code != http.StatusFound || res.Header().Get("Location") != cdn.URL+"/movie.mp4" {
			t.Fatal("TV URL did not reach client", res.Code, res.Header(), res.Body.String())
		}
	}
	cfg.Redirect.Mode = lc.RedirectPublic
	handler = proxy.New(provider, r, a.links.stats, cfg.Redirect)
	req := httptest.NewRequest(http.MethodGet, "/Videos/movie/stream.mp4", nil)
	req.RemoteAddr = "192.168.1.20:9000"
	req.Header.Set("User-Agent", "Hills/1.10.1 (android; 16)")
	req.Header.Set("X-Emby-Token", "client-token")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != "0123456789" {
		t.Fatal("private client in public-redirect mode did not relay the final CDN", res.Code, res.Body.String())
	}
	if mock.saves != 1 {
		t.Fatal("playback did not reuse the confirmed temporary file", mock.saves)
	}
}
