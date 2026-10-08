package app

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDefaultTaskCron(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	for _, kind := range []string{"strm", "cas", "ed2k"} {
		for _, minute := range []int{0, 5, 59} {
			now := time.Date(2026, 10, 8, 14, minute, 0, 0, zone)
			task := Task{Kind: kind, Cron: " \t", Enabled: true}
			defaultTaskCron(&task, now)
			if task.Cron != "0 14 * * *" {
				t.Fatal(task.Cron)
			}
			if got := nextRun(task, now); !got.Equal(time.Date(2026, 10, 9, 14, 0, 0, 0, zone)) {
				t.Fatal(got)
			}
		}
	}
	for _, task := range []Task{{Kind: "cache", Interval: 60}, {Kind: "strm", Cron: "15 3 * * *"}} {
		before := task.Cron
		defaultTaskCron(&task, time.Now())
		if task.Cron != before {
			t.Fatal(task)
		}
	}
}

func TestTaskCreatePersistsDefaultCron(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, Storage{ID: "cron-local", Name: "Local", Type: "local", Enabled: true, Config: map[string]string{"root": t.TempDir()}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	before := time.Now()
	w := request(t, h, "POST", "/api/tasks", Task{Name: "default cron", Kind: "strm", StorageID: "cron-local", Mode: "incremental", Enabled: true}, cookie)
	after := time.Now()
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var task Task
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	first, last := Task{Kind: "strm"}, Task{Kind: "strm"}
	defaultTaskCron(&first, before)
	defaultTaskCron(&last, after)
	if (task.Cron != first.Cron && task.Cron != last.Cron) || task.NextRun.IsZero() {
		t.Fatal(task)
	}
	store, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if store.snapshot().Tasks[0].Cron != task.Cron {
		t.Fatal("cron not persisted")
	}
	task.Cron = ""
	w = request(t, h, "PUT", "/api/tasks/"+task.ID, task, cookie)
	if w.Code != 200 || a.store.snapshot().Tasks[0].Cron != "" {
		t.Fatal("editing unexpectedly filled cron", w.Body.String())
	}
}
