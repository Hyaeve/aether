package app

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

func Test115AuthenticatedCDNHeadersTrustBoundary(t *testing.T) {
	for _, tc := range []struct {
		address string
		login   bool
	}{
		{"https://cdnfhnfile.115cdn.net/book", true}, {"https://115cdn.com/book", true}, {"https://cdn.115.com/book", true},
		{"http://cdn.115cdn.net/book", false}, {"https://115cdn.net.attacker.invalid/book", false},
		{"https://evil115cdn.net/book", false}, {"https://cdn.115cdn.net:444/book", false}, {"https://unrelated.invalid/book", false},
	} {
		address := tc.address
		t.Run(address, func(t *testing.T) {
			jar, _ := cookiejar.New(nil)
			info := &driver.DownloadInfo{Url: driver.FileDownloadUrl{Url: address}, Header: http.Header{
				"Cookie":        {"UID=uid; CID=cid; SEID=seid; KID=kid; irrelevant=other; Path=/"},
				"Authorization": {"Bearer secret"}, "Content-Type": {"application/x-www-form-urlencoded"},
			}}
			bind115CDNTickets(info, jar, []*http.Cookie{{Name: "download_ticket", Value: "ticket"}})
			got, err := finish115Download(info, jar, pan115ReadUA, "test")
			if err != nil {
				t.Fatal(err)
			}
			cookies := (&http.Request{Header: got.Header}).Cookies()
			want := 1
			if tc.login {
				want = 5
			}
			if len(cookies) != want || got.Header.Get("Authorization") != "" || got.Header.Get("Content-Type") != "" || strings.Contains(got.Header.Get("Cookie"), "Path=") || strings.Contains(got.Header.Get("Cookie"), "irrelevant") {
				t.Fatal("download credential boundary violated", got.Header)
			}
		})
	}
}

// Exercise a real HTTP wire read: a CDN requiring the SDK's CK rejects the old
// response-ticket-only header, even when refreshing that response ticket.
func Test115DAVMountAuthenticatedWireRead(t *testing.T) {
	cdn := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name, value := range map[string]string{"UID": "uid", "CID": "cid", "SEID": "seid", "download_ticket": "ticket"} {
			cookie, err := r.Cookie(name)
			if err != nil || cookie.Value != value || r.UserAgent() != pan115ReadUA {
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, `{"message":"no cookie value"}`)
				return
			}
		}
		if r.Header.Get("Range") == "bytes=4-" {
			w.Header().Set("Content-Range", "bytes 4-7/8")
			w.WriteHeader(http.StatusPartialContent)
			io.WriteString(w, "able")
			return
		}
		io.WriteString(w, "readable")
	}))
	defer cdn.Close()
	oldRequest, _ := http.NewRequest(http.MethodGet, cdn.URL+"/book", nil)
	oldRequest.Header.Set("User-Agent", pan115ReadUA)
	oldRequest.AddCookie(&http.Cookie{Name: "download_ticket", Value: "ticket"})
	rejected, err := cdn.Client().Do(oldRequest)
	if err != nil {
		t.Fatal(err)
	}
	rejected.Body.Close()
	if rejected.StatusCode != http.StatusForbidden {
		t.Fatal("response-ticket-only regression did not reproduce", rejected.StatusCode)
	}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	a := testApp(t)
	s := Storage{ID: "authenticated-read", Name: "115-books", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie, "device": "qandroid"}}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	for _, mounted := range []bool{false, true} {
		t.Run(map[bool]string{false: "WebDAV", true: "mount"}[mounted], func(t *testing.T) {
			cdnCalls := 0
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/files" {
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Request: r, Body: io.NopCloser(strings.NewReader(`{"state":true,"cid":"0","count":1,"data":[{"fid":"file","cid":"0","n":"book.epub","s":"8","pc":"pick"}]}`))}, nil
				}
				cdnCalls++
				copy := r.Clone(r.Context())
				target, _ := url.Parse(cdn.URL)
				copy.URL.Scheme, copy.URL.Host = target.Scheme, target.Host
				return cdn.Client().Transport.RoundTrip(copy)
			})}
			var f *davFile
			if mounted {
				got, err := (mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: "0"}}).OpenFile(context.Background(), "/book.epub", os.O_RDONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				f = got.(*davFile)
			} else {
				got, err := (davFS{a}).OpenFile(context.Background(), "/115-books/book.epub", os.O_RDONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				f = got.(*davFile)
			}
			defer f.Close()
			// The upstream RSA response cannot be generated without 115's private
			// key. Supply decoded SDK data, retain production header/reader logic.
			f.open = nil
			info := &driver.DownloadInfo{Url: driver.FileDownloadUrl{Url: "https://cdnfhnfile.115cdn.net/book"}, Header: http.Header{"Cookie": {"UID=uid; CID=cid; SEID=seid"}}}
			jar, _ := cookiejar.New(nil)
			bind115CDNTickets(info, jar, []*http.Cookie{{Name: "download_ticket", Value: "ticket"}})
			info, err := finish115Download(info, jar, pan115ReadUA, "android")
			if err != nil {
				t.Fatal(err)
			}
			f.download = Download{URL: info.Url.Url, Headers: info.Header}
			data, err := io.ReadAll(f)
			if err != nil || string(data) != "readable" || f.refreshed {
				t.Fatal(string(data), err, f.refreshed)
			}
			if _, err := f.Seek(4, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			data, err = io.ReadAll(f)
			if err != nil || string(data) != "able" || cdnCalls != 2 {
				t.Fatal(string(data), err, cdnCalls)
			}
		})
	}
}

func Test115InternalReadUsesBrowserUAButExplicitClientIsPreserved(t *testing.T) {
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	a := testApp(t)
	s := Storage{Type: "115", Config: map[string]string{"cookie": test115Cookie, "device": "qandroid"}}
	for _, ua := range []string{pan115ReadUA, "Client/1"} {
		calls := 0
		apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.UserAgent() != ua || r.URL.Path != "/android/2.0/ufile/download" {
				t.Fatal(r.Header, r.URL.Path)
			}
			return &http.Response{StatusCode: 200, Request: r, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"state":false,"errno":99}`))}, nil
		})}
		if ua == pan115ReadUA {
			a.download(context.Background(), s, "file", "pick")
		} else {
			a.downloadWithUA(context.Background(), s, "file", "pick", ua)
		}
		if calls != 1 {
			t.Fatal(calls)
		}
	}
}

func Test115AuthenticatedReadDropsCKOnCrossHostRedirect(t *testing.T) {
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			if !strings.Contains(r.Header.Get("Cookie"), "CID=cid") {
				t.Fatal("initial official CDN lost CK")
			}
			return &http.Response{StatusCode: 302, Request: r, Header: http.Header{"Location": {"https://unrelated.invalid/book"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		if calls != 2 || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Proxy-Authorization") != "" {
			t.Fatal("redirect leaked credentials", r.Header)
		}
		return &http.Response{StatusCode: 200, Request: r, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("readable"))}, nil
	})}
	jar, _ := cookiejar.New(nil)
	info, err := finish115Download(&driver.DownloadInfo{Url: driver.FileDownloadUrl{Url: "https://cdnfhnfile.115cdn.net/book"}, Header: http.Header{"Cookie": {"CID=cid"}}}, jar, pan115ReadUA, "test")
	if err != nil {
		t.Fatal(err)
	}
	f := &davFile{ctx: context.Background(), info: davInfo{File{Size: 8}}, download: Download{URL: info.Url.Url, Headers: info.Header}}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil || string(data) != "readable" || calls != 2 {
		t.Fatal(string(data), err, calls)
	}
}
