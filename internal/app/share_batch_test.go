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

func TestMobileShareNestedBatchLimit(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "mobile", Type: "mobile", Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
	shareMock(t, func(r *http.Request) string {
		var body struct {
			Request struct {
				Parent string `json:"pCaID"`
			} `json:"getOutLinkInfoReq"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Request.Parent != "directory" {
			t.Fatalf("parent: %s", body.Request.Parent)
		}
		files := []map[string]string{}
		for i := 1; i <= 1001; i++ {
			files = append(files, map[string]string{"coID": fmt.Sprint(i), "coName": fmt.Sprintf("%d.mkv", i), "coPath": fmt.Sprintf("root/directory/%d", i)})
		}
		data, _ := json.Marshal(map[string]any{"code": "0", "data": map[string]any{"nodNum": 1001, "coLst": files}})
		return string(data)
	})
	plan, err := a.planShare(context.Background(), s, "link", "", []shareEntry{{ID: "directory", Name: "Show", IsDir: true}})
	if err != nil || len(plan) != 11 {
		t.Fatal(len(plan), err)
	}
	if len(plan[0].Items) != 100 || len(plan[10].Items) != 1 {
		t.Fatal("incorrect batches")
	}
	for _, batch := range plan {
		if batch.Directory != "Show" {
			t.Fatal(batch.Directory)
		}
		for _, file := range batch.Items {
			if file.IsDir {
				t.Fatal("unexpanded directory")
			}
		}
	}
}

func TestShareBatchReplayAndSequence(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "secret"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	r, _ := http.NewRequest("POST", "/", nil)
	r.AddCookie(cookie)
	item := shareEntry{ID: "file", Name: "film.mkv", Token: "private"}
	a.sharePreviews = map[string]*sharePreview{"key": {Owner: authorizationOwner(r), Config: shareConfig(s), Storage: s.ID, Expires: time.Now().Add(time.Minute), Items: []shareEntry{item}, Batches: []shareBatch{{Items: []shareEntry{item}}, {Items: []shareEntry{{ID: "two", Name: "two.mkv", Token: "private"}}}}}}
	saves := 0
	shareMock(t, func(r *http.Request) string {
		if strings.HasSuffix(r.URL.Path, "/sort") {
			return `{"code":0,"data":{"list":[]}}`
		}
		if strings.HasSuffix(r.URL.Path, "/task") {
			return `{"code":0,"data":{"status":2}}`
		}
		saves++
		return `{"code":0,"data":{"task_id":"job"}}`
	})
	body := map[string]any{"storageId": "q", "preview": "key", "parent": "0", "batch": 0}
	if w := request(t, h, "POST", "/api/files/share/batch", body, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(t, h, "POST", "/api/files/share/batch", body, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", "/api/files/share/batch", body, cookie); w.Code != 409 {
		t.Fatal("replay", w.Code)
	}
	body["batch"] = 1
	if w := request(t, h, "POST", "/api/files/share/batch", body, cookie); w.Code != 409 {
		t.Fatal("overlapping quark job", w.Code)
	}
	if w := request(t, h, "POST", "/api/files/share/status", body, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(t, h, "POST", "/api/files/share/batch", body, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if saves != 2 {
		t.Fatal(saves)
	}
}
