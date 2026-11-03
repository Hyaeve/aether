package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRulePresetCRUDPersistsAndSeparatesKinds(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	if w := request(t, h, "GET", "/api/rule-presets?kind=naming", nil, nil); w.Code != 401 {
		t.Fatal("presets not protected")
	}
	var first rulePreset
	for _, kind := range []string{"naming", "categories", "upgrade"} {
		w := request(t, h, "POST", "/api/rule-presets", map[string]string{"id": "client-id", "kind": kind, "name": " 我的预设 ", "description": "自定义说明"}, cookie)
		var saved rulePreset
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &saved) != nil || saved.ID == "client-id" || saved.Name != "我的预设" || saved.CreatedAt.IsZero() {
			t.Fatal(w.Code, w.Body.String())
		}
		if kind == "naming" {
			first = saved
		}
		w = request(t, h, "GET", "/api/rule-presets?kind="+kind, nil, cookie)
		var list []rulePreset
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list) != 1 || list[0].Kind != kind {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request(t, h, "POST", "/api/rule-presets", map[string]string{"kind": "naming", "name": "我的预设"}, cookie); w.Code != 409 {
		t.Fatal("same-kind duplicate accepted", w.Code)
	}
	if w := request(t, h, "PUT", "/api/rule-presets/"+first.ID, map[string]string{"kind": "upgrade", "name": "改类型"}, cookie); w.Code != 409 {
		t.Fatal("changed preset kind")
	}
	if w := request(t, h, "POST", "/api/rule-presets", map[string]string{"kind": "recognition", "name": "无效"}, cookie); w.Code != 400 {
		t.Fatal("unsupported rule fields accepted")
	}
	if w := request(t, h, "PUT", "/api/rule-presets/"+first.ID, map[string]string{"kind": "naming", "name": "新名称", "description": "多行\n说明"}, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.snapshot().RulePresets; len(got) != 3 || got[0].Name != "新名称" || got[0].Description != "多行\n说明" {
		t.Fatal("presets not durable", got)
	}
	if w := request(t, h, "DELETE", "/api/rule-presets/"+first.ID, nil, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(a.store.snapshot().RulePresets) != 2 {
		t.Fatal("delete affected another kind")
	}
}

func TestOrganizeTaskDefinitionStorageModesAndDurability(t *testing.T) {
	a := testApp(t)
	local := addLocal(t, a)
	input := filepath.Join(local.Config["root"], "untouched.txt")
	if err := os.WriteFile(input, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	cloud := Storage{ID: "quark-source", Name: "夸克源", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "private"}}
	cloud2 := cloud
	cloud2.ID = "quark-target"
	a.store.update(func(st *State) error { st.Storages = append(st.Storages, cloud, cloud2); return nil })
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	if w := request(t, h, "GET", "/api/organize-tasks", nil, nil); w.Code != 401 {
		t.Fatal("tasks not protected")
	}
	var first organizeTaskDefinition
	for _, method := range []string{"move", "copy", "hardlink", "symlink"} {
		body := organizeTaskDefinition{Name: "本地" + method, StorageID: local.ID, Source: "/in", SourcePath: "/in", TargetStorageID: local.ID, Target: "/out", TargetPath: "/out", Method: method}
		w := request(t, h, "POST", "/api/organize-tasks", body, cookie)
		var saved organizeTaskDefinition
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &saved) != nil || saved.Method != method {
			t.Fatal(w.Code, w.Body.String())
		}
		if first.ID == "" {
			first = saved
		}
	}
	for _, method := range []string{"move", "copy", "hardlink", "symlink"} {
		body := organizeTaskDefinition{Name: "云端" + method, StorageID: cloud.ID, Source: "source", TargetStorageID: cloud2.ID, Target: "archive", Method: method}
		w := request(t, h, "POST", "/api/organize-tasks", body, cookie)
		want := 201
		if method == "hardlink" || method == "symlink" {
			want = 400
		}
		if w.Code != want {
			t.Fatal(method, w.Code, w.Body.String())
		}
	}
	for _, scenario := range []string{"cross-kind", "same-directory", "missing", "invalid-method"} {
		body := first
		switch scenario {
		case "cross-kind":
			body.TargetStorageID = cloud.ID
		case "same-directory":
			body.Target = body.Source
		case "missing":
			body.StorageID = "gone"
		case "invalid-method":
			body.Method = "delete"
		}
		w := request(t, h, "PUT", "/api/organize-tasks/"+first.ID, body, cookie)
		if w.Code != 400 {
			t.Fatal(scenario, w.Code, w.Body.String())
		}
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.snapshot().OrganizeTasks) != 6 {
		t.Fatal("task definitions not durable")
	}
	if len(a.store.snapshot().Tasks) != 0 {
		t.Fatal("configuration accidentally registered executable tasks")
	}
	if w := request(t, h, "POST", "/api/organize-tasks/"+first.ID+"/run", nil, cookie); w.Code != 404 && w.Code != 405 {
		t.Fatal("unfinished rule workflow executed", w.Code)
	}
	if raw, err := os.ReadFile(input); err != nil || string(raw) != "preserve" {
		t.Fatal("saving a task changed source files")
	}
	w := request(t, h, "POST", "/api/config/backup", map[string]string{"password": "backup-test"}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	plain, err := openBackup(w.Body.Bytes(), "backup-test")
	if err != nil {
		t.Fatal(err)
	}
	var bundle configBundle
	if json.Unmarshal(plain, &bundle) != nil || len(bundle.State.OrganizeTasks) != 6 || validateBundle(bundle) != nil {
		t.Fatal("backup omitted definitions")
	}
	if w := request(t, h, "DELETE", "/api/organize-tasks/"+first.ID, nil, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(a.store.snapshot().OrganizeTasks) != 5 {
		t.Fatal("delete not saved")
	}
}
