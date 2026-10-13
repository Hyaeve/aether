package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMobileShareNestedBatchLimit(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "mobile", Type: "mobile", Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
	calls := 0
	shareMock(t, func(r *http.Request) string {
		calls++
		var body struct {
			Request struct {
				Parent string `json:"pCaID"`
				Start  int    `json:"bNum"`
				End    int    `json:"eNum"`
			} `json:"getOutLinkInfoReq"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Request.Parent != "directory" || body.Request.Start != (calls-1)*100+1 || body.Request.End != calls*100 {
			t.Fatalf("unexpected listing request: %+v", body.Request)
		}
		files := []map[string]string{}
		for i := body.Request.Start; i <= min(body.Request.End, 1001); i++ {
			files = append(files, map[string]string{"coID": fmt.Sprint(i), "coName": fmt.Sprintf("%d.mkv", i), "coPath": fmt.Sprintf("root/directory/%d", i)})
		}
		data, _ := json.Marshal(map[string]any{"code": "0", "data": map[string]any{"nodNum": 1001, "coLst": files}})
		return string(data)
	})
	plan, err := a.planShare(context.Background(), s, "link", "", []shareEntry{{ID: "directory", Name: "Show", IsDir: true}})
	if err != nil || len(plan) != 3 || calls != 11 {
		t.Fatal(len(plan), calls, err)
	}
	if len(plan[0].Items) != 499 || len(plan[1].Items) != 499 || len(plan[2].Items) != 3 {
		t.Fatal("incorrect batches")
	}
	seen := map[string]bool{}
	for _, batch := range plan {
		if batch.Directory != "Show" {
			t.Fatal(batch.Directory)
		}
		for _, file := range batch.Items {
			if file.IsDir || seen[file.ID] || file.Token != "root/directory/"+file.ID {
				t.Fatal("invalid or repeated file", file)
			}
			seen[file.ID] = true
		}
	}
	if len(seen) != 1001 {
		t.Fatal("missing files", len(seen))
	}
}

func TestShareBatchProviderLimits(t *testing.T) {
	for _, provider := range []string{"mobile", "115", "quark"} {
		for _, count := range []int{100, 101, 498, 499, 500, 501, 998, 999, 1001} {
			t.Run(fmt.Sprintf("%s/%d", provider, count), func(t *testing.T) {
				a := testApp(t)
				s := Storage{Type: provider, Config: map[string]string{"mode": "native"}}
				items := make([]shareEntry, count)
				for i := range items {
					items[i] = shareEntry{ID: fmt.Sprint(i), Name: fmt.Sprintf("%d.mkv", i), Token: fmt.Sprintf("root/%d", i)}
				}
				limit := 499
				plan, err := a.planShare(context.Background(), s, "code", "", items)
				if err != nil || len(plan) != (count+limit-1)/limit {
					t.Fatal(len(plan), err)
				}
				all := []shareEntry{}
				for _, batch := range plan {
					if len(batch.Items) > limit {
						t.Fatal("oversized batch", len(batch.Items))
					}
					all = append(all, batch.Items...)
				}
				if !reflect.DeepEqual(all, items) {
					t.Fatal("batch order or contents changed")
				}
			})
		}
	}
}

func TestMobileShareSubmits499Files(t *testing.T) {
	s := Storage{Type: "mobile", Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
	items := make([]shareEntry, 499)
	want := make([]string, 499)
	for i := range items {
		want[i] = fmt.Sprintf("root/%d", i)
		items[i] = shareEntry{ID: fmt.Sprint(i), Name: fmt.Sprintf("%d.mkv", i), Token: want[i]}
	}
	calls := 0
	shareMock(t, func(r *http.Request) string {
		calls++
		var body struct {
			Request struct {
				TaskInfo struct {
					Files  []string `json:"contentInfoList"`
					Parent string   `json:"newCatalogID"`
				} `json:"taskInfo"`
			} `json:"createOuterLinkBatchOprTaskReq"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(r.URL.Path, "/createOuterLinkBatchOprTask") || body.Request.TaskInfo.Parent != "target" || !reflect.DeepEqual(body.Request.TaskInfo.Files, want) {
			t.Fatal("incorrect submission", body.Request.TaskInfo)
		}
		return `{"code":"0","data":{"taskID":"job"}}`
	})
	task, err := submitShare(context.Background(), s, sharePreview{Code: "code"}, "target", items)
	if err != nil || task != "job" || calls != 1 {
		t.Fatal(task, calls, err)
	}
}

func TestShareRejectsFiveHundredItems(t *testing.T) {
	for _, provider := range []string{"mobile", "115", "quark"} {
		t.Run(provider, func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: provider, Type: provider, Enabled: true, Config: map[string]string{"mode": "native"}}
			if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
				t.Fatal(err)
			}
			h := a.Handler(t.TempDir())
			cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
			r, _ := http.NewRequest("POST", "/", nil)
			r.AddCookie(cookie)
			a.sharePreviews = map[string]*sharePreview{"key": {Owner: authorizationOwner(r), Config: shareConfig(s), Storage: s.ID, Expires: time.Now().Add(time.Minute)}}
			ids := make([]string, 500)
			for i := range ids {
				ids[i] = fmt.Sprint(i)
			}
			calls := 0
			shareMock(t, func(r *http.Request) string { calls++; return `{}` })
			w := request(t, h, "POST", "/api/files/share/save", map[string]any{"storageId": s.ID, "preview": "key", "ids": ids}, cookie)
			if w.Code != 400 || !strings.Contains(w.Body.String(), "499") {
				t.Fatal(w.Code, w.Body.String())
			}
			if _, err := submitShare(context.Background(), s, sharePreview{}, "target", make([]shareEntry, 500)); err == nil || !strings.Contains(err.Error(), "499") {
				t.Fatal("oversized submission accepted", err)
			}
			if calls != 0 {
				t.Fatal("oversized request reached upstream", calls)
			}
		})
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
