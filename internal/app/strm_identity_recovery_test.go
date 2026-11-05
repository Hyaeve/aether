package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func Test115LifeResumesAfterFullSyncIdentityRecovery(t *testing.T) {
	a, s, file := strm115Fixture(t)
	task := lifeRevisionTask(t, a, s)
	shareMock(t, func(r *http.Request) string {
		if r.Method != "GET" {
			t.Fatal("unexpected cloud write", r.Method, r.URL.Path)
		}
		switch r.URL.Path {
		case "/files/get_info":
			return `{"state":true,"data":[{"file_id":"10","parent_id":"0","file_name":"Show","file_category":0}]}`
		case "/behavior/detail":
			return `{"state":true,"data":{"count":1,"list":[{"id":"3040163688862324736","type":2,"file_id":"file","parent_id":"10","file_name":"Episode01.mkv","file_category":1,"update_time":` + timeNowUnix() + `}]}}`
		case "/files":
			return `{"state":true,"cid":"10","count":1,"path":[{"cid":"0","name":"根目录"},{"cid":"10","name":"Show"}],"data":[{"fid":"file","cid":"10","n":"Episode01.mkv","s":42,"pc":"` + file.PickCode + `"}]}`
		}
		t.Errorf("unexpected endpoint %s", r.URL.Path)
		return `{"state":false}`
	})
	if _, err := a.executeSTRMLife(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	base, prefix, err := a.outputLocation(task.Target)
	if err != nil {
		t.Fatal(err)
	}
	h, err := a.beginSTRMSync(task, s, base, filepath.ToSlash(filepath.Join(prefix, "Show")))
	if err != nil {
		t.Fatal(err)
	}
	h.manifest.RootIdentity = "old-device:old-inode"
	if err := h.save(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	if err := a.startTaskWithTrigger(a.ctx, task.ID, done, "full-sync"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal("manual full sync could not recover life task", err)
	}
	if _, err := a.executeSTRMLife(a.ctx, task, s); err != nil {
		t.Fatal("life remained blocked", err)
	}
	body, err := os.ReadFile(filepath.Join(task.Target, "Show", "Episode01.strm"))
	if err != nil || !strings.Contains(string(body), file.PickCode) {
		t.Fatal("pickcode output missing", err)
	}
}

func TestSTRMDirectoryIdentityRecovery(t *testing.T) {
	for _, mode := range []string{"deleted-marker", "mount-volume", "full-sync-replaced", "full-sync-empty", "modified", "foreign-marker", "unregistered"} {
		t.Run(mode, func(t *testing.T) {
			a, s, task := strmModeFixture(t)
			writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "media")
			if _, err := a.executeTask(a.ctx, task, s); err != nil {
				t.Fatal(err)
			}
			h, err := a.beginSTRMSync(task, s, task.Target, "")
			if err != nil {
				t.Fatal(err)
			}
			original := h.manifest.RootIdentity
			if mode == "deleted-marker" {
				h.manifest.RootIdentity = ""
				writeTest(t, filepath.Join(task.Target, strmOwnerName(task.ID)), h.manifest.Token)
				if err := os.Remove(filepath.Join(task.Target, strmOwnerName(task.ID))); err != nil {
					t.Fatal(err)
				}
			} else {
				parts := strings.SplitN(original, ":", 2)
				h.manifest.RootIdentity = "changed-volume:" + parts[1]
			}
			if err := h.save(); err != nil {
				t.Fatal(err)
			}
			if mode == "full-sync-replaced" || mode == "full-sync-empty" {
				body, err := os.ReadFile(filepath.Join(task.Target, "film.strm"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(task.Target, task.Target+"-previous"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(task.Target, 0755); err != nil {
					t.Fatal(err)
				}
				if mode == "full-sync-replaced" {
					writeTest(t, filepath.Join(task.Target, "film.strm"), string(body))
					writeTest(t, filepath.Join(task.Target, "notes.txt"), "keep")
				}
			}
			if mode == "modified" {
				writeTest(t, filepath.Join(task.Target, "film.strm"), "user edit")
			}
			if mode == "foreign-marker" {
				writeTest(t, filepath.Join(task.Target, strmOwnerName(task.ID)), "foreign")
			}
			if mode == "unregistered" {
				if err := os.Remove(filepath.Join(task.Target, "film.strm")); err != nil {
					t.Fatal(err)
				}
				writeTest(t, filepath.Join(task.Target, "notes.txt"), "keep")
			}
			task.fullSyncGeneration = mode != "mount-volume"
			_, err = a.executeTask(a.ctx, task, s)
			blocked := mode == "modified" || mode == "foreign-marker" || mode == "unregistered"
			if blocked {
				if err == nil {
					t.Fatal("unverified library recovered")
				}
				return
			}
			if err != nil {
				t.Fatal("verified library did not recover", err)
			}
			h, err = a.beginSTRMSync(task, s, task.Target, "")
			if err != nil || h.manifest.RootIdentity == "" {
				t.Fatal("recovery not durable", err)
			}
			root, err := os.OpenRoot(task.Target)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			identity, err := strmRootIdentity(root)
			if err != nil || identity != h.manifest.RootIdentity {
				t.Fatal("incorrect recovered identity", err)
			}
			if _, err := os.Stat(filepath.Join(task.Target, strmOwnerName(task.ID))); !os.IsNotExist(err) {
				t.Fatal("owner recreated", err)
			}
			if mode == "full-sync-replaced" {
				body, err := os.ReadFile(filepath.Join(task.Target, "notes.txt"))
				if err != nil || string(body) != "keep" {
					t.Fatal("unknown file changed", err)
				}
			}
			if _, err := a.executeTask(a.ctx, task, s); err != nil {
				t.Fatal("subsequent run failed", err)
			}
		})
	}
}
