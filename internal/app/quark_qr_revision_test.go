package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestQuarkTVJSONReportsBusinessErrorWithoutLeakingBody(t *testing.T) {
	previous := apiClient
	t.Cleanup(func() { apiClient = previous })
	apiClient = &http.Client{Transport: casTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"errno":11003,"error_info":"private authorization material"}`))}, nil
	})}
	var out struct {
		Errno int `json:"errno"`
	}
	err := quarkTVJSON(context.Background(), "GET", "https://quark.example/authorize", http.Header{}, nil, &out)
	var status *quarkTVHTTPError
	if !errors.As(err, &status) || status.Errno != 11003 || out.Errno != 11003 || strings.Contains(err.Error(), "private") {
		t.Fatal(out, err)
	}
}

func TestQuarkTVDeviceLimitIsActionableAtAllEnvelopeLevels(t *testing.T) {
	previous := apiClient
	t.Cleanup(func() { apiClient = previous })
	for _, body := range []string{`{"errno":32009,"error_info":"private"}`, `{"code":400,"data":{"errno":32009,"error_info":"private"}}`} {
		apiClient = &http.Client{Transport: casTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		var out map[string]any
		err := quarkTVJSON(context.Background(), "GET", "https://quark.example/user", http.Header{}, nil, &out)
		if err == nil || !strings.Contains(err.Error(), "设备数超限") || !strings.Contains(err.Error(), "设备管理") || strings.Contains(err.Error(), "private") {
			t.Fatal(err)
		}
	}
}

func TestQuarkTVDeviceLimitDoesNotBindAndRetryKeepsDevice(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "ck"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	previous := apiClient
	t.Cleanup(func() { apiClient = previous })
	limited := true
	device := ""
	userCalls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		body, status := "", 200
		switch r.URL.Host + r.URL.Path {
		case "open-api-drive.quark.cn/oauth/authorize":
			if device != "" && device != r.URL.Query().Get("device_id") {
				t.Fatal("retry registered a new device")
			}
			device = r.URL.Query().Get("device_id")
			body = `{"qr_data":"https://quark.example/qr","query_token":"query"}`
		case "open-api-drive.quark.cn/oauth/code":
			body = `{"code":"code"}`
		case "broker.example/token":
			body = `{"code":200,"data":{"access_token":"secret","refresh_token":"refresh","expires_in":3600}}`
		case "open-api-drive.quark.cn/user":
			userCalls++
			body = `{"data":{"nickname":"owner"}}`
			if limited {
				status = 400
				body = `{"status":-1,"errno":32009,"error_info":"private-device-info"}`
			}
		case "pan.quark.cn/account/info":
			body = `{"success":true,"data":{"nickname":"owner"}}`
		default:
			t.Fatal("unexpected endpoint", r.URL.Path)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	for attempt := 0; attempt < 2; attempt++ {
		w := request(t, h, "POST", "/api/quark-takeover/q/qr", map[string]any{"broker": "https://broker.example", "consent": true}, cookie)
		var qr map[string]string
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &qr) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		w = request(t, h, "POST", "/api/quark-takeover/q/poll", map[string]string{"session": qr["session"]}, cookie)
		if limited {
			if w.Code != 400 || !strings.Contains(w.Body.String(), "设备数超限") || strings.Contains(w.Body.String(), "private") || a.store.snapshot().QuarkTV["q"].AccessToken != "" {
				t.Fatal(w.Code, w.Body.String())
			}
			limited = false
		} else if w.Code != 200 || a.store.snapshot().QuarkTV["q"].AccessToken != "secret" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if userCalls != 2 {
		t.Fatal("device errors were retried automatically", userCalls)
	}
}
