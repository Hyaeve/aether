package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOrganizeFailureAndFileFilterPersistWithoutExecution(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	in := organizeTaskDefinition{Name: "待整理", StorageID: s.ID, Source: "/in", TargetStorageID: s.ID, Target: "/out", FailureDirectory: "/unrecognized", FailurePath: "/unrecognized", FailureLabel: "未识别", FailureTrail: []DirectoryCrumb{{ID: "/", Name: "根目录"}}, FileFilterMB: 512}
	w := request(t, h, "POST", "/api/organize-tasks", in, cookie)
	var got organizeTaskDefinition
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.FailureDirectory != in.FailureDirectory || got.FileFilterMB != 512 || len(got.FailureTrail) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || reopened.snapshot().OrganizeTasks[0].FileFilterMB != 512 || reopened.snapshot().OrganizeTasks[0].FailurePath != in.FailurePath {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"fileFilterMB":-1}`, `{"fileFilterMB":1.5}`, `{"fileFilterMB":9007199254740992}`} {
		var body map[string]any
		json.Unmarshal(w.Body.Bytes(), &body)
		var invalid map[string]any
		json.Unmarshal([]byte(raw), &invalid)
		body["fileFilterMB"] = invalid["fileFilterMB"]
		if bad := request(t, h, "PUT", "/api/organize-tasks/"+got.ID, body, cookie); bad.Code != 400 {
			t.Fatal("invalid filter accepted", raw, bad.Code)
		}
	}
	got.FileFilterMB = 0
	got.FailureDirectory = ""
	w = request(t, h, "PUT", "/api/organize-tasks/"+got.ID, got, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	stored := a.store.snapshot().OrganizeTasks[0]
	if stored.FileFilterMB != 0 || stored.FailureDirectory != "" || stored.FailurePath != "" || stored.FailureLabel != "" || len(stored.FailureTrail) != 0 {
		t.Fatal(stored)
	}
	if len(a.running) != 0 || len(a.store.snapshot().Tasks) != 0 {
		t.Fatal("definition started real work")
	}
	bad := in
	bad.FailureDirectory = "bad\npath"
	if validateOrganizeDefinition(&bad, a.store.snapshot(), true) == nil {
		t.Fatal("control character accepted")
	}
	bad = in
	bad.FailureTrail = []DirectoryCrumb{{ID: "x", Name: strings.Repeat("a", 8193)}}
	if validateOrganizeDefinition(&bad, a.store.snapshot(), true) == nil {
		t.Fatal("invalid failure trail accepted")
	}
}
