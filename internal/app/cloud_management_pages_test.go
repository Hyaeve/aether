package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestCloudManagementPagesLazyFlexibleNumbersAndPaths(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "paged115", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	reads := []string{}
	shareMock(t, func(r *http.Request) string {
		reads = append(reads, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/share/slist":
			rows := []map[string]any{}
			if r.URL.Query().Get("offset") == "0" {
				for i := 0; i < 100; i++ {
					rows = append(rows, map[string]any{"share_code": fmt.Sprint("code", i), "share_title": "影片", "receive_code": "ABCD", "share_state": 1})
				}
			}
			b, _ := json.Marshal(map[string]any{"state": true, "data": map[string]any{"list": rows, "count": 500}})
			return string(b)
		case "/lixian/":
			if r.Method != "POST" {
				t.Fatal("unexpected offline read method")
			}
			if r.URL.Query().Get("page") == "2" {
				return `{"state":false}`
			}
			return `{"state":true,"page_count":"2","tasks":[{"info_hash":"hash","name":"film.iso","size":"37961990144","add_time":"100","status":"1","percentDone":"12.5","wp_path_id":"20"}]}`
		case "/rb":
			return `{"state":true,"data":[{"id":"deleted","file_name":"film.mkv","file_size":"4","cid":"20","parent_name":"Season 1","dtime":"100"}]}`
		case "/files":
			return `{"state":true,"cid":"20","path":[{"cid":"0","name":"根目录"},{"cid":"10","name":"剧集"},{"cid":"20","name":"Season 1"}],"data":[]}`
		}
		t.Fatal("unexpected endpoint", r.URL)
		return ""
	})
	for _, kind := range []string{"shares", "downloads", "trash"} {
		reads = nil
		w := request(t, h, "GET", "/api/cloud/manage?storage="+s.ID+"&kind="+kind+"&page=1", nil, cookie)
		var page struct {
			Items []cloudManagementEntry `json:"items"`
			Next  int                    `json:"nextPage"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil {
			t.Fatal(kind, w.Code, w.Body.String())
		}
		if kind == "shares" {
			if len(page.Items) != 100 || page.Next != 2 || len(reads) != 1 || !strings.Contains(page.Items[0].URL, "password=ABCD") {
				t.Fatal(page, reads)
			}
		} else {
			if len(page.Items) != 1 || len(reads) != 2 || page.Items[0].Path != "/剧集/Season 1" {
				t.Fatal(page, reads)
			}
			if kind == "downloads" && (page.Next != 2 || page.Items[0].Size != 37961990144 || page.Items[0].Progress != .125) {
				t.Fatal(page)
			}
		}
	}
	if w := request(t, h, "GET", "/api/cloud/manage?storage="+s.ID+"&kind=downloads&page=2", nil, cookie); w.Code != 502 {
		t.Fatal(w.Code, w.Body.String())
	}
	before := len(reads)
	if request(t, h, "GET", "/api/cloud/manage?storage="+s.ID+"&kind=trash&page=-1", nil, cookie).Code != 400 || len(reads) != before {
		t.Fatal("invalid page accessed upstream")
	}
}

func TestCloudManagementAggregateCASAndUnsupportedCapabilities(t *testing.T) {
	a := testApp(t)
	m := Storage{ID: "m", Name: "移动1", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native"}}
	c := Storage{ID: "t", Name: "天翼1", Type: "tianyi", Enabled: true, Config: map[string]string{"username": "test", "password": "test"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{m, c}; return nil })
	a.beginCloudCASRecord(m, "/mobile", CASInfo{Name: "a.mkv", Size: 12})
	a.beginCloudCASRecord(c, "/tianyi", CASInfo{Name: "b.mkv", Size: 13})
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	shareMock(t, func(r *http.Request) string { t.Fatal("unexpected upstream", r.URL); return "" })
	w := request(t, h, "GET", "/api/cloud/manage?kind=downloads&storage=cas&page=1", nil, cookie)
	var result struct {
		Items []cloudManagementEntry `json:"items"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Items) != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
	if result.Items[0].StorageName != "天翼1" || result.Items[0].Provider != "tianyi" || result.Items[1].StorageID != "m" {
		t.Fatal(result)
	}
	for _, kind := range []string{"trash", "shares"} {
		if request(t, h, "GET", "/api/cloud/manage?kind="+kind+"&storage=m", nil, cookie).Code != 400 {
			t.Fatal("unsupported capability exposed")
		}
	}
}
