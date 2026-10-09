package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type AutomationStep struct {
	Kind      string `json:"kind"`
	TaskID    string `json:"taskId,omitempty"`
	Seconds   int    `json:"seconds,omitempty"`
	Condition string `json:"condition"`
}
type Automation struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Enabled    bool             `json:"enabled"`
	Trigger    string           `json:"trigger"`
	Cron       string           `json:"cron"`
	SourceTask string           `json:"sourceTask"`
	Steps      []AutomationStep `json:"steps"`
	Status     string           `json:"status"`
	Message    string           `json:"message"`
	LastRun    time.Time        `json:"lastRun"`
	NextRun    time.Time        `json:"nextRun"`
}

func automationNext(r Automation, now time.Time) time.Time {
	if !r.Enabled || r.Trigger != "cron" || r.Cron == "" {
		return time.Time{}
	}
	s, err := cronParser.Parse(r.Cron)
	if err != nil {
		return time.Time{}
	}
	return s.Next(now)
}
func validateAutomation(r *Automation, state State) error {
	r.Name = strings.TrimSpace(r.Name)
	r.Cron = strings.TrimSpace(r.Cron)
	if r.Name == "" || len(r.Name) > 200 || len(r.Steps) < 1 || len(r.Steps) > 50 {
		return errors.New("填写联动名称，并添加1–50个动作")
	}
	known := map[string]bool{}
	for _, t := range state.Tasks {
		known[t.ID] = true
	}
	switch r.Trigger {
	case "manual":
		r.Cron = ""
		r.SourceTask = ""
	case "cron":
		if _, err := cronParser.Parse(r.Cron); err != nil {
			return errors.New("联动 Cron 表达式无效")
		}
		r.SourceTask = ""
	case "task":
		if !known[r.SourceTask] {
			return errors.New("触发任务不存在")
		}
		r.Cron = ""
	default:
		return errors.New("联动触发方式无效")
	}
	for _, s := range r.Steps {
		if s.Condition != "success" && s.Condition != "failure" && s.Condition != "always" {
			return errors.New("动作执行条件无效")
		}
		switch s.Kind {
		case "task":
			if !known[s.TaskID] {
				return errors.New("联动动作任务不存在")
			}
		case "delay":
			if s.Seconds < 1 || s.Seconds > 3600 {
				return errors.New("延时需为1–3600秒")
			}
		case "refresh":
		default:
			return errors.New("联动动作类型无效")
		}
	}
	return nil
}
func (a *App) automationUpdate(id string, fn func(*Automation)) error {
	return a.store.update(func(s *State) error {
		for i := range s.Automations {
			if s.Automations[i].ID == id {
				fn(&s.Automations[i])
				return nil
			}
		}
		return errors.New("联动不存在")
	})
}
func (a *App) automationAPI(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if action := r.PathValue("action"); action != "" {
		var err error
		if action == "run" {
			err = a.startAutomation(id)
		} else if action == "stop" {
			a.automationMu.Lock()
			if cancel := a.automationRuns[id]; cancel != nil {
				cancel()
			} else {
				err = errors.New("联动未运行")
			}
			a.automationMu.Unlock()
		} else {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			fail(w, 409, err)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method == "GET" && id == "" {
		rules := a.store.snapshot().Automations
		if rules == nil {
			rules = []Automation{}
		}
		jsonResponse(w, 200, rules)
		return
	}
	if r.Method != "POST" && r.Method != "PUT" && r.Method != "DELETE" {
		w.WriteHeader(405)
		return
	}
	if r.Method == "POST" && id != "" || r.Method != "POST" && id == "" {
		w.WriteHeader(405)
		return
	}
	var in Automation
	if r.Method != "DELETE" {
		if !decode(w, r, &in) {
			return
		}
		if err := validateAutomation(&in, a.store.snapshot()); err != nil {
			fail(w, 400, err)
			return
		}
	}
	a.automationMu.Lock()
	defer a.automationMu.Unlock()
	if a.automationRuns[id] != nil {
		fail(w, 409, errors.New("请先停止联动"))
		return
	}
	err := a.store.update(func(s *State) error {
		if r.Method == "POST" {
			if len(s.Automations) >= 100 {
				return errors.New("最多100个联动")
			}
			in.ID = newAutomationID()
			in.Status = "idle"
			in.Message = ""
			in.LastRun = time.Time{}
			in.NextRun = automationNext(in, time.Now())
			s.Automations = append(s.Automations, in)
			return nil
		}
		for i, old := range s.Automations {
			if old.ID != id {
				continue
			}
			if r.Method == "DELETE" {
				s.Automations = append(s.Automations[:i], s.Automations[i+1:]...)
			} else {
				in.ID = id
				in.Status = old.Status
				in.Message = old.Message
				in.LastRun = old.LastRun
				in.NextRun = automationNext(in, time.Now())
				s.Automations[i] = in
			}
			return nil
		}
		return errors.New("联动不存在")
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func newAutomationID() string { return id() }
func (a *App) triggerAutomations(taskID string) {
	for _, r := range a.store.snapshot().Automations {
		if r.Enabled && r.Trigger == "task" && r.SourceTask == taskID {
			if err := a.startAutomation(r.ID); err != nil {
				a.store.event("warn", "tasks", r.Name+"：联动触发失败："+err.Error())
			}
		}
	}
}
func (a *App) startAutomation(id string) error {
	a.automationMu.Lock()
	defer a.automationMu.Unlock()
	if a.ctx.Err() != nil {
		return a.ctx.Err()
	}
	if a.automationRuns[id] != nil {
		return errors.New("联动正在执行")
	}
	var rule Automation
	st := a.store.snapshot()
	for _, r := range st.Automations {
		if r.ID == id {
			rule = r
			break
		}
	}
	if rule.ID == "" {
		return errors.New("联动不存在")
	}
	if err := validateAutomation(&rule, st); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 6*time.Hour)
	if err := a.automationUpdate(id, func(r *Automation) {
		r.Status = "running"
		r.Message = "准备执行"
		r.LastRun = time.Now()
		r.NextRun = automationNext(*r, time.Now())
	}); err != nil {
		cancel()
		return err
	}
	if a.automationRuns == nil {
		a.automationRuns = map[string]context.CancelFunc{}
	}
	a.automationRuns[id] = cancel
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer cancel()
		success, failed := true, false
		message := "执行完成"
		for i, step := range rule.Steps {
			if ctx.Err() != nil {
				failed = true
				message = "联动已停止"
				break
			}
			if i > 0 && (step.Condition == "success" && !success || step.Condition == "failure" && success) {
				continue
			}
			_ = a.automationUpdate(id, func(r *Automation) { r.Message = fmt.Sprintf("执行动作 %d/%d", i+1, len(rule.Steps)) })
			var err error
			switch step.Kind {
			case "refresh":
				a.cache.clear()
			case "delay":
				timer := time.NewTimer(time.Duration(step.Seconds) * time.Second)
				select {
				case <-timer.C:
				case <-ctx.Done():
					err = ctx.Err()
				}
				timer.Stop()
			case "task":
				done := make(chan error, 1)
				err = a.startTaskContext(ctx, step.TaskID, done)
				if err == nil {
					err = <-done
				}
			}
			success = err == nil
			if err != nil {
				failed = true
				message = fmt.Sprintf("动作 %d 失败：%s", i+1, err)
				a.store.event("warn", "tasks", rule.Name+"："+message)
			}
		}
		status := "success"
		if failed {
			status = "error"
		}
		if ctx.Err() != nil {
			status = "cancelled"
			message = "联动已停止"
		}
		if err := a.automationUpdate(id, func(r *Automation) { r.Status = status; r.Message = message }); err != nil {
			a.logger.Printf("persist automation: %v", err)
		}
		a.store.event(status, "tasks", rule.Name+"："+message)
		a.automationMu.Lock()
		delete(a.automationRuns, id)
		a.automationMu.Unlock()
	}()
	return nil
}
