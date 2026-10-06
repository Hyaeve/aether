package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNativeCloudFileActions(t *testing.T) {
	old := apiClient
	defer func() { apiClient = old }()
	for _, kind := range []string{"mobile", "tianyi", "115", "quark"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: kind, Type: kind, Enabled: true, Config: map[string]string{"mode": "native", "username": "test", "password": "secret", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:13900000000:token")), "accessToken": "secret", "cookie": "secret"}}
			// Avoid external login; all protocol traffic remains in this transport.
			if kind == "tianyi" {
				// tianyiSessionFor computes the same identity before reusing a session.
				a.tianyiSessions[s.ID] = tianyiSession{Key: "key", Secret: "secret", Expires: time.Now().Add(time.Hour)}
				session := a.tianyiSessions[s.ID]
				session.Credentials = sha256.Sum256([]byte(s.Config["username"] + "\x00" + s.Config["password"]))
				a.tianyiSessions[s.ID] = session
			}
			calls := 0
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				payload := `{"state":true,"success":true,"code":0,"status":200,"data":{}}`
				switch {
				case strings.Contains(r.URL.Path, "route"):
					payload = `{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"https://personal.yun.139.com"}]}}`
				case r.URL.Path == "/file/list":
					payload = `{"success":true,"data":{"items":[{"fileId":"123","name":"movie.mp4","type":"file","size":5}]}}`
				case strings.Contains(r.URL.Path, "listFiles.action"):
					payload = `{"res_code":0,"fileListAO":{"fileList":[{"id":"123","name":"movie.mp4","size":5}]}}`
				case r.URL.Path == "/open/ufile/files":
					payload = `{"state":true,"data":[{"file_id":"123","file_name":"movie.mp4","file_category":"1","size":5}]}`
				case strings.HasSuffix(r.URL.Path, "/file/sort"):
					payload = `{"code":0,"status":200,"data":{"list":[{"fid":"123","file_name":"movie.mp4","file_type":1,"size":5}]}}`
				default:
					calls++
					if kind == "tianyi" {
						payload = `{"res_code":0}`
						if strings.Contains(r.URL.Path, "createBatchTask") {
							payload = `{"res_code":0,"taskId":"job"}`
						}
						if strings.Contains(r.URL.Path, "checkBatchTask") {
							payload = `{"res_code":0,"taskStatus":4,"successedCount":1}`
						}
					}
					if r.Method != "POST" {
						t.Error("write was not POST")
					}
					if kind == "mobile" || kind == "quark" {
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						// Mobile wraps the payload; check endpoint independently.
					} else {
						r.ParseForm()
						if kind == "115" && strings.HasSuffix(r.URL.Path, "/update") && r.Form.Get("file_name") != "new.mp4" {
							t.Error(r.Form)
						}
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
			})}
			err := a.cloudFileAction(context.Background(), s, s, fileActionRequest{Action: "rename", Source: "/", IDs: []string{"123"}, Name: "new.mp4"})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatal("write calls", calls)
			}
			if err := a.cloudFileAction(context.Background(), s, s, fileActionRequest{Action: "delete", Source: "/", IDs: []string{"123"}}); err != nil {
				t.Fatal("delete", err)
			}
			if err := a.cloudFileAction(context.Background(), s, s, fileActionRequest{Action: "rename", Source: "/", IDs: []string{"not-found"}, Name: "bad.mp4"}); err == nil {
				t.Fatal("unknown file accepted")
			}
			other := s
			other.ID = "other"
			if a.cloudFileAction(context.Background(), s, other, fileActionRequest{Action: "move"}) == nil {
				t.Fatal("cross-account accepted")
			}
		})
	}
}
