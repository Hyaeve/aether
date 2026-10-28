package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mobilePCFixture(client string) Storage {
	return Storage{ID: "mobile-pc", Name: "移动大文件", Type: "mobile", Enabled: true, Config: map[string]string{
		"mode": "native", "root": "/", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:test-token")), "clientType": client,
	}}
}

func TestMobilePCClientHeaders(t *testing.T) {
	a := testApp(t)
	original := apiClient
	defer func() { apiClient = original }()
	for _, tc := range []struct {
		name, client, want string
		cas                bool
	}{
		{"legacy", "", "13", false},
		{"missing", "", "13", false},
		{"web", "web", "9", false},
		{"pc", "pc", "13", false},
		{"cas-default", "", "11", true},
		{"cas-web", "web", "11", true},
		{"cas-pc", "pc", "11", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				if !strings.HasPrefix(r.Header.Get("X-Yun-Client-Info"), "||"+tc.want+"|") {
					t.Errorf("client info = %q, want type %s", r.Header.Get("X-Yun-Client-Info"), tc.want)
				}
				if !tc.cas && !strings.HasPrefix(r.Header.Get("X-DeviceInfo"), "||9|") {
					t.Errorf("ordinary device info changed: %q", r.Header.Get("X-DeviceInfo"))
				}
				body, _ := io.ReadAll(r.Body)
				sign := strings.Split(r.Header.Get("Mcloud-Sign"), ",")
				if len(sign) != 3 || sign[2] != mobileSign(string(body), sign[0], sign[1]) {
					t.Error("request signature changed")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"success":true,"data":{}}`)), Header: http.Header{}}, nil
			})}
			s := mobilePCFixture(tc.client)
			if tc.name == "missing" {
				delete(s.Config, "clientType")
			}
			if err := a.mobilePost(context.Background(), s, "https://personal.yun.139.com/file/create", map[string]string{"name": "movie.mkv"}, tc.cas, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMobilePCClientValidation(t *testing.T) {
	for _, client := range []string{"", "web", "pc"} {
		s := mobilePCFixture(client)
		if err := validateStorage(&s); err != nil {
			t.Fatalf("client %q: %v", client, err)
		}
	}
	for _, client := range []string{"PC", "13", "android", "pc\r\nInjected: true"} {
		s := mobilePCFixture(client)
		if err := validateStorage(&s); err == nil {
			t.Errorf("accepted invalid client %q", client)
		}
	}
}

type mobilePCProbeReader struct{ reads int }

func (r *mobilePCProbeReader) Read([]byte) (int, error) {
	r.reads++
	return 0, io.EOF
}

func TestMobilePCUploadAndBackupSizeGates(t *testing.T) {
	const large = int64(100<<30) + 1
	t.Run("browser-pc", func(t *testing.T) {
		a := testApp(t)
		input := &mobilePCProbeReader{}
		err := a.receiveFile(context.Background(), mobilePCFixture("pc"), "/", "large.mkv", input, large)
		if input.reads == 0 || err == nil || !strings.Contains(err.Error(), "不完整") {
			t.Fatalf("PC upload stopped at old size gate: reads=%d error=%v", input.reads, err)
		}
	})
	t.Run("browser-web", func(t *testing.T) {
		a := testApp(t)
		input := &mobilePCProbeReader{}
		err := a.receiveFile(context.Background(), mobilePCFixture("web"), "/", "large.mkv", input, large)
		if input.reads != 0 || err == nil || !strings.Contains(err.Error(), "100 GiB") {
			t.Fatalf("legacy limit changed: reads=%d error=%v", input.reads, err)
		}
	})
	t.Run("backup-pc", func(t *testing.T) {
		a := testApp(t)
		source := Storage{ID: "source", Type: "local", Config: map[string]string{"root": t.TempDir()}}
		err := a.copyBackupFile(context.Background(), source, mobilePCFixture("pc"), File{ID: "/missing.mkv", Name: "missing.mkv", Size: large}, "/", "large.mkv", "skip")
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("PC backup must reach source staging, got %v", err)
		}
	})
	t.Run("browser-over-500GB", func(t *testing.T) {
		a := testApp(t)
		input := &mobilePCProbeReader{}
		err := a.receiveFile(context.Background(), mobilePCFixture("pc"), "/", "large.mkv", input, 500_000_000_001)
		if input.reads != 0 || err == nil || !strings.Contains(err.Error(), "500 GB") {
			t.Fatalf("missing exact PC upper bound: reads=%d error=%v", input.reads, err)
		}
		if files, _ := filepath.Glob(filepath.Join(a.transferTempDirectory(), "file-upload", "*")); len(files) != 0 {
			t.Fatal("oversized upload created staged content")
		}
	})
}

func TestMobilePCConfigPersistence(t *testing.T) {
	a := testApp(t)
	s := mobilePCFixture("pc")
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(a.store.dir, "storage", "storage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) || !strings.Contains(string(raw), `"clientType": "pc"`) || strings.Contains(string(raw), s.Config["authorization"]) {
		t.Fatal("client type or credential encryption not preserved")
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.snapshot().Storages[0]
	if got.Config["clientType"] != "pc" || got.Config["authorization"] != s.Config["authorization"] {
		t.Fatal("restart lost client selection or credentials")
	}
	st := a.store.snapshot()
	st.Storages[0].Config["clientType"] = "android"
	if err := validateResourceState(&st); err == nil {
		t.Fatal("configuration import accepted unknown mobile client")
	}
}

func TestMobilePCSizePolicyAndParts(t *testing.T) {
	for _, s := range []Storage{
		mobilePCFixture("web"),
		{Type: "quark", Config: map[string]string{"clientType": "pc"}},
		{Type: "mobile", Config: map[string]string{"mode": "gateway", "clientType": "pc"}},
	} {
		if got := uploadFileLimit(s); got != 100<<30 {
			t.Fatalf("unrelated storage limit changed: %d", got)
		}
	}
	for _, client := range []string{"", "pc"} {
		if got := uploadFileLimit(mobilePCFixture(client)); got != 500_000_000_000 {
			t.Fatal("default/explicit PC limit must use decimal GB", client, got)
		}
	}
	for _, size := range []int64{0, 1, 100 << 20, 100<<20 + 1, 30 << 30, 30<<30 + 1, 100<<30 + 1, 500_000_000_000} {
		parts, err := mobileUploadParts(size)
		if err != nil || len(parts) == 0 || len(parts) > 1000 {
			t.Fatalf("parts for %d: %d %v", size, len(parts), err)
		}
		offset := int64(0)
		for i, part := range parts {
			if part.Number != i+1 || part.HashContext.Offset != offset || part.Size < 0 || part.Size > 512<<20 {
				t.Fatalf("invalid part for %d: %+v", size, part)
			}
			offset += part.Size
		}
		if offset != size {
			t.Fatalf("parts cover %d, want %d", offset, size)
		}
	}
	for _, size := range []int64{-1, 500_000_000_001, 1<<63 - 1} {
		if _, err := mobileUploadParts(size); err == nil {
			t.Fatal("oversized plan accepted", size)
		}
	}
}

func TestMobilePCDefaultPersistence(t *testing.T) {
	for _, value := range []string{"missing", "", "web", "pc"} {
		t.Run(value, func(t *testing.T) {
			a := testApp(t)
			s := mobilePCFixture(value)
			if value == "missing" {
				delete(s.Config, "clientType")
			}
			if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
				t.Fatal(err)
			}
			reopened, err := NewStore(a.store.dir)
			if err != nil {
				t.Fatal(err)
			}
			got := reopened.snapshot().Storages[0]
			if mobilePCClient(got) != (value != "web") {
				t.Fatal("missing client must default to PC without overriding explicit Web", value)
			}
			if got.Config["clientType"] != s.Config["clientType"] || got.Config["authorization"] != s.Config["authorization"] {
				t.Fatal("default selection rewrote saved configuration or authorization")
			}
		})
	}
}

type mobilePCVirtualSource struct {
	offsets []int64
}

func (s *mobilePCVirtualSource) ReadAt(p []byte, offset int64) (int, error) {
	s.offsets = append(s.offsets, offset)
	for i := range p {
		p[i] = byte((offset + int64(i)) % 251)
	}
	return len(p), nil
}

func TestMobilePC500GBMultipartProtocol(t *testing.T) {
	a := testApp(t)
	original := apiClient
	defer func() { apiClient = original }()
	const size = int64(500_000_000_000)
	parts, _ := mobileUploadParts(size)
	source := &mobilePCVirtualSource{}
	uploaded, batches, creates, completed := 0, 0, 0, 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		raw := `{"success":true,"data":{}}`
		if r.URL.Host == "upload.test" {
			if r.Method != "PUT" || uploaded >= len(parts) || r.ContentLength != parts[uploaded].Size {
				t.Fatalf("invalid upload %d, size %d", uploaded, r.ContentLength)
			}
			var first [1]byte
			if _, err := io.ReadFull(r.Body, first[:]); err != nil || first[0] != byte(parts[uploaded].HashContext.Offset%251) {
				t.Fatalf("part %d did not use its exact source offset: %v", uploaded, err)
			}
			uploaded++
		} else {
			if !strings.HasPrefix(r.Header.Get("X-Yun-Client-Info"), "||13|") {
				t.Error("upload metadata request did not use PC type 13", r.URL.Path)
			}
			switch r.URL.Path {
			case "/user/route/qryRoutePolicy":
				raw = `{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"https://personal.yun.139.com"}]}}`
			case "/file/create", "/file/getUploadUrl":
				var body struct {
					Size      int64              `json:"size"`
					Parts     []mobileUploadPart `json:"partInfos"`
					Account   map[string]any     `json:"commonAccountInfo"`
					ContentID string             `json:"contentHash"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if len(body.Parts) == 0 || len(body.Parts) > 100 {
					t.Fatal("upload URL batch outside 1–100", len(body.Parts))
				}
				if r.URL.Path == "/file/create" {
					creates++
					if body.Size != size || len(body.Parts) != 100 || body.ContentID != strings.Repeat("a", 64) {
						t.Fatal("large file create metadata changed")
					}
				} else {
					batches++
					if body.Account["account"] != "account" || body.Parts[0].Number != uploaded+1 {
						t.Fatal("continuation skipped or repeated parts")
					}
				}
				urls := []mobileUploadPartURL{}
				for _, part := range body.Parts {
					if part != parts[part.Number-1] {
						t.Fatal("part offsets or sizes changed")
					}
					urls = append(urls, mobileUploadPartURL{Number: part.Number, URL: fmt.Sprintf("https://upload.test/%d", part.Number)})
				}
				encoded, _ := json.Marshal(map[string]any{"success": true, "data": map[string]any{"fileId": "file", "uploadId": "session", "partInfos": urls}})
				raw = string(encoded)
			case "/file/complete":
				completed++
				if uploaded != len(parts) {
					t.Fatal("completed before all parts")
				}
			default:
				t.Fatal("unexpected upload request", r.URL.Path)
			}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}, nil
	})}
	if err := a.uploadMobileContent(context.Background(), mobilePCFixture("pc"), "/", "large.mkv", source, size, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if creates != 1 || completed != 1 || uploaded != len(parts) || len(source.offsets) != len(parts) || batches != (len(parts)-1)/100 {
		t.Fatal("incomplete upload protocol", creates, completed, uploaded, batches)
	}
}

func TestMobilePCMultipartFailureAndCancel(t *testing.T) {
	for _, variant := range []string{"denied", "invalid-part", "missing-part", "put-failure", "complete-failure", "cancel"} {
		t.Run(variant, func(t *testing.T) {
			a := testApp(t)
			original := apiClient
			defer func() { apiClient = original }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if variant == "cancel" {
				cancel()
			}
			calls := map[string]int{}
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				calls[r.URL.Path]++
				raw, status := `{"success":true,"data":{}}`, 200
				switch r.URL.Path {
				case "/user/route/qryRoutePolicy":
					raw = `{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"https://personal.yun.139.com"}]}}`
				case "/file/create":
					raw = `{"success":true,"data":{"fileId":"file","uploadId":"session","partInfos":[{"partNumber":1,"uploadUrl":"https://upload.test/part"}]}}`
					if variant == "denied" {
						raw = `{"success":false,"message":"private-account-token"}`
					} else if variant == "invalid-part" {
						raw = `{"success":true,"data":{"fileId":"file","uploadId":"session","partInfos":[{"partNumber":2,"uploadUrl":"https://upload.test/part"}]}}`
					} else if variant == "missing-part" {
						raw = `{"success":true,"data":{"fileId":"file","uploadId":"session"}}`
					}
				case "/part":
					if variant == "put-failure" {
						status = 503
					}
				case "/file/complete":
					if variant == "complete-failure" {
						status = 503
					}
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}, nil
			})}
			err := a.uploadMobileContent(ctx, mobilePCFixture("pc"), "/", "test.mkv", &mobilePCVirtualSource{}, 5, strings.Repeat("a", 64))
			if err == nil || strings.Contains(err.Error(), "private-account-token") {
				t.Fatal("failed upload accepted or leaked upstream body", err)
			}
			for _, n := range calls {
				if n > 1 {
					t.Fatal("unknown write automatically replayed", calls)
				}
			}
			if variant == "cancel" && (len(calls) != 0 || !errors.Is(err, context.Canceled)) {
				t.Fatal("cancelled upload sent requests", calls, err)
			}
			if variant != "complete-failure" && calls["/file/complete"] != 0 {
				t.Fatal("failed upload committed", calls)
			}
		})
	}
}

func TestMobilePCSyncAndStagingLimits(t *testing.T) {
	large := int64(100<<30) + 1
	replicas := []syncReplica{{storage: Storage{Type: "local"}}, {storage: mobilePCFixture("pc")}, {storage: mobilePCFixture("web")}}
	op := syncOperation{to: 1, file: syncFile{backupEntry: backupEntry{file: File{Size: large}}}}
	if limit, err := syncUploadStageLimit(replicas, []syncOperation{op}); err != nil || limit != mobilePCUploadFileLimit {
		t.Fatal("PC synchronization retained old staging limit", limit, err)
	}
	for _, target := range []int{0, 2} {
		op.to = target
		if _, err := syncUploadStageLimit(replicas, []syncOperation{op}); err == nil {
			t.Fatal("unrelated sync target inherited PC limit", target)
		}
	}
	a := testApp(t)
	dir := t.TempDir()
	source := Storage{Type: "local", Config: map[string]string{"root": dir}}
	_, err := a.stageBackupSource(context.Background(), source, File{ID: "/missing", Size: large}, dir, mobilePCUploadFileLimit)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal("PC staging retained 100 GiB gate", err)
	}
	_, err = a.stageBackupSource(context.Background(), source, File{ID: "/missing", Size: large}, dir, defaultUploadFileLimit)
	if err == nil || !strings.Contains(err.Error(), "100 GiB") {
		t.Fatal("feature download/default staging limit changed", err)
	}
}

func TestMobilePCMountWriteLimit(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "mount")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	f := &cloudWriteFile{File: file, storage: mobilePCFixture("pc")}
	if _, err = f.Seek(mobilePCUploadFileLimit, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if n, err := f.Write([]byte{1}); n != 0 || err == nil || f.writeErr == nil {
		t.Fatal("mount write crossed 500 GB", n, err)
	}
	if info, err := file.Stat(); err != nil || info.Size() != 0 {
		t.Fatal("oversized write consumed disk", info, err)
	}
	if err := f.publish(context.Background()); err == nil || !strings.Contains(err.Error(), "500 GB") {
		t.Fatal("flush could publish an incomplete failed write", err)
	}
	a := testApp(t)
	calls := 0
	original := apiClient
	defer func() { apiClient = original }()
	apiClient = &http.Client{Transport: casTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected cloud request")
	})}
	ctx := context.WithValue(context.Background(), mountPutLength{}, mobilePCUploadFileLimit+1)
	_, err = (mountFS{app: a}).openCloudWrite(ctx, mobilePCFixture("pc"), "/", "large.mkv", os.O_CREATE|os.O_TRUNC|os.O_WRONLY)
	if err == nil || calls != 0 {
		t.Fatal("oversized DAV body reached cloud", calls, err)
	}
}

func TestMobilePCStorageAPI(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	storage := mobilePCFixture("pc")
	if w := request(t, h, "POST", "/api/storages", storage, nil); w.Code != http.StatusUnauthorized {
		t.Fatal("unprotected storage client setting", w.Code)
	}
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "mobile-pc-owner", Password: "test"}, nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/storages", storage, cookie)
	if w.Code != http.StatusCreated {
		t.Fatal(w.Code, w.Body.String())
	}
	saved := a.store.snapshot().Storages[0]
	w = request(t, h, "GET", "/api/state", nil, cookie)
	var response struct {
		Storages []Storage `json:"storages"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || len(response.Storages) != 1 {
		t.Fatal("storage state response", w.Code, w.Body.String())
	}
	if response.Storages[0].Config["clientType"] != "pc" || response.Storages[0].Config["authorization"] != "********" {
		t.Fatal("client selection hidden or credential exposed")
	}
	edit := response.Storages[0]
	edit.Config["clientType"] = "web"
	w = request(t, h, "PUT", "/api/storages/"+saved.ID, edit, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	current := a.store.snapshot().Storages[0]
	if current.Config["clientType"] != "web" || current.Config["authorization"] != storage.Config["authorization"] {
		t.Fatal("editing client replaced credentials")
	}
	edit.Config["clientType"] = "13"
	w = request(t, h, "PUT", "/api/storages/"+saved.ID, edit, cookie)
	if w.Code != 400 || a.store.snapshot().Storages[0].Config["clientType"] != "web" {
		t.Fatal("invalid client modified saved storage", w.Code)
	}
}

func TestMobilePCRapidUploadDoesNotSendContent(t *testing.T) {
	for _, flag := range []string{"rapidUpload", "exist"} {
		t.Run(flag, func(t *testing.T) {
			a := testApp(t)
			original := apiClient
			defer func() { apiClient = original }()
			calls := 0
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				raw := `{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"https://personal.yun.139.com"}]}}`
				if r.URL.Path == "/file/create" {
					raw = fmt.Sprintf(`{"success":true,"data":{"%s":true}}`, flag)
				} else if r.URL.Path != "/user/route/qryRoutePolicy" {
					t.Fatal("rapid upload unexpectedly sent content or completed again", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}, nil
			})}
			source := &mobilePCVirtualSource{}
			if err := a.uploadMobileContent(context.Background(), mobilePCFixture("pc"), "/", "rapid.mkv", source, 500_000_000_000, strings.Repeat("a", 64)); err != nil {
				t.Fatal(err)
			}
			if calls != 2 || len(source.offsets) != 0 {
				t.Fatal("rapid upload read media", calls, source.offsets)
			}
		})
	}
}

func TestMobilePCSyncScanUsesConfiguredLimit(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "scan", Type: "webdav", Enabled: true, Config: map[string]string{"address": "https://scan.test", "root": "/"}}
	now := time.Now().UTC().Truncate(time.Second)
	large := File{ID: "/large.mkv", Name: "large.mkv", Size: defaultUploadFileLimit + 1, Modified: now}
	cached := syncBaseline{Files: []map[string]string{{"large.mkv": "digest"}}, Metadata: []map[string]string{{"large.mkv": syncMetadata(large)}}}
	original := apiClient
	defer func() { apiClient = original }()
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "PROPFIND" {
			t.Fatal("scan must reuse validated metadata cache, not read large content", r.Method)
		}
		raw := fmt.Sprintf(`<d:multistatus xmlns:d="DAV:"><d:response><d:href>/large.mkv</d:href><d:propstat><d:prop><d:displayname>large.mkv</d:displayname><d:getcontentlength>%d</d:getcontentlength><d:getlastmodified>%s</d:getlastmodified></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`, large.Size, now.Format(http.TimeFormat))
		return &http.Response{StatusCode: 207, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}, nil
	})}
	rule := BackupRule{}
	visited := 0
	rep, err := a.scanSyncReplica(context.Background(), BackupLocation{StorageID: s.ID, Path: "/"}, s, rule, map[string]bool{}, &visited, mobilePCUploadFileLimit, cached)
	if err != nil || len(rep.files) != 1 {
		t.Fatal("PC sync scan rejected large source", len(rep.files), err)
	}
	visited = 0
	if _, err := a.scanSyncReplica(context.Background(), BackupLocation{StorageID: s.ID, Path: "/"}, s, rule, map[string]bool{}, &visited, defaultUploadFileLimit, cached); err == nil {
		t.Fatal("ordinary synchronization scan limit changed")
	}
}
