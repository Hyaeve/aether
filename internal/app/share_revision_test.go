package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMobileSharePaginatedSubdirectory(t *testing.T) {
	a := testApp(t)
	s := Storage{Type: "mobile", Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
	calls := 0
	shareMock(t, func(r *http.Request) string {
		calls++
		var body struct {
			Req map[string]any `json:"getOutLinkInfoReq"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Req["pCaID"] != "folder" {
			t.Fatal(body.Req)
		}
		start := 1
		if value, ok := body.Req["bNum"].(float64); ok {
			start = int(value)
			if int(body.Req["eNum"].(float64)) != start+99 {
				t.Fatal(body.Req)
			}
		}
		files := []map[string]string{}
		for i := start; i <= min(start+99, 235); i++ {
			files = append(files, map[string]string{"coID": fmt.Sprint(i), "coName": fmt.Sprintf("%d.mp4", i), "coPath": fmt.Sprintf("folder/%d", i)})
		}
		b, _ := json.Marshal(map[string]any{"resultCode": "0", "data": map[string]any{"nodNum": 9999, "coLst": files}})
		return string(b)
	})
	_, items, err := a.readShareAt(context.Background(), s, "link", "", "folder")
	if err != nil || len(items) != 235 || calls != 3 {
		t.Fatal(len(items), calls, err)
	}
}
func TestShareBatchSkipsDuplicatesAndMergesDirectories(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "secret"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	r, _ := http.NewRequest("POST", "/", nil)
	r.AddCookie(cookie)
	a.sharePreviews = map[string]*sharePreview{"key": {Owner: authorizationOwner(r), Config: shareConfig(s), Storage: s.ID, Expires: time.Now().Add(time.Minute), Batches: []shareBatch{{Directory: "Show", Source: "source-folder", Items: []shareEntry{{ID: "old", Name: "old.mp4", Token: "old-token"}, {ID: "new", Name: "new.mp4", Token: "new-token"}}}}}}
	saves := 0
	shareMock(t, func(r *http.Request) string {
		if strings.HasSuffix(r.URL.Path, "/sort") {
			if r.URL.Query().Get("pdir_fid") == "0" {
				return `{"code":0,"data":{"list":[{"fid":"existing-folder","file_name":"Show","dir":true}]}}`
			}
			return `{"code":0,"data":{"list":[{"fid":"existing-file","file_name":"old.mp4"}]}}`
		}
		if !strings.HasSuffix(r.URL.Path, "/save") {
			t.Fatal("unexpected write", r.URL)
		}
		saves++
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		if in["pdir_fid"] != "source-folder" || in["to_pdir_fid"] != "existing-folder" || len(in["fid_list"].([]any)) != 1 || in["fid_list"].([]any)[0] != "new" {
			t.Fatal(in)
		}
		return `{"code":0,"data":{"task_id":"job"}}`
	})
	w := request(t, h, "POST", "/api/files/share/batch", map[string]any{"storageId": "q", "preview": "key", "parent": "0", "batch": 0}, cookie)
	if w.Code != 200 || saves != 1 || !strings.Contains(w.Body.String(), `"skipped":1`) {
		t.Fatal(w.Code, w.Body.String(), saves)
	}
}
