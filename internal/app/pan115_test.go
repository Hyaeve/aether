package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

func Test115CookieValidation(t *testing.T) {
	for _, device := range []string{"web", "android", "ios", "tv", "alipaymini", "wechatmini", "qandroid"} {
		s := Storage{Name: "115", Type: "115", Config: map[string]string{"cookie": test115Cookie + "; extra=a=b;", "device": device, "accessToken": "old"}}
		if err := validateStorage(&s); err != nil || s.Config["accessToken"] != "" {
			t.Fatal(device, err)
		}
	}
	for _, config := range []map[string]string{
		{"accessToken": "old"}, {"cookie": "UID=1;CID=2"}, {"cookie": test115Cookie, "device": "invalid"}, {"cookie": test115Cookie + "\r\nX: invalid"},
	} {
		s := Storage{Name: "115", Type: "115", Config: config}
		if validateStorage(&s) == nil {
			t.Fatal("invalid credentials accepted")
		}
	}
}

func Test115OfficialQRDeviceAndSession(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	old := apiClient
	defer func() { apiClient = old }()
	loginCalls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		raw := ""
		switch r.URL.Path {
		case "/api/1.0/web/1.0/token":
			raw = `{"state":1,"data":{"uid":"qr-id","time":123,"sign":"qr-sign","qrcode":"https://115.com/scan/test"}}`
		case "/get/status/":
			if r.URL.Query().Get("uid") != "qr-id" || r.URL.Query().Get("sign") != "qr-sign" {
				t.Fatal("QR state lost")
			}
			raw = `{"state":1,"data":{"status":2}}`
		case "/app/1.0/ios/1.0/login/qrcode":
			r.ParseForm()
			if r.Form.Get("app") != "ios" || r.Form.Get("account") != "qr-id" {
				t.Fatal("device/session mismatch")
			}
			loginCalls++
			raw = `{"state":1,"data":{"cookie":{"UID":"123_A1","CID":"cid","SEID":"secret","KID":"kid"}}}`
		default:
			t.Fatal("unexpected authorization URL", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	start := request(t, h, "POST", "/api/authorization/115/start", map[string]string{"device": "ios"}, cookie)
	var qr struct{ Token, Image string }
	if start.Code != 200 || json.Unmarshal(start.Body.Bytes(), &qr) != nil || !strings.HasPrefix(qr.Image, "data:image/png;base64,") || strings.Contains(qr.Token, "qr-sign") {
		t.Fatal(start.Body.String())
	}
	poll := request(t, h, "POST", "/api/authorization/115/poll", map[string]string{"token": qr.Token}, cookie)
	var result map[string]string
	json.Unmarshal(poll.Body.Bytes(), &result)
	if poll.Code != 200 || result["device"] != "ios" || result["status"] != "success" || !strings.Contains(result["cookie"], "SEID=secret") || loginCalls != 1 {
		t.Fatal(poll.Body.String())
	}
	if poll.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credentials response cacheable")
	}
	if request(t, h, "POST", "/api/authorization/quark/poll", map[string]string{"token": qr.Token}, cookie).Code != 400 {
		t.Fatal("cross-provider session accepted")
	}
}

func Test115DownloadUploadAndCancellation(t *testing.T) {
	a := testApp(t)
	s := Storage{Type: "115", Config: map[string]string{"cookie": test115Cookie}}
	old := apiClient
	defer func() { apiClient = old }()
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "" || !strings.Contains(r.Header.Get("Cookie"), "CID=cid") {
			t.Fatal("incorrect auth")
		}
		switch r.URL.Path {
		case "/app/chrome/downurl":
			r.ParseForm()
			if r.Form.Get("data") == "" || r.UserAgent() != pan115ReadUA {
				t.Fatal("missing encrypted download payload or UA")
			}
		case "/app/uploadinfo":
		default:
			t.Fatal("unexpected driver endpoint", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"state":false,"errno":10008,"error":"rejected"}`)), Request: r}, nil
	})}
	if _, err := a.download(context.Background(), s, "1", "pick"); err == nil {
		t.Fatal("rejected download succeeded")
	}
	f, _ := os.CreateTemp(t.TempDir(), "upload")
	defer f.Close()
	f.WriteString("test")
	if err := a.upload115(context.Background(), s, "0", "test.mp4", f, 4); err == nil {
		t.Fatal("rejected upload succeeded")
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.download(ctx, s, "1", "pick"); !errors.Is(err, context.Canceled) || calls != 2 {
		t.Fatal("cancel ignored", err, calls)
	}
}

func Test115OSSUploadCallbackAndMultipart(t *testing.T) {
	old := apiClient
	defer func() { apiClient = old }()
	for _, size := range []int64{4, (16 << 20) + 1} {
		t.Run(string(rune('a'+size%10)), func(t *testing.T) {
			f, _ := os.CreateTemp(t.TempDir(), "upload")
			defer f.Close()
			if err := f.Truncate(size); err != nil {
				t.Fatal(err)
			}
			parts, total, aborted := 0, int64(0), false
			confirmed := true
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Scheme != "https" || r.Header.Get("x-oss-security-token") != "token" || r.Header.Get("Cookie") != "" {
					t.Fatal("OSS security boundary", r.URL)
				}
				raw, headers := `{"state":true}`, http.Header{}
				switch {
				case r.Method == "POST" && r.URL.Query().Has("uploads"):
					raw = `<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>file</Key><UploadId>upload</UploadId></InitiateMultipartUploadResult>`
				case r.Method == "PUT":
					n, _ := io.Copy(io.Discard, r.Body)
					total += n
					parts++
					headers.Set("ETag", `"part"`)
				case r.Method == "DELETE":
					aborted = true
				}
				if !confirmed && (size <= 16<<20 || r.Method == "POST" && r.URL.Query().Get("uploadId") != "") {
					raw = `{"state":false}`
				}
				return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
			})}
			params := &driver.UploadOSSParams{Bucket: "bucket", Object: "file"}
			params.Callback.Callback = `{"callbackUrl":"https://115.com/callback"}`
			token := &driver.UploadOSSTokenResp{AccessKeyID: "key", AccessKeySecret: "secret", SecurityToken: "token"}
			if err := upload115OSS(context.Background(), f, size, params, token); err != nil || total != size || aborted {
				t.Fatal(err, total, size, aborted)
			}
			if size > 16<<20 && parts != 2 {
				t.Fatal("multipart not used", parts)
			}
			confirmed = false
			if err := upload115OSS(context.Background(), f, size, params, token); err == nil {
				t.Fatal("negative callback accepted")
			}
		})
	}
}
