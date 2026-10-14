package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func casTestLogMessages(t *testing.T, a *App) string {
	t.Helper()
	var lines []string
	for _, log := range a.store.logSnapshot() {
		if log.Module != "tasks" || !strings.HasPrefix(log.Message, "CAS ") {
			continue
		}
		for _, secret := range []string{"https://media.example", "secret", "13900000000", strings.Repeat("a", 64), strings.Repeat("B", 32)} {
			if strings.Contains(log.Message, secret) {
				t.Fatalf("CAS log leaked sensitive data: %s", log.Message)
			}
		}
		lines = append(lines, log.Level+" "+log.Message)
	}
	return strings.Join(lines, "\n")
}

func TestCASFailureLogsAreSafePersistentAndClassified(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "mobile", Name: "移动影音", Type: "mobile"}
	a.casFailureLog(s, "film.mkv", "秒传提交", errors.New("https://media.example/?secret=13900000000"))
	a.casFailureLog(s, "film.mkv", "直链获取", context.DeadlineExceeded)
	a.casFailureLog(s, "film.mkv", "秒传提交", context.Canceled)
	logs := casTestLogMessages(t, a)
	for _, expected := range []string{"error CAS 秒传提交失败", "error CAS 直链获取超时", "warn CAS 秒传提交已取消", `文件 "film.mkv"`, `存储池 "移动影音"（"mobile"）`} {
		if !strings.Contains(logs, expected) {
			t.Fatal("missing log", expected, logs)
		}
	}
	reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if casTestLogMessages(t, reopened) != logs {
		t.Fatal("CAS logs did not survive restart")
	}
}

func TestCASUnconfiguredPoolFailureIsLogged(t *testing.T) {
	a := testApp(t)
	info := casPoolRevisionInfo()
	_, done, err := a.casDownload(a.ctx, Storage{}, streamClaim{CAS: &info})
	done()
	if err == nil || !strings.Contains(casTestLogMessages(t, a), "绑定池存储选择失败") {
		t.Fatal("unconfigured pool has no failure log", err)
	}
}

func TestCASOfflineLogsPerFileOutcome(t *testing.T) {
	for _, mode := range []string{"success", "miss", "collision", "upstream"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: "mobile", Name: "移动影音", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:13900000000:token")), "root": "/"}}
			old := apiClient
			defer func() { apiClient = old }()
			writes := 0
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				status := 200
				data := any(map[string]any{})
				switch r.URL.Path {
				case "/user/route/qryRoutePolicy":
					data = map[string]any{"routePolicyList": []any{map[string]string{"modName": "personal", "httpsUrl": "https://personal.yun.139.com"}}}
				case "/file/list":
					items := []any{}
					if mode == "collision" {
						items = append(items, map[string]any{"fileId": "existing", "name": "film.mkv", "type": "file", "size": 5})
					}
					data = map[string]any{"items": items}
				case "/file/create":
					writes++
					data = map[string]any{"fileId": "restored", "rapidUpload": mode == "success"}
					if mode == "upstream" {
						status = 503
					}
				default:
					t.Fatalf("unexpected request %s", r.URL.Path)
				}
				raw, _ := json.Marshal(map[string]any{"success": true, "data": data})
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(raw))), Header: http.Header{}, Request: r}, nil
			})}
			info := casPoolRevisionInfo()
			info.Name = "film.mkv"
			err := a.restoreCASInto(a.ctx, s, "folder", info)
			logs := casTestLogMessages(t, a)
			if (err == nil) != (mode == "success") {
				t.Fatal(err)
			}
			if mode == "success" {
				if !strings.Contains(logs, "info CAS 离线秒传成功") {
					t.Fatal(logs)
				}
			} else if !strings.Contains(logs, "error CAS 离线秒传") || !strings.Contains(logs, "失败") || strings.Contains(logs, "秒传成功") {
				t.Fatal("wrong failure outcome", logs)
			}
			if mode == "collision" && writes != 0 {
				t.Fatal("collision replayed a write")
			}
			if mode == "miss" && !strings.Contains(logs, "未命中秒传") {
				t.Fatal("miss reason missing", logs)
			}
		})
	}
}

func TestCASOfflineInvalidFeatureIsLoggedBeforeSubmission(t *testing.T) {
	a := testApp(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("cas", "broken.cas")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("invalid CAS data"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/files/offline", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	if err := r.ParseMultipartForm(casLimit); err != nil {
		t.Fatal(err)
	}
	defer r.MultipartForm.RemoveAll()
	w := httptest.NewRecorder()
	s := Storage{ID: "mobile", Name: "移动影音", Type: "mobile", Config: map[string]string{"mode": "native"}}
	a.importCAS(w, r, a.ctx, s, "/")
	logs := casTestLogMessages(t, a)
	if w.Code != http.StatusBadRequest || !strings.Contains(logs, "离线秒传特征校验失败") || !strings.Contains(logs, "broken.cas") {
		t.Fatal(w.Code, logs)
	}
}
