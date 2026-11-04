package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMobileLibraryModesHLSAndLateSettingsChange(t *testing.T) {
	a := testApp(t)
	m, card := mockMobileLibrary(t, a)
	m.list["root"][0]["presentUURL"] = "https://mobile-hls.test/high.m3u8"
	m.list["root"][0]["presentHURL"] = "https://mobile-hls.test/low.m3u8"
	if _, err := a.runMobileLibrary(a.ctx, card); err != nil {
		t.Fatal(err)
	}
	h, _ := mobileLibraryHandler(t, a)
	target := "/139-share/CodeAb-Ab12/" + url.PathEscape("剧集 [A] #1.mkv")
	w := request(t, h, "GET", target, nil, nil)
	if w.Code != 302 || w.Header().Get("Location") != "https://mobile-hls.test/high.m3u8" || m.downloads != 0 {
		t.Fatal(w.Code, w.Header(), m.downloads)
	}
	m.list["root"][0]["presentUURL"] = "javascript:alert(1)"
	w = request(t, h, "GET", target, nil, nil)
	if w.Code != 302 || w.Header().Get("Location") != "https://mobile-hls.test/low.m3u8" {
		t.Fatal(w.Code, w.Header())
	}
	m.onRead = func() {
		_ = a.store.update(func(st *State) error {
			cfg := effectiveMobileLibrary(*st)
			cfg.PlaybackMode = "relay"
			st.MobileLibrary = &cfg
			return nil
		})
	}
	w = request(t, h, "GET", target, nil, nil)
	if w.Code != 409 || w.Header().Get("Location") != "" {
		t.Fatal("late settings returned a stale stream", w.Code, w.Header())
	}
}

type mobilePlaybackPersonal struct {
	files                              map[string][]File
	creates, transfers, deletes, links int
	unknown                            bool
	onDelete                           func()
}

func mockMobilePlaybackPersonal(t *testing.T, a *App, m *mobileLibraryMock, resources ...string) *mobilePlaybackPersonal {
	t.Helper()
	p := &mobilePlaybackPersonal{files: map[string][]File{"/": {{ID: "aether", Name: "Aether", IsDir: true}}, "aether": {{ID: "share-parent", Name: "Share", IsDir: true}}}}
	wire := apiClient.Transport
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/user/route/qryRoutePolicy" && r.URL.Host != "personal-njs.yun.139.com" {
			return wire.RoundTrip(r)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("native request missing bound account")
		}
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		str := func(key string) string { var v string; _ = json.Unmarshal(body[key], &v); return v }
		var data any
		switch r.URL.Path {
		case "/user/route/qryRoutePolicy":
			data = map[string]any{"routePolicyList": []any{map[string]string{"modName": "personal", "httpsUrl": "https://personal-njs.yun.139.com"}}}
		case "/file/list":
			items := []map[string]any{}
			for _, f := range p.files[str("parentFileId")] {
				kind := "file"
				if f.IsDir {
					kind = "folder"
				}
				items = append(items, map[string]any{"fileId": f.ID, "name": f.Name, "size": f.Size, "type": kind, "contentHash": f.SHA256, "contentHashAlgorithm": "SHA256"})
			}
			data = map[string]any{"items": items}
		case "/file/create":
			p.creates++
			fid := fmt.Sprintf("folder-%d", p.creates)
			p.files[str("parentFileId")] = append(p.files[str("parentFileId")], File{ID: fid, Name: str("name"), IsDir: true})
			p.files[fid] = []File{}
			data = map[string]string{"fileId": fid}
		case "/file/getDownloadUrl":
			p.links++
			data = map[string]string{"url": "https://mobile-original.test/original.mkv"}
		case "/recyclebin/batchTrash", "/file/batchDelete":
			p.deletes++
			var ids []string
			_ = json.Unmarshal(body["fileIds"], &ids)
			for folder, files := range p.files {
				next := []File{}
				for _, f := range files {
					keep := true
					for _, fid := range ids {
						if fid == f.ID {
							keep = false
						}
					}
					if keep {
						next = append(next, f)
					}
				}
				p.files[folder] = next
			}
			if p.onDelete != nil {
				p.onDelete()
			}
			data = map[string]any{}
		default:
			t.Error("unexpected native call", r.URL.Path)
			data = map[string]any{}
		}
		raw, _ := json.Marshal(map[string]any{"success": true, "data": data})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})
	m.extra = func(r *http.Request) (string, bool) {
		method := filepath.Base(r.URL.Path)
		if method != "createOuterLinkBatchOprTask" && method != "queryBatchOprTaskDetail" {
			return "", false
		}
		var data any
		if method == "createOuterLinkBatchOprTask" {
			p.transfers++
			var in struct {
				Request struct {
					Task struct {
						Files  []string `json:"contentInfoList"`
						Folder string   `json:"newCatalogID"`
					} `json:"taskInfo"`
				} `json:"createOuterLinkBatchOprTaskReq"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			expectedResource := "video"
			if len(resources) > 0 {
				expectedResource = resources[0]
			}
			if len(in.Request.Task.Files) != 1 || in.Request.Task.Files[0] != expectedResource {
				t.Error("transfer expanded beyond requested source", in)
			}
			p.files[in.Request.Task.Folder] = []File{{ID: "temporary-video", Name: "剧集 [A] #1.mkv", Size: 10, SHA256: strings.Repeat("a", 64)}}
			if p.unknown {
				return `{"resultCode":"500","data":{}}`, true
			}
			data = map[string]any{"taskID": "transfer-job"}
		} else {
			data = map[string]any{"queryBatchOprTaskDetailRes": map[string]any{"batchOprTask": map[string]any{"taskID": "transfer-job", "taskStatus": "2", "taskResultCode": "0", "resultCode": "0"}}}
		}
		raw, _ := json.Marshal(map[string]any{"resultCode": "0", "data": data})
		return string(raw), true
	}
	return p
}

func TestMobileLibraryModesTransferReuseCleanupAndRuntime(t *testing.T) {
	a := testApp(t)
	m, card := mockMobileLibrary(t, a)
	p := mockMobilePlaybackPersonal(t, a, m)
	if _, err := a.runMobileLibrary(a.ctx, card); err != nil {
		t.Fatal(err)
	}
	_ = a.store.update(func(st *State) error {
		cfg := effectiveMobileLibrary(*st)
		cfg.PlaybackMode = "transfer"
		st.MobileLibrary = &cfg
		return nil
	})
	h, _ := mobileLibraryHandler(t, a)
	target := "/139-share/CodeAb-Ab12/" + url.PathEscape("剧集 [A] #1.mkv")
	before := len(m.calls)
	if w := request(t, h, "HEAD", target, nil, nil); w.Code != 200 || p.creates != 0 || p.transfers != 0 || len(m.calls) != before {
		t.Fatal("HEAD performed cloud writes", w.Code)
	}
	for i := 0; i < 2; i++ {
		w := request(t, h, "GET", target, nil, nil)
		if w.Code != 302 || w.Header().Get("Location") != "https://mobile-original.test/original.mkv" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if p.transfers != 1 || p.creates != 1 || p.links != 2 || m.downloads != 0 {
		t.Fatal("repeat playback did not reuse the verified copy", p)
	}
	reopened, err := NewStore(a.store.dir)
	if err == nil {
		err = reopened.initRuntime(a.store.runtimeDir)
	}
	if err != nil || len(reopened.snapshot().MobileLibraryRuntime.Temporary) != 1 {
		t.Fatal("runtime receipt not persistent", err)
	}
	a.cleanupMobileLibraryTemporary(time.Now().Add(11 * time.Hour))
	if p.deletes != 0 {
		t.Fatal("copy expired early")
	}
	var folder string
	for _, r := range a.store.snapshot().MobileLibraryRuntime.Temporary {
		folder = r.Folder
	}
	p.files[folder][0].SHA256 = strings.Repeat("b", 64)
	a.cleanupMobileLibraryTemporary(time.Now().Add(13 * time.Hour))
	if p.deletes != 0 {
		t.Fatal("changed copy was deleted")
	}
	p.files[folder][0].SHA256 = strings.Repeat("a", 64)
	a.cleanupMobileLibraryTemporary(time.Now().Add(13 * time.Hour))
	if p.deletes != 1 || len(a.store.snapshot().MobileLibraryRuntime.Temporary) != 0 {
		t.Fatal("verified copy was not cleaned", p.deletes)
	}
	if len(p.files["/"]) != 1 || p.files["/"][0].Name != "Aether" {
		t.Fatal("shared parent was removed")
	}
	if _, ok := a.store.snapshot().MobileLibraryRuntime.Files[nextMobileGrant(t, a).Key]; !ok {
		t.Fatal("cleanup revoked generated STRM")
	}
}

func nextMobileGrant(t *testing.T, a *App) quarkLibraryFile {
	t.Helper()
	for _, f := range a.store.snapshot().MobileLibraryRuntime.Files {
		return f
	}
	t.Fatal("missing grant")
	return quarkLibraryFile{}
}

func TestMobileLibraryModesUnknownTransferNeverReplays(t *testing.T) {
	a := testApp(t)
	m, card := mockMobileLibrary(t, a)
	p := mockMobilePlaybackPersonal(t, a, m)
	p.unknown = true
	if _, err := a.runMobileLibrary(a.ctx, card); err != nil {
		t.Fatal(err)
	}
	_ = a.store.update(func(st *State) error {
		cfg := effectiveMobileLibrary(*st)
		cfg.PlaybackMode = "transfer"
		st.MobileLibrary = &cfg
		return nil
	})
	h, _ := mobileLibraryHandler(t, a)
	target := "/139-share/CodeAb-Ab12/" + url.PathEscape("剧集 [A] #1.mkv")
	for i := 0; i < 2; i++ {
		w := request(t, h, "GET", target, nil, nil)
		if w.Code != 502 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if p.transfers != 1 || p.links != 0 {
		t.Fatal("unknown transfer repeated or exposed", p)
	}
	reopened, err := NewStore(a.store.dir)
	if err == nil {
		err = reopened.initRuntime(a.store.runtimeDir)
	}
	if err != nil || len(reopened.snapshot().MobileLibraryRuntime.Temporary) != 1 {
		t.Fatal("pending intent did not survive restart", err)
	}
	for _, r := range reopened.snapshot().MobileLibraryRuntime.Temporary {
		if !r.Attempted || r.FileID != "" || r.Task != "" {
			t.Fatal("unknown write was marked confirmed", r)
		}
	}
	a.cleanupMobileLibraryTemporary(time.Now().Add(24 * time.Hour))
	if p.deletes != 0 {
		t.Fatal("unknown copy automatically deleted")
	}
}

func TestLibraryRotationAndMobileSettingsDefaults(t *testing.T) {
	s := []Storage{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	for i := 0; i < 6; i++ {
		got := rotateLibraryStorages(s, 24, time.Unix(int64(i*24*3600), 0))
		if got[0].ID != s[i%3].ID || len(got) != 3 {
			t.Fatal(i, got)
		}
	}
	if s[0].ID != "a" {
		t.Fatal("rotation mutated configured order")
	}
	cfg := effectiveMobileLibrary(State{})
	if cfg.InvalidDays != 15 || cfg.RetentionHours != 12 || cfg.RotationHours != 24 || cfg.PlaybackMode != "share" {
		t.Fatal(cfg)
	}
	a := testApp(t)
	_, _ = mockMobileLibrary(t, a)
	st := a.store.snapshot()
	cfg = effectiveMobileLibrary(st)
	cfg.PlaybackMode = "transfer"
	cfg.RotationHours = 48
	cfg.RetentionHours = 8
	st.MobileLibrary = &cfg
	if err := validateMobileLibrary(st); err != nil {
		t.Fatal(err)
	}
	cfg.RotationHours = -1
	if validateMobileLibrary(st) == nil {
		t.Fatal("negative rotation accepted")
	}
	q := effectiveQuarkLibrary(State{})
	if q.RotationHours != 24 {
		t.Fatal(q)
	}
}

func TestMobileLibraryModesInvalidExpiryKeepsUnownedFiles(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(fmt.Sprint(external), func(t *testing.T) {
			a := testApp(t)
			m, card := mockMobileLibrary(t, a)
			if _, err := a.runMobileLibrary(a.ctx, card); err != nil {
				t.Fatal(err)
			}
			if external {
				writeTest(t, filepath.Join(quarkLibraryRoot(card), "personal.txt"), "keep")
			}
			old := time.Now().Add(-16 * 24 * time.Hour)
			_ = a.store.update(func(st *State) error {
				r := st.MobileLibraryRuntime.Results[card.ID]
				r.InvalidSince, r.Status = old, "invalid"
				st.MobileLibraryRuntime.Results[card.ID] = r
				return nil
			})
			m.extra = func(r *http.Request) (string, bool) {
				if strings.HasSuffix(r.URL.Path, "getOutLinkGeneral") {
					return `{"resultCode":"0","data":{"getOutLinkGeneralResp":{"outLinkGeneral":[]}}}`, true
				}
				return "", false
			}
			a.mobileLibraryExpiryTick(context.Background(), a.store.snapshot(), time.Now())
			_, exists := mobileLibraryCardAt(a.store.snapshot(), card.ID)
			if exists != external {
				t.Fatal("expiry record protection", exists, external)
			}
			if !external {
				if _, err := os.Stat(quarkLibraryRoot(card)); !os.IsNotExist(err) {
					t.Fatal("owned expired output directory remained", err)
				}
			}
			if external {
				if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), "剧集 [A] #1.mkv.share.strm")); err != nil {
					t.Fatal("partial cleanup before full inventory", err)
				}
			}
		})
	}
	var unavailable *mobileLibraryUnavailable
	if !errors.As(fmt.Errorf("stage: %w", &mobileLibraryUnavailable{}), &unavailable) {
		t.Fatal("lost typed invalid classification")
	}
}
