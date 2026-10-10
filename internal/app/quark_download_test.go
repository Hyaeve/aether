package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuarkBrowserDownloadHeadersAndRotatedCookie(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "quark-download", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "__pus=old; account=keep"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	s, _ = a.store.storage(s.ID)
	oldAPI, oldTransport := apiClient, http.DefaultTransport
	t.Cleanup(func() { apiClient, http.DefaultTransport = oldAPI, oldTransport })
	calls := 0
	transport := casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.UserAgent() != quarkClientUA || r.Header.Get("Referer") != "https://pan.quark.cn/" {
			t.Fatal("incomplete Quark download headers", r.Header)
		}
		response := &http.Response{StatusCode: 200, Header: http.Header{}, Request: r}
		if r.URL.Host == "drive.quark.cn" {
			if r.Method != "POST" || r.URL.Path != "/1/clouddrive/file/download" || r.Header.Get("Content-Type") != "application/json" {
				t.Fatal("wrong download request")
			}
			response.Header.Add("Set-Cookie", "__pus=rotated; Path=/; Secure; HttpOnly")
			response.Header.Add("Set-Cookie", "unrelated=discard; Path=/")
			response.Body = io.NopCloser(strings.NewReader(`{"code":0,"status":200,"data":[{"download_url":"https://download.example/book"}]}`))
		} else {
			if r.Header.Get("Cookie") != "__pus=rotated; account=keep" || r.Header.Get("Range") != "bytes=0-3" {
				t.Fatal("missing rotated ticket or range", r.Header)
			}
			response.StatusCode = 206
			response.Header.Set("Content-Range", "bytes 0-3/4")
			response.Body = io.NopCloser(strings.NewReader("book"))
		}
		return response, nil
	})
	apiClient = &http.Client{Transport: transport}
	http.DefaultTransport = transport
	r := httptest.NewRequest("GET", a.streamURL(s.ID, "file", "")+"&download=1", nil)
	r.Header.Set("User-Agent", "ordinary-browser")
	r.Header.Set("Range", "bytes=0-3")
	w := httptest.NewRecorder()
	a.Handler(t.TempDir()).ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "book" || calls != 2 || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("proxy download failed", w.Code, w.Body.String(), calls)
	}
	if got := a.store.snapshot().Storages[0].Config["cookie"]; got != "__pus=rotated; account=keep" || s.Config["cookie"] != "__pus=old; account=keep" {
		t.Fatal("cookie rotation did not persist independently", got)
	}
	raw, err := os.ReadFile(filepath.Join(a.store.dir, "storage", "storage.json"))
	if err != nil || strings.Contains(string(raw), "rotated") {
		t.Fatal("rotated credentials were not encrypted", err)
	}
}

func TestQuarkDownloadFailureDiagnostics(t *testing.T) {
	a := testApp(t)
	s := Storage{Type: "quark", Config: map[string]string{"cookie": "secret"}}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"dns", &net.DNSError{Err: "secret", Name: "secret"}, "DNS"},
		{"timeout", &net.OpError{Op: "read", Err: context.DeadlineExceeded}, "超时"},
		{"tls", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, "TLS"},
		{"eof", io.EOF, "提前断开"},
		{"unknown", errors.New("secret cookie token"), "网络连接"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			apiClient = &http.Client{Transport: casTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, tc.err })}
			_, err := a.download(context.Background(), s, "file", "")
			if err == nil || !strings.Contains(err.Error(), "夸克取下载链接失败") || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "secret") || calls != 1 {
				t.Fatal("unsafe or unclassified failure", err, calls)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(upstreamConnectionError(ctx, &url.Error{Err: errors.New("secret")}), context.Canceled) {
		t.Fatal("cancellation was hidden")
	}
}

func TestQuarkDoesNotAbsorbCrossOriginCookies(t *testing.T) {
	a := testApp(t)
	s := Storage{Type: "quark", Config: map[string]string{"cookie": "__pus=old"}}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		final := r.Clone(r.Context())
		final.URL, _ = url.Parse("https://other.example/final")
		return &http.Response{StatusCode: 200, Header: http.Header{"Set-Cookie": {"__pus=untrusted; Path=/"}}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: final}, nil
	})}
	var result any
	updated, err := a.quarkJSON(context.Background(), s, "GET", "https://drive.quark.cn/test", nil, &result)
	if err != nil || updated.Config["cookie"] != s.Config["cookie"] {
		t.Fatal("absorbed a cookie from another origin", err)
	}
}

func TestQuarkCookieMergePreservesConfigAndIgnoresDeletedCookies(t *testing.T) {
	s := Storage{Config: map[string]string{"cookie": "account=keep; __pus=old", "root": "folder"}}
	updated := mergeQuarkCookies(s, http.Header{"Set-Cookie": {
		"__pus=discard; Max-Age=0",
		"__puus=extra; Path=/",
		"unrelated=discard; Path=/",
	}})
	if updated.Config["cookie"] != "account=keep; __pus=old; __puus=extra" || updated.Config["root"] != "folder" || s.Config["cookie"] != "account=keep; __pus=old" {
		t.Fatal("unsafe merge", updated.Config)
	}
}

func TestQuarkDownloadRejectsBusinessErrorsAndEmptyLinks(t *testing.T) {
	a := testApp(t)
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	for _, body := range []string{
		`{"code":31001,"status":401,"data":[{"download_url":"https://invalid.example"}]}`,
		`{"code":99,"status":400,"data":[{"download_url":"https://invalid.example"}]}`,
		`{"code":0,"status":200,"data":[{"download_url":""}]}`,
	} {
		apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}
		if _, err := a.download(context.Background(), Storage{Type: "quark", Config: map[string]string{}}, "file", ""); err == nil {
			t.Fatal("accepted rejected or empty link", body)
		}
	}
}

func TestQuarkCookieRotationCannotOverwriteEditedStorage(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "quark", Type: "quark", Config: map[string]string{"cookie": "__pus=old"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	s = a.store.snapshot().Storages[0]
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		a.store.update(func(st *State) error { st.Storages[0].Config["cookie"] = "__pus=edited"; return nil })
		return &http.Response{StatusCode: 200, Header: http.Header{"Set-Cookie": {"__pus=late; Path=/"}}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
	})}
	var result any
	_, err := a.quarkJSON(context.Background(), s, "GET", "https://drive.quark.cn/test", nil, &result)
	if err != nil || a.store.snapshot().Storages[0].Config["cookie"] != "__pus=edited" {
		t.Fatal("late response replaced edited credentials", err)
	}
}
