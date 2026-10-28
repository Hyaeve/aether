package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func archivePageForTest(t *testing.T, h http.Handler, cookie *http.Cookie, endpoint string) map[string]any {
	t.Helper()
	w := request(t, h, "GET", endpoint, nil, cookie)
	var result map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatalf("archive page %d: %s", w.Code, w.Body.String())
	}
	return result
}

func TestTaiyiArchivesScrapeResultsAndSafeRetry(t *testing.T) {
	a := testApp(t)
	base := t.TempDir()
	for _, file := range []string{"Film/one.strm", "Missing/one.strm", "Doubt/one.strm", "Broken/one.strm"} {
		writeTest(t, filepath.Join(base, filepath.FromSlash(file)), "http://example.test/video")
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/3/search/movie":
			title := r.URL.Query().Get("query")
			switch title {
			case "Film":
				jsonResponse(w, 200, map[string]any{"results": []any{map[string]any{"id": 1, "title": title}}})
			case "Doubt":
				jsonResponse(w, 200, map[string]any{"results": []any{map[string]any{"id": 2, "title": title}, map[string]any{"id": 3, "title": title}}})
			case "Broken":
				w.WriteHeader(503)
			default:
				jsonResponse(w, 200, map[string]any{"results": []any{}})
			}
		case "/3/movie/1":
			jsonResponse(w, 200, map[string]any{"id": 1, "title": "Canonical Film"})
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	if err := a.store.update(func(st *State) error {
		st.Tasks = []Task{{ID: "t", Name: "测试库", Kind: "strm", Target: base}}
		st.Plugins = map[string]PluginConfig{"tmdb": {Enabled: true, APIURL: up.URL, ImageURL: up.URL, APIKey: "key", RequestInterval: 1}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	for _, endpoint := range []string{"/api/archives/scrape", "/api/archives/tasks"} {
		if w := request(t, h, "GET", endpoint, nil, nil); w.Code != 401 {
			t.Fatal("archive is not protected", w.Code)
		}
	}
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	if w := request(t, h, "POST", "/api/strm-scrape/identify", map[string]any{"taskId": "t", "scrape": true}, cookie); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitRevisionScrape(t, a)
	logged := false
	for _, event := range a.store.snapshot().Logs {
		if strings.HasPrefix(event.Message, "档案中心：STRM ") {
			logged = true
		}
	}
	if !logged {
		t.Fatal("archive events must use the current display name")
	}
	page := archivePageForTest(t, h, cookie, "/api/archives/scrape")
	if page["total"] != float64(4) {
		t.Fatalf("missing actual file results: %#v", page)
	}
	results := map[string]map[string]any{}
	for _, value := range page["items"].([]any) {
		record := value.(map[string]any)
		results[record["path"].(string)] = record
		if record["trigger"] != "manual" || record["taskName"] != "测试库" || record["finishedAt"] == nil {
			t.Fatal(record)
		}
		encoded, _ := json.Marshal(record)
		if strings.Contains(string(encoded), base) || strings.Contains(string(encoded), "example.test") || record["fingerprint"] != nil {
			t.Fatal("private source data leaked", string(encoded))
		}
	}
	for file, status := range map[string]string{"Film/one.strm": "success", "Missing/one.strm": "failed", "Doubt/one.strm": "doubt", "Broken/one.strm": "failed"} {
		if results[file]["status"] != status {
			t.Fatal(file, results[file])
		}
	}
	if results["Film/one.strm"]["title"] != "Canonical Film" {
		t.Fatal("TMDB canonical name lost", results)
	}
	items := request(t, h, "GET", "/api/strm-scrape/items?taskId=t&group=true", nil, cookie)
	if !strings.Contains(items.Body.String(), `"matchedTitle":"Canonical Film"`) {
		t.Fatal("rescanning discarded canonical title", items.Body.String())
	}
	confirmed := map[string]bool{"confirmed": true}
	id := results["Film/one.strm"]["id"].(string)
	retry := "/api/archives/scrape/" + id + "/retry"
	otherStarted := results["Doubt/one.strm"]["startedAt"]
	if w := request(t, h, "POST", "/api/strm-scrape/identify", map[string]any{
		"archiveId": id, "confirmed": true, "paths": []string{"Doubt/one.strm"}, "group": true,
	}, cookie); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitRevisionScrape(t, a)
	page = archivePageForTest(t, h, cookie, "/api/archives/scrape")
	for _, value := range page["items"].([]any) {
		record := value.(map[string]any)
		if record["path"] == "Doubt/one.strm" && record["startedAt"] != otherStarted {
			t.Fatal("archive retry allowed a client-selected different file with identical STRM contents")
		}
	}
	if w := request(t, h, "POST", retry, nil, nil); w.Code != 401 {
		t.Fatal("unprotected retry", w.Code)
	}
	if w := request(t, h, "POST", retry, map[string]bool{}, cookie); w.Code != 400 {
		t.Fatal("unconfirmed retry", w.Code)
	}
	if w := request(t, h, "POST", retry, confirmed, cookie); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitRevisionScrape(t, a)
	if page = archivePageForTest(t, h, cookie, "/api/archives/scrape"); page["total"] != float64(4) {
		t.Fatal("retry duplicated STRM rows", page)
	}
	// Same path is insufficient: never scrape a replacement file or a different task root.
	writeTest(t, filepath.Join(base, "Film", "one.strm"), "http://example.test/replacement")
	if w := request(t, h, "POST", retry, confirmed, cookie); w.Code != 409 {
		t.Fatal("changed source retried", w.Code, w.Body.String())
	}
	writeTest(t, filepath.Join(base, "Film", "one.strm"), "http://example.test/video")
	a.store.update(func(st *State) error { st.Tasks[0].Target = t.TempDir(); return nil })
	if w := request(t, h, "POST", retry, confirmed, cookie); w.Code != 409 {
		t.Fatal("changed output root retried", w.Code, w.Body.String())
	}
}

func TestTaiyiTaskArchivePersistsRuns(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "film.mp4"), "video")
	task := Task{ID: "t", Name: "生成库", Kind: "strm", StorageID: s.ID, Source: "/", Target: t.TempDir(), MediaExtensions: "mp4", Mode: "full"}
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	for range 2 {
		if err := a.startTask(task.ID); err != nil {
			t.Fatal(err)
		}
		a.wg.Wait()
	}
	result := archivePageForTest(t, h, cookie, "/api/archives/tasks?page=999")
	if result["total"] != float64(2) || result["page"] != float64(1) {
		t.Fatal(result)
	}
	for _, v := range result["items"].([]any) {
		record := v.(map[string]any)
		if record["trigger"] != "manual" || record["status"] != "success" || record["taskName"] != task.Name || record["taskKind"] != "strm" {
			t.Fatal(record)
		}
	}
	restored, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil {
		t.Fatal(err)
	}
	restoredHandler := restored.Handler(t.TempDir())
	result = archivePageForTest(t, restoredHandler, cookie, "/api/archives/tasks")
	if result["total"] != float64(2) {
		t.Fatal("execution archives lost after restart", result)
	}
	state := request(t, h, "GET", "/api/state", nil, cookie)
	if strings.Contains(state.Body.String(), `"archives"`) {
		t.Fatal("archives should be loaded on demand, not in polling state")
	}
	if _, err := os.Stat(filepath.Join(a.dataDir, "runtime", "taiyi-archives.json")); err != nil {
		t.Fatal(err)
	}
}

func TestTaiyiArchiveGroupsPaginationAndRecovery(t *testing.T) {
	a := testApp(t)
	if a.archives.file != filepath.Join(a.dataDir, "runtime", "taiyi-archives.json") {
		t.Fatal("display name must not change the persisted archive path", a.archives.file)
	}
	for i := range 66 {
		group := "show"
		if i >= 64 {
			group = "other-root-same-folder"
		}
		record := archiveRecord{archiveEntry: archiveEntry{
			ID: fmt.Sprintf("scrape-%03d", i), Category: "scrape", TaskID: "t", TaskName: "库",
			Title: "标准剧名", Kind: "tv", TMDB: 1, Path: fmt.Sprintf("Show/%d.strm", i),
			StartedAt: time.Now().Add(time.Duration(i) * time.Second), Status: "success", Trigger: "manual",
		}, GroupID: group}
		if err := a.archives.put(record); err != nil {
			t.Fatal(err)
		}
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	page := archivePageForTest(t, h, cookie, "/api/archives/scrape?page=999")
	if page["page"] != float64(3) || page["pages"] != float64(3) || page["total"] != float64(66) || len(page["items"].([]any)) != 6 {
		t.Fatal(page)
	}
	page = archivePageForTest(t, h, cookie, "/api/archives/scrape?mode=group")
	if page["total"] != float64(2) {
		t.Fatal("same-named folders from different roots were merged", page)
	}
	page = archivePageForTest(t, h, cookie, "/api/archives/scrape?group=show&page=2")
	if page["total"] != float64(64) || len(page["items"].([]any)) != 30 {
		t.Fatal("group children are not paginated", page)
	}
	page = archivePageForTest(t, h, cookie, "/api/archives/scrape?q=Show%2F65")
	if page["total"] != float64(1) {
		t.Fatal("source search failed", page)
	}
	if err := a.archives.put(archiveRecord{archiveEntry: archiveEntry{ID: "unfinished", Category: "tasks", Status: "running", StartedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	reopened, err := openExecutionArchives(a.store, a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	record, found := reopened.get("unfinished")
	if !found || record.Status != "interrupted" || record.FinishedAt.IsZero() {
		t.Fatal("unfinished operation was not interrupted", record)
	}
	// Authentication failure must be explicit and must not overwrite the evidence.
	raw, err := os.ReadFile(a.archives.file)
	if err != nil {
		t.Fatal(err)
	}
	tampered := []byte(strings.Replace(string(raw), "标准剧名", "伪造剧名", 1))
	if err := os.WriteFile(a.archives.file, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openExecutionArchives(a.store, a.dataDir); err == nil || err.Error() != "档案中心完整性校验失败" {
		t.Fatal("tampered archive must use the current display name", err)
	}
	after, _ := os.ReadFile(a.archives.file)
	if string(after) != string(tampered) {
		t.Fatal("corrupt archive evidence was overwritten")
	}
}

func TestTaiyiArchiveRetentionAndFailedWriteRollback(t *testing.T) {
	a := testApp(t)
	for i := range 2002 {
		a.archives.records = append(a.archives.records, archiveRecord{archiveEntry: archiveEntry{
			ID: fmt.Sprintf("run-%04d", i), Category: "tasks", Status: "success", StartedAt: time.Unix(int64(i), 0),
		}})
	}
	active := archiveRecord{archiveEntry: archiveEntry{ID: "active-old", Category: "tasks", Status: "running", StartedAt: time.Unix(0, 0)}}
	if err := a.archives.put(active); err != nil {
		t.Fatal(err)
	}
	if len(a.archives.records) != 2001 {
		t.Fatal("retention did not retain 2000 recent runs and the active run", len(a.archives.records))
	}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	writeTest(t, blocked, "file")
	a.archives.file = filepath.Join(blocked, "archives.json")
	active.Status = "success"
	if err := a.archives.put(active); err == nil {
		t.Fatal("failed archive write was ignored")
	}
	if current, ok := a.archives.get(active.ID); !ok || current.Status != "running" {
		t.Fatal("failed write did not roll back memory", current)
	}
}

func TestTaiyiTaskArchivesDistinguishTriggersAndFailures(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "one.mp4"), "video")
	blocked := filepath.Join(t.TempDir(), "file")
	writeTest(t, blocked, "not a directory")
	task := Task{ID: "t", Name: "失败任务", Kind: "strm", Source: "/", StorageID: s.ID, Target: blocked, MediaExtensions: "mp4"}
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []string{"manual", "scheduled", "monitor", "automation"} {
		if err := a.startTaskWithTrigger(context.Background(), task.ID, nil, trigger); err != nil {
			t.Fatal(err)
		}
		a.wg.Wait()
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	page := archivePageForTest(t, h, cookie, "/api/archives/tasks?status=failed")
	if page["total"] != float64(4) {
		t.Fatal("failed task records are not filterable", page)
	}
	for _, trigger := range []string{"manual", "scheduled", "monitor", "automation"} {
		page = archivePageForTest(t, h, cookie, "/api/archives/tasks?trigger="+trigger)
		if page["total"] != float64(1) {
			t.Fatal("trigger provenance lost", trigger, page)
		}
	}
}

func TestTaiyiArchiveWriteFailurePreventsTaskStart(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	task := Task{ID: "t", Name: "不能登记", Kind: "cache", StorageID: s.ID, Source: "/"}
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(t.TempDir(), "file")
	writeTest(t, blocked, "file")
	a.archives.file = filepath.Join(blocked, "archives.json")
	if err := a.startTask(task.ID); err == nil {
		t.Fatal("task started without a durable archive")
	}
	if len(a.running) != 0 || a.store.snapshotWithLogLimit(0).Tasks[0].Status == "running" || len(a.archives.records) != 0 {
		t.Fatal("failed start left a running task or phantom archive")
	}
	target := t.TempDir()
	original := filepath.Join(target, "keep.strm")
	writeTest(t, original, "user output")
	task.Kind, task.Target, task.Mode = "strm", target, "full"
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.startTask(task.ID, true); err == nil {
		t.Fatal("reset started without a durable archive")
	}
	if data, err := os.ReadFile(original); err != nil || string(data) != "user output" {
		t.Fatal("output was reset before archive registration", string(data), err)
	}
}

func TestTaiyiArchiveCancellationKeepsUnfinishedTimeHonest(t *testing.T) {
	a := testApp(t)
	base := t.TempDir()
	writeTest(t, filepath.Join(base, "Film.strm"), "http://example.test/video")
	entered := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	}))
	defer up.Close()
	defer func() {
		a.scrapeMu.Lock()
		defer a.scrapeMu.Unlock()
		if a.scrapeCancel != nil {
			a.scrapeCancel()
		}
	}()
	if err := a.store.update(func(st *State) error {
		st.Tasks = []Task{{ID: "t", Name: "等待识别", Kind: "strm", Target: base}}
		st.Plugins = map[string]PluginConfig{"tmdb": {Enabled: true, APIURL: up.URL, ImageURL: up.URL, APIKey: "key", RequestInterval: 1}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	if w := request(t, h, "POST", "/api/strm-scrape/identify", map[string]any{"taskId": "t"}, cookie); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("scraper never reached upstream")
	}
	page := archivePageForTest(t, h, cookie, "/api/archives/scrape")
	record := page["items"].([]any)[0].(map[string]any)
	if record["status"] != "running" || record["finishedAt"] != nil {
		t.Fatal("unfinished result has a fabricated finish time", record)
	}
	if w := request(t, h, "POST", "/api/strm-scrape/stop", map[string]any{}, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitRevisionScrape(t, a)
	page = archivePageForTest(t, h, cookie, "/api/archives/scrape")
	record = page["items"].([]any)[0].(map[string]any)
	if record["status"] != "cancelled" || record["finishedAt"] == nil {
		t.Fatal("cancelled request is not recorded honestly", record)
	}
	if entries, _ := os.ReadDir(base); len(entries) != 1 {
		t.Fatal("cancellation wrote metadata", entries)
	}
}

func TestTaiyiAutomationArchivesIncludeTheCardAndSteps(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	if err := a.store.update(func(st *State) error {
		st.Tasks = []Task{{ID: "child", Name: "子任务", Kind: "cache", StorageID: s.ID, Source: "/", Enabled: true, Interval: 60}}
		st.Automations = []Automation{
			{ID: "rule", Name: "联动卡片", Enabled: true, Trigger: "cron", Cron: "0 2 * * *", Steps: []AutomationStep{{Kind: "task", TaskID: "child", Condition: "always"}, {Kind: "refresh", Condition: "success"}}},
			{ID: "event", Name: "完成事件", Enabled: true, Trigger: "task", SourceTask: "child", Steps: []AutomationStep{{Kind: "refresh", Condition: "always"}}},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.startAutomation("rule"); err != nil {
		t.Fatal(err)
	}
	a.wg.Wait()
	if err := a.startAutomationWithTrigger("rule", "scheduled"); err != nil {
		t.Fatal(err)
	}
	a.wg.Wait()
	a.triggerAutomations("child")
	a.wg.Wait()
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	page := archivePageForTest(t, h, cookie, "/api/archives/tasks")
	if page["total"] != float64(5) {
		t.Fatal("missing parent or step archives", page)
	}
	kinds, triggers := map[string]int{}, map[string]int{}
	for _, value := range page["items"].([]any) {
		record := value.(map[string]any)
		kinds[record["taskKind"].(string)]++
		triggers[record["trigger"].(string)]++
		if record["status"] != "success" {
			t.Fatal(record)
		}
		if record["taskId"] == "rule" && record["processed"] != float64(2) {
			t.Fatal("automation did not count completed actions", record)
		}
	}
	if kinds["automation"] != 3 || kinds["cache"] != 2 || triggers["manual"] != 1 || triggers["scheduled"] != 1 || triggers["automation"] != 3 {
		t.Fatal("trigger provenance or parent identity lost", kinds, triggers)
	}
	blocked := filepath.Join(t.TempDir(), "file")
	writeTest(t, blocked, "not a directory")
	a.archives.file = filepath.Join(blocked, "archives.json")
	if err := a.startAutomation("rule"); err == nil || len(a.automationRuns) != 0 {
		t.Fatal("automation started without a durable archive", err)
	}
}
