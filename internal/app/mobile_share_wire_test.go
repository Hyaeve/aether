package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestMobileShareOfficialWireProtocol(t *testing.T) {
	a := testApp(t)
	s := Storage{Type: "mobile", Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(r.Body)
		sign := strings.Split(r.Header.Get("Mcloud-Sign"), ",")
		if json.Valid(body) || len(sign) != 3 || len(sign) == 3 && sign[2] != strings.ToUpper(mobileMD5(string(body)+"|"+sign[0]+"|"+sign[1])) || r.Header.Get("X-Yun-Client-Info") == "" || r.Header.Get("Authorization") != "Basic "+s.Config["authorization"] {
			t.Error("mobile share must use the existing encrypted/signed transport")
		}
		data := `{"resultCode":"0","data":{"coLst":[{"coID":"file","coName":"Film.mkv","coPath":"root/file"}]}}`
		if strings.HasSuffix(r.URL.Path, "/createOuterLinkBatchOprTask") {
			data = `{"resultCode":"0","data":{"taskID":"job"}}`
		}
		encrypted, err := mobileShareEncrypt([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(encrypted)), Request: r}, nil
	})}
	token, items, err := a.readShare(context.Background(), s, "code", "Ab12")
	if err != nil || len(items) != 1 {
		t.Fatal("encrypted directory response not read", err)
	}
	if task, err := submitShare(context.Background(), s, sharePreview{Code: "code", Pass: "Ab12", Token: token}, "target", items); err != nil || task != "job" || calls != 2 {
		t.Fatal(task, calls, err)
	}
}

func TestMobileShareRequestKeepsCancellationIdentity(t *testing.T) {
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	ctx, cancel := context.WithCancel(context.Background())
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) { cancel(); return nil, context.Canceled })}
	var data any
	if err := mobileGenerationShareJSON(ctx, "getOutLinkInfoV6", map[string]any{}, &data); err != context.Canceled {
		t.Fatal("request cancellation lost its original identity", err)
	}
}

func TestTelegramMobileTransferUsesSharedWireAndReadFailureIsNotSubmitted(t *testing.T) {
	for _, failRead := range []bool{false, true} {
		t.Run(map[bool]string{false: "submitted", true: "dns-failure"}[failRead], func(t *testing.T) {
			a, cfg := telegramFixture(t)
			s := Storage{ID: "mobile", Name: "移动影视", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
			target := telegramTarget{ID: "mobile-target", Provider: "mobile", StorageID: s.ID, Parent: "target", Path: "/Library/Test"}
			job := telegramTransfer{ID: "mobile-job", ChatID: 123, UserID: 123, Auth: telegramDigest(cfg), Bot: telegramDigest(cfg.Token), Provider: "mobile", Status: "queued", Selected: 0, Targets: []telegramTarget{target}, Link: "https://yun.139.com/shareweb/#/w/i/code?pwd=Ab12"}
			a.store.update(func(st *State) error {
				st.Storages = []Storage{s}
				st.Notifications.Targets = []telegramTarget{target}
				st.TelegramRuntime.Jobs = []telegramTransfer{job}
				return nil
			})
			reads, saves := 0, 0
			shareMock(t, func(r *http.Request) string {
				if r.URL.Host == "api.telegram.org" {
					return `{"ok":true,"result":{"message_id":10}}`
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/getOutLinkInfoV6"):
					reads++
					return `{"resultCode":"0","data":{"coLst":[{"coID":"file","coName":"Film.mkv","coPath":"root/file"}]}}`
				case strings.HasSuffix(r.URL.Path, "/qryRoutePolicy"):
					return `{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"https://personal.yun.139.com"}]}}`
				case strings.HasSuffix(r.URL.Path, "/file/list"):
					return `{"success":true,"data":{"items":[]}}`
				case strings.HasSuffix(r.URL.Path, "/createOuterLinkBatchOprTask"):
					saves++
					var data map[string]map[string]any
					json.NewDecoder(r.Body).Decode(&data)
					request := data["createOuterLinkBatchOprTaskReq"]
					info := request["taskInfo"].(map[string]any)
					if info["newCatalogID"] != "target" || len(info["contentInfoList"].([]any)) != 1 {
						t.Fatal("wrong destination or transfer size")
					}
					return `{"resultCode":"0","data":{"taskID":"submitted-job"}}`
				default:
					t.Error("unexpected cloud operation", r.URL.Path)
					return `{}`
				}
			})
			if failRead {
				transport := apiClient.Transport
				apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
					if r.URL.Host == "share-kd-njs.yun.139.com" {
						reads++
						return nil, &net.DNSError{Err: "private resolver failure", Name: "private"}
					}
					return transport.RoundTrip(r)
				})
			}
			a.executeTelegramTransfer(context.Background(), job)
			current := a.store.snapshot().TelegramRuntime.Jobs[0]
			if failRead {
				if current.Status != "failed" || reads != 1 || saves != 0 || current.Batches != 0 || current.Processed != 0 {
					t.Fatal("read error submitted or retried", current.Status, reads, saves)
				}
			} else if current.Status != "submitted" || reads != 1 || saves != 1 || current.Processed != 1 {
				t.Fatal("TG mobile transfer failed", current.Status, reads, saves)
			}
			if current.Link != "" {
				t.Fatal("terminal job retained link")
			}
			logs, _ := json.Marshal(a.store.snapshot().Logs)
			if strings.Contains(string(logs), "Ab12") || strings.Contains(string(logs), "private") || strings.Contains(string(logs), cfg.Token) {
				t.Fatal("sensitive error log")
			}
			if failRead && !strings.Contains(string(logs), "DNS解析失败") {
				t.Fatal("transport cause not actionable")
			}
			a.executeTelegramTransfer(context.Background(), job)
			if reads != 1 {
				t.Fatal("terminal job replayed")
			}
		})
	}
}
