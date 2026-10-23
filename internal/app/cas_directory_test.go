package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCASDefaultDirectory(t *testing.T) {
	for _, provider := range []string{"mobile", "tianyi"} {
		for _, mode := range []string{"create both", "reuse root", "reuse both", "root file conflict", "cas file conflict", "create failure", "missing id"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				a := testApp(t)
				s := Storage{ID: "cas", Type: provider, Enabled: true, Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:13900000000:token")), "username": "account", "password": "secret"}}
				a.tianyiSessions[s.ID] = tianyiSession{Key: "session", Secret: "0123456789abcdef", Credentials: sha256.Sum256([]byte("account\x00secret")), Expires: time.Now().Add(time.Hour)}
				old := apiClient
				t.Cleanup(func() { apiClient = old })
				root, first, leaf := "/", "20", "21"
				if provider == "tianyi" {
					root = "-11"
				}
				writes := []string{}
				lists := []string{}
				apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
					var input map[string]any
					if r.Body != nil && provider == "mobile" {
						_ = json.NewDecoder(r.Body).Decode(&input)
					}
					data := map[string]any{}
					code := 200
					if strings.HasSuffix(r.URL.Path, "/list") || strings.HasSuffix(r.URL.Path, "/listFiles.action") {
						parent := r.FormValue("folderId")
						if provider == "mobile" {
							parent, _ = input["parentFileId"].(string)
						}
						lists = append(lists, parent)
						items := []map[string]any{}
						name, id := "Aether", first
						if parent == first {
							name, id = "CAS", leaf
						} else if parent != root {
							t.Fatalf("unexpected parent %q", parent)
						}
						conflict := parent == root && mode == "root file conflict" || parent == first && mode == "cas file conflict"
						exists := parent == root && mode != "create both" && mode != "missing id" || parent == first && mode == "reuse both"
						if exists || conflict {
							items = append(items, map[string]any{"id": id, "fileId": id, "name": name, "type": "folder"})
						}
						if provider == "mobile" {
							if conflict {
								items[0]["type"] = "file"
							}
							data["items"] = items
						} else if conflict {
							data["fileListAO"] = map[string]any{"folderList": []any{}, "fileList": items}
						} else {
							data["fileListAO"] = map[string]any{"folderList": items, "fileList": []any{}}
						}
					} else if strings.HasSuffix(r.URL.Path, "/create") || strings.HasSuffix(r.URL.Path, "/createFolder.action") {
						parent, name := r.URL.Query().Get("parentFolderId"), r.URL.Query().Get("folderName")
						if provider == "mobile" {
							parent, _ = input["parentFileId"].(string)
							name, _ = input["name"].(string)
						}
						writes = append(writes, parent+"/"+name)
						id := first
						if name == "CAS" && parent == first {
							id = leaf
						} else if name != "Aether" || parent != root {
							t.Fatalf("unexpected creation %q/%q", parent, name)
						}
						if mode == "create failure" {
							code = 503
						} else if mode != "missing id" {
							data = map[string]any{"id": id, "fileId": id}
						}
					} else {
						t.Fatalf("unexpected request %s", r.URL.Path)
					}
					var raw []byte
					if provider == "mobile" {
						raw, _ = json.Marshal(map[string]any{"success": true, "data": data})
					} else {
						raw, _ = json.Marshal(data)
					}
					return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(string(raw))), Header: http.Header{}, Request: r}, nil
				})}
				folder, err := a.casFolder(context.Background(), s, "https://personal.yun.139.com")
				success := mode == "create both" || mode == "reuse root" || mode == "reuse both"
				if success && (err != nil || folder != leaf) || !success && err == nil {
					t.Fatalf("directory=%q err=%v", folder, err)
				}
				want := []string{}
				switch mode {
				case "create both":
					want = []string{root + "/Aether", first + "/CAS"}
				case "reuse root", "create failure":
					want = []string{first + "/CAS"}
				case "missing id":
					want = []string{root + "/Aether"}
				}
				if !reflect.DeepEqual(writes, want) {
					t.Fatalf("writes=%v want=%v", writes, want)
				}
				if success && !reflect.DeepEqual(lists, []string{root, first}) {
					t.Fatalf("incorrect hierarchy %v", lists)
				}
			})
		}
	}
}
