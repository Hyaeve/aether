package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOrganizeQuarkNativeMoveAndRenameReceipts(t *testing.T) {
	for _, scenario := range []string{"async-move", "rename-delay", "rename-read-error", "wrong-moved-id", "wrong-renamed-id", "failed-task"} {
		t.Run(scenario, func(t *testing.T) {
			a, s, task, _, _ := organizeFixture(t)
			s.Type, s.Config = "quark", map[string]string{"cookie": "private", "deleteMode": "trash"}
			task.Source, task.Target = "in", "out"
			if err := a.store.update(func(st *State) error { st.Storages[0] = s; st.OrganizeTasks[0] = task; return nil }); err != nil {
				t.Fatal(err)
			}
			h := &organizeRunner{a: a, t: task, source: s, target: s}
			entry := organizeEntry{file: File{ID: "source-file", Name: "Show.S01E01.mkv", Size: 1234, Modified: time.Unix(1700000000, 0).UTC()}, parent: "in"}
			moved, renamed, renameReads := false, false, 0
			moves, renames, queries := 0, 0, 0
			shareMock(t, func(r *http.Request) string {
				switch r.URL.Path {
				case "/1/clouddrive/file/sort":
					if renamed && scenario == "rename-read-error" && renameReads == 0 {
						renameReads++
						return `{"code":500}`
					}
					files := []map[string]any{}
					parent := r.URL.Query().Get("pdir_fid")
					if parent == "in" && !moved || parent == "out" && moved {
						name, fid := entry.file.Name, entry.file.ID
						if parent == "out" && scenario == "wrong-moved-id" {
							fid = "unrelated-file"
						}
						if renamed {
							name = "Show.E01.mkv"
							if scenario == "rename-delay" && renameReads < 2 {
								name = entry.file.Name
								renameReads++
							}
							if scenario == "wrong-renamed-id" {
								fid = "unrelated-file"
							}
						}
						files = append(files, map[string]any{"fid": fid, "file_name": name, "file_type": 1, "size": entry.file.Size, "updated_at": entry.file.Modified.Unix()})
					}
					if parent != "in" && parent != "out" {
						t.Error("organizer left its allowed directories", parent)
					}
					raw, err := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"list": files}})
					if err != nil {
						t.Fatal(err)
					}
					return string(raw)
				case "/1/clouddrive/file/move":
					moves++
					if moves != 1 {
						t.Error("native move was replayed")
					}
					if scenario == "async-move" || scenario == "failed-task" {
						return `{"code":0,"data":{"task_id":"move-task"}}`
					}
					moved = true
					return `{"code":0}`
				case "/1/clouddrive/task":
					queries++
					if r.URL.Query().Get("task_id") != "move-task" {
						t.Error("queried an unrelated task")
					}
					if scenario == "failed-task" {
						return `{"code":0,"data":{"status":3}}`
					}
					moved = true
					return `{"code":0,"data":{"status":2}}`
				case "/1/clouddrive/file/rename":
					renames++
					if scenario == "wrong-moved-id" {
						t.Error("organizer renamed a same-size file with an unrelated ID")
					}
					if renames != 1 {
						t.Error("native rename was replayed")
					}
					renamed = true
					return `{"code":0}`
				}
				t.Error("unexpected cloud request", r.Method, r.URL.Path)
				return `{"code":500}`
			})
			ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
			defer cancel()
			file, err := h.place(ctx, s, entry, s, "out", "Show.E01.mkv", "move")
			if strings.HasPrefix(scenario, "wrong-") || scenario == "failed-task" {
				if err == nil {
					t.Fatal("unconfirmed or unrelated destination was accepted", file)
				}
				if scenario == "failed-task" && (queries != 1 || renames != 0) {
					t.Fatal("failed asynchronous move was not reconciled correctly", queries, renames, err)
				}
			} else if err != nil || file.ID != entry.file.ID || file.Name != "Show.E01.mkv" || moves != 1 || renames != 1 || scenario == "async-move" && queries != 1 {
				t.Fatal("confirmed move or delayed rename was reported as a failure", file, err, fmt.Sprint(moves, renames, queries))
			}
		})
	}
}

func TestQuarkNativeCopyTaskOutcome(t *testing.T) {
	for _, outcome := range []int{2, 3} {
		t.Run(fmt.Sprint(outcome), func(t *testing.T) {
			a, s, _ := liveTestSetup(t)
			writes, queries := 0, 0
			shareMock(t, func(r *http.Request) string {
				switch r.URL.Path {
				case "/1/clouddrive/file/sort":
					if r.URL.Query().Get("pdir_fid") == "in" {
						return `{"code":0,"data":{"list":[{"fid":"file","file_name":"film.mkv","file_type":1,"size":42}]}}`
					}
					return `{"code":0,"data":{"list":[]}}`
				case "/1/clouddrive/file/copy":
					writes++
					return `{"code":0,"data":{"task_id":"copy-task"}}`
				case "/1/clouddrive/task":
					queries++
					return fmt.Sprintf(`{"code":0,"data":{"status":%d}}`, outcome)
				}
				t.Error("unexpected native copy request", r.URL.Path)
				return `{"code":500}`
			})
			ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
			defer cancel()
			err := a.cloudFileAction(ctx, s, s, fileActionRequest{Action: "copy", Source: "in", IDs: []string{"file"}, Target: "out"})
			if writes != 1 || queries != 1 || outcome == 2 && err != nil || outcome == 3 && (err == nil || !strings.Contains(err.Error(), "已确认失败")) {
				t.Fatal("native copy task completion was misclassified", writes, queries, err)
			}
		})
	}
}
