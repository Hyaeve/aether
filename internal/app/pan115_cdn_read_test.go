package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
	"golang.org/x/net/publicsuffix"
)

func cdn115TestDownload(t *testing.T, ticket string) Download {
	t.Helper()
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	info := &driver.DownloadInfo{Url: driver.FileDownloadUrl{Url: "https://cdn.115cdn.net/book?private-url=" + ticket}}
	bind115CDNTickets(info, jar, []*http.Cookie{
		{Name: "download_ticket", Value: ticket, Domain: ".115.com", Path: "/app/", Secure: true},
		{Name: "UID", Value: "private-login", Domain: ".115.com"},
		{Name: "CID", Value: "private-login"},
		{Name: "SEID", Value: "private-login"},
		{Name: "KID", Value: "private-login"},
	})
	got, err := finish115Download(info, jar, pan115UA, "test")
	if err != nil {
		t.Fatal(err)
	}
	if got.Header.Get("Cookie") != "download_ticket="+ticket {
		t.Fatal("response ticket missing or login credential leaked", got.Header)
	}
	for _, address := range []string{"https://other.115cdn.net/file", "http://cdn.115cdn.net/file", "https://proapi.115.com/app/file"} {
		u, _ := url.Parse(address)
		if len(jar.Cookies(u)) != 0 {
			t.Fatal("ticket escaped returned HTTPS host", address)
		}
	}
	return Download{URL: got.Url.Url, Headers: got.Header}
}

func Test115CDNReadRefreshPreservesOffsetAndHeaders(t *testing.T) {
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	for _, offset := range []int64{0, 7} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			calls, refreshes := 0, 0
			var bodies []*seekRevisionBody
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				wantRange := ""
				if offset > 0 {
					wantRange = fmt.Sprintf("bytes=%d-", offset)
				}
				if r.Header.Get("Range") != wantRange || r.UserAgent() != pan115UA || r.Referer() != "https://115.com/" || r.Header.Get("Accept-Encoding") != "identity" {
					t.Fatal("CDN headers or offset changed", r.Header)
				}
				ticket := "old"
				status, text := 403, "private-error-body"
				if calls > 1 {
					ticket, status, text = "new", 206, "readable"
				}
				if r.Header.Get("Cookie") != "download_ticket="+ticket {
					t.Fatal("stale ticket or credential leak", r.Header)
				}
				body := &seekRevisionBody{Reader: strings.NewReader(text)}
				bodies = append(bodies, body)
				return &http.Response{StatusCode: status, Body: body, Header: http.Header{}, Request: r}, nil
			})}
			f := &davFile{ctx: context.Background(), info: davInfo{File{Name: "book.epub", Size: 100}}, offset: offset, download: cdn115TestDownload(t, "old")}
			f.refreshDownload = func() (Download, error) {
				refreshes++
				if !bodies[0].closed {
					t.Fatal("rejected response not closed before refresh")
				}
				return cdn115TestDownload(t, "new"), nil
			}
			buf := make([]byte, 8)
			n, err := f.Read(buf)
			if err != nil || string(buf[:n]) != "readable" || calls != 2 || refreshes != 1 || f.offset != offset+8 {
				t.Fatal(n, err, calls, refreshes, f.offset)
			}
			if err := f.Close(); err != nil || !bodies[1].closed {
				t.Fatal("final body not closed", err)
			}
		})
	}
}

func Test115CDNReadRefreshBoundedAndPrivate(t *testing.T) {
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	for _, status := range []int{401, 403, 416, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls, refreshes := 0, 0
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("private-response")), Header: http.Header{}, Request: r}, nil
			})}
			f := &davFile{ctx: context.Background(), info: davInfo{File{Size: 100}}, download: cdn115TestDownload(t, "old")}
			f.refreshDownload = func() (Download, error) { refreshes++; return cdn115TestDownload(t, "new"), nil }
			_, err := f.Read(make([]byte, 1))
			wantRefreshes := 0
			if status == 401 || status == 403 {
				wantRefreshes = 1
			}
			if err == nil || strings.Contains(err.Error(), "private-") || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status)) || refreshes != wantRefreshes || calls != 1+wantRefreshes {
				t.Fatal("unbounded retry or unsafe diagnostic", err, calls, refreshes)
			}
			if _, err := f.Seek(7, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			f.Read(make([]byte, 1))
			if refreshes != wantRefreshes {
				t.Fatal("seek reset retry budget", refreshes)
			}
		})
	}
}

func Test115CDNReadRefreshCancellation(t *testing.T) {
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
	})}
	f := &davFile{ctx: ctx, info: davInfo{File{Size: 100}}, download: cdn115TestDownload(t, "old")}
	f.refreshDownload = func() (Download, error) { cancel(); return Download{}, ctx.Err() }
	if _, err := f.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatal("refresh lost cancellation", err)
	}
}

func Test115DAVAndMountUseProductionRefresh(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "115-read", Name: "115-books", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie, "device": "web"}}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	for _, mounted := range []bool{false, true} {
		t.Run(fmt.Sprint(mounted), func(t *testing.T) {
			refreshes := 0
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				status, raw := 200, ""
				switch r.URL.Path {
				case "/files":
					raw = `{"state":true,"cid":"0","count":1,"data":[{"fid":"file","cid":"0","n":"book.epub","s":"8","pc":"pick"}]}`
				case "/book":
					status = 403
				case "/app/chrome/downurl":
					refreshes++
					if !strings.Contains(r.Header.Get("Cookie"), "CID=cid") || r.UserAgent() != pan115UA {
						t.Fatal("refresh lost storage credentials or bound UA")
					}
					// Successful envelopes require 115's private RSA key. Verify
					// the real refresh path with a safe protocol error instead.
					raw = `{"state":false,"errno":987654,"error":"private-error"}`
				default:
					t.Fatal("unexpected request path", r.URL.Path)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(raw)), Header: http.Header{"Content-Type": {"application/json"}}, Request: r}, nil
			})}
			var file interface{ Close() error }
			var f *davFile
			if mounted {
				got, err := (mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: "0"}}).OpenFile(context.Background(), "/book.epub", os.O_RDONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				f, file = got.(*davFile), got
			} else {
				got, err := (davFS{a}).OpenFile(context.Background(), "/115-books/book.epub", os.O_RDONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				f, file = got.(*davFile), got
			}
			defer file.Close()
			// Supply a stale first ticket without forging an encrypted API success.
			f.open = nil
			f.download = cdn115TestDownload(t, "stale")
			if _, err := f.Read(make([]byte, 1)); err == nil || !strings.Contains(err.Error(), "票据刷新失败") || !strings.Contains(err.Error(), "987654") || strings.Contains(err.Error(), "private-") || refreshes != 1 {
				t.Fatal("DAV/mount did not invoke production refresh", err, refreshes)
			}
		})
	}
}
