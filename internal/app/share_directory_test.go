package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func shareDirectoryPointer(value string) *string { return &value }

func TestSharePreviewCustomDirectoryKeepsSourceIdentity(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "private"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	shareMock(t, func(r *http.Request) string {
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			return `{"code":0,"data":{"stoken":"private"}}`
		case strings.HasSuffix(r.URL.Path, "/detail"):
			if r.URL.Query().Get("pdir_fid") == "0" {
				return `{"code":0,"data":{"list":[{"fid":"source-dir","file_name":"Original","dir":true}]}}`
			}
			return `{"code":0,"data":{"list":[{"fid":"file","file_name":"episode.mkv","share_fid_token":"private"}]}}`
		default:
			t.Fatal("preview mutated cloud", r.URL.Path)
			return ""
		}
	})
	w := request(t, h, "POST", "/api/files/share/preview", map[string]any{"storageId": s.ID, "url": "https://pan.quark.cn/s/code", "saveDirectory": "Renamed"}, cookie)
	var result struct{ Preview string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	p := a.sharePreviews[result.Preview]
	if len(p.Batches) != 1 || p.Batches[0].Directory != "Renamed" || p.Batches[0].Source != "source-dir" || p.Batches[0].Items[0].ID != "file" {
		t.Fatal("save directory did not replace the outer folder without changing source", p.Batches)
	}
}

func TestShareDirectoryMappingAndValidation(t *testing.T) {
	root := []shareEntry{{ID: "source", Name: "A", IsDir: true}}
	batches := []shareBatch{{Directory: "A", Source: "source", Items: []shareEntry{{ID: "file", Name: "one.mkv"}}}, {Directory: "A/A/Sub", Source: "nested"}, {Directory: "A/Empty", Source: "empty"}}
	for _, tc := range []struct {
		name  string
		value *string
		want  []string
	}{
		{"legacy", nil, []string{"A", "A/A/Sub", "A/Empty"}},
		{"same", shareDirectoryPointer("A"), []string{"A", "A/A/Sub", "A/Empty"}},
		{"renamed", shareDirectoryPointer("自定义"), []string{"自定义", "自定义/A/Sub", "自定义/Empty"}},
		{"flat", shareDirectoryPointer(""), []string{"", "A/Sub", "Empty"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := shareDirectoryBatches(root, batches, tc.value)
			if err != nil {
				t.Fatal(err)
			}
			for i, b := range out {
				if b.Directory != tc.want[i] || b.Source != batches[i].Source || !reflect.DeepEqual(b.Items, batches[i].Items) {
					t.Fatal(out)
				}
			}
			if batches[0].Directory != "A" {
				t.Fatal("input mutated")
			}
		})
	}
	for _, items := range [][]shareEntry{{{ID: "f", Name: "one.mkv"}}, {{ID: "a", Name: "A", IsDir: true}, {ID: "b", Name: "two.mkv"}}} {
		out, err := shareDirectoryBatches(items, []shareBatch{{Directory: "", Items: items}, {Directory: "A/Sub"}}, shareDirectoryPointer("Wrapper"))
		if err != nil || out[0].Directory != "Wrapper" || out[1].Directory != "Wrapper/A/Sub" {
			t.Fatal(out, err)
		}
		if shareRootDirectory(items) != "" {
			t.Fatal("file/multi-root share acquired a default wrapper")
		}
	}
	for _, name := range []string{"..", ".", "../x", "a/b", `a\b`, "a:b", "a\nb", "a\x00b", "a?b", strings.Repeat("a", 256)} {
		if validShareDirectory(name) {
			t.Fatal("unsafe name accepted", name)
		}
	}
	if !validShareDirectory(strings.Repeat("目录", 100)) {
		t.Fatal("valid Unicode folder rejected by byte count")
	}
}

func TestShareInfoReadsRootOnlyAndInvalidDirectoryNeverWrites(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "private"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	calls := 0
	shareMock(t, func(r *http.Request) string {
		calls++
		if strings.HasSuffix(r.URL.Path, "/token") {
			return `{"code":0,"data":{"stoken":"secret"}}`
		}
		if r.URL.Query().Get("pdir_fid") != "0" {
			t.Fatal("metadata recursively scanned a folder")
		}
		return `{"code":0,"data":{"list":[{"fid":"dir","file_name":"Original","dir":true}]}}`
	})
	input := map[string]any{"storageId": s.ID, "url": "https://pan.quark.cn/s/code"}
	if w := request(t, h, "POST", "/api/files/share/info", input, nil); w.Code != 401 || calls != 0 {
		t.Fatal("unauthenticated metadata read")
	}
	w := request(t, h, "POST", "/api/files/share/info", input, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"directory":"Original"`) || calls != 2 || len(a.sharePreviews) != 0 {
		t.Fatal(w.Code, w.Body.String(), calls)
	}
	input["saveDirectory"] = "../bad"
	if w := request(t, h, "POST", "/api/files/share/preview", input, cookie); w.Code != 400 || calls != 2 {
		t.Fatal("invalid wrapper reached upstream", w.Code)
	}
}

func TestShareCustomDirectoryBatchDestinationAndReplay(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "private"}}
			a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			r, _ := http.NewRequest("POST", "/", nil)
			r.AddCookie(cookie)
			a.sharePreviews = map[string]*sharePreview{"key": {Owner: authorizationOwner(r), Config: shareConfig(s), Storage: s.ID, Expires: time.Now().Add(time.Minute), SaveDirectory: shareDirectoryPointer("Renamed"), Batches: []shareBatch{{Directory: "Renamed/Sub", Source: "original-source", Items: []shareEntry{{ID: "file", Name: "episode.mkv", Token: "secret"}}}}}}
			created, saves := []string{}, 0
			shareMock(t, func(r *http.Request) string {
				switch {
				case strings.HasSuffix(r.URL.Path, "/sort"):
					id := r.URL.Query().Get("pdir_fid")
					name, fid := "", ""
					if id == "0" {
						name, fid = "Renamed", "destination"
					}
					if id == "destination" {
						name, fid = "Sub", "nested"
					}
					if name != "" {
						dir := true
						if conflict && id == "0" {
							dir = false
						}
						kind := 0
						if !dir {
							kind = 1
						}
						b, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"list": []any{map[string]any{"fid": fid, "file_name": name, "dir": dir, "file_type": kind}}}})
						return string(b)
					}
					return `{"code":0,"data":{"list":[]}}`
				case strings.HasSuffix(r.URL.Path, "/save"):
					saves++
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if body["to_pdir_fid"] != "nested" || body["pdir_fid"] != "original-source" {
						t.Fatal("source or destination changed incorrectly", body)
					}
					return `{"code":0,"data":{"task_id":"job"}}`
				default:
					created = append(created, r.URL.Path)
					return `{}`
				}
			})
			body := map[string]any{"storageId": s.ID, "preview": "key", "parent": "0", "batch": 0}
			w := request(t, h, "POST", "/api/files/share/batch", body, cookie)
			if conflict {
				if w.Code != 400 || saves != 0 {
					t.Fatal("file conflict was overwritten", w.Code)
				}
			} else {
				if w.Code != 200 || saves != 1 || !strings.Contains(w.Body.String(), `"destination":"destination"`) {
					t.Fatal(w.Code, w.Body.String(), saves)
				}
			}
			if len(created) != 0 {
				t.Fatal("unnecessary folder creation", created)
			}
			if w := request(t, h, "POST", "/api/files/share/batch", body, cookie); w.Code != 409 {
				t.Fatal("replayed a consumed batch", w.Code)
			}
			if !conflict && saves != 1 {
				t.Fatal("duplicate cloud write")
			}
		})
	}
}

func TestShareSaveDirectoryCreatedOnceAndUncertainCreationNotReplayed(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprint(uncertain), func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "private"}}
			a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			r, _ := http.NewRequest("POST", "/", nil)
			r.AddCookie(cookie)
			batches := []shareBatch{{Directory: "Wrapped", Source: "root", Items: []shareEntry{{ID: "one", Name: "one.mkv", Token: "token-one"}}}, {Directory: "Wrapped", Source: "root", Items: []shareEntry{{ID: "two", Name: "two.mkv", Token: "token-two"}}}}
			a.sharePreviews = map[string]*sharePreview{"key": {Owner: authorizationOwner(r), Config: shareConfig(s), Storage: s.ID, Expires: time.Now().Add(time.Minute), SaveDirectory: shareDirectoryPointer("Wrapped"), Batches: batches}}
			creates, saves := 0, 0
			shareMock(t, func(r *http.Request) string {
				switch {
				case strings.HasSuffix(r.URL.Path, "/sort"):
					if r.URL.Query().Get("pdir_fid") == "0" && creates > 0 {
						return `{"code":0,"data":{"list":[{"fid":"new-directory","file_name":"Wrapped","dir":true}]}}`
					}
					return `{"code":0,"data":{"list":[]}}`
				case strings.HasSuffix(r.URL.Path, "/file"):
					creates++
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if body["pdir_fid"] != "0" || body["file_name"] != "Wrapped" {
						t.Fatal(body)
					}
					if uncertain {
						return `{"code":1}`
					}
					return `{"code":0}`
				case strings.HasSuffix(r.URL.Path, "/save"):
					saves++
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if body["to_pdir_fid"] != "new-directory" || body["pdir_fid"] != "0" {
						t.Fatal("file share wrapped incorrectly", body)
					}
					return `{"code":0,"data":{"task_id":"job"}}`
				case strings.HasSuffix(r.URL.Path, "/task"):
					return `{"code":0,"data":{"status":2}}`
				default:
					t.Fatal("unexpected cloud operation", r.URL.Path)
					return ""
				}
			})
			body := map[string]any{"storageId": s.ID, "preview": "key", "parent": "0", "batch": 0}
			w := request(t, h, "POST", "/api/files/share/batch", body, cookie)
			if uncertain {
				if w.Code != 400 || saves != 0 {
					t.Fatal(w.Code, saves)
				}
			} else {
				if w.Code != 200 || saves != 1 {
					t.Fatal(w.Code, w.Body.String(), saves)
				}
				if w = request(t, h, "POST", "/api/files/share/status", body, cookie); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				body["batch"] = 1
				if w = request(t, h, "POST", "/api/files/share/batch", body, cookie); w.Code != 200 || saves != 2 {
					t.Fatal(w.Code, w.Body.String(), saves)
				}
			}
			if creates != 1 {
				t.Fatal("wrapper was created repeatedly", creates)
			}
			if w = request(t, h, "POST", "/api/files/share/batch", body, cookie); w.Code != 409 || creates != 1 {
				t.Fatal("uncertain/successful batch replayed", w.Code)
			}
		})
	}
}
