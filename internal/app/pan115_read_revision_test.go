package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
	"golang.org/x/net/publicsuffix"
)

func Test115ReadEndpointAndPrivateDiagnostics(t *testing.T) {
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	for _, device := range []string{"", "web", "android", "ios", "tv", "qandroid", "alipaymini", "wechatmini"} {
		t.Run(device, func(t *testing.T) {
			calls := 0
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				want := "/app/chrome/downurl"
				if device == "android" {
					want = "/android/2.0/ufile/download"
				}
				if r.URL.Path != want || r.UserAgent() != "Reader/1" || !strings.Contains(r.Header.Get("Cookie"), "CID=cid") {
					t.Fatal("incorrect download request", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"state":false,"errno":987654,"error":"SEID=private-secret https://cdn.invalid/?token=private-token"}`)), Request: r}, nil
			})}
			s := Storage{Type: "115", Config: map[string]string{"device": device, "cookie": test115Cookie}}
			_, err := download115(context.Background(), s, "pick", "Reader/1")
			if err == nil || !strings.Contains(err.Error(), "987654") || !strings.Contains(err.Error(), "HTTP 200") || strings.Contains(err.Error(), "private-") || calls != 1 {
				t.Fatal("unsafe or insufficient diagnostic", err, calls)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := download115(ctx, s, "pick", "Reader/1"); !errors.Is(err, context.Canceled) || calls != 1 {
				t.Fatal("cancellation ignored", err)
			}
		})
	}
}

func Test115ReadHeadersAreResponseScoped(t *testing.T) {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	origin, _ := url.Parse("https://proapi.115.com/app/chrome/downurl")
	set115DownloadCookies(jar, origin, []*http.Cookie{
		{Name: "UID", Value: "account-secret", Domain: ".115.com", Path: "/"},
		{Name: "SEID", Value: "login-secret", Domain: ".115.com", Path: "/"},
		{Name: "download_ticket", Value: "ticket", Domain: ".115.com", Path: "/", Secure: true},
		{Name: "host_only", Value: "private", Path: "/"},
		{Name: "bad", Value: "bad", Domain: ".com", Path: "/"},
	})
	for _, address := range []string{"https://cdn.115.com/file", "https://cdn.invalid/file", "http://cdn.115.com/file"} {
		info := &driver.DownloadInfo{Url: driver.FileDownloadUrl{Url: address}, Header: http.Header{"Cookie": {"CID=login-secret"}, "Authorization": {"Bearer private"}, "Content-Type": {"application/x-www-form-urlencoded"}}}
		got, err := finish115Download(info, jar, "Reader/1", "test")
		if err != nil {
			t.Fatal(err)
		}
		if got.Header.Get("User-Agent") != "Reader/1" || got.Header.Get("Authorization") != "" || got.Header.Get("Content-Type") != "" {
			t.Fatal("unexpected headers", got.Header)
		}
		want := ""
		if address == "https://cdn.115.com/file" {
			want = "download_ticket=ticket"
		}
		if got.Header.Get("Cookie") != want {
			t.Fatal("credential leak", got.Header.Get("Cookie"))
		}
	}
	for _, address := range []string{"", "javascript:alert(1)", "https://user:secret@cdn.115.com/file", "/relative"} {
		if _, err := finish115Download(&driver.DownloadInfo{Url: driver.FileDownloadUrl{Url: address}}, jar, "", "test"); err == nil {
			t.Fatal("unsafe URL accepted", address)
		}
	}
}
