package app

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEmptyTaskCronIsManual(t *testing.T) {
	for _, kind := range []string{"strm", "cas", "ed2k"} {
		if got := nextRun(Task{Kind: kind, Enabled: true}, time.Now()); !got.IsZero() {
			t.Fatal(kind, got)
		}
	}
	if nextRun(Task{Kind: "strm", Enabled: true, Cron: "0 14 * * *"}, time.Now()).IsZero() {
		t.Fatal("explicit cron lost")
	}
	if nextRun(Task{Kind: "cache", Enabled: true, Interval: 60}, time.Now()).IsZero() {
		t.Fatal("cache interval lost")
	}
}
func TestTaskCreatePersistsEmptyCron(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, Storage{ID: "cron-local", Name: "Local", Type: "local", Enabled: true, Config: map[string]string{"root": t.TempDir()}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/tasks", Task{Name: "manual cron", Kind: "strm", StorageID: "cron-local", Mode: "incremental", Enabled: true, Cron: " \t"}, cookie)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var task Task
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if task.Cron != "" || !task.NextRun.IsZero() {
		t.Fatal(task)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.snapshot().Tasks[0].Cron != "" || !reopened.snapshot().Tasks[0].NextRun.IsZero() {
		t.Fatal("empty cron changed after restart")
	}
	for _, cron := range []string{"0 14 * * *", ""} {
		task.Cron = cron
		w = request(t, h, "PUT", "/api/tasks/"+task.ID, task, cookie)
		if w.Code != 200 || a.store.snapshot().Tasks[0].Cron != cron {
			t.Fatal(w.Body.String())
		}
	}
}
