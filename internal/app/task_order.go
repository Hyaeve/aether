package app

import (
	"errors"
	"net/http"
)

func (a *App) reorderTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var move struct {
		ID     string `json:"id"`
		Target string `json:"target"`
	}
	if !decode(w, r, &move) {
		return
	}
	err := a.store.update(func(st *State) error {
		from, to := -1, -1
		for i, task := range st.Tasks {
			if task.ID == move.ID {
				from = i
			}
			if task.ID == move.Target {
				to = i
			}
		}
		if from < 0 || to < 0 {
			return errors.New("任务不存在，请刷新后重试")
		}
		if st.Tasks[from].Kind != st.Tasks[to].Kind {
			return errors.New("只能调整同类任务的顺序")
		}
		// Only rearrange slots of this kind; leave other task views untouched.
		slots, tasks := []int{}, []Task{}
		for i, task := range st.Tasks {
			if task.Kind == st.Tasks[from].Kind {
				slots = append(slots, i)
				tasks = append(tasks, task)
			}
		}
		for i, slot := range slots {
			if slot == from {
				from = i
				break
			}
		}
		for i, slot := range slots {
			if slot == to {
				to = i
				break
			}
		}
		item := tasks[from]
		if from < to {
			copy(tasks[from:to], tasks[from+1:to+1])
		}
		if from > to {
			copy(tasks[to+1:from+1], tasks[to:from])
		}
		tasks[to] = item
		for i, slot := range slots {
			st.Tasks[slot] = tasks[i]
		}
		return nil
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
