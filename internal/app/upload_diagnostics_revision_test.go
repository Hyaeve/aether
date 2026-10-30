package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"golang.org/x/net/webdav"
)

func Test115MultipartUsesProviderSequentialContract(t *testing.T) {
	old := apiClient
	defer func() { apiClient = old }()
	file, err := os.CreateTemp(t.TempDir(), "large")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	const size = (16 << 20) + 17
	if err := file.Truncate(size); err != nil {
		t.Fatal(err)
	}
	var received int64
	parts, initiated, completed := 0, false, false
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		raw, status, headers := `{"state":true}`, 200, http.Header{}
		switch {
		case r.Method == "POST" && r.URL.Query().Has("uploads"):
			initiated = true
			if !r.URL.Query().Has("sequential") || !r.URL.Query().Has("x-oss-enable-sha1") || r.UserAgent() != driver.OSSUserAgent {
				status = 403
				raw = `<Error><Code>AccessDenied</Code><Message>private credential https://signed.invalid/?token=secret</Message></Error>`
			} else {
				raw = `<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>file</Key><UploadId>own-upload</UploadId></InitiateMultipartUploadResult>`
			}
		case r.Method == "PUT":
			n, err := io.Copy(io.Discard, r.Body)
			if err != nil {
				return nil, err
			}
			received += n
			parts++
			headers.Set("ETag", `"verified-part"`)
		case r.Method == "POST" && r.URL.Query().Get("uploadId") == "own-upload":
			completed = true
		default:
			t.Errorf("unexpected upload operation: %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	params := &driver.UploadOSSParams{Bucket: "bucket", Object: "file"}
	params.Callback.Callback = `{"callbackUrl":"https://115.com/callback"}`
	token := &driver.UploadOSSTokenResp{AccessKeyID: "key", AccessKeySecret: "secret", SecurityToken: "token"}
	if err := upload115OSS(context.Background(), file, size, params, token); err != nil {
		t.Fatal("provider rejected multipart request", transferFailureReason(err))
	}
	if !initiated || !completed || received != size || parts != 2 {
		t.Fatal("multipart upload did not complete exact bytes", initiated, completed, received, parts)
	}
}

func Test115UploadFailureReportsStageWithoutSecrets(t *testing.T) {
	old := apiClient
	defer func() { apiClient = old }()
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		raw := `<Error><Code>AccessDenied</Code><Message>credential=private-secret https://private.invalid/?token=private-secret</Message><RequestId>private-secret</RequestId></Error>`
		return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	f, err := os.CreateTemp(t.TempDir(), "upload")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate((16 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	err = upload115OSS(context.Background(), f, (16<<20)+1, &driver.UploadOSSParams{Bucket: "bucket", Object: "file"},
		&driver.UploadOSSTokenResp{AccessKeyID: "key", AccessKeySecret: "secret", SecurityToken: "token"})
	message := transferFailureReason(err)
	for _, text := range []string{"115", "初始化分片", "403", "AccessDenied"} {
		if !strings.Contains(message, text) {
			t.Fatalf("missing safe upload detail %q: %s", text, message)
		}
	}
	if strings.Contains(message, "private-secret") || strings.Contains(message, "https://") || strings.Contains(err.Error(), "private-secret") {
		t.Fatal("provider error leaked secrets", message)
	}
}

func TestBackupUploadFailureReportsHTTPStage(t *testing.T) {
	a, _, sourceDir, _ := backupFixture(t)
	dav := &webdav.Handler{FileSystem: webdav.Dir(t.TempDir()), LockSystem: webdav.NewMemLS()}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			http.Error(w, "private-secret signed-url", http.StatusForbidden)
			return
		}
		dav.ServeHTTP(w, r)
	}))
	defer remote.Close()
	source := a.store.snapshotWithLogLimit(0).Storages[0]
	target := Storage{ID: "target", Name: "Cloud", Type: "webdav", Enabled: true, Config: map[string]string{"address": remote.URL}}
	if err := a.store.update(func(st *State) error { st.Storages[1] = target; return nil }); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(sourceDir, "backup.cbz"), "content")
	err := a.copyBackupFile(context.Background(), source, target, File{ID: "/backup.cbz", Name: "backup.cbz", Size: 7}, "/", "backup.cbz", "skip")
	if err == nil {
		t.Fatal("rejected backup succeeded")
	}
	for _, entry := range a.transfers.items {
		if entry.Kind != "upload" {
			continue
		}
		if !strings.Contains(entry.Message, "上传目标文件") || !strings.Contains(entry.Message, "403") || strings.Contains(entry.Message, "private-secret") {
			t.Fatal("backup upload failure lost safe HTTP stage", entry.Message)
		}
		var diagnosed *fileTransferError
		if !errors.As(err, &diagnosed) {
			t.Fatal("backup did not retain stage", err)
		}
		return
	}
	t.Fatal("missing failed backup upload record")
}

func Test115UploadErrorCategoriesStaySafe(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		text string
	}{
		{"auth", driver.GetErr(99, "private-secret"), "重新扫码"},
		{"signature", driver.ErrUploadSigInvalid, "签名"},
		{"parameters", driver.ErrUploadSH1Invalid, "文件参数"},
		{"token", &oss.ServiceError{StatusCode: 403, Code: "SecurityTokenExpired", Message: "private-secret"}, "SecurityTokenExpired"},
		{"unknown-code", oss.ServiceError{StatusCode: 500, Code: "private-secret", Message: "private-secret"}, "未识别"},
		{"canceled", context.Canceled, "取消"},
		{"timeout", context.DeadlineExceeded, "超时"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := upload115Failure("测试阶段", tc.err)
			if !strings.Contains(err.Error(), tc.text) || strings.Contains(err.Error(), "private-secret") || !errors.Is(err, tc.err) {
				t.Fatal("unsafe or untraceable upload category", err)
			}
			if again := upload115Failure("外层", err); again != err {
				t.Fatal("precise stage overwritten by outer wrapper", again)
			}
		})
	}
}

func Test115UploadDeniedStopsBeforeHashOrSubmission(t *testing.T) {
	a := testApp(t)
	old := apiClient
	defer func() { apiClient = old }()
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/app/uploadinfo" {
			t.Errorf("denied upload reached another endpoint: %s", r.URL.Path)
		}
		raw := `{"state":true,"user_id":123,"userkey":"private-secret","size_limit":100000000000,"upload_allowed":false,"upload_allowed_msg":"private-secret"}`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	f, err := os.CreateTemp(t.TempDir(), "source")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("content"); err != nil {
		t.Fatal(err)
	}
	s := Storage{Type: "115", Config: map[string]string{"cookie": test115Cookie}}
	err = a.upload115(a.ctx, s, "0", "book.cbz", f, 7)
	message := transferFailureReason(err)
	offset, seekErr := f.Seek(0, io.SeekCurrent)
	if err == nil || seekErr != nil || offset != 0 || calls != 1 || !strings.Contains(message, "检查上传权限") || !strings.Contains(message, "不允许上传") || strings.Contains(message, "private-secret") {
		t.Fatal("denied upload did not stop safely before hashing", message, calls, offset, seekErr)
	}
}
