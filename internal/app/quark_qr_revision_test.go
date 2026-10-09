package app

import (
	"context"
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
