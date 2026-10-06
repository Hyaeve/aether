package app

import (
	"strings"
	"testing"
)

func TestExplicitSecretReveal(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	keys := []string{"cookie", "token", "accessToken", "refreshToken", "authorization", "password"}
	if err := a.store.update(func(st *State) error {
		config := map[string]string{}
		for _, key := range keys {
			config[key] = "real-" + key
		}
		st.Storages = []Storage{{ID: "pool", Config: config}}
		st.Links = []MediaLink{{ID: "link", APIKey: "real-api", Password: "real-link-password"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/api/state", "/api/links"} {
		if strings.Contains(request(t, h, "GET", endpoint, nil, cookie).Body.String(), "real-") {
			t.Fatal("list leaks secrets")
		}
	}
	for _, resource := range []struct {
		path   string
		fields []string
	}{
		{"/api/storages/pool/secret", keys},
		{"/api/links/link/secret", []string{"apiKey", "password"}},
	} {
		if request(t, h, "POST", resource.path, map[string]string{"field": resource.fields[0]}, nil).Code != 401 {
			t.Fatal("authentication")
		}
		if request(t, h, "GET", resource.path, nil, cookie).Code != 405 {
			t.Fatal("method")
		}
		for _, field := range resource.fields {
			metadata := request(t, h, "POST", resource.path, map[string]any{"field": field, "metadataOnly": true}, cookie)
			if metadata.Code != 200 || !strings.Contains(metadata.Body.String(), `"length":`) || strings.Contains(metadata.Body.String(), "real-") || strings.Contains(metadata.Body.String(), `"value"`) {
				t.Fatal("secret length metadata leaks or fails", metadata.Body.String())
			}
			response := request(t, h, "POST", resource.path, map[string]string{"field": field}, cookie)
			if response.Code != 200 || !strings.Contains(response.Body.String(), "real-") || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("reveal failed", response.Body.String())
			}
		}
		if request(t, h, "POST", resource.path, map[string]string{"field": "master.key"}, cookie).Code != 400 {
			t.Fatal("invalid field")
		}
	}
	if request(t, h, "POST", "/api/storages/missing/secret", map[string]string{"field": "cookie"}, cookie).Code != 404 {
		t.Fatal("missing resource")
	}
	for _, entry := range a.store.snapshot().Logs {
		if strings.Contains(entry.Message, "real-") {
			t.Fatal("log leaks secret")
		}
	}
}
