package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMobileDownloadHonorsCDNSwitch(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "mobile", Type: "mobile", Config: map[string]string{"mode": "native", "authorization": "cGM6YWNjb3VudDp0b2tlbg=="}}
	old := apiClient.Transport
	t.Cleanup(func() { apiClient.Transport = old })
	for _, tc := range []struct{ body, want string }{
		{`{"url":"https://origin.test/file","cdnUrl":"https://cdn.test/file","cdnSwitch":false}`, "https://origin.test/file"},
		{`{"url":"https://origin.test/file","cdnUrl":"https://cdn.test/file","cdnSwitch":true}`, "https://cdn.test/file"},
		{`{"url":"https://origin.test/file","cdnSwitch":true}`, "https://origin.test/file"},
		{`{"cdnUrl":"https://cdn.test/file","cdnSwitch":false}`, ""},
	} {
		apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"success":true,"data":` + tc.body + `}`)), Request: r}, nil
		})
		d, err := a.mobileLinkAt(context.Background(), s, "https://personal.yun.139.com", "file")
		if tc.want == "" {
			if err == nil {
				t.Fatal("disabled CDN used without an origin")
			}
		} else if err != nil || d.URL != tc.want {
			t.Fatal(d, err, tc.want)
		}
	}
}
