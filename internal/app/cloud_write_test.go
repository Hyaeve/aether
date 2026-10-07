package app

import (
	"context"
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

func TestMountWebDAVWriteLifecycle(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	remote := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()})
	defer remote.Close()
	s := Storage{ID: "dav-write", Name: "dav", Type: "webdav", Enabled: true, Config: map[string]string{"address": remote.URL, "root": "/", "deleteMode": "trash"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	fs := mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: "/"}}
	ctx := context.Background()
	if err := fs.Mkdir(ctx, "/books", 0755); err != nil {
		t.Fatal(err)
	}
	write := func(contents string, expected int64) error {
		f, err := fs.OpenFile(context.WithValue(ctx, mountPutLength{}, expected), "/books/book.txt", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if _, err = f.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
		return f.Close()
	}
	if err := write("original", 8); err != nil {
		t.Fatal(err)
	}
	if err := write("replacement", 11); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "books", "book.txt"))
	if err != nil || string(data) != "replacement" {
		t.Fatal(string(data), err)
	}
	if err := write("partial", 100); err == nil {
		t.Fatal("accepted truncated body")
	}
	data, _ = os.ReadFile(filepath.Join(root, "books", "book.txt"))
	if string(data) != "replacement" {
		t.Fatal("partial replaced old file")
	}
	if err := fs.Rename(ctx, "/books/book.txt", "/books/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Mkdir(ctx, "/other", 0755); err != nil {
		t.Fatal(err)
	}
	if err := fs.Rename(ctx, "/books/renamed.txt", "/other/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if err := fs.RemoveAll(ctx, "/other/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(root, "other", ".aether-trash", "*", "renamed.txt"))
	if len(matches) != 1 {
		t.Fatal("missing trash", matches)
	}
	fs.config.ReadOnly = true
	if err := fs.Mkdir(ctx, "/denied", 0755); err == nil {
		t.Fatal("readonly mkdir")
	}
	if err := fs.RemoveAll(ctx, "/books"); err == nil {
		t.Fatal("readonly remove")
	}
	pending, _ := filepath.Glob(filepath.Join(a.dataDir, "cache", "mount-upload", "pending-*"))
	if len(pending) != 1 {
		t.Fatal("failed stage not retained", pending)
	}
}

func TestNativeUploadProtocols(t *testing.T) {
	for _, kind := range []string{"mobile", "tianyi", "quark"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			data := "cloud upload content"
			file, err := os.CreateTemp(t.TempDir(), "source")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			file.WriteString(data)
			s := Storage{ID: kind, Type: kind, Enabled: true, Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:secret")), "username": "account", "password": "password", "accessToken": "access", "cookie": "cookie"}}
			a.tianyiSessions[s.ID] = tianyiSession{Key: "session", Secret: "0123456789abcdef", Credentials: sha256.Sum256([]byte(s.Config["username"] + "\x00" + s.Config["password"])), Expires: time.Now().Add(time.Hour)}
			original := apiClient
			defer func() { apiClient = original }()
			uploaded, completed := false, false
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				raw := `{}`
				h := http.Header{}
				status := 200
				var body []byte
				if r.Body != nil {
					body, _ = io.ReadAll(r.Body)
				}
				switch {
				case r.URL.Host == "upload.test" || strings.HasPrefix(r.URL.Host, "bucket."):
					if r.Method == "PUT" {
						if string(body) != data {
							t.Errorf("wrong uploaded bytes %q", body)
						}
						uploaded = true
						h.Set("ETag", `"part-etag"`)
					} else if r.Method == "POST" {
						raw = `{"state":true}`
					}
				case kind == "mobile":
					switch r.URL.Path {
					case "/user/route/qryRoutePolicy":
						raw = `{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"https://personal.yun.139.com"}]}}`
					case "/file/create":
						raw = `{"success":true,"data":{"fileId":"file","uploadId":"upload","partInfos":[{"partNumber":1,"uploadUrl":"https://upload.test/part"}]}}`
					case "/file/complete":
						completed = true
						raw = `{"success":true,"data":{}}`
					default:
						t.Errorf("unexpected mobile path %s", r.URL.Path)
					}
				case kind == "tianyi":
					encrypted := r.URL.Query().Get("params")
					ciphertext, err := hex.DecodeString(encrypted)
					if err != nil || len(ciphertext)%16 != 0 {
						t.Fatal("invalid encrypted params")
					}
					block, _ := aes.NewCipher([]byte("0123456789abcdef"))
					plain := make([]byte, len(ciphertext))
					for i := 0; i < len(plain); i += 16 {
						block.Decrypt(plain[i:i+16], ciphertext[i:i+16])
					}
					if len(plain) == 0 {
						t.Fatal("empty params")
					}
					plain = plain[:len(plain)-int(plain[len(plain)-1])]
					mac := hmac.New(sha1.New, []byte("0123456789abcdef"))
					fmt.Fprintf(mac, "SessionKey=session&Operate=GET&RequestURI=%s&Date=%s&params=%s", r.URL.Path, r.Header.Get("Date"), encrypted)
					if r.Header.Get("Signature") != strings.ToUpper(hex.EncodeToString(mac.Sum(nil))) {
						t.Error("invalid upload signature")
					}
					switch r.URL.Path {
					case "/person/initMultiUpload":
						if !strings.Contains(string(plain), "fileName=test.txt") {
							t.Error("missing filename")
						}
						raw = `{"uploadFileId":"upload","fileDataExists":0}`
					case "/person/getMultiUploadUrls":
						raw = `{"uploadUrls":{"partNumber_1":{"requestURL":"https://upload.test/part","requestHeader":"X-Part=one"}}}`
					case "/person/commitMultiUploadFile":
						completed = true
						raw = `{"file":{"userFileId":"file"}}`
					default:
						t.Errorf("unexpected tianyi path %s", r.URL.Path)
					}
				case kind == "quark":
					switch r.URL.Path {
					case "/1/clouddrive/file/upload/pre":
						raw = `{"code":0,"data":{"task_id":"task","obj_key":"file","upload_id":"upload","bucket":"bucket","upload_url":"oss.test","auth_info":"auth","callback":{}}}`
					case "/1/clouddrive/file/update/hash":
						raw = `{"code":0,"data":{"finish":false}}`
					case "/1/clouddrive/file/upload/auth":
						raw = `{"code":0,"data":{"auth_key":"signed"}}`
					case "/1/clouddrive/file/upload/finish":
						completed = true
						raw = `{"code":0,"data":{}}`
					default:
						t.Errorf("unexpected quark path %s", r.URL.Path)
					}
				default:
					t.Errorf("unexpected host %s", r.URL.Host)
				}
				return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
			})}
			if err := a.uploadCloud(context.Background(), s, "parent", "test.txt", file.Name()); err != nil {
				t.Fatal(err)
			}
			if !uploaded || !completed {
				t.Fatal("upload not completed", uploaded, completed)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := a.uploadCloud(ctx, s, "parent", "cancel.txt", file.Name()); err == nil {
				t.Fatal("cancelled upload succeeded")
			}
		})
	}
}
