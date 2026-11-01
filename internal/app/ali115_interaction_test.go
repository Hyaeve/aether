package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

func TestAli115TelegramSingleAndPaginatedConfirmation(t *testing.T) {
	for _, count := range []int{1, 6} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := newAli115Fixture(t, "")
			a := f.a
			cfg := telegramConfig{Enabled: true, Token: "123456789:" + strings.Repeat("a", 35), ChatID: "123", UserIDs: []string{"123"}}
			if err := a.store.update(func(st *State) error {
				st.Ali115Receipts = nil
				st.Notifications.Telegram = cfg
				st.TelegramRuntime.Bot = telegramDigest(cfg.Token)
				for i := 1; i < count; i++ {
					st.Ali115.Targets = append(st.Ali115.Targets, ali115Target{ID: fmt.Sprintf("target%d", i), StorageID: "115", Directory: fmt.Sprint(i), Path: fmt.Sprintf("/目录%d", i)})
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			base := apiClient.Transport
			var markup telegramTestMarkup
			apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "api.telegram.org" {
					return base.RoundTrip(r)
				}
				if err := json.NewDecoder(r.Body).Decode(&markup); err != nil {
					t.Fatal(err)
				}
				return ali115Response(r, 200, map[string]any{"ok": true, "result": map[string]any{"message_id": 77}}), nil
			})
			if err := a.handleTelegramUpdate(a.ctx, cfg, telegramTextUpdate(1, 123, 123, "https://www.alipan.com/s/abcdefghi?pwd=1234")); err != nil {
				t.Fatal(err)
			}
			job := a.store.snapshot().TelegramRuntime.Jobs[0]
			if job.Kind != "ali-rapid" || job.Provider != "aliyun" || len(job.Targets) != count {
				t.Fatal(job)
			}
			if count == 1 {
				if job.Status != "queued" || job.Selected != 0 {
					t.Fatal("single must directly queue", job.Status)
				}
				a.executeTelegramTransfer(a.ctx, job)
				saved := a.store.snapshot()
				if saved.TelegramRuntime.Jobs[0].Status != "completed" || len(saved.Ali115Receipts) != 1 || saved.Ali115Receipts[0].Source != "TG Bot" || saved.Ali115Receipts[0].Skipped != 1 {
					t.Fatalf("%+v", saved.Ali115Receipts)
				}
			} else {
				if job.Status != "choosing" || len(markup.Markup.Rows) != 6 || markup.Markup.Rows[5][0].Text != "下一页" {
					t.Fatalf("%s %+v", job.Status, markup)
				}
				if err := a.telegramCallback(a.ctx, cfg, 123, 123, 77, "t:"+job.ID+":5"); err == nil {
					t.Fatal("offpage accepted")
				}
				if err := a.telegramCallback(a.ctx, cfg, 123, 123, 77, "p:"+job.ID+":1"); err != nil {
					t.Fatal(err)
				}
				if len(markup.Markup.Rows) != 2 || !strings.Contains(markup.Markup.Rows[0][0].Text, "目录5") {
					t.Fatal(markup)
				}
				if err := a.telegramCallback(a.ctx, cfg, 123, 123, 77, "t:"+job.ID+":5"); err != nil {
					t.Fatal(err)
				}
				if a.store.snapshot().TelegramRuntime.Jobs[0].Status != "confirming" || markup.Markup.Rows[0][1].Text != "确认阿里云盘秒传" {
					t.Fatal("missing confirmation", markup)
				}
				if err := a.telegramCallback(a.ctx, cfg, 123, 999, 77, "c:"+job.ID+":5"); err == nil {
					t.Fatal("foreign confirmation")
				}
				if err := a.store.update(func(st *State) error { st.Ali115.Revision = "changed"; return nil }); err != nil {
					t.Fatal(err)
				}
				if err := a.telegramCallback(a.ctx, cfg, 123, 123, 77, "c:"+job.ID+":5"); err == nil {
					t.Fatal("changed account accepted")
				}
				if f.copies != 0 || f.deletes != 0 {
					t.Fatal("cloud write before confirmation")
				}
			}
		})
	}
}

func TestAli115QRLoginOwnerAndStableDevice(t *testing.T) {
	a := testApp(t)
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	devices := []string{}
	rotations := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		var out any
		switch r.URL.Path {
		case "/newlogin/qrcode/generate.do":
			out = map[string]any{"content": map[string]any{"data": map[string]any{"t": 123456, "ck": "secret-ck", "codeContent": "https://www.alipan.com/scan/test"}}}
		case "/newlogin/qrcode/query.do":
			raw, _ := json.Marshal(map[string]any{"pds_login_result": map[string]any{"refreshToken": strings.Repeat("r", 32)}})
			out = map[string]any{"content": map[string]any{"data": map[string]any{"qrCodeStatus": "CONFIRMED", "bizExt": base64.StdEncoding.EncodeToString(raw)}}}
		case "/v2/account/token":
			rotations++
			out = map[string]any{"access_token": "access", "refresh_token": fmt.Sprintf("%032d", rotations), "user_id": "alice"}
		case "/v2/user/get":
			if a.store.snapshot().Ali115.RefreshToken != fmt.Sprintf("%032d", rotations) {
				t.Fatal("rotation missing")
			}
			out = map[string]any{"user_id": "alice", "nick_name": "阿里账号", "default_drive_id": "backup", "resource_drive_id": "resource"}
		case "/users/v1/users/device/create_session":
			var body struct {
				Pub     string `json:"pubKey"`
				Refresh string `json:"refreshToken"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			device := r.Header.Get("X-Device-Id")
			devices = append(devices, device)
			sig, err := hex.DecodeString(r.Header.Get("X-Signature"))
			if err != nil || len(sig) != 65 {
				t.Fatal("signature", err)
			}
			digest := sha256.Sum256([]byte("5dde4e1bdf9e4966b387ba58f4b3fdc3:" + device + ":alice:0"))
			compact := append([]byte{27 + sig[64]}, sig[:64]...)
			pub, _, err := ecdsa.RecoverCompact(compact, digest[:])
			if err != nil || hex.EncodeToString(pub.SerializeUncompressed()[1:]) != body.Pub {
				t.Fatal("signature key mismatch", err)
			}
			out = map[string]any{}
		default:
			t.Fatalf("unexpected API %s", r.URL.Path)
		}
		return ali115Response(r, 200, out), nil
	})}
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "pw"}, nil)
	cookie := setup.Result().Cookies()[0]
	if request(t, h, "GET", "/api/ali115", nil, nil).Code != 401 {
		t.Fatal("unprotected config")
	}
	res := request(t, h, "POST", "/api/ali115/qr", map[string]any{}, cookie)
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	var qr map[string]string
	_ = json.Unmarshal(res.Body.Bytes(), &qr)
	if strings.Contains(res.Body.String(), "secret-ck") || !strings.HasPrefix(qr["image"], "data:image/png;base64,") {
		t.Fatal("QR metadata exposed")
	}
	wrong := httptest.NewRequest("GET", "/api/ali115/qr?id="+qr["id"], nil)
	wrong.AddCookie(&http.Cookie{Name: "aether_session", Value: "another"})
	w := httptest.NewRecorder()
	a.aliQRAPI(w, wrong)
	if w.Code != 410 {
		t.Fatal("QR owner")
	}
	res = request(t, h, "GET", "/api/ali115/qr?id="+qr["id"], nil, cookie)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "CONFIRMED") {
		t.Fatal(res.Body.String())
	}
	if request(t, h, "GET", "/api/ali115/qr?id="+qr["id"], nil, cookie).Code != 410 {
		t.Fatal("QR consumed twice")
	}
	if _, err := a.aliLogin(a.ctx, a.store.snapshot().Ali115.RefreshToken, false); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 || devices[0] != devices[1] || a.store.snapshot().Ali115.Drive != "resource" {
		t.Fatal("unstable device or wrong drive", devices)
	}
}

func TestAli115ChangedTemporaryIdentityNeverDeletes(t *testing.T) {
	f := newAli115Fixture(t, "")
	temp := ali115Temp{ID: "copy", Parent: "filedir", Name: "Movie.mkv", Type: "file", Size: 42, SHA1: strings.Repeat("a", 40)}
	if err := f.a.ali115Update(f.job.ID, func(r *ali115Receipt) { r.Temporary = []ali115Temp{temp} }); err != nil {
		t.Fatal(err)
	}
	f.own["copy"] = aliFile{ID: "copy", Parent: "filedir", Name: "changed.mkv", Type: "file", Size: 42, SHA1: temp.SHA1}
	h := &ali115Run{a: f.a, job: f.job, storage: f.s, session: f.a.ali115Session}
	if err := h.remove(f.a.ctx, temp); err == nil || f.deletes != 0 {
		t.Fatal("changed temp deleted", err)
	}
	f.a.ali115Gate.Lock()
	defer f.a.ali115Gate.Unlock()
	w := httptest.NewRecorder()
	f.a.ali115API(w, httptest.NewRequest("PUT", "/api/ali115", strings.NewReader(`{"extensions":"mp4"}`)))
	if w.Code != 409 {
		t.Fatal("config changed while running")
	}
}

func TestAli115TargetAPINormalizesAndRejectsDuplicate(t *testing.T) {
	f := newAli115Fixture(t, "")
	w := httptest.NewRecorder()
	f.a.ali115API(w, httptest.NewRequest("POST", "/api/ali115", strings.NewReader(`{"storageId":"115","directory":"/","path":"/"}`)))
	if w.Code != 409 {
		t.Fatal("root alias duplicate", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	f.a.ali115API(w, httptest.NewRequest("POST", "/api/ali115", strings.NewReader(`{"storageId":"115","directory":"10","path":"/untrusted-label"}`)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	st := f.a.store.snapshot()
	if len(st.Ali115.Targets) != 2 || st.Ali115.Targets[1].Path != "/Movie" {
		t.Fatal("directory path must be read from115", st.Ali115.Targets)
	}
	w = httptest.NewRecorder()
	f.a.ali115API(w, httptest.NewRequest("POST", "/api/ali115", strings.NewReader(`{"storageId":"115","directory":"10","path":"/Movie"}`)))
	if w.Code != 409 {
		t.Fatal("duplicate child accepted")
	}
	if f.creates != 0 || f.copies != 0 || f.deletes != 0 {
		t.Fatal("adding target wrote cloud")
	}
}

func TestAli115ListRejectsUnboundedAndUnsafePages(t *testing.T) {
	for _, mode := range []string{"cycle", "unsafe-name", "duplicate-path"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			old := apiClient
			t.Cleanup(func() { apiClient = old })
			calls := 0
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				if strings.Contains(r.URL.Path, "get_share_by_anonymous") {
					return ali115Response(r, 200, map[string]any{"share_name": "Movie"}), nil
				}
				if r.URL.Path != "/adrive/v3/file/list" {
					t.Fatal("unexpected write", r.URL.Path)
				}
				calls++
				items := []aliFile{{ID: "a", Name: "Movie.mkv", Type: "file", Size: 42}}
				next := ""
				if mode == "unsafe-name" {
					items[0].Name = "bad\nname.mkv"
				}
				if mode == "cycle" {
					next = "repeat"
				}
				if mode == "duplicate-path" {
					items = append(items, aliFile{ID: "b", Name: "Movie.mkv", Type: "file", Size: 42})
				}
				return ali115Response(r, 200, map[string]any{"items": items, "next_marker": next}), nil
			})}
			if _, _, err := ali115Plan(a.ctx, &aliSession{Drive: "drive"}, aliShare{Code: "share", Folder: "root"}, "token", "mkv"); err == nil || calls > 2 {
				t.Fatal("unsafe scan accepted", err, calls)
			}
		})
	}
}

func TestAli115PoolChangeGuardAndDeleteDetachesTargets(t *testing.T) {
	f := newAli115Fixture(t, "")
	h := f.a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "pw"}, nil)
	cookie := setup.Result().Cookies()[0]
	res := request(t, h, "DELETE", "/api/storages/115", nil, cookie)
	if res.Code != 409 || !strings.Contains(res.Body.String(), "阿里云盘秒传") {
		t.Fatal("active pool not protected", res.Code, res.Body.String())
	}
	if err := f.a.ali115Update(f.job.ID, func(r *ali115Receipt) { r.Status = "completed" }); err != nil {
		t.Fatal(err)
	}
	res = request(t, h, "DELETE", "/api/storages/115", nil, cookie)
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	st := f.a.store.snapshot()
	if len(st.Ali115.Targets) != 0 || len(st.Ali115Receipts) != 1 {
		t.Fatal("pool deletion lost receipts or kept dangling targets")
	}
}

func TestAli115UnregisteredTemporaryFileIsRetained(t *testing.T) {
	f := newAli115Fixture(t, "")
	temp := ali115Temp{ID: "userfile", Parent: "filedir", Name: "Movie.mkv", Type: "file", Size: 42, SHA1: strings.Repeat("a", 40)}
	jobFolder := ali115Temp{ID: "jobdir", Parent: "aether", Name: "rapid-" + f.job.ID, Type: "folder"}
	fileFolder := ali115Temp{ID: "filedir", Parent: "jobdir", Name: "1", Type: "folder"}
	for _, v := range []ali115Temp{jobFolder, fileFolder, temp} {
		f.own[v.ID] = aliFile{ID: v.ID, Parent: v.Parent, Name: v.Name, Type: v.Type, Size: v.Size, SHA1: v.SHA1}
	}
	if err := f.a.ali115Update(f.job.ID, func(r *ali115Receipt) { r.Temporary = []ali115Temp{jobFolder, fileFolder} }); err != nil {
		t.Fatal(err)
	}
	h := &ali115Run{a: f.a, job: f.job, storage: f.s, session: f.a.ali115Session}
	if err := h.remove(f.a.ctx, temp); err == nil || f.deletes != 0 {
		t.Fatal("unregistered user file removed", err)
	}
}

func TestAli115ReaddedTargetKeepsUncertainHistoryAndNoReplay(t *testing.T) {
	f := newAli115Fixture(t, "copy-unknown")
	f.a.runAli115(f.a.ctx, f.job, nil)
	if err := f.a.store.update(func(st *State) error { st.Ali115.Targets[0].ID = "readded"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.newAli115Receipt("readded", f.job.Link, "浏览器", "new-request"); err == nil {
		t.Fatal("uncertain job replayed after removing/readding same directory")
	}
	w := httptest.NewRecorder()
	f.a.ali115RecordsAPI(w, httptest.NewRequest("GET", "/api/ali115/records?target=readded", nil))
	var records []ali115Receipt
	if err := json.Unmarshal(w.Body.Bytes(), &records); err != nil || len(records) != 1 || records[0].Intent != "" || len(records[0].Temporary) != 0 {
		t.Fatal("history lost or private runtime exposed", err, w.Body.String())
	}
}
