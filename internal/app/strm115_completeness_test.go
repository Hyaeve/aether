package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSTRMLifeNamedTypesAndFieldAliases(t *testing.T) {
	for _, shape := range []string{"named-type", "aliases", "created-time", "zero-updated-time"} {
		t.Run(shape, func(t *testing.T) {
			a, s, _ := strm115Fixture(t)
			until := time.Now()
			cursor := strmLifeCursor{ID: "300", Since: until.Add(-time.Hour).Unix()}
			shareMock(t, func(r *http.Request) string {
				row := fmt.Sprintf(`{"id":"302","type":"upload_file","file_id":"file","parent_id":"10","file_name":"film.mkv","update_time":%d}`, until.Unix())
				if shape == "aliases" {
					row = fmt.Sprintf(`{"id":"302","type":2,"fid":"file","cid":"10","n":"film.mkv","pc":"pick1234567890","time":%d}`, until.Unix())
				}
				if shape == "created-time" {
					row = fmt.Sprintf(`{"id":"302","type":2,"file_id":"file","parent_id":"10","file_name":"film.mkv","create_time":%d}`, until.Unix())
				}
				if shape == "zero-updated-time" {
					row = fmt.Sprintf(`{"id":"302","type":2,"file_id":"file","parent_id":"10","file_name":"film.mkv","update_time":0,"create_time":%d}`, until.Unix())
				}
				return `{"state":true,"data":{"count":1,"list":[` + row + `]}}`
			})
			events, next, err := a.pullSTRMLife(a.ctx, s, cursor, until)
			if err != nil || len(events) != 1 || next.ID != "302" || events[0].Type != 2 || events[0].FileID != "file" || events[0].Parent != "10" || events[0].Name != "film.mkv" {
				t.Fatal("valid source event was lost or consumed without processing", events, next, err)
			}
		})
	}
}

func Test115LifeFullSyncFallbackRepairsOutputsAndContinuesMonitoring(t *testing.T) {
	original := "大卫·艾登堡：大猩猩的故事.A.Gorilla.Story:.Told.by.David.Attenborough.2026.4K.NF.WEB-DL.DV.H265.DDP.5.1-HiveWeb.mkv"
	added := "E:03.mkv"
	if runtime.GOOS == "windows" {
		original, added = "E02.mkv", "E03.mkv"
	}
	localName := strings.TrimSuffix(original, ".mkv") + ".strm"
	a, s, task, _ := strmBulkFixture(t, 2, "")
	task.STRMSource, task.fullSyncGeneration, task.STRMLifeSince = "life", true, time.Now().Add(-time.Hour)
	if err := a.store.update(func(st *State) error { st.Tasks[0] = task; return nil }); err != nil {
		t.Fatal(err)
	}
	base := apiClient.Transport
	monitoring := false
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/behavior/detail" {
			if r.URL.Query().Get("limit") == "1" {
				return jsonHTTPResponse(`{"state":true,"data":{"list":[{"id":"300"}]}}`), nil
			}
			return jsonHTTPResponse(`{"state":true,"data":{"count":1,"list":[{"id":"302","type":"upload_file","file_id":"10003","parent_id":"20","file_name":"` + added + `","update_time":` + timeNowUnix() + `}]}}`), nil
		}
		if strings.Contains(r.URL.Path, "downfolders") {
			return jsonHTTPResponse(`{"state":true,"data":{"list":[{"fid":"20","pid":"20","fn":"broken-entry"}],"has_next_page":false}}`), nil
		}
		if r.URL.Path == "/category/get" {
			return jsonHTTPResponse(`{"file_category":0,"pick_code":"folderpick12345","count":3,"folder_count":2}`), nil
		}
		if r.URL.Path != "/files" {
			return base.RoundTrip(r)
		}
		if r.URL.Query().Get("show_dir") == "0" {
			t.Error("invalid directory table was accepted as a complete bulk snapshot")
		}
		cid := r.URL.Query().Get("cid")
		trail := []map[string]string{{"cid": "0", "name": "根目录"}, {"cid": "10", "name": "Show"}}
		list := []map[string]any{}
		if cid == "10" {
			list = []map[string]any{{"cid": "20", "pid": "10", "n": "Season 1"}, {"cid": "21", "pid": "10", "n": "Season 2"}}
		} else if cid == "20" || cid == "21" {
			season := "Season 1"
			numbers := []int{1, 2}
			if monitoring {
				numbers = append(numbers, 3)
			}
			if cid == "21" {
				season, numbers = "Season 2", []int{21}
			}
			trail = append(trail, map[string]string{"cid": cid, "name": season})
			for _, n := range numbers {
				name := fmt.Sprintf("E%02d.mkv", n)
				if n == 2 {
					name = original
				} else if n == 3 {
					name = added
				}
				list = append(list, map[string]any{"fid": strconv.Itoa(10000 + n), "cid": cid, "n": name, "s": 42, "pc": fmt.Sprintf("pick%012d", n)})
			}
		} else {
			t.Error("fallback left the selected tree", cid)
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		// Deliberately return short pages. The next offset must use the actual
		// returned length, including a one-file final page.
		end := min(len(list), offset+1)
		if offset > len(list) {
			t.Fatal("fallback jumped over a short page", offset, len(list))
		}
		raw, err := json.Marshal(map[string]any{"state": true, "cid": cid, "offset": offset, "count": len(list), "path": trail, "data": list[offset:end]})
		if err != nil {
			t.Fatal(err)
		}
		return jsonHTTPResponse(string(raw)), nil
	})
	t.Cleanup(func() { apiClient.Transport = base })
	if _, err := a.strm115BulkPlan(a.ctx, s, task.Source, nil); err == nil || !strings.Contains(err.Error(), "目录表条目无效") || !strings.Contains(err.Error(), "broken-entry") {
		t.Fatal("invalid bulk entry did not identify its cause", err)
	}
	if n, err := a.executeTask(a.ctx, task, s); err != nil || n != 3 {
		t.Fatal("fallback full sync did not generate all files", n, err)
	}
	missing := filepath.Join(task.Target, "Show", "Season 1", localName)
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	if n, err := a.executeTask(a.ctx, task, s); err != nil || n != 1 {
		t.Fatal("fallback full sync did not repair its missing tail output", n, err)
	}
	for _, tc := range []struct{ season, episode int }{{1, 1}, {1, 2}, {2, 21}} {
		name := filepath.Join(task.Target, "Show", fmt.Sprintf("Season %d", tc.season), fmt.Sprintf("E%02d.strm", tc.episode))
		if tc.episode == 2 {
			name = missing
		}
		body, err := os.ReadFile(name)
		if err != nil || !strings.Contains(string(body), fmt.Sprintf("/pick%012d/", tc.episode)) {
			t.Fatal("fallback output missing or had an incorrect pickcode", name, err)
		}
		if tc.episode == 2 {
			link, err := url.Parse(strings.TrimSpace(string(body)))
			ref, refErr := a.readSTRMReference("333554321/pick000000000002")
			if err != nil || !strings.HasSuffix(link.Path, "/"+original) || refErr != nil || ref.Name != original || ref.File != "10002" {
				t.Fatal("playback did not retain the exact cloud filename", link, ref, err, refErr)
			}
		}
	}
	monitoring = true
	if n, err := a.executeSTRMLife(a.ctx, task, s); err != nil || n != 1 {
		t.Fatal("monitoring did not resume after fallback full sync", n, err)
	}
	if body, err := os.ReadFile(filepath.Join(task.Target, "Show", "Season 1", strings.TrimSuffix(added, ".mkv")+".strm")); err != nil || !strings.Contains(string(body), "/pick000000000003/") {
		t.Fatal("new life event after fallback full sync was missed", err)
	}
}

func TestSTRMLifeDeferredEventBlocksAdvancingPastItsIdentity(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	until := time.Now()
	cursor := strmLifeCursor{ID: "300", Since: until.Add(-time.Hour).Unix()}
	shareMock(t, func(r *http.Request) string {
		return fmt.Sprintf(`{"state":true,"data":{"count":2,"list":[{"id":"303","type":2,"file_id":"newer","parent_id":"10","file_name":"newer.mkv","update_time":%d},{"id":"302","type":2,"file_id":"deferred","parent_id":"10","file_name":"deferred.mkv","update_time":%d}]}}`, until.Unix(), until.Add(time.Second).Unix())
	})
	events, next, err := a.pullSTRMLife(a.ctx, s, cursor, until)
	if err != nil || len(events) != 0 || next != cursor {
		t.Fatal("cursor advanced past a deferred event and would permanently skip it", events, next, err)
	}
	events, next, err = a.pullSTRMLife(a.ctx, s, cursor, until.Add(2*time.Second))
	if err != nil || len(events) != 2 || events[0].FileID != "deferred" || next.ID != "303" {
		t.Fatal("deferred event was not processed in the next eligible poll", events, next, err)
	}
}

func TestSTRMLifeFullSyncBaselineUsesRequestStart(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			a, s, _ := strm115Fixture(t)
			started := time.Now().Unix()
			shareMock(t, func(r *http.Request) string {
				if r.URL.Query().Get("limit") == "1" {
					time.Sleep(1100 * time.Millisecond)
					if empty {
						return `{"state":true,"data":{"list":[]}}`
					}
					return `{"state":true,"data":{"list":[{"id":"300"}]}}`
				}
				return fmt.Sprintf(`{"state":true,"data":{"count":1,"list":[{"id":"302","type":2,"file_id":"during-request","parent_id":"10","file_name":"during-request.mkv","update_time":%d}]}}`, started)
			})
			cursor, err := a.latestSTRMLifeCursor(a.ctx, s, time.Unix(started-3600, 0))
			if err != nil || cursor.Since > started {
				t.Fatal("baseline lost the time while its request was in flight", cursor, started, err)
			}
			events, next, err := a.pullSTRMLife(a.ctx, s, cursor, time.Now())
			if err != nil || len(events) != 1 || next.ID != "302" {
				t.Fatal("event during baseline request was permanently missed", events, next, err)
			}
		})
	}
}

func Test115FullSyncDoesNotSilentlySkipUnsupportedNames(t *testing.T) {
	for _, kind := range []string{"file", "directory", "excluded-file", "excluded-directory", "unselected-file", "excluded-colon-file"} {
		t.Run(kind, func(t *testing.T) {
			a, s, _ := strm115Fixture(t)
			task := Task{ID: "unsupported-name", Name: "名称检查", Kind: "strm", StorageID: s.ID, Source: "/", Target: t.TempDir(), Mode: strmUpdate, STRMSource: "life", MediaExtensions: "mkv", fullSyncGeneration: true}
			if kind == "excluded-file" {
				task.ExcludeFiles = `film\part.mkv`
			}
			if kind == "excluded-directory" {
				task.ExcludeDirs = "Season:1"
			}
			if kind == "excluded-colon-file" {
				task.ExcludeFiles = "film:part.mkv"
			}
			if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
				t.Fatal(err)
			}
			shareMock(t, func(r *http.Request) string {
				if r.URL.Path == "/behavior/detail" {
					return `{"state":true,"data":{"list":[]}}`
				}
				if r.URL.Path == "/files" && r.URL.Query().Get("cid") == "0" {
					row := `{"fid":"file","cid":"0","n":"film\\part.mkv","pc":"pick1234567890","s":42}`
					if strings.HasSuffix(kind, "directory") {
						row = `{"cid":"20","pid":"0","n":"Season:1","pc":"folderpick12345"}`
					}
					if kind == "unselected-file" {
						row = `{"fid":"file","cid":"0","n":"read\\me.txt","pc":"pick1234567890","s":42}`
					}
					if kind == "excluded-colon-file" {
						row = `{"fid":"file","cid":"0","n":"film:part.mkv","pc":"pick1234567890","s":42}`
					}
					return `{"state":true,"cid":"0","offset":0,"count":1,"data":[` + row + `]}`
				}
				t.Error("unexpected name preflight request", r.Method, r.URL.Path)
				return `{"state":false}`
			})
			n, err := a.executeTask(a.ctx, task, s)
			if kind == "file" || kind == "directory" {
				if err == nil || !strings.Contains(err.Error(), "名称") || n != 0 {
					t.Fatal("full sync reported success after silently omitting a selected item", n, err)
				}
			} else if err != nil || n != 0 {
				t.Fatal("task filters should still exclude unwanted items", n, err)
			}
		})
	}
}

func Test115FullSyncPreservesBothColonNames(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	task := Task{ID: "colon-conflict", Kind: "strm", StorageID: s.ID, Source: "/", Target: t.TempDir(), Mode: strmUpdate, MediaExtensions: "mkv", fullSyncGeneration: true}
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	const wide = `{"fid":"wide","cid":"0","n":"film：cut.mkv","pc":"widepick123456","s":42}`
	const ascii = `{"fid":"ascii","cid":"0","n":"film:cut.mkv","pc":"asciipick123456","s":42}`
	items := []string{ascii, wide}
	shareMock(t, func(r *http.Request) string {
		if r.Method != "GET" || r.URL.Path != "/files" || r.URL.Query().Get("cid") != "0" {
			t.Fatal("unexpected root scan request", r.Method, r.URL)
		}
		return fmt.Sprintf(`{"state":true,"cid":"0","count":%d,"offset":0,"data":[%s]}`, len(items), strings.Join(items, ","))
	})
	if runtime.GOOS == "windows" {
		if n, err := a.executeTask(a.ctx, task, s); n != 0 || err == nil || !strings.Contains(err.Error(), "原样保存含英文冒号") {
			t.Fatal("Windows must report its path limitation without rewriting a source name", n, err)
		}
		if entries, err := os.ReadDir(task.Target); err != nil || len(entries) != 0 {
			t.Fatal("preflight wrote an output despite the unsupported path", entries, err)
		}
	}
	task.ExcludeFiles = "film:cut.mkv"
	if err := a.store.update(func(st *State) error { st.Tasks[0] = task; return nil }); err != nil {
		t.Fatal(err)
	}
	if n, err := a.executeTask(a.ctx, task, s); n != 1 || err != nil {
		t.Fatal("excluding the raw cloud name should leave one unambiguous source", n, err)
	}
	if n, err := a.executeTask(a.ctx, task, s); n != 0 || err != nil {
		t.Fatal("unchanged full-width filename was rewritten", n, err)
	}
	wideOutput := filepath.Join(task.Target, "film：cut.strm")
	if body, err := os.ReadFile(wideOutput); err != nil || !strings.Contains(string(body), "/widepick123456/film：cut.mkv") {
		t.Fatal("full-width name or its playback address changed", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	task.ExcludeFiles = ""
	if err := a.store.update(func(st *State) error { st.Tasks[0] = task; return nil }); err != nil {
		t.Fatal(err)
	}
	if n, err := a.executeTask(a.ctx, task, s); n != 1 || err != nil {
		t.Fatal("the ASCII colon file must be generated alongside the full-width name", n, err)
	}
	asciiOutput := filepath.Join(task.Target, "film:cut.strm")
	if body, err := os.ReadFile(asciiOutput); err != nil || !strings.Contains(string(body), "/asciipick123456/film:cut.mkv") {
		t.Fatal("ASCII colon name or its playback address changed", err)
	}
	if err := os.Remove(asciiOutput); err != nil {
		t.Fatal(err)
	}
	items = []string{ascii}
	if n, err := a.executeTask(a.ctx, task, s); n != 1 || err != nil {
		t.Fatal("full sync must restore the ASCII colon file and keep the previous full-width output", n, err)
	}
	if body, err := os.ReadFile(wideOutput); err != nil || !strings.Contains(string(body), "/widepick123456/film：cut.mkv") {
		t.Fatal("full-width output was removed or replaced", err)
	}
}
