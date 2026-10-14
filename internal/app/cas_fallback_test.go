package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCASPlaybackProviderFallback(t *testing.T) {
	for _, tc := range []struct {
		name, mobileFailure                                 string
		bound, single, cancel, both, disabled, preferTianyi bool
		stream, live                                        bool
	}{
		{name: "mobile restore failure", mobileFailure: "restore"},
		{name: "first signed playback falls back", mobileFailure: "restore", stream: true},
		{name: "live mobile link failure does not restore mobile again", mobileFailure: "link", live: true},
		{name: "mobile link failure", mobileFailure: "link"},
		{name: "mobile empty link", mobileFailure: "empty"},
		{name: "successful mobile needs no fallback"},
		{name: "both providers fail", mobileFailure: "restore", both: true},
		{name: "bound legacy does not switch accounts", mobileFailure: "restore", bound: true},
		{name: "single hash cannot use tianyi", mobileFailure: "restore", single: true},
		{name: "request cancellation does not restore elsewhere", mobileFailure: "restore", cancel: true},
		{name: "disabled fallback is not used", mobileFailure: "restore", disabled: true},
		{name: "tianyi preferred failure falls back to mobile", preferTianyi: true, both: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			mobile := Storage{ID: "mobile", Name: "移动影音", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:13900000000:token")), "root": "/"}}
			tianyi := Storage{ID: "tianyi", Name: "天翼影音", Type: "tianyi", Enabled: !tc.disabled, Config: map[string]string{"username": "account", "password": "secret"}}
			pool := normalizedCASPool(CASPoolConfig{Mobile: CASPoolGroup{StorageIDs: []string{mobile.ID}, RetentionHours: 24}, Tianyi: CASPoolGroup{StorageIDs: []string{tianyi.ID}, RetentionHours: 48}})
			if tc.preferTianyi {
				pool.Preferred = "tianyi"
			}
			if err := a.store.update(func(st *State) error { st.Storages = []Storage{mobile, tianyi}; st.CASPool = pool; return nil }); err != nil {
				t.Fatal(err)
			}
			a.tianyiSessions[tianyi.ID] = tianyiSession{Key: "session", Secret: "0123456789abcdef", Credentials: sha256.Sum256([]byte("account\x00secret")), Expires: time.Now().Add(time.Hour)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			old := apiClient
			defer func() { apiClient = old }()
			writes := []string{}
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				var payload map[string]any
				if r.Body != nil && strings.HasPrefix(r.URL.Path, "/file/") {
					_ = json.NewDecoder(r.Body).Decode(&payload)
				}
				raw, status := "", 200
				data := any(map[string]any{})
				switch r.URL.Path {
				case "/user/route/qryRoutePolicy":
					data = map[string]any{"routePolicyList": []any{map[string]string{"modName": "personal", "httpsUrl": "https://personal.yun.139.com"}}}
				case "/file/list":
					data = map[string]any{"items": []any{map[string]string{"fileId": "folder", "name": "Aether", "type": "folder"}}}
				case "/file/create":
					writes = append(writes, "mobile")
					if payload["contentHash"] != strings.Repeat("a", 64) || payload["type"] != "file" {
						t.Error("unexpected mobile restore", payload)
					}
					if tc.cancel {
						cancel()
						return nil, context.Canceled
					}
					if tc.mobileFailure == "restore" {
						status = 503
					}
					data = map[string]any{"fileId": "mobile-file", "rapidUpload": true}
				case "/file/getDownloadUrl":
					if tc.mobileFailure == "link" {
						status = 503
					}
					if tc.mobileFailure != "empty" {
						data = map[string]string{"cdnUrl": "https://media.example/mobile"}
					}
				case "/listFiles.action":
					raw = `{"fileListAO":{"folderList":[{"id":20,"name":"Aether"}],"fileList":[]}}`
				case "/person/initMultiUpload":
					writes = append(writes, "tianyi")
					params := tianyiTestUploadParams(t, r, "0123456789abcdef")
					if params.Get("sliceSize") != "10485760" {
						t.Error("fallback lost slice size")
					}
					if tc.both {
						status = 503
					}
					raw = `{"uploadFileId":"30"}`
				case "/person/commitMultiUploadFile":
					params := tianyiTestUploadParams(t, r, "0123456789abcdef")
					if params.Get("fileMd5") != strings.Repeat("B", 32) || params.Get("sliceMd5") != strings.Repeat("C", 32) {
						t.Error("fallback lost dual MD5")
					}
					raw = `{"file":{"userFileId":"40"}}`
				case "/getFileDownloadUrl.action":
					raw = `{"fileDownloadUrl":"https://media.example/tianyi"}`
				default:
					t.Errorf("unexpected upstream request %s", r.URL.Path)
					status = 500
				}
				if raw == "" {
					body, _ := json.Marshal(map[string]any{"success": true, "data": data})
					raw = string(body)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(raw)), Header: http.Header{}, Request: r}, nil
			})}
			info := casPoolRevisionInfo()
			if tc.single {
				info.MD5, info.SliceMD5, info.SliceSize = "", "", 0
			}
			claim := streamClaim{CAS: &info, Redirect: true}
			if tc.bound {
				claim.Storage = mobile.ID
			}
			if tc.live {
				if err := a.store.update(func(st *State) error {
					st.CASTemporary = []CASTemporary{{Key: "live-mobile", Feature: casFeatureKey(info), StorageID: mobile.ID, FileID: "mobile-file", Ready: true, LastUsed: time.Now(), RetentionHours: 24}}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.stream {
				u, _ := url.Parse(a.signedStreamURL(claim))
				w := httptest.NewRecorder()
				a.Handler(t.TempDir()).ServeHTTP(w, httptest.NewRequest("GET", u.RequestURI(), nil))
				if w.Code != http.StatusFound || w.Header().Get("Location") != "https://media.example/tianyi" {
					t.Fatal("first signed playback did not switch providers", w.Code, w.Body.String())
				}
			}
			d, release, err := a.casDownload(ctx, mobile, claim)
			wantErr := tc.mobileFailure != "" && (tc.both || tc.bound || tc.single || tc.cancel || tc.disabled)
			if (err != nil) != wantErr {
				t.Fatalf("download=%+v err=%v", d, err)
			}
			wantWrites := "mobile"
			if tc.live {
				wantWrites = "tianyi"
			} else if tc.preferTianyi {
				wantWrites = "tianyi,mobile"
			} else if tc.mobileFailure != "" && !tc.bound && !tc.single && !tc.cancel && !tc.disabled {
				wantWrites = "mobile,tianyi"
			}
			if strings.Join(writes, ",") != wantWrites {
				t.Fatalf("must not replay writes: %v want %s", writes, wantWrites)
			}
			logs := casTestLogMessages(t, a)
			if tc.cancel {
				if !strings.Contains(logs, "已取消") {
					t.Fatal("cancelled restore not logged", logs)
				}
			} else if tc.mobileFailure != "" || tc.preferTianyi {
				if !strings.Contains(logs, "失败") || !strings.Contains(logs, "影音") {
					t.Fatal("failed provider not identified", logs)
				}
			}
			if strings.Contains(wantWrites, ",") || tc.live {
				if !strings.Contains(logs, "转交另一类绑定存储") {
					t.Fatal("fallback not logged", logs)
				}
			}
			if err != nil {
				release()
				return
			}
			wantProvider := "mobile"
			if tc.mobileFailure != "" {
				wantProvider = "tianyi"
			}
			if d.URL != "https://media.example/"+wantProvider || d.storageID != wantProvider {
				t.Fatal("wrong download origin", d)
			}
			release()
			if !strings.Contains(logs, "秒传成功") || !strings.Contains(logs, "直链获取成功") {
				t.Fatal("successful restore or link not logged", logs)
			}
			if len(a.casActive) != 0 {
				t.Fatal("playback lease leaked")
			}
			// A second signed playback reuses the live fallback file without restoring again.
			before := len(writes)
			beforeSuccess := strings.Count(casTestLogMessages(t, a), "CAS 秒传成功：")
			u, _ := url.Parse(a.signedStreamURL(claim))
			w := httptest.NewRecorder()
			a.Handler(t.TempDir()).ServeHTTP(w, httptest.NewRequest("GET", u.RequestURI(), nil))
			if w.Code != http.StatusFound || w.Header().Get("Location") != d.URL || len(writes) != before {
				t.Fatal("stream fallback reuse", w.Code, w.Body.String(), writes)
			}
			if len(a.casActive) != 0 {
				t.Fatal("stream lease leaked")
			}
			logs = casTestLogMessages(t, a)
			if !strings.Contains(logs, "复用存活秒传文件") || strings.Count(logs, "CAS 秒传成功：") != beforeSuccess {
				t.Fatal("reuse logged as a new restore", logs)
			}
			for _, entry := range a.store.snapshot().CASTemporary {
				if entry.StorageID == "mobile" && tc.mobileFailure != "" && entry.Ready {
					t.Fatal("failed mobile link retained as ready")
				}
				if entry.StorageID == "tianyi" && entry.RetentionHours != 48 {
					t.Fatal("fallback retention lost")
				}
			}
		})
	}
}
