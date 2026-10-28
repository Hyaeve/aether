package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

func TestShareSelectedTreeExcludesSubtrees(t *testing.T) {
	items := []shareEntry{{ID: "d", Name: "Show", IsDir: true}, {ID: "e", Name: "Empty", IsDir: true}}
	batches := []shareBatch{{Directory: "Show/Season 1", Items: []shareEntry{{ID: "1", Name: "one.mkv"}, {ID: "2", Name: "two.mkv"}}}, {Directory: "Show/Season 2", Items: []shareEntry{{ID: "3", Name: "three.mkv"}}}, {Directory: "Empty"}}
	tree := shareTree(items, batches)
	if len(tree) != 7 {
		t.Fatal(tree)
	}
	selected, err := selectedShareBatches(batches, tree, []string{"Show/Season 1/two.mkv", "Show/Season 2", "Empty"})
	if err != nil || len(selected) != 1 || len(selected[0].Items) != 1 || selected[0].Items[0].ID != "1" {
		t.Fatal(selected, err)
	}
	if _, err = selectedShareBatches(batches, tree, []string{"missing"}); err == nil {
		t.Fatal("unknown selection accepted")
	}
	if len(batches[0].Items) != 2 {
		t.Fatal("mutated original plan")
	}
}

func TestVirtual115TemporaryPlaybackSafety(t *testing.T) {
	for _, mode := range []string{"success", "uncertain", "wrong-hash", "wrong-parent", "changed-credentials", "different-file-id"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: "pan", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
			a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
			source := shareEntry{ID: "source", Name: "movie.mkv", Size: 42, SHA1: strings.Repeat("a", 40)}
			submits := 0
			downloads := 0
			file := &driver.File{FileID: "copy", ParentID: "folder", Name: source.Name, Size: source.Size, Sha1: source.SHA1, PickCode: "validpickcode"}
			if mode == "wrong-hash" {
				file.Sha1 = strings.Repeat("b", 40)
			}
			if mode == "wrong-parent" {
				file.ParentID = "other"
			}
			ops := virtualPlaybackOps{folder: func(context.Context) (string, error) { return "folder", nil }, submit: func(context.Context, string) error {
				submits++
				if mode == "uncertain" {
					return errors.New("lost response")
				}
				return nil
			}, files: func(context.Context, string) ([]*driver.File, error) { return []*driver.File{file}, nil }, download: func(context.Context, *driver.File) (Download, error) {
				downloads++
				if mode == "changed-credentials" {
					a.store.update(func(st *State) error { st.Storages[0].Config = map[string]string{"cookie": "changed"}; return nil })
				}
				return Download{URL: "https://cdn.example/file"}, nil
			}, interval: time.Millisecond}
			call := func() (Download, error) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				files := ops.files
				opsCopy := ops
				opsCopy.files = func(ctx context.Context, folder string) ([]*driver.File, error) {
					items, e := files(ctx, folder)
					if mode == "wrong-hash" || mode == "wrong-parent" || file.FileID == "externally-replaced" {
						cancel()
					}
					return items, e
				}
				return a.virtualShareDownload(ctx, s, normalizedED2KPool(ED2KPoolConfig{}), "test-key", source, opsCopy)
			}
			_, err := call()
			if mode == "success" || mode == "different-file-id" {
				if err != nil || downloads != 1 {
					t.Fatal(downloads, err)
				}
			} else if err == nil {
				t.Fatal("unsafe playback accepted")
			}
			if mode == "success" {
				_, err = call()
				if err != nil || downloads != 2 || submits != 1 {
					t.Fatal("temporary file not reused", submits, downloads, err)
				}
				st := a.store.snapshotWithLogLimit(0)
				if len(st.ED2KTemporary) != 1 || st.ED2KTemporary[0].SHA1 != source.SHA1 || st.ED2KTemporary[0].FileID != "copy" {
					t.Fatal(st.ED2KTemporary)
				}
			}
			if mode == "different-file-id" {
				file.FileID = "externally-replaced"
				_, err = call()
				if err == nil || downloads != 1 || submits != 1 {
					t.Fatal("changed file reused", downloads, submits, err)
				}
			}
			if mode == "uncertain" {
				_, err = call()
				if err != nil || submits != 1 {
					t.Fatal("uncertain POST replayed or not recovered", submits, err)
				}
			}
		})
	}
}

func TestStrmReplacementPreviewRegexAndUnicode(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	dir := t.TempDir()
	file := filepath.Join(dir, "one.strm")
	original := "中文https://old/42\n"
	os.WriteFile(file, []byte(original), 0600)
	input := map[string]any{"directory": dir, "find": `old/(\d+)`, "replace": "new/$1", "mode": "regex"}
	if w := request(t, h, "POST", "/api/strm-replace/preview", input, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := request(t, h, "POST", "/api/strm-replace/preview", input, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var preview strmReplacePreview
	json.Unmarshal(w.Body.Bytes(), &preview)
	if preview.Count != 1 || len(preview.Samples) != 1 || preview.Samples[0].After != "中文https://new/42\n" || preview.Samples[0].Matches[0] != [2]int{10, 16} {
		t.Fatal(preview)
	}
	data, _ := os.ReadFile(file)
	if string(data) != original {
		t.Fatal("preview wrote source")
	}
	input["find"] = "["
	if w = request(t, h, "POST", "/api/strm-replace/preview", input, cookie); w.Code != 400 {
		t.Fatal(w.Code)
	}
	input["find"] = `old/(\d+)`
	input["confirmed"] = true
	w = request(t, h, "POST", "/api/strm-replace", input, cookie)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	a.wg.Wait()
	data, _ = os.ReadFile(file)
	if string(data) != "中文https://new/42\n" {
		t.Fatal(string(data))
	}
	if _, _, _, err := transformStrm([]byte("old old"), "old", strings.Repeat("x", strmReplaceMaxBytes), "literal"); err == nil {
		t.Fatal("size overflow accepted")
	}
}

func TestFullAndOverCapacityUsage(t *testing.T) {
	for _, free := range []int64{0, -20, 100, 101, -1 << 63} {
		t.Run(fmt.Sprint(free), func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: "mobile", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:123:token")), "userDomainId": "domain"}}
			a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
			shareMock(t, func(r *http.Request) string {
				return fmt.Sprintf(`{"success":true,"data":{"diskSize":"100","freeDiskSize":"%d"}}`, free)
			})
			result, err := a.cachedStorageUsage(context.Background(), s)
			if err != nil {
				t.Fatal(err)
			}
			if free <= 100 && free >= -20 {
				if result.Total != 100<<20 || result.Used != (100-free)<<20 {
					t.Fatal(result)
				}
			} else if result.Total != 0 {
				t.Fatal("invalid quota accepted", result)
			}
		})
	}
	if !usableStorageQuota(storageUsage{Used: 120, Total: 100}) || usableStorageQuota(storageUsage{Used: -1, Total: 100}) {
		t.Fatal("quota validity")
	}
}

func TestVirtual115GenerateAndPortableKey(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "115", Type: "115", Enabled: true, Config: map[string]string{"cookie": "UID=123_A1;CID=test;SEID=secret"}}
	a.store.update(func(st *State) error {
		st.Storages = []Storage{s}
		st.Settings.STRMURL = "http://10.0.0.31:15151"
		return nil
	})
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	shareMock(t, func(r *http.Request) string {
		if r.URL.Query().Get("cid") == "99" {
			return `{"state":true,"data":{"count":1,"list":[{"fid":"3416909918244961440","n":"片名.mkv","s":"10","sha":"0123456789012345678901234567890123456789"}]}}`
		}
		return `{"state":true,"data":{"count":1,"list":[{"cid":"99","n":"Show"}]}}`
	})
	input := map[string]any{"storageId": "115", "url": "https://115cdn.com/s/swf1h7936et?password=YJFX#"}
	if w := request(t, h, "POST", "/api/virtual-library/115/preview", input, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := request(t, h, "POST", "/api/virtual-library/115/preview", input, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Preview string `json:"preview"`
		Files   []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"files"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	want := "http://10.0.0.31:15151/115-share/swf1h7936et-YJFX/片名.mkv"
	if len(result.Files) != 1 || result.Files[0].Content != want || result.Files[0].Path != "Show/片名.mkv.strm" {
		t.Fatal(result)
	}
	directory := t.TempDir()
	generate := map[string]any{"storageId": "115", "directory": directory, "preview": result.Preview}
	w = request(t, h, "POST", "/api/virtual-library/115/generate", generate, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(directory, "Show", "片名.mkv.strm"))
	if err != nil || strings.TrimSpace(string(data)) != want {
		t.Fatal(string(data), err)
	}
	if err = a.virtualGrant("swf1h7936et_YJFX_3416909918244961440.mkv", false); err != nil {
		t.Fatal(err)
	}
	if w = request(t, h, "POST", "/api/virtual-library/115/generate", generate, cookie); w.Code != 409 {
		t.Fatal("generation replay", w.Code)
	}
	for _, bad := range []string{"../../escape", "code_pass_1.mkv/../../", "code_pass_bad.mkv"} {
		if virtual115Key.MatchString(bad) {
			t.Fatal(bad)
		}
	}
	if _, err = virtualShareURL("http://x", "code", "abc_def", shareEntry{ID: "1", Name: "x.mkv"}); err == nil {
		t.Fatal("ambiguous password accepted")
	}
	// HEAD validates but must not create a temporary directory or receive request.
	w = request(t, h, "HEAD", "/115-share/swf1h7936et-YJFX/%E7%89%87%E5%90%8D.mkv", nil, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err = os.Stat(filepath.Join(a.dataDir, "ed2k-playback")); !os.IsNotExist(err) {
		t.Fatal("HEAD wrote temporary state", err)
	}
}
