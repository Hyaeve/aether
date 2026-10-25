package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSTRMRetainedExtensionDefaultsAndOverrides(t *testing.T) {
	empty, iso, mp4, comic := "", "iso", "mp4", "cbz;cbr;zip;rar;epub;pdf"
	for _, provider := range []string{"local", "115", "quark", "mobile", "tianyi", "openlist", "webdav"} {
		for _, tc := range []struct {
			name, want string
			retained   *string
		}{
			{"film.mp4", "film.strm", nil},
			{"disc.ISO", "disc.ISO.strm", nil},
			{"film.mp4", "film.strm", &iso},
			{"disc.iso", "disc.strm", &empty},
			{"film.mp4", "film.mp4.strm", &mp4},
			{"film.mkv", "film.strm", &mp4},
			{"books/chapter.01.CBZ", "books/chapter.01.CBZ.strm", &comic},
			{"books/chapter.01.CBZ", "books/chapter.01.strm", &empty},
		} {
			if got := taskOutputName(Task{Kind: "strm", RetainedExtensions: tc.retained}, Storage{Type: provider}, tc.name, ".strm"); got != tc.want {
				t.Errorf("%s retained %v: %q, want %q", provider, tc.retained, got, tc.want)
			}
		}
	}
}

func TestSTRMRemovedExtensionsStillRejectNameCollision(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	for _, name := range []string{"film.mp4", "film.mkv"} {
		writeTest(t, filepath.Join(s.Config["root"], name), "media")
	}
	empty := ""
	task := Task{Kind: "strm", StorageID: s.ID, Source: "/", Target: "collision", Mode: "full", RetainedExtensions: &empty}
	count, err := a.executeTask(a.ctx, task, s)
	if err == nil || !strings.Contains(err.Error(), "输出文件名冲突") || count != 1 {
		t.Fatal("colliding extensions must not silently overwrite", count, err)
	}
	files, err := os.ReadDir(filepath.Join(a.outputDir, "collision"))
	if err != nil || len(files) != 1 || files[0].Name() != "film.strm" {
		t.Fatal(files, err)
	}
	for _, name := range []string{"film.mp4", "film.mkv"} {
		if _, err := os.Stat(filepath.Join(s.Config["root"], name)); err != nil {
			t.Fatal("collision removed source", err)
		}
	}
}

func TestSTRMRetainedSettingRoundTripAndGeneratedNames(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	for _, name := range []string{"film.mp4", "disc.iso", "book.cbz"} {
		writeTest(t, filepath.Join(s.Config["root"], name), "media")
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	task := Task{Name: "retained", Kind: "strm", StorageID: s.ID, Source: "/", Target: "generated", Mode: "full", MediaExtensions: "mp4;iso;cbz", APIInterval: 0}
	response := request(t, h, "POST", "/api/tasks", task, cookie)
	if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &task) != nil {
		t.Fatal(response.Code, response.Body.String())
	}
	if task.RetainedExtensions == nil || *task.RetainedExtensions != "iso" {
		t.Fatal("new STRM default must persist only iso")
	}
	for i, retained := range []string{"iso", "mp4", ""} {
		task.RetainedExtensions = &retained
		task.Target = filepath.Join(a.outputDir, "retained-"+string(rune('0'+i)))
		response = request(t, h, "PUT", "/api/tasks/"+task.ID, task, cookie)
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
		if err != nil {
			t.Fatal(err)
		}
		var saved Task
		for _, candidate := range reopened.store.snapshot().Tasks {
			if candidate.ID == task.ID {
				saved = candidate
			}
		}
		if saved.RetainedExtensions == nil || *saved.RetainedExtensions != retained {
			t.Fatal("explicit retained setting lost after restart", retained)
		}
		if n, err := reopened.executeTask(reopened.ctx, saved, s); err != nil || n != 3 {
			t.Fatal(n, err)
		}
		names := []string{"film.strm", "disc.strm", "book.strm"}
		if retained == "iso" {
			names[1] = "disc.iso.strm"
		} else if retained == "mp4" {
			names[0] = "film.mp4.strm"
		}
		for _, name := range names {
			if _, err := os.Stat(filepath.Join(task.Target, name)); err != nil {
				t.Fatal(name, err)
			}
		}
		files, err := os.ReadDir(task.Target)
		if err != nil || len(files) != 3 {
			t.Fatal("unexpected generated names", files, err)
		}
	}
}
