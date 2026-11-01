package app

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ali115Response(r *http.Request, status int, body any) *http.Response {
	raw, _ := json.Marshal(body)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}
}

type ali115Fixture struct {
	a                                   *App
	s                                   Storage
	job                                 ali115Receipt
	own                                 map[string]aliFile
	target                              map[string]map[string]any
	deletes, copies, creates, downloads int
	mode                                string
}

func newAli115Fixture(t *testing.T, mode string) *ali115Fixture {
	t.Helper()
	a := testApp(t)
	f := &ali115Fixture{a: a, mode: mode, s: Storage{ID: "115", Name: "接收115", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}, own: map[string]aliFile{}, target: map[string]map[string]any{}}
	f.own["aether"] = aliFile{ID: "aether", Parent: "root", Name: "Aether", Type: "folder"}
	f.own["unrelated"] = aliFile{ID: "unrelated", Parent: "aether", Name: "用户原有文件.txt", Type: "file", Size: 5}
	f.target["101"] = map[string]any{"fid": "101", "cid": "10", "n": "Movie.mkv", "s": "42", "sha": strings.Repeat("a", 40), "pc": "pc101"}
	err := a.store.update(func(st *State) error {
		st.Storages = []Storage{f.s}
		st.Ali115 = ali115Settings{RefreshToken: strings.Repeat("r", 32), UserID: "alice", Nick: "阿里", Drive: "drive", Revision: "rev", Extensions: ali115DefaultExtensions, Targets: []ali115Target{{ID: "target", StorageID: "115", Directory: "0", Path: "/"}}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a.ali115Session = &aliSession{Refresh: strings.Repeat("r", 32), Access: "access", UserID: "alice", Drive: "drive", Revision: "rev", Until: time.Now().Add(time.Hour)}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		var out any
		status := 200
		if r.URL.Host == "api.alipan.com" {
			switch r.URL.Path {
			case "/v2/share_link/get_share_token":
				out = map[string]any{"share_token": "share-token"}
			case "/adrive/v3/share_link/get_share_by_anonymous":
				out = map[string]any{"share_name": "Movie"}
			case "/adrive/v3/file/list":
				items := []aliFile{}
				parent, _ := body["parent_file_id"].(string)
				if body["share_id"] != "" && body["share_id"] != nil {
					if parent == "root" {
						items = append(items, aliFile{ID: "share-folder", Name: "Movie", Type: "folder"})
					} else {
						items = append(items, aliFile{ID: "source", Parent: parent, Name: "Movie.mkv", Type: "file", Size: 42}, aliFile{ID: "image", Parent: parent, Name: "poster.jpg", Type: "file", Size: 3})
					}
				} else {
					for _, v := range f.own {
						if v.Parent == parent {
							items = append(items, v)
						}
					}
				}
				out = map[string]any{"items": items}
			case "/adrive/v2/file/createWithFolders":
				f.creates++
				if a.store.snapshot().Ali115Receipts[0].Intent == "" {
					t.Fatal("directory write without intent")
				}
				v := aliFile{ID: fmt.Sprintf("own%d", f.creates), Parent: body["parent_file_id"].(string), Name: body["name"].(string), Type: "folder"}
				f.own[v.ID] = v
				out = v
			case "/adrive/v2/batch":
				f.copies++
				if a.store.snapshot().Ali115Receipts[0].Intent == "" {
					t.Fatal("copy without intent")
				}
				if mode == "copy-unknown" {
					return nil, errors.New("private-token-connection-reset")
				}
				request := body["requests"].([]any)[0].(map[string]any)["body"].(map[string]any)
				digest := strings.Repeat("a", 40)
				if mode == "bad-sha" {
					digest = ""
				}
				f.own["copy"] = aliFile{ID: "copy", Parent: request["to_parent_file_id"].(string), Name: "Movie.mkv", Type: "file", Size: 42, SHA1: digest}
				out = map[string]any{"responses": []any{map[string]any{"id": "0", "status": 201, "body": map[string]any{"file_id": "copy"}}}}
			case "/v3/file/delete":
				f.deletes++
				key := body["file_id"].(string)
				if key == "aether" || key == "unrelated" || key == "source" {
					t.Fatalf("unowned delete %s", key)
				}
				if a.store.snapshot().Ali115Receipts[0].Intent == "" {
					t.Fatal("delete without intent")
				}
				delete(f.own, key)
				out = map[string]any{}
			default:
				t.Fatalf("unexpected Ali API %s", r.URL.Path)
			}
		} else if strings.HasSuffix(r.URL.Host, "115.com") {
			switch r.URL.Path {
			case "/files":
				cid := r.URL.Query().Get("cid")
				items := []any{}
				if cid == "0" {
					items = append(items, map[string]any{"cid": "10", "pid": "0", "n": "Movie"})
				} else if cid == "10" {
					for _, v := range f.target {
						items = append(items, v)
					}
				} else {
					t.Fatalf("wrong 115 dir %s", cid)
				}
				trail := []any{map[string]string{"cid": "0", "name": "根目录"}}
				if cid == "10" {
					trail = append(trail, map[string]string{"cid": "10", "name": "Movie"})
				}
				out = map[string]any{"state": true, "cid": cid, "offset": 0, "count": len(items), "data": items, "path": trail}
			case "/files/get_info":
				out = map[string]any{"state": true, "data": []any{f.target[r.URL.Query().Get("file_id")]}}
			default:
				t.Fatalf("unexpected115API %s", r.URL.Path)
			}
		} else {
			t.Fatalf("unexpected origin %s", r.URL.Host)
		}
		return ali115Response(r, status, out), nil
	})}
	f.job, err = a.newAli115Receipt("target", "https://www.alipan.com/s/abcdefghi?pwd=1234", "浏览器", "request")
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAli115LinkExtensionsAndDuplicateValidation(t *testing.T) {
	for _, raw := range []string{"https://www.alipan.com/s/abc123?pwd=A1b2 提取码 A1b2", "https://www.aliyundrive.com/s/abc123/folder/folder-123\n提取码：1234"} {
		if _, err := parseAliShare(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{"https://www.alipan.com.evil/s/id", "https://www.alipan.com/s/id/other/path", "https://www.alipan.com/s/id?pwd=12345", "https://www.alipan.com/s/id?pwd=1234 提取码：5678", "https://www.alipan.com/s/a https://www.alipan.com/s/b", "https://user@www.alipan.com/s/id"} {
		if _, err := parseAliShare(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if got, err := ali115Extensions(".MKV;ass;MKV"); err != nil || got != "mkv;ass" {
		t.Fatal(got, err)
	}
	for _, v := range []string{"", "mkv;../png", "mkv;*.mp4"} {
		if _, err := ali115Extensions(v); err == nil {
			t.Fatal(v)
		}
	}
	f := newAli115Fixture(t, "")
	cfg := f.a.store.snapshot().Ali115
	cfg.Targets = append(cfg.Targets, ali115Target{ID: "other", StorageID: "115", Directory: "0", Path: "/"})
	if validateAli115(cfg, f.a.store.snapshot(), false) == nil {
		t.Fatal("duplicate target")
	}
	if _, err := f.a.newAli115Receipt("target", f.job.Link, "浏览器", "request"); err == nil {
		t.Fatal("duplicate request")
	}
}

func TestAli115StageSkipAndScopedCleanup(t *testing.T) {
	f := newAli115Fixture(t, "")
	f.a.runAli115(f.a.ctx, f.job, nil)
	r := f.a.store.snapshot().Ali115Receipts[0]
	if r.Status != "completed" || r.Skipped != 1 || r.Total != 1 || len(r.Files) != 1 || r.Intent != "" || len(r.Temporary) != 0 || f.deletes != 3 || f.copies != 1 {
		t.Fatalf("result=%+v del=%d copy=%d", r, f.deletes, f.copies)
	}
	if len(f.own) != 2 || f.own["unrelated"].Name != "用户原有文件.txt" {
		t.Fatal("unrelated content changed")
	}
}

func TestAli115UnknownCopyAndInvalidSHAKeepTemporary(t *testing.T) {
	for _, mode := range []string{"copy-unknown", "bad-sha"} {
		t.Run(mode, func(t *testing.T) {
			f := newAli115Fixture(t, mode)
			f.a.runAli115(f.a.ctx, f.job, nil)
			r := f.a.store.snapshot().Ali115Receipts[0]
			if r.Status != "failed" || r.Intent == "" || len(r.Temporary) != 2 || f.deletes != 0 || f.copies != 1 || strings.Contains(r.Message, "private-token") {
				t.Fatalf("%+v del%d", r, f.deletes)
			}
			if _, err := f.a.newAli115Receipt("target", f.job.Link, "浏览器", "new-request"); err == nil {
				t.Fatal("uncertain write replayed")
			}
		})
	}
}

func TestAli115NewFileReadbackAndNoOverwrite(t *testing.T) {
	for _, mode := range []string{"success", "conflict", "uncertain", "bad-readback"} {
		t.Run(mode, func(t *testing.T) {
			f := newAli115Fixture(t, "")
			delete(f.target, "101")
			if mode == "conflict" {
				f.target["101"] = map[string]any{"fid": "101", "cid": "10", "n": "Movie.mkv", "s": "43", "sha": strings.Repeat("b", 40), "pc": "pc101"}
			}
			c, err := client115(f.a.ctx, f.s)
			if err != nil {
				t.Fatal(err)
			}
			h := &ali115Run{a: f.a, job: f.job, storage: f.s, client: c}
			calls := 0
			_, err = h.uploadWith(f.a.ctx, ali115Temp{Name: "Movie.mkv", Size: 42, SHA1: strings.Repeat("a", 40)}, "10", func() (string, bool, error) {
				calls++
				if f.a.store.snapshot().Ali115Receipts[0].Intent == "" {
					t.Fatal("no upload intent")
				}
				if mode == "uncertain" {
					return "", false, errors.New("secret")
				}
				digest := strings.Repeat("a", 40)
				if mode == "bad-readback" {
					digest = strings.Repeat("b", 40)
				}
				f.target["500"] = map[string]any{"fid": "500", "cid": "10", "n": "Movie.mkv", "s": "42", "sha": digest, "pc": "newpick"}
				return "newpick", false, nil
			})
			if mode == "success" {
				if err != nil || calls != 1 || f.a.store.snapshot().Ali115Receipts[0].Intent != "" {
					t.Fatal(err, calls)
				}
			} else if err == nil {
				t.Fatal("expected protection")
			}
			if mode == "conflict" && calls != 0 {
				t.Fatal("overwrote conflicting target")
			}
			if f.deletes != 0 {
				t.Fatal("unexpected Ali deletion")
			}
		})
	}
}

func TestAli115RefreshRotationPersistsBeforeSessionFailure(t *testing.T) {
	a := testApp(t)
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	next := strings.Repeat("n", 32)
	failUser := true
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v2/account/token":
			return ali115Response(r, 200, map[string]any{"access_token": "access", "refresh_token": next, "user_id": "alice"}), nil
		case "/v2/user/get":
			if a.store.snapshot().Ali115.RefreshToken != next {
				t.Fatal("token rotation not durable before next request")
			}
			if failUser {
				return ali115Response(r, 403, map[string]any{"message": "secret-token"}), nil
			}
			return ali115Response(r, 200, map[string]any{"user_id": "alice", "nick_name": "阿里", "default_drive_id": "drive"}), nil
		case "/users/v1/users/device/create_session":
			return ali115Response(r, 200, map[string]any{}), nil
		default:
			t.Fatal(r.URL.Path)
		}
		return nil, nil
	})}
	if _, err := a.aliLogin(a.ctx, strings.Repeat("r", 32), true); err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatal(err)
	}
	if a.store.snapshot().Ali115.RefreshToken != next {
		t.Fatal("rotated token lost")
	}
	_ = filepath.WalkDir(a.store.dir, func(p string, d os.DirEntry, e error) error {
		if e == nil && !d.IsDir() && strings.HasSuffix(p, ".json") {
			raw, _ := os.ReadFile(p)
			if strings.Contains(string(raw), next) {
				t.Fatal("plaintext refresh token on disk")
			}
		}
		return e
	})
	w := httptest.NewRecorder()
	a.ali115API(w, httptest.NewRequest("GET", "/api/ali115", nil))
	if strings.Contains(w.Body.String(), next) || strings.Contains(w.Body.String(), "refreshToken") {
		t.Fatal("secret exposed")
	}
	failUser = false
	w = httptest.NewRecorder()
	a.ali115AccountAPI(w, httptest.NewRequest("POST", "/api/ali115/account", strings.NewReader(`{}`)))
	if w.Code != 200 || a.store.snapshot().Ali115.Drive != "drive" {
		t.Fatal("stored rotating token cannot reconnect", w.Code, w.Body.String())
	}
}

func TestAli115RuntimeRestartAndImportExclude(t *testing.T) {
	f := newAli115Fixture(t, "")
	if err := f.a.ali115Update(f.job.ID, func(r *ali115Receipt) { r.Intent = "uncertain copy" }); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(f.a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.initRuntime(f.a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	r := store.snapshot().Ali115Receipts[0]
	if r.Status != "interrupted" || r.Intent != "uncertain copy" {
		t.Fatalf("%+v", r)
	}
	raw, _ := json.Marshal(f.a.store.jsonValues())
	if strings.Contains(string(raw), "uncertain copy") {
		t.Fatal("runtime intent in config modules")
	}
	h := f.a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "password"}, nil)
	cookie := setup.Result().Cookies()[0]
	res := request(t, h, "POST", "/api/config/backup", map[string]string{"password": "backup-secret"}, cookie)
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	plain, err := openBackup(res.Body.Bytes(), "backup-secret")
	if err != nil {
		t.Fatal(err)
	}
	var bundle configBundle
	if err = json.Unmarshal(plain, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.State.Ali115Receipts) != 0 {
		t.Fatal("receipts exported")
	}
	bundle.State.Ali115Receipts = []ali115Receipt{{ID: f.job.ID, Status: "completed"}}
	plain, _ = json.Marshal(bundle)
	nonce := make([]byte, f.a.store.aead.NonceSize())
	sealed := f.a.store.aead.Seal(nonce, nonce, plain, []byte("config-import"))
	if err = os.WriteFile(filepath.Join(f.a.store.dir, "import.pending"), sealed, 0600); err != nil {
		t.Fatal(err)
	}
	if err = applyPendingConfig(f.a.store); err != nil {
		t.Fatal(err)
	}
	if got := f.a.store.snapshot().Ali115Receipts; len(got) != 1 || got[0].Intent != "uncertain copy" || got[0].Status != "running" {
		t.Fatal("import replaced local receipts", got)
	}
}

func TestAli115QRRefreshAndRangeBounds(t *testing.T) {
	value := strings.Repeat("r", 32)
	raw, _ := json.Marshal(map[string]any{"pds_login_result": map[string]any{"refreshToken": value}})
	if got, err := aliQRRefresh(raw); err != nil || got != value {
		t.Fatal(got, err)
	}
	for _, raw := range []string{`{}`, `{"a":{"refreshToken":"aaaaaaaaaaaaaaaa"},"refreshToken":"bbbbbbbbbbbbbbbb"}`} {
		if _, err := aliQRRefresh([]byte(raw)); err == nil {
			t.Fatal("bad QR accepted")
		}
	}
	for _, spec := range []string{"-1-2", "0-42", "0-1048576", "1-0"} {
		if _, _, err := aliRange(spec, 42); err == nil {
			t.Fatal(spec)
		}
	}
	if start, end, err := aliRange("2-4", 42); err != nil || start != 2 || end != 4 {
		t.Fatal(start, end, err)
	}
}

func TestAli115ChallengeReadsOnlyRequestedRange(t *testing.T) {
	f := newAli115Fixture(t, "")
	temp := ali115Temp{ID: "copy", Parent: "filedir", Name: "Movie.mkv", Type: "file", Size: 42, SHA1: strings.Repeat("a", 40)}
	jobFolder := ali115Temp{ID: "jobdir", Parent: "aether", Name: "rapid-" + f.job.ID, Type: "folder"}
	fileFolder := ali115Temp{ID: "filedir", Parent: "jobdir", Name: "1", Type: "folder"}
	for _, v := range []ali115Temp{jobFolder, fileFolder, temp} {
		f.own[v.ID] = aliFile{ID: v.ID, Parent: v.Parent, Name: v.Name, Type: v.Type, Size: v.Size, SHA1: v.SHA1}
	}
	if err := f.a.ali115Update(f.job.ID, func(r *ali115Receipt) { r.Temporary = []ali115Temp{jobFolder, fileFolder, temp} }); err != nil {
		t.Fatal(err)
	}
	base := apiClient.Transport
	downloads := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v2/file/get_download_url" {
			return ali115Response(r, 200, map[string]any{"url": "https://cdn.aliyuncs.com/movie"}), nil
		}
		if r.URL.Host == "cdn.aliyuncs.com" {
			downloads++
			if r.Header.Get("Range") != "bytes=2-4" {
				t.Fatal(r.Header)
			}
			return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": {"bytes 2-4/42"}, "Content-Length": {"3"}}, Body: io.NopCloser(strings.NewReader("abc")), ContentLength: 3, Request: r}, nil
		}
		return base.RoundTrip(r)
	})}
	h := &ali115Run{a: f.a, job: f.job, session: f.a.ali115Session, storage: f.s}
	got, err := h.checksum(f.a.ctx, temp, "2-4")
	expected := sha1.Sum([]byte("abc"))
	if err != nil || got != strings.ToUpper(hex.EncodeToString(expected[:])) || downloads != 1 {
		t.Fatal(got, err, downloads)
	}
	if _, err = h.checksum(f.a.ctx, temp, "0-1048576"); err == nil || downloads != 1 {
		t.Fatal("unbounded download")
	}
}
