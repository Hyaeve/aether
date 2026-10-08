package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

func TestED2KNativeOfflineStatusProtocol(t *testing.T) {
	a, _, binding, _, claim := ed2kFixture(t)
	b, _ := json.Marshal([]any{claim.TaskID, *claim.ED2K})
	sum := sha256.Sum256(b)
	if err := a.writeED2KRecord(hex.EncodeToString(sum[:]), ed2kRecord{Attempted: true, Folder: "folder", Hash: "offline-hash"}); err != nil {
		t.Fatal(err)
	}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	pages := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.Host != "lixian.115.com" || r.URL.Path != "/lixian/" || r.URL.Query().Get("ct") != "lixian" || r.URL.Query().Get("ac") != "task_lists" {
			t.Fatalf("unexpected status request: %s %s", r.Method, r.URL)
		}
		pages++
		want := "1"
		if pages == 2 {
			want = "2"
		}
		if r.URL.Query().Get("page") != want {
			t.Fatal("wrong pagination", r.URL)
		}
		if !strings.Contains(r.Header.Get("Cookie"), "SEID=s") {
			t.Fatal("missing cookie")
		}
		result := map[string]any{"state": true, "page_count": 2, "tasks": []any{}}
		if pages == 2 {
			result["tasks"] = []any{map[string]any{"info_hash": "offline-hash", "url": claim.ED2K.URI(), "wp_path_id": "folder", "status": -1}}
		}
		raw, _ := json.Marshal(result)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})}
	if _, err := a.ed2kDownload(context.Background(), binding, claim); err == nil || !strings.Contains(err.Error(), "失败") {
		t.Fatal(err)
	}
	if pages != 2 {
		t.Fatal(pages)
	}
}

func TestED2KCorruptRecordFailsClosed(t *testing.T) {
	a, _, binding, _, claim := ed2kFixture(t)
	var submissions atomic.Int32
	ops := ed2kTestOps(claim, &submissions)
	if _, err := a.ed2kDownloadWith(context.Background(), binding, claim, ops); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(a.dataDir, "ed2k-playback", "*.enc"))
	if err := os.WriteFile(files[0], []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ed2kDownloadWith(context.Background(), binding, claim, ops); err == nil {
		t.Fatal("corruption ignored")
	}
	if submissions.Load() != 1 {
		t.Fatal("corrupt state resubmitted")
	}
}

func TestED2KPlaybackForwardsRequestUserAgent(t *testing.T) {
	a, _, binding, _, claim := ed2kFixture(t)
	b, _ := json.Marshal([]any{claim.TaskID, *claim.ED2K})
	sum := sha256.Sum256(b)
	if err := a.writeED2KRecord(hex.EncodeToString(sum[:]), ed2kRecord{Attempted: true, Folder: "12", FileID: "file"}); err != nil {
		t.Fatal(err)
	}
	var gotUA string
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		raw := `{"state":false,"errno":10008}`
		switch r.URL.Path {
		case "/files":
			if r.URL.Query().Get("cid") != "12" {
				t.Fatal("wrong folder", r.URL)
			}
			raw = `{"state":true,"cid":"12","count":1,"data":[{"fid":"file","cid":"12","n":"movie.mkv","s":"3","pc":"pick"}]}`
		case "/app/chrome/downurl":
			gotUA = r.Header.Get("User-Agent")
		default:
			t.Fatal("unexpected request", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	// A rejected API response avoids forging 115's encrypted success payload.
	// Exercise the production callback twice to catch UA reuse across clients.
	for _, ua := range []string{"Aether-Test-Client/1", "VLC/3.0.21"} {
		gotUA = ""
		if _, err := a.ed2kDownloadWithUA(context.Background(), binding, claim, ua); err == nil {
			t.Fatal("rejected API accepted")
		}
		if gotUA != ua {
			t.Fatalf("download UA = %q, want %q", gotUA, ua)
		}
	}
}

func ed2kFixture(t *testing.T) (*App, Storage, Storage, Task, streamClaim) {
	t.Helper()
	a := testApp(t)
	source := addLocal(t, a)
	binding := Storage{ID: "ed2k-115", Type: "115", Enabled: true, Config: map[string]string{"cookie": "UID=1_A1; CID=c; SEID=s"}}
	retained := "iso"
	task := Task{ID: "ed2k-task", Kind: "ed2k", Enabled: true, StorageID: source.ID, ED2KBindingID: binding.ID, Source: "/", Target: "ed2k", RetainedExtensions: &retained}
	if err := a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, binding)
		st.Tasks = append(st.Tasks, task)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	info := ED2KInfo{Name: "movie.mkv", Size: 3, Hash: "a448017aaf21d8525fc10ae87aa6729d", Binding: a.ed2kBindingStamp(task, source, binding)}
	return a, source, binding, task, streamClaim{Storage: binding.ID, TaskID: task.ID, File: "/movie.mkv", ED2K: &info}
}

func ed2kTestOps(claim streamClaim, submissions *atomic.Int32) ed2kPlaybackOps {
	i := *claim.ED2K
	return ed2kPlaybackOps{
		folder: func(context.Context) (string, error) { return "folder", nil },
		submit: func(context.Context, string, string) (string, error) { submissions.Add(1); return "offline-hash", nil },
		status: func(context.Context, ed2kRecord, ED2KInfo) (*driver.OfflineTask, error) {
			return &driver.OfflineTask{Status: 2, InfoHash: "offline-hash", FileId: "file", DirId: "folder", Url: i.URI(), Size: i.Size}, nil
		},
		files: func(context.Context, string) ([]File, error) {
			return []File{{ID: "file", Name: i.Name, Size: i.Size, PickCode: "pick"}}, nil
		},
		download: func(context.Context, File) (Download, error) {
			return Download{URL: "https://download.example/movie"}, nil
		},
		interval: time.Millisecond,
	}
}

func TestED2KPlaybackConcurrentAndPersistent(t *testing.T) {
	a, _, binding, _, claim := ed2kFixture(t)
	var submissions atomic.Int32
	ops := ed2kTestOps(claim, &submissions)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := a.ed2kDownloadWith(context.Background(), binding, claim, ops)
			if err != nil || d.URL == "" {
				t.Errorf("download: %+v %v", d, err)
			}
		}()
	}
	wg.Wait()
	if submissions.Load() != 1 {
		t.Fatalf("submitted %d", submissions.Load())
	}
	files, err := filepath.Glob(filepath.Join(a.dataDir, "ed2k-playback", "*.enc"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	b, err := os.ReadFile(files[0])
	if err != nil || strings.Contains(string(b), "offline-hash") {
		t.Fatal("not encrypted", err)
	}
	reopened, err := newWithDirectories(context.Background(), a.store.dir, a.dataDir, a.outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.ed2kDownloadWith(context.Background(), binding, claim, ops); err != nil {
		t.Fatal(err)
	}
	if submissions.Load() != 1 {
		t.Fatal("resubmitted after restart")
	}
}

func TestED2KPlaybackUnknownSubmissionNeverReplays(t *testing.T) {
	a, _, binding, _, claim := ed2kFixture(t)
	var submissions atomic.Int32
	ops := ed2kTestOps(claim, &submissions)
	ops.submit = func(context.Context, string, string) (string, error) {
		submissions.Add(1)
		return "", context.DeadlineExceeded
	}
	if _, err := a.ed2kDownloadWith(context.Background(), binding, claim, ops); err == nil {
		t.Fatal("ambiguous accepted")
	}
	ops.status = func(context.Context, ed2kRecord, ED2KInfo) (*driver.OfflineTask, error) { return nil, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if _, err := a.ed2kDownloadWith(ctx, binding, claim, ops); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if submissions.Load() != 1 {
		t.Fatal("ambiguous replayed")
	}
	ops = ed2kTestOps(claim, &submissions)
	if _, err := a.ed2kDownloadWith(context.Background(), binding, claim, ops); err != nil {
		t.Fatal(err)
	}
	if submissions.Load() != 1 {
		t.Fatal("recovery resubmitted")
	}
}

func TestED2KPlaybackRequiresCompletedExactFile(t *testing.T) {
	for _, mode := range []string{"running", "failed", "wrong-folder", "wrong-size", "wrong-id", "missing", "wrong-name"} {
		t.Run(mode, func(t *testing.T) {
			a, _, binding, _, claim := ed2kFixture(t)
			var submissions atomic.Int32
			ops := ed2kTestOps(claim, &submissions)
			status := ops.status
			ops.status = func(ctx context.Context, r ed2kRecord, i ED2KInfo) (*driver.OfflineTask, error) {
				v, e := status(ctx, r, i)
				switch mode {
				case "running":
					v.Status = 1
				case "failed":
					v.Status = -1
				case "wrong-folder":
					v.DirId = "other"
				case "wrong-size":
					v.Size++
				case "wrong-id":
					v.FileId = "other"
				}
				return v, e
			}
			if mode == "missing" {
				ops.files = func(context.Context, string) ([]File, error) { return nil, nil }
			}
			if mode == "wrong-name" {
				ops.files = func(context.Context, string) ([]File, error) {
					return []File{{ID: "file", Name: "other.mkv", Size: 3, PickCode: "pick"}}, nil
				}
			}
			ops.download = func(context.Context, File) (Download, error) { t.Error("unverified download"); return Download{}, nil }
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			if _, err := a.ed2kDownloadWith(ctx, binding, claim, ops); err == nil {
				t.Fatal("accepted incomplete file")
			}
		})
	}
}

func TestED2KPlaybackConfigurationRevocation(t *testing.T) {
	for _, mode := range []string{"disabled", "unbound", "source", "binding", "removed", "during-submit"} {
		t.Run(mode, func(t *testing.T) {
			a, _, binding, _, claim := ed2kFixture(t)
			change := func() {
				if err := a.store.update(func(st *State) error {
					switch mode {
					case "disabled", "during-submit":
						st.Tasks[0].Enabled = false
					case "unbound":
						st.Tasks[0].ED2KBindingID = ""
					case "source":
						st.Storages[0].Config["root"] = "changed"
					case "binding":
						st.Storages[1].Config["cookie"] = "changed"
					case "removed":
						st.Tasks = nil
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			var submissions atomic.Int32
			ops := ed2kTestOps(claim, &submissions)
			if mode == "during-submit" {
				submit := ops.submit
				ops.submit = func(ctx context.Context, f, u string) (string, error) {
					h, e := submit(ctx, f, u)
					change()
					return h, e
				}
			} else {
				change()
			}
			ops.download = func(context.Context, File) (Download, error) {
				t.Error("revoked claim downloaded")
				return Download{}, nil
			}
			if _, err := a.ed2kDownloadWith(context.Background(), binding, claim, ops); err == nil {
				t.Fatal("revoked accepted")
			}
		})
	}
}

func TestED2KBoundGenerationAndNaming(t *testing.T) {
	a, source, _, task, _ := ed2kFixture(t)
	writeTest(t, filepath.Join(source.Config["root"], "Series", "movie.mkv"), "abc")
	writeTest(t, filepath.Join(source.Config["root"], "Series", "disc.ISO"), "abc")
	n, err := a.executeTask(context.Background(), task, source)
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	for _, name := range []string{"movie.ed2k.strm", "disc.ISO.ed2k.strm"} {
		b, err := os.ReadFile(filepath.Join(a.outputDir, task.Target, "Series", name))
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(strings.TrimSpace(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(u.Path, "/stream/"))
		if err != nil {
			t.Fatal(err)
		}
		var claim streamClaim
		if err = json.Unmarshal(payload, &claim); err != nil || claim.ED2K == nil || claim.Storage != task.ED2KBindingID || claim.TaskID != task.ID || u.Query().Get("sign") == "" {
			t.Fatal(string(payload), err)
		}
	}
	writeTest(t, filepath.Join(source.Config["root"], "Series", "movie.mp4"), "abc")
	a.cache.clear()
	if _, err := a.executeTask(context.Background(), task, source); err == nil || !strings.Contains(err.Error(), "冲突") {
		t.Fatal("collision accepted", err)
	}
	for _, tc := range []struct {
		kind, provider, name, suffix, want string
		retained                           *string
	}{
		{"strm", "local", "a.mkv", ".strm", "a.mkv.strm", nil},
		{"strm", "openlist", "a.mkv", ".strm", "a.strm", nil},
		{"cas", "local", "a.mkv", ".cas", "a.mkv.cas", nil},
		{"cas", "local", "a.mkv", ".cas", "a.cas", task.RetainedExtensions},
		{"cas", "local", "a.iso", ".cas", "a.iso.cas", task.RetainedExtensions},
	} {
		if got := taskOutputName(Task{Kind: tc.kind, RetainedExtensions: tc.retained}, Storage{Type: tc.provider}, tc.name, tc.suffix); got != tc.want {
			t.Errorf("%s != %s", got, tc.want)
		}
	}
}
