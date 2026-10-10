package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShareNoticesRequireFinalAcceptedPlan(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Name: "Quark", Type: "quark"}
	p := sharePreview{Batches: make([]shareBatch, 2), Next: 1, Submitted: 12, Skipped: 3}
	a.recordShareNotice("key", s, p)
	p.Next = 2
	p.TaskID = "pending"
	a.recordShareNotice("key", s, p)
	p.TaskID = ""
	p.Failed = true
	a.recordShareNotice("key", s, p)
	p.Failed = false
	p.BatchBusy = true
	a.recordShareNotice("key", s, p)
	if len(a.store.snapshot().ShareNotices) != 0 {
		t.Fatal("premature success notification")
	}
	p.BatchBusy = false
	a.recordShareNotice("key", s, p)
	a.recordShareNotice("key", s, p)
	st := a.store.snapshot()
	if len(st.ShareNotices) != 1 || st.ShareNotices[0].Status != "completed" || st.ShareNotices[0].Message != "转存成功 · 12 项 · 跳过 3 项" {
		t.Fatal(st.ShareNotices)
	}
	logs := 0
	for _, l := range st.Logs {
		if strings.Contains(l.Message, "[key]") {
			logs++
		}
	}
	if logs != 1 {
		t.Fatal("duplicate completion log", logs)
	}
	for _, provider := range []string{"115", "mobile"} {
		s.Type = provider
		a.recordShareNotice(provider, s, p)
		n := a.store.snapshot().ShareNotices
		if n[len(n)-1].Status != "submitted" || strings.Contains(n[len(n)-1].Message, "成功") {
			t.Fatal(n)
		}
	}
}

func TestShareNoticeRuntimePersistenceAndBound(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "p", Name: "private-share-result", Type: "quark"}
	a.recordShareNotice("saved", s, sharePreview{Used: true, Submitted: 5})
	for _, module := range jsonModules {
		raw, err := os.ReadFile(filepath.Join(a.store.dir, filepath.FromSlash(module)+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "private-share-result") {
			t.Fatal("result stored in config", module)
		}
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if n := reopened.snapshot().ShareNotices; len(n) != 1 || n[0].ID != "saved" {
		t.Fatal(n)
	}
	for i := 0; i < 201; i++ {
		a.recordShareNotice(fmt.Sprint(i), s, sharePreview{Used: true})
	}
	if n := a.store.snapshot().ShareNotices; len(n) != 200 || n[0].ID != "1" {
		t.Fatal("unbounded notices", len(n))
	}
}

func TestShareBatchQuarkCompletionRecordsOnceAfterLastBatch(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Name: "Quark", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "private-cookie"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	r, _ := http.NewRequest("POST", "/", nil)
	r.AddCookie(cookie)
	a.sharePreviews = map[string]*sharePreview{"key": {Owner: authorizationOwner(r), Config: shareConfig(s), Storage: s.ID, Expires: time.Now().Add(time.Minute), Batches: []shareBatch{
		{Items: []shareEntry{{ID: "f1", Name: "a.strm", Token: "t1"}}},
		{Items: []shareEntry{{ID: "f2", Name: "b.strm", Token: "t2"}}},
	}}}
	shareMock(t, func(r *http.Request) string {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sort"):
			return `{"code":0,"data":{"list":[]}}`
		case strings.HasSuffix(r.URL.Path, "/task"):
			return `{"code":0,"data":{"status":2}}`
		default:
			return `{"code":0,"data":{"task_id":"job"}}`
		}
	})
	for batch := 0; batch < 2; batch++ {
		in := map[string]any{"storageId": "q", "preview": "key", "parent": "0", "batch": batch}
		w := request(t, h, "POST", "/api/files/share/batch", in, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if len(a.store.snapshot().ShareNotices) != 0 {
			t.Fatal("recorded before confirmation")
		}
		w = request(t, h, "POST", "/api/files/share/status", in, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if batch == 0 && len(a.store.snapshot().ShareNotices) != 0 {
			t.Fatal("recorded partial plan")
		}
	}
	w := request(t, h, "GET", "/api/state", nil, cookie)
	var st State
	if json.Unmarshal(w.Body.Bytes(), &st) != nil || len(st.ShareNotices) != 1 || st.ShareNotices[0].Message != "转存成功 · 2 项" {
		t.Fatal(w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private-cookie") {
		t.Fatal("credential leaked")
	}
}
