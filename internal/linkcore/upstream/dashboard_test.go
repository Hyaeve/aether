package upstream

import (
	"aether/internal/linkcore/config"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDashboardUsesFnosLoginAndStopsRedirectCredentials(t *testing.T) {
	var logins, leaked atomic.Int32
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer outside.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/emby/Users/AuthenticateByName" {
			logins.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"AccessToken":"dashboard-token","User":{"Id":"u1"}}`)
			return
		}
		if !strings.Contains(r.Header.Get("X-Emby-Authorization"), `Token="dashboard-token"`) {
			t.Error("fnOS full identity missing")
		}
		if r.URL.Path == "/emby/Library/VirtualFolders" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"Name":"影视","ItemId":"lib"}]`)
			return
		}
		http.Redirect(w, r, outside.URL, http.StatusFound)
	}))
	defer source.Close()
	provider, err := NewDashboard(config.Upstream{Name: "fn", Type: config.UpstreamFnos, BaseURL: source.URL, Username: "name", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Transport().(*http.Transport).CloseIdleConnections()
	libs, err := provider.Libraries(context.Background())
	if err != nil || len(libs) != 1 {
		t.Fatal(libs, err)
	}
	var out any
	if err := DashboardRead(context.Background(), provider, "/Sessions", nil, &out); err == nil {
		t.Fatal("redirect response accepted")
	}
	if leaked.Load() != 0 || logins.Load() != 1 {
		t.Fatal("credential redirect or repeated login", leaked.Load(), logins.Load())
	}
	if err := DashboardRead(context.Background(), provider, "/../private", nil, &out); err == nil {
		t.Fatal("arbitrary endpoint accepted")
	}
}
