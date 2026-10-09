package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitAutomation(t *testing.T, a *App, id string) Automation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, r := range a.store.snapshot().Automations {
			if r.ID == id && r.Status != "running" {
				return r
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("automation did not finish")
	return Automation{}
}

func TestAutomationAPIAuthCronAndPersistence(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	if w := request(t, h, "GET", "/api/automations", nil, nil); w.Code != 401 {
		t.Fatal("unprotected")
	}
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	rule := Automation{Name: "Daily", Enabled: true, Trigger: "cron", Cron: "0 2 * * *", Steps: []AutomationStep{{Kind: "refresh", Condition: "always"}}}
	if w := request(t, h, "POST", "/api/automations", rule, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := request(t, h, "GET", "/api/automations", nil, cookie)
	var rules []Automation
	if json.Unmarshal(w.Body.Bytes(), &rules) != nil || len(rules) != 1 || rules[0].NextRun.IsZero() {
		t.Fatal(w.Body.String())
	}
	s, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.snapshot().Automations) != 1 {
		t.Fatal("rule not persisted")
	}
	rule = rules[0]
	rule.Enabled = false
	if w := request(t, h, "PUT", "/api/automations/"+rule.ID, rule, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if !a.store.snapshot().Automations[0].NextRun.IsZero() {
		t.Fatal("disabled rule has schedule")
	}
	if w := request(t, h, "DELETE", "/api/automations/"+rule.ID, nil, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if len(a.store.snapshot().Automations) != 0 {
		t.Fatal("rule not deleted")
	}
}
func TestAutomationSequentialConditionsAndTriggerIsolation(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	if err := os.WriteFile(filepath.Join(s.Config["root"], "film.mp4"), []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.store.update(func(st *State) error {
		st.Tasks = []Task{{ID: "generate", Name: "Generate", Kind: "strm", StorageID: s.ID, Source: "/", Mode: "full", Enabled: true, MediaExtensions: "mp4"}, {ID: "failure", Kind: "strm", StorageID: "missing", Enabled: true}}
		st.Automations = []Automation{{ID: "rule", Name: "Rule", Trigger: "manual", Steps: []AutomationStep{{Kind: "task", TaskID: "failure", Condition: "always"}, {Kind: "task", TaskID: "generate", Condition: "failure"}, {Kind: "refresh", Condition: "success"}}}, {ID: "event", Name: "Event", Enabled: true, Trigger: "task", SourceTask: "generate", Steps: []AutomationStep{{Kind: "refresh", Condition: "always"}}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.startAutomation("rule"); err != nil {
		t.Fatal(err)
	}
	if r := waitAutomation(t, a, "rule"); r.Status != "error" {
		t.Fatal(r)
	}
	if a.store.snapshot().Tasks[0].Status != "success" {
		t.Fatal("failure branch did not execute")
	}
	if a.store.snapshot().Automations[1].Status != "" {
		t.Fatal("linked task recursively triggered event")
	}
	a.triggerAutomations("generate")
	if r := waitAutomation(t, a, "event"); r.Status != "success" {
		t.Fatal(r)
	}
}
func TestAutomationCancellationAndValidation(t *testing.T) {
	a := testApp(t)
	rule := Automation{ID: "delay", Name: "Delay", Trigger: "manual", Steps: []AutomationStep{{Kind: "delay", Seconds: 60, Condition: "always"}}}
	if err := a.store.update(func(s *State) error { s.Automations = []Automation{rule}; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.startAutomation(rule.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.startAutomation(rule.ID); err == nil {
		t.Fatal("duplicate run accepted")
	}
	a.automationMu.Lock()
	a.automationRuns[rule.ID]()
	a.automationMu.Unlock()
	if r := waitAutomation(t, a, rule.ID); r.Status != "cancelled" {
		t.Fatal(r)
	}
	rule.Trigger = "cron"
	rule.Cron = "bad"
	if validateAutomation(&rule, State{}) == nil {
		t.Fatal("bad cron accepted")
	}
	rule.Trigger = "task"
	rule.SourceTask = "missing"
	if validateAutomation(&rule, State{}) == nil {
		t.Fatal("missing trigger accepted")
	}
}
