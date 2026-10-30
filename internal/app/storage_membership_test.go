package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestStorageMembershipReadOnlyAndExplicitTiers(t *testing.T) {
	oldClient, oldTransport := apiClient, http.DefaultTransport
	defer func() { apiClient, http.DefaultTransport = oldClient, oldTransport }()
	for _, tc := range []struct{ kind, tier, body string }{
		{"115", "annual", `{"state":true,"data":{"user_id":"1","vip_info":{"level_name":"年费VIP"}}}`},
		{"115", "permanent", `{"state":true,"data":{"user_id":"1","vip_info":{"level_name":"永久VIP"}}}`},
		{"quark", "svip", `{"code":0,"status":200,"data":{"member_type":"SUPER_VIP"}}`},
		{"quark", "88vip", `{"code":0,"status":200,"data":{"member_type":"EXP_SVIP"}}`},
		{"quark", "svip-plus", `{"code":0,"status":200,"data":{"member_type":"Z_VIP"}}`},
		{"quark", "vip", `{"code":0,"status":200,"data":{"member_type":"VIP"}}`},
		{"quark", "mini", `{"code":0,"status":200,"data":{"member_type":"MINI_VIP"}}`},
		{"quark", "", `{"code":0,"status":200,"data":{"member_type":"UNKNOWN","total_capacity":10995116277760}}`},
		{"quark", "", `{"code":403,"status":403,"data":{"member_type":"EXP_SVIP"}}`},
		{"mobile", "diamond", `{"success":true,"data":{"userSubMemberList":[{"memberLvName":"白银会员"},{"memberLvName":"钻石会员"}]}}`},
		{"mobile", "gold", `{"success":true,"data":{"userSubMemberList":[{"memberLvName":"黄金会员"}]}}`},
		{"mobile", "silver", `{"success":true,"data":{"userSubMemberList":[{"memberLvName":"白银会员"}]}}`},
		{"mobile", "", `{"success":true,"data":{"userSubMemberList":[{"memberLevel":3,"memberLvName":"未知等级"}]}}`},
		{"tianyi", "platinum", `{"res_code":0,"data":{"vipInfoList":[{"memberLevel":2000,"endTime":"2099-01-01 00:00:00"}]}}`},
		{"tianyi", "gold", `{"res_code":0,"data":{"vipInfoList":[{"memberLevel":1000,"endTime":"2099-01-01 00:00:00"},{"memberLevel":2000,"endTime":"2020-01-01 00:00:00"}]}}`},
		{"tianyi", "", `{"res_code":0,"data":{"vipInfoList":[{"memberLevel":3000,"endTime":"2099-01-01 00:00:00"}]}}`},
	} {
		t.Run(tc.kind+"-"+tc.tier, func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: "member", Type: tc.kind, Enabled: true, Config: map[string]string{"mode": "native", "cookie": "UID=1_A1;CID=test;SEID=secret", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token")), "username": "account", "password": "private"}}
			if tc.kind == "tianyi" {
				a.tianyiSessions[s.ID] = tianyiSession{Key: "key", Secret: "secret", Credentials: tianyiCredentials(s), Expires: time.Now().Add(time.Hour)}
			}
			transport := casTransport(func(r *http.Request) (*http.Response, error) {
				switch tc.kind {
				case "quark":
					if r.Method != "GET" || r.URL.Host != "drive-pc.quark.cn" || r.URL.Path != "/1/clouddrive/member" || r.Header.Get("Cookie") != s.Config["cookie"] || !strings.Contains(r.UserAgent(), "quark-cloud-drive") {
						t.Errorf("unexpected quark membership request %s %s", r.Method, r.URL)
					}
				case "115":
					if r.Method != "GET" || r.URL.Host != "passportapi.115.com" || r.URL.Path != "/app/1.0/web/26.0/user/base_info" {
						t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					}
				case "mobile":
					var body map[string]int
					_ = json.NewDecoder(r.Body).Decode(&body)
					if r.Method != "POST" || r.URL.Path != "/orchestration/group-rebuild/member/v1.0/queryUserBenefits" || body["isNeedBenefit"] != 0 {
						t.Error("unexpected mobile membership request")
					}
				case "tianyi":
					if r.Method != "GET" || r.URL.Path != "/order/queryUserLogo.action" {
						t.Error("unexpected tianyi membership request")
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})
			apiClient, http.DefaultTransport = &http.Client{Transport: transport}, transport
			m := a.readStorageMembership(context.Background(), s)
			if tc.tier == "" {
				if m != nil {
					t.Fatal(m)
				}
				return
			}
			if m == nil || m.Tier != tc.tier || freshMembership(m) == nil {
				t.Fatal(m)
			}
		})
	}
}
func TestStorageMembershipFreshnessAndNoQuotaGuess(t *testing.T) {
	m := membership("gold", "黄金会员", time.Now().Add(time.Hour))
	m.CheckedAt = time.Now().Add(-6 * time.Minute)
	if freshMembership(m) != nil {
		t.Fatal("old membership retained")
	}
	m.CheckedAt = time.Now()
	expired := time.Now().Add(-time.Second)
	m.ExpiresAt = &expired
	if freshMembership(m) != nil {
		t.Fatal("expired membership retained")
	}
	if membershipTime(json.RawMessage(`"invalid"`)).IsZero() == false {
		t.Fatal("invalid date accepted")
	}
	if m := membership("annual", "年费VIP", time.Now().Add(-time.Hour)); m != nil {
		t.Fatal(fmt.Sprint(m))
	}
}

func TestStorageMembershipFailureDropsTierButKeepsQuota(t *testing.T) {
	old := apiClient
	defer func() { apiClient = old }()
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("unavailable")), Request: r}, nil
	})}
	a := testApp(t)
	s := Storage{ID: "m", Name: "移动", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "userDomainId": "domain", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
	s.Usage = &storageUsageState{storageUsage: storageUsage{Total: 1000, Used: 250, Membership: membership("diamond", "钻石会员", time.Time{})}, Stamp: storageRevision(s), UpdatedAt: time.Now(), AttemptedAt: time.Now().Add(-time.Hour)}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	value, err := a.cachedStorageUsage(context.Background(), s)
	if err != nil || value.Total != 1000 || value.Used != 250 || value.Membership != nil {
		t.Fatal(value, err)
	}
}

func TestStorageMembershipDoesNotWaitForQuota(t *testing.T) {
	old := apiClient
	defer func() { apiClient = old }()
	a := testApp(t)
	s := Storage{ID: "mobile", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "userDomainId": "domain", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	memberRead := make(chan struct{})
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		var raw string
		switch r.URL.Path {
		case "/orchestration/group-rebuild/member/v1.0/queryUserBenefits":
			close(memberRead)
			raw = `{"success":true,"data":{"userSubMemberList":[{"memberLvName":"钻石会员"}]}}`
		case "/user/disk/quota/detail":
			select {
			case <-memberRead:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			raw = `{"success":true,"data":{"diskSize":"1000","freeDiskSize":"750"}}`
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	value, err := a.cachedStorageUsage(ctx, s)
	if err != nil || value.Membership == nil || value.Membership.Tier != "diamond" || value.Total != 1000<<20 {
		t.Fatal("quota starved membership lookup", value, err)
	}
}
