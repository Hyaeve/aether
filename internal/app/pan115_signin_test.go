package app

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPan115SigninSettingsProtocolAndDailyReceipt(t *testing.T) {
	for _, mode := range []string{"success", "unknown", "redirect", "invalid", "rejected"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			pool := Storage{ID: "p", Name: "115", Type: "115", Enabled: true, Config: map[string]string{"cookie": "UID=123_A1;CID=test;SEID=secret"}}
			if err := a.store.update(func(st *State) error { st.Storages = []Storage{pool}; return nil }); err != nil {
				t.Fatal(err)
			}
			h := a.Handler(t.TempDir())
			if got := request(t, h, "GET", "/api/115-signin", nil, nil); got.Code != 401 {
				t.Fatalf("signin route must require auth, got %d", got.Code)
			}
			cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "secret"}, nil).Result().Cookies()[0]
			body := map[string]any{"enabled": true, "rules": []map[string]string{{"storageId": "p", "cron": "0 8 * * *"}}}
			if got := request(t, h, "PUT", "/api/115-signin", body, cookie); got.Code != 200 {
				t.Fatal(got.Code, got.Body.String())
			}
			old := apiClient
			defer func() { apiClient = old }()
			writes := 0
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				writes++
				if r.Method != "POST" || r.URL.String() != "https://proapi.115.com/android/2.0/user/points_sign" {
					t.Fatal("unexpected signin endpoint", r.Method, r.URL)
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				sum := sha1.Sum([]byte("123-Points_Sign@#115-" + r.Form.Get("token_time")))
				if r.Form.Get("token") != hex.EncodeToString(sum[:]) || r.Form.Get("token_time") == "" {
					t.Fatal("invalid signature")
				}
				if c, e := r.Cookie("UID"); e != nil || c.Value != "123_A1" {
					t.Fatal("missing credential")
				}
				if mode == "unknown" {
					return nil, errors.New("connection lost with secret")
				}
				code, payload := 200, `{"state":true,"data":{"continuous_day":3}}`
				if mode == "redirect" {
					code = 302
				} else if mode == "invalid" {
					payload = `{"data":{}}`
				} else if mode == "rejected" {
					payload = `{"state":false,"errno":99,"error":"secret body"}`
				}
				return &http.Response{StatusCode: code, Header: http.Header{"Location": {"https://untrusted.test/secret"}}, Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
			})}
			result := request(t, h, "POST", "/api/115-signin/run", map[string]string{"storageId": "p"}, cookie)
			if (mode == "success" && result.Code != 200) || (mode != "success" && result.Code != 502) || writes != 1 {
				t.Fatal(mode, result.Code, result.Body.String(), writes)
			}
			if strings.Contains(result.Body.String(), "secret") {
				t.Fatal("leaked upstream diagnostics")
			}
			if got := request(t, h, "POST", "/api/115-signin/run", map[string]string{"storageId": "p"}, cookie); writes != 1 || mode != "success" && got.Code == 200 {
				t.Fatal("replayed or falsely confirmed write", got.Code, writes)
			}
			reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
			if err != nil {
				t.Fatal(err)
			}
			next := request(t, reopened.Handler(t.TempDir()), "POST", "/api/115-signin/run", map[string]string{"storageId": "p"}, cookie)
			if writes != 1 || mode == "success" && next.Code != 200 || mode != "success" && next.Code == 200 {
				t.Fatal("daily receipt lost after restart", next.Code, writes)
			}
			get := request(t, h, "GET", "/api/115-signin", nil, cookie)
			var saved map[string]any
			if json.Unmarshal(get.Body.Bytes(), &saved) != nil || len(saved["rules"].([]any)) != 1 {
				t.Fatal("saved rule missing")
			}
		})
	}
}

func TestPan115SigninRejectsDuplicateAccountsAndUnsafeCron(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{{ID: "p", Type: "115", Enabled: true, Config: map[string]string{"cookie": "UID=123_A1;CID=x;SEID=x"}}, {ID: "alias", Type: "115", Enabled: true, Config: map[string]string{"cookie": "UID=123_B2;CID=y;SEID=y"}}, {ID: "q", Type: "quark", Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "secret"}, nil).Result().Cookies()[0]
	for _, rules := range [][]map[string]string{
		{{"storageId": "p", "cron": "*/14 * * * *"}},
		{{"storageId": "q", "cron": "0 8 * * *"}},
		{{"storageId": "p", "cron": "0 8 * * *"}, {"storageId": "alias", "cron": "0 9 * * *"}},
	} {
		if got := request(t, h, "PUT", "/api/115-signin", map[string]any{"enabled": true, "rules": rules}, cookie); got.Code != 400 {
			t.Fatal("accepted invalid signin settings", got.Code, got.Body.String())
		}
	}
}

func TestPan115SigninScheduleAndLegacyConfiguration(t *testing.T) {
	now := time.Date(2026, 10, 28, 8, 0, 0, 0, time.FixedZone("CST", 8*3600))
	rule := pan115SigninRule{StorageID: "p", Cron: "*/15 * * * *"}
	settings := pan115SigninSettings{Enabled: true, Rules: []pan115SigninRule{rule, {StorageID: "legacy", Cron: "*/14 * * * *"}}}
	next := map[string]time.Time{}
	if len(due115SigninRules(settings, next, now)) != 0 || !next["p"].Equal(now.Add(15*time.Minute)) || !next["legacy"].IsZero() {
		t.Fatal("startup caught up or scheduled unsafe legacy interval", next)
	}
	if got := due115SigninRules(settings, next, now.Add(15*time.Minute)); len(got) != 1 || got[0] != rule {
		t.Fatal("valid cron not dispatched", got)
	}
	if len(due115SigninRules(settings, next, now.Add(15*time.Minute))) != 0 {
		t.Fatal("same due time repeated")
	}
	settings.Enabled = false
	if len(due115SigninRules(settings, next, now.Add(time.Hour))) != 0 {
		t.Fatal("disabled cron dispatched")
	}
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.Pan115Signin = settings
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil {
		t.Fatal("legacy short or missing pool prevented startup", err)
	}
	if validate115Signin(reopened.store.snapshot().Pan115Signin, reopened.store.snapshot(), false) == nil {
		t.Fatal("new save/import accepted missing pool or short cron")
	}
}

func TestPan115SigninDisabledCancelledStaleAndRuntimeIsolation(t *testing.T) {
	a := testApp(t)
	rule := pan115SigninRule{StorageID: "p", Cron: "0 8 * * *"}
	pool := Storage{ID: "p", Name: "115", Type: "115", Enabled: true, Config: map[string]string{"cookie": "UID=123_A1;CID=test;SEID=secret"}}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{pool}
		st.Pan115Signin = pan115SigninSettings{Enabled: true, Rules: []pan115SigninRule{rule}}
		st.Pan115SigninReceipts = map[string]pan115SigninReceipt{"runtime-proof": {Status: "pending", Time: time.Now()}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(a.store.jsonValues())
	if bytes.Contains(raw, []byte("runtime-proof")) || bytes.Contains(raw, []byte("pan115SigninReceipts")) {
		t.Fatal("signin runtime leaked into config modules")
	}
	old := apiClient
	defer func() { apiClient = old }()
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected network")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.run115Signin(ctx, rule, "手动执行"); err == nil {
		t.Fatal("cancelled signin accepted")
	}
	stale := rule
	stale.Cron = "0 9 * * *"
	if _, err := a.run115Signin(a.ctx, stale, "Cron计划"); err == nil {
		t.Fatal("stale cron executed")
	}
	if err := a.store.update(func(st *State) error { st.Pan115Signin.Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := a.run115Signin(a.ctx, rule, "手动执行"); err == nil || calls != 0 {
		t.Fatal("disabled/cancelled/stale signin reached cloud", calls, err)
	}
}
