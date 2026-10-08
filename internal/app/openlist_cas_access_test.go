package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	linkstrm "aether/internal/linkcore/strm"
)

func TestOpenListAccountReadWriteAndRenew(t *testing.T) {
	logins, lists := 0, 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path == "/api/auth/login" {
			logins++
			if body["username"] != "owner" || body["password"] != "secret" {
				t.Error("invalid credentials")
			}
			jsonResponse(w, 200, map[string]any{"code": 200, "data": map[string]string{"token": "session"}})
			return
		}
		if r.Header.Get("Authorization") != "session" {
			t.Error("missing token")
		}
		if body["password"] == "secret" {
			t.Error("account password leaked as directory password")
		}
		if r.URL.Path == "/api/fs/list" {
			lists++
			if lists == 1 {
				jsonResponse(w, 200, map[string]int{"code": 401})
				return
			}
			io.WriteString(w, `{"code":200,"data":{"content":[{"name":"file.mp4"}],"total":1}}`)
			return
		}
		io.WriteString(w, `{"code":200,"data":{"raw_url":"https://cdn.example/file"}}`)
	}))
	defer upstream.Close()
	a := testApp(t)
	s := Storage{ID: id(), Type: "openlist", Config: map[string]string{"address": upstream.URL, "authMode": "account", "username": "owner", "password": "secret"}}
	files, err := a.rawList(context.Background(), s, "/")
	if err != nil || len(files) != 1 || logins != 2 {
		t.Fatal(files, err, logins)
	}
	if _, err = a.download(context.Background(), s, "/file.mp4", ""); err != nil {
		t.Fatal(err)
	}
	if err = openlistWriteJSON(context.Background(), s, "mkdir", map[string]string{"path": "/new"}); err != nil {
		t.Fatal(err)
	}
	if logins != 2 {
		t.Fatal("session not reused")
	}
}

func TestDuplicateStorageCreationAndRename(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	s := Storage{Name: "Quark", Type: "local", Enabled: true, Config: map[string]string{"root": t.TempDir()}}
	if w := request(t, h, "POST", "/api/storages", s, cookie); w.Code != 201 {
		t.Fatal(w.Code)
	}
	s.Name = " quark "
	if w := request(t, h, "POST", "/api/storages", s, cookie); w.Code != 400 {
		t.Fatal("duplicate accepted")
	}
	s.Name = "Other"
	request(t, h, "POST", "/api/storages", s, cookie)
	storages := a.store.snapshot().Storages
	s = storages[1]
	s.Name = "QUARK"
	if w := request(t, h, "PUT", "/api/storages/"+s.ID, s, cookie); w.Code != 400 {
		t.Fatal("duplicate rename accepted")
	}
}

func TestGeneratedCASPlaybackAndPermanentImport(t *testing.T) {
	a := testApp(t)
	local := addLocal(t, a)
	binding := addCASBindings(t, a)[0]
	binding.Config["authorization"] = base64.StdEncoding.EncodeToString([]byte("pc:13900000000:test"))
	a.store.update(func(st *State) error {
		for i := range st.Storages {
			if st.Storages[i].ID == binding.ID {
				st.Storages[i] = binding
			}
		}
		return nil
	})
	writeTest(t, filepath.Join(local.Config["root"], "film.mp4"), "media")
	task := Task{ID: "cas-task", Name: "CAS", Kind: "cas", StorageID: local.ID, CASBindingID: binding.ID, Source: "/", Mode: "full", Target: "cas", RetentionHours: 12}
	if _, err := a.executeTask(context.Background(), task, local); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(a.outputDir, "cas", "film.mp4.cas.strm")
	content, _ := os.ReadFile(file)
	info := generatedCASClaim(t, content)
	if info.MD5 == "" || info.SHA256 == "" {
		t.Fatal(info)
	}
	pointer, err := linkstrm.Read(file, nil)
	if err != nil || pointer.URL != info.PlaybackURL {
		t.Fatal(pointer, err)
	}
	original := apiClient
	defer func() { apiClient = original }()
	restores := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if r.Body != nil {
			json.NewDecoder(r.Body).Decode(&body)
		}
		var data any = map[string]any{}
		switch r.URL.Path {
		case "/user/route/qryRoutePolicy":
			data = map[string]any{"routePolicyList": []any{map[string]string{"modName": "personal", "httpsUrl": "https://personal.yun.139.com"}}}
		case "/file/list":
			data = map[string]any{"items": []any{}}
		case "/file/create":
			if body["type"] == "folder" {
				data = map[string]string{"fileId": "aether-folder"}
			} else {
				restores++
				if body["contentHash"] != info.SHA256 {
					t.Error("wrong hash")
				}
				data = map[string]any{"fileId": "restored", "rapidUpload": true}
			}
		case "/file/getDownloadUrl":
			data = map[string]string{"cdnUrl": "https://cdn.example/film.mp4"}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		raw, _ := json.Marshal(map[string]any{"success": true, "data": data})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(raw)), Request: r}, nil
	})}
	h := a.Handler(t.TempDir())
	for range 2 {
		w := request(t, h, "GET", info.PlaybackURL, nil, nil)
		if w.Code != 302 || w.Header().Get("Location") != "https://cdn.example/film.mp4" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if restores != 1 || len(a.store.snapshot().CASTemporary) != 1 {
		t.Fatal("temporary reuse failed")
	}
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writer.WriteField("mode", "cas")
	writer.WriteField("storageId", binding.ID)
	writer.WriteField("parent", "/")
	part, _ := writer.CreateFormFile("cas", "film.mp4.cas")
	legacy, _ := json.Marshal(info)
	part.Write([]byte(base64.StdEncoding.EncodeToString(legacy)))
	writer.Close()
	req := httptest.NewRequest("POST", "/api/files/offline", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(setup.Result().Cookies()[0])
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"success":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if restores != 2 || len(a.store.snapshot().CASTemporary) != 1 {
		t.Fatal("durable import was tracked as temporary")
	}
	raw, _ := base64.StdEncoding.DecodeString(string(content))
	if strings.Contains(string(raw), binding.Config["authorization"]) {
		t.Fatal("credential in CAS")
	}
}
