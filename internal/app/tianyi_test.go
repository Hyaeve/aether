package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTianyiNativeLifecycle(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "189-test", Name: "Tianyi", Type: "tianyi", Enabled: true, Config: map[string]string{"username": "18900000000", "password": "test-secret"}}
	if err := validateStorage(&s); err != nil {
		t.Fatal(err)
	}
	if rootOf(s) != "-11" || s.Config["mode"] != "native" {
		t.Fatal("wrong defaults")
	}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	hash := strings.Repeat("b", 32)
	cas := base64.StdEncoding.EncodeToString([]byte(`{"provider":"189","name":"Movie.mkv","size":123,"md5":"` + hash + `"}`))
	logins, commits, deletes := 0, 0, 0
	needCaptcha, secondDevice, badPassword, badKey, noHit, failedDelete := false, false, false, false, false, false
	expired := false
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("SessionKey") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials leaked")
		}
		http.ServeContent(w, r, "movie.mkv", time.Time{}, strings.NewReader("media-content"))
	}))
	defer media.Close()
	original := apiClient
	defer func() { apiClient = original }()
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() == "127.0.0.1" {
			return http.DefaultTransport.RoundTrip(r)
		}
		_ = r.ParseForm()
		raw := `{}`
		headers := http.Header{}
		status := 200
		if r.Header.Get("SessionKey") != "" {
			mac := hmac.New(sha1.New, []byte("secret"))
			fmt.Fprintf(mac, "SessionKey=session&Operate=%s&RequestURI=%s&Date=%s", r.Method, r.URL.Path, r.Header.Get("Date"))
			if r.Header.Get("Signature") != strings.ToUpper(hex.EncodeToString(mac.Sum(nil))) {
				t.Error("bad signature")
			}
			if expired {
				raw = `{"errorCode":"InvalidSessionKey"}`
				expired = false
				goto respond
			}
		}
		switch r.URL.Path {
		case "/api/portal/unifyLoginForPC.action":
			headers.Set("Location", tianyiAuth+"/login?lt=test-lt&reqId=test-req")
			status = 302
		case "/login":
			raw = "login"
		case "/api/logbox/oauth2/appConf.do":
			if r.Header.Get("Lt") != "test-lt" {
				t.Error("missing login headers")
			}
			raw = `{"result":"0","data":{"paramId":"parameter"}}`
		case "/api/logbox/config/encryptConf.do":
			pub := base64.StdEncoding.EncodeToString(der)
			if badKey {
				pub = "bad"
			}
			raw = `{"data":{"pubKey":"` + pub + `","pre":"{RSA}"}}`
		case "/api/logbox/oauth2/needcaptcha.do":
			raw = "0"
			if needCaptcha {
				raw = "1"
			}
		case "/api/logbox/oauth2/loginSubmit.do":
			logins++
			for field, expected := range map[string]string{"userName": s.Config["username"], "epd": s.Config["password"]} {
				cipher, e := hex.DecodeString(strings.TrimPrefix(r.Form.Get(field), "{RSA}"))
				if e != nil {
					t.Fatal(e)
				}
				plain, e := rsa.DecryptPKCS1v15(rand.Reader, key, cipher)
				if e != nil || string(plain) != expected {
					t.Error("credential RSA mismatch")
				}
			}
			raw = `{"result":0,"toUrl":"https://cloud.189.cn/session-return"}`
			if secondDevice {
				raw = `{"result":-133}`
			}
			if badPassword {
				raw = `{"result":-1}`
			}
		case "/getSessionForPC.action":
			if r.URL.Query().Get("returnType") != "JSON" || r.Header.Get("X-Request-ID") == "" || r.Header.Get("Referer") != "https://cloud.189.cn/" {
				t.Error("missing PC session exchange parameters")
			}
			// Some upstream responses still use XML despite the requested JSON.
			raw = `<userSession><res_code>0</res_code><sessionKey>session</sessionKey><sessionSecret>secret</sessionSecret></userSession>`
		case "/listFiles.action":
			raw = fmt.Sprintf(`{"fileListAO":{"fileList":[{"id":10,"name":"Movie.mkv.cas","size":%d}],"folderList":[]}}`, len(cas))
		case "/getFileDownloadUrl.action":
			address := media.URL
			if r.Form.Get("fileId") == "10" {
				address = "https://cdn.cloud.189.cn/feature"
			}
			raw = `{"fileDownloadUrl":"` + address + `"}`
		case "/feature":
			raw = cas
		case "/createFolder.action":
			if r.URL.Query().Get("parentFolderId") != "-11" || r.URL.Query().Get("folderName") != "Aether" {
				t.Error("bad temp parent")
			}
			raw = `{"id":"20"}`
		case "/createUploadFile.action":
			if r.Form.Get("md5") != strings.ToUpper(hash) || r.Form.Get("parentFolderId") != "20" {
				t.Error("wrong restore parameters")
			}
			raw = `{"uploadFileId":30,"fileDataExists":1,"fileCommitUrl":"https://upload.cloud.189.cn/commit"}`
			if noHit {
				raw = `{"uploadFileId":30,"fileDataExists":0}`
			}
		case "/commit":
			commits++
			raw = `<file><id>40</id></file>`
		case "/batch/createBatchTask.action":
			deletes++
			if !strings.Contains(r.Form.Get("taskInfos"), `"fileId":"40"`) || r.Form.Get("type") != "DELETE" {
				t.Error("untargeted delete")
			}
			raw = `{"taskId":"cleanup"}`
		case "/batch/checkBatchTask.action":
			raw = `{"taskStatus":4,"successedCount":1}`
			if failedDelete {
				raw = `{"taskStatus":4,"failedCount":1}`
			}
		default:
			t.Errorf("unexpected endpoint %s", r.URL)
		}
	respond:
		return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	task := Task{Name: "189 CAS", Kind: "cas", StorageID: s.ID, Source: "/", Target: "189-cas", Mode: "incremental", APIInterval: 200}
	if err := a.validateTask(&task); err != nil {
		t.Fatal(err)
	}
	// Exercise the legacy decoder for already-issued playback links.
	task.CASOperation = "restore"
	if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	content, err := os.ReadFile(filepath.Join(a.outputDir, "189-cas", "Movie.mkv.strm"))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(strings.TrimSpace(string(content)))
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(u.Path, "/stream/"))
	var claim streamClaim
	if err := json.Unmarshal(raw, &claim); err != nil || claim.CAS == nil || claim.CAS.MD5 != hash {
		t.Fatal("missing MD5 claim", err)
	}
	for _, method := range []string{"GET", "HEAD"} {
		r := httptest.NewRequest(method, u.RequestURI(), nil)
		if method == "GET" {
			r.Header.Set("Range", "bytes=0-4")
		}
		w := httptest.NewRecorder()
		a.Handler(t.TempDir()).ServeHTTP(w, r)
		if method == "GET" && (w.Code != 206 || w.Body.String() != "media") {
			t.Fatal(w.Code, w.Body.String())
		}
		if method == "HEAD" && (w.Code != 200 || w.Body.Len() != 0) {
			t.Fatal("HEAD failed")
		}
	}
	if commits != 1 || logins != 1 {
		t.Fatal("session/restore not reused", commits, logins)
	}
	reopened, err := newWithDirectories(context.Background(), a.store.dir, a.dataDir, a.outputDir)
	if err != nil {
		t.Fatal(err)
	}
	a = reopened
	if _, done, err := a.casDownload(context.Background(), s, claim); err != nil {
		t.Fatal(err)
	} else {
		done()
	}
	if commits != 1 {
		t.Fatal("restart duplicated restore")
	}
	_ = a.store.update(func(st *State) error { st.CASTemporary[0].LastUsed = time.Now().Add(-13 * time.Hour); return nil })
	failedDelete = true
	if _, err := a.cleanupCAS(context.Background()); err == nil || len(a.store.snapshot().CASTemporary) != 1 {
		t.Fatal("failed delete lost record")
	}
	failedDelete = false
	if n, err := a.cleanupCAS(context.Background()); err != nil || n != 1 || deletes != 2 {
		t.Fatal(n, err, deletes)
	}
	noHit = true
	if _, _, err := a.casDownload(context.Background(), s, claim); err == nil || commits != 1 {
		t.Fatal("nonhit committed")
	}
	expired = true
	if _, err := a.tianyiList(context.Background(), s, "-11"); err == nil {
		t.Fatal("expired session accepted")
	}
	if _, err := a.tianyiList(context.Background(), s, "-11"); err != nil {
		t.Fatal("relogin failed", err)
	}
	for _, flag := range []*bool{&needCaptcha, &secondDevice, &badPassword, &badKey} {
		*flag = true
		if _, err := a.tianyiLogin(context.Background(), s); err == nil {
			t.Fatal("login failure accepted")
		}
		*flag = false
	}
	if validateCASFor(s, CASInfo{Name: "Movie.mkv", Size: 12, SHA256: strings.Repeat("a", 64)}) == nil {
		t.Fatal("SHA256-only Tianyi accepted")
	}
	if _, err := decodeCAS([]byte(cas), ".cas"); err == nil {
		t.Fatal("empty filename accepted")
	}
	for _, address := range []string{"http://api.cloud.189.cn", "https://189.cn.evil.example", "https://user:pass@api.cloud.189.cn", "https://api.cloud.189.cn:444"} {
		if trustedTianyi(address) {
			t.Fatal("unsafe endpoint", address)
		}
	}
	legacy := Storage{Name: "old", Type: "tianyi", Config: map[string]string{"address": "https://openlist.example"}}
	if validateStorage(&legacy) == nil {
		t.Fatal("gateway accepted")
	}
}

func TestTianyiSessionResponseFormats(t *testing.T) {
	for _, raw := range []string{
		`{"res_code":0,"sessionKey":"key","sessionSecret":"secret"}`,
		`<?xml version="1.0"?><userSession><res_code>0</res_code><sessionKey>key</sessionKey><sessionSecret>secret</sessionSecret></userSession>`,
		`<userSession xmlns="https://api.cloud.189.cn"><sessionKey>key</sessionKey><sessionSecret>secret</sessionSecret></userSession>`,
	} {
		var session tianyiSession
		if err := tianyiDecode([]byte(raw), &session); err != nil || session.Key != "key" || session.Secret != "secret" {
			t.Fatalf("valid session lost: %+v %v", session, err)
		}
	}
	for _, raw := range []string{
		`<error><code>InvalidSessionKey</code></error>`,
		`<userSession><res_code>1</res_code><sessionKey>key</sessionKey></userSession>`,
		`<userSession><errorCode>InvalidSessionKey</errorCode></userSession>`,
		`{"res_code":1,"sessionKey":"key","sessionSecret":"secret"}`,
		`<html>not a session`,
	} {
		var session tianyiSession
		if err := tianyiDecode([]byte(raw), &session); err == nil {
			t.Fatal("invalid response accepted", raw)
		}
	}
}

func TestTianyiPermanentCleanupResume(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "189", Type: "tianyi", Enabled: true, Config: map[string]string{"username": "account", "password": "password", "deleteMode": "permanent"}}
	_ = a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, s)
		st.CASTemporary = []CASTemporary{{Key: "temp", StorageID: s.ID, FileID: "40", Name: "movie.mkv", LastUsed: time.Now().Add(-13 * time.Hour), Trashed: true}}
		return nil
	})
	a.tianyiSessions[s.ID] = tianyiSession{Key: "session", Secret: "secret", Credentials: sha256.Sum256([]byte("account\x00password")), Expires: time.Now().Add(time.Hour)}
	original := apiClient
	defer func() { apiClient = original }()
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		_ = r.ParseForm()
		if r.Form.Get("type") != "CLEAR_RECYCLE" {
			t.Error("repeated trash operation")
		}
		raw := `{"taskId":"cleanup"}`
		if strings.Contains(r.URL.Path, "checkBatchTask") {
			raw = `{"taskStatus":4,"successedCount":1}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	if n, err := a.cleanupCAS(context.Background()); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
