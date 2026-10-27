package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

func TestCrossStoragePreflightDiagnostics(t *testing.T) {
	for _, scenario := range []struct {
		name, phase, reason string
	}{
		{"source", "读取来源目录", "HTTP 403"},
		{"target", "读取目标目录", "HTTP 403"},
		{"selection", "核对复制选区", "源目录已变化"},
		{"conflict", "检查目标同名项目", "目标已有同名项目"},
		{"scan", "扫描来源目录", "HTTP 403"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			a := testApp(t)
			sourceRoot, targetRoot := t.TempDir(), t.TempDir()
			if err := os.Mkdir(filepath.Join(sourceRoot, "Series"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sourceRoot, "Series", "film.mkv"), []byte("verified bytes"), 0600); err != nil {
				t.Fatal(err)
			}
			if scenario.name == "conflict" {
				if err := os.Mkdir(filepath.Join(targetRoot, "Series"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			var writes atomic.Int32
			dav := func(root, side string) *httptest.Server {
				handler := &webdav.Handler{FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" && r.Method != "HEAD" && r.Method != "PROPFIND" {
						writes.Add(1)
					}
					if r.Method == "PROPFIND" && (scenario.name == side || scenario.name == "scan" && side == "source" && strings.Trim(r.URL.Path, "/") == "Series") {
						http.Error(w, "https://private.invalid/?token=secret Cookie=secret", http.StatusForbidden)
						return
					}
					handler.ServeHTTP(w, r)
				}))
				t.Cleanup(server.Close)
				return server
			}
			sourceServer, targetServer := dav(sourceRoot, "source"), dav(targetRoot, "target")
			source := Storage{ID: "source", Type: "webdav", Name: "来源", Enabled: true, Config: map[string]string{"address": sourceServer.URL, "root": "/"}}
			target := Storage{ID: "target", Type: "webdav", Name: "目标", Enabled: true, Config: map[string]string{"address": targetServer.URL, "root": "/"}}
			if err := a.store.update(func(st *State) error {
				st.Storages = []Storage{source, target}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			selected := "/Series"
			if scenario.name == "selection" {
				selected = "/missing"
			}
			done, err := a.crossStorageFileAction(a.ctx, source, target, fileActionRequest{Action: "copy", Source: "/", Target: "/", IDs: []string{selected}})
			if done != 0 || err == nil || !strings.Contains(err.Error(), scenario.phase) || !strings.Contains(err.Error(), scenario.reason) {
				t.Fatalf("missing safe preflight diagnosis: done=%d err=%v", done, err)
			}
			if writes.Load() != 0 {
				t.Fatal("preflight failure performed cloud writes")
			}
			a.transfers.mu.Lock()
			for _, entry := range a.transfers.items {
				if entry.Kind == "copy" && (entry.Status != "failed" || entry.Done != 0 || entry.Total != 0 || !strings.Contains(entry.Message, scenario.phase)) {
					t.Error("missing failed record diagnosis", entry)
				}
			}
			a.transfers.mu.Unlock()
			events := transferEvents(a)
			if len(events) != 2 || !strings.Contains(events[1].Message, scenario.phase) || !strings.Contains(events[1].Message, scenario.reason) {
				t.Fatal("missing transfer log diagnosis", events)
			}
			for _, event := range events {
				if strings.Contains(event.Message, "private.invalid") || strings.Contains(event.Message, "secret") {
					t.Fatal("private upstream response logged", event)
				}
			}
		})
	}
}

func TestTransferFailureClassifiesLocalResourceAndAuthErrors(t *testing.T) {
	for _, item := range []struct {
		err  error
		want string
	}{
		{&os.PathError{Op: "write", Path: "/private/path", Err: syscall.ENOSPC}, "磁盘空间不足"},
		{&os.PathError{Op: "write", Path: "/private/path", Err: syscall.EROFS}, "文件系统只读"},
		{&os.LinkError{Op: "link", Old: "/private/staged", New: "/private/target", Err: os.ErrExist}, "已存在"},
		{&os.PathError{Op: "mkdir", Path: "/private/path", Err: syscall.ENOTDIR}, "不是目录"},
		{authStorageError(errors.New("private-token"), true), "存储授权失效或访问被拒绝"},
		{context.Canceled, "已取消或连接中断"},
		{&url.Error{Op: "Get", URL: "https://private.invalid/?token=secret", Err: &net.DNSError{Name: "private.invalid", Err: "private-token", IsNotFound: true}}, "域名解析失败"},
		{&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, "拒绝连接"},
		{&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, "连接中断"},
		{&net.DNSError{Name: "private.invalid", Err: "private-token", IsTimeout: true}, "超时"},
	} {
		got := transferFailureReason(item.err)
		if !strings.Contains(got, item.want) || strings.Contains(got, "private") {
			t.Errorf("unsafe or unclassified reason %q, want %q", got, item.want)
		}
	}
}

func TestFileTransferDiagnosticsAreScoped(t *testing.T) {
	cause := fmt.Errorf("private-token: %w", context.Canceled)
	if got := diagnoseFileTransfer(context.Background(), "下载来源文件到本机暂存", cause); got != cause {
		t.Fatal("changed shared backup error without a file-manager operation", got)
	}
	ctx := context.WithValue(context.Background(), fileTransferDiagnosticKey{}, true)
	diagnosed := diagnoseFileTransfer(ctx, "下载来源文件到本机暂存", cause)
	if !errors.Is(diagnosed, context.Canceled) || strings.Contains(diagnosed.Error(), "private-token") {
		t.Fatal("lost error cause or leaked private cause", diagnosed)
	}
	if got := diagnoseFileTransfer(ctx, "暂存并写入目标文件", diagnosed); got != diagnosed {
		t.Fatal("outer step replaced the precise failure stage", got)
	}
	if got := rejectFileTransfer(context.Background(), "核对单文件大小", "备份单文件上限100GiB"); got.Error() != "备份单文件上限100GiB" {
		t.Fatal("changed legacy backup rejection", got)
	}
}

func TestCrossStorageDiagnosticPreservesCauseWithoutLoggingIt(t *testing.T) {
	a := testApp(t)
	a.transfers.file = filepath.Join(t.TempDir(), "failed.json")
	cause := fmt.Errorf("https://private.invalid/?token=secret: %w", context.Canceled)
	err := &fileTransferError{phase: "读取来源目录", reason: transferFailureReason(cause), cause: cause}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("diagnostic lost cancellation")
	}
	progress := a.beginTransfer(a.ctx, "copy", Storage{Name: "来源"}, "复制", 0)
	progress.finish(err)
	events := transferEvents(a)
	if len(events) != 2 || events[1].Level != "warn" || !strings.Contains(events[1].Message, "读取来源目录") {
		t.Fatal("incorrect diagnostic lifecycle", events)
	}
	for _, event := range events {
		if strings.Contains(event.Message, "private.invalid") || strings.Contains(event.Message, "secret") {
			t.Fatal("private cause escaped diagnostic", event)
		}
	}
	entry := a.transfers.items[progress.id]
	if entry.Message != "读取来源目录：已取消或连接中断" {
		t.Fatal("failed record lost diagnostic", entry)
	}
	var restored transferLog
	restored.restore(a.transfers.file)
	if restored.items[progress.id].Message != entry.Message {
		t.Fatal("restart lost failed record diagnosis", restored.items)
	}
}

func TestCrossStorageLocalFailureDetails(t *testing.T) {
	for _, scenario := range []struct {
		name, phase, reason string
	}{
		{"download", "下载来源文件到本机暂存", "HTTP 403"},
		{"temporary", "准备本机暂存目录", ""},
		{"incomplete", "核对来源文件完整性", "源文件内容不完整"},
		{"publish", "发布本地目标文件", "已存在"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			a := testApp(t)
			remoteRoot, localRoot := t.TempDir(), t.TempDir()
			const content = "verified bytes"
			for _, dir := range []string{"Series", "Series/Season 1"} {
				if err := os.Mkdir(filepath.Join(remoteRoot, dir), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(remoteRoot, "Series", "Season 1", "film.mkv"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			var reads, writes atomic.Int32
			handler := &webdav.Handler{FileSystem: webdav.Dir(remoteRoot), LockSystem: webdav.NewMemLS()}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" && r.Method != "HEAD" && r.Method != "PROPFIND" {
					writes.Add(1)
				}
				if r.Method == "GET" {
					reads.Add(1)
					switch scenario.name {
					case "download":
						http.Error(w, "https://private.invalid/?token=secret Cookie=secret", 403)
						return
					case "incomplete":
						_, _ = io.WriteString(w, "short")
						return
					case "publish":
						if err := os.WriteFile(filepath.Join(localRoot, "Series", "Season 1", "film.mkv"), []byte("keep existing"), 0600); err != nil {
							t.Error(err)
						}
					}
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			source := Storage{ID: "source", Type: "webdav", Name: "远程来源", Enabled: true, Config: map[string]string{"address": server.URL, "root": "/"}}
			target := Storage{ID: "local", Type: "local", Name: "家庭影音", Enabled: true, Config: map[string]string{"root": localRoot}}
			temp := t.TempDir()
			if scenario.name == "temporary" {
				temp = filepath.Join(temp, "not-a-directory")
				if err := os.WriteFile(temp, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.store.update(func(st *State) error {
				st.Storages = []Storage{source, target}
				st.TransferSettings = normalizedTransferSettings(st.TransferSettings)
				st.TransferSettings.UploadDelay = 0
				st.TransferSettings.TempDirectory = temp
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			a.transfers.file = filepath.Join(t.TempDir(), "failed.json")
			done, err := a.crossStorageFileAction(a.ctx, source, target, fileActionRequest{Action: "move", Source: "/", Target: "/", IDs: []string{"/Series"}})
			for _, want := range []string{scenario.phase, scenario.reason, "Series/Season 1/film.mkv"} {
				if done != 0 || err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("missing detailed diagnosis %q: done=%d err=%v", want, done, err)
				}
			}
			if writes.Load() != 0 || reads.Load() > 1 || scenario.name == "temporary" && reads.Load() != 0 {
				t.Fatal("unexpected upstream write or retry", writes.Load(), reads.Load())
			}
			if _, err := os.Stat(filepath.Join(remoteRoot, "Series", "Season 1", "film.mkv")); err != nil {
				t.Fatal("source was removed after failure", err)
			}
			targetContent, targetErr := os.ReadFile(filepath.Join(localRoot, "Series", "Season 1", "film.mkv"))
			if scenario.name == "publish" {
				if targetErr != nil || string(targetContent) != "keep existing" {
					t.Fatal("target conflict overwritten", targetErr)
				}
			} else if !os.IsNotExist(targetErr) {
				t.Fatal("unexpected target publication", targetErr)
			}
			last := transferEvents(a)
			message := last[len(last)-1].Message
			processed := 0
			if scenario.name == "incomplete" {
				processed = len("short")
			} else if scenario.name == "publish" {
				processed = len(content)
			}
			for _, want := range []string{scenario.phase, "Series/Season 1/film.mkv", "家庭影音", fmt.Sprintf("%d/%d 字节", processed, len(content))} {
				if !strings.Contains(message, want) {
					t.Errorf("persistent log missing %q: %s", want, message)
				}
			}
			for _, event := range last {
				for _, private := range []string{"private.invalid", "secret", temp, localRoot} {
					if strings.Contains(event.Message, private) {
						t.Fatal("sensitive error detail logged", event)
					}
				}
			}
			var restored transferLog
			restored.restore(a.transfers.file)
			found := false
			for _, entry := range restored.items {
				if entry.Kind == "copy" {
					found = true
					if entry.Message != err.Error() {
						t.Fatal("failure details lost on restart", entry)
					}
				}
			}
			if !found {
				t.Fatal("aggregate failure missing after restart")
			}
		})
	}
}

func TestCrossStorageMobileToLocalDiagnostics(t *testing.T) {
	old := apiClient.Transport
	t.Cleanup(func() { apiClient.Transport = old })
	for _, scenario := range []struct{ name, phase, reason string }{
		{"list", "读取来源目录", "HTTP 503"},
		{"link", "获取来源下载链接", "HTTP 429"},
		{"body", "下载来源文件到本机暂存", "HTTP 403"},
		{"cancel", "下载来源文件到本机暂存", "已取消或连接中断"},
		{"success", "", ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			a := testApp(t)
			const content = "mobile bytes"
			var links, reads atomic.Int32
			apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
				status, body := 200, ""
				switch r.URL.Path {
				case "/user/route/qryRoutePolicy":
					body = `{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"https://personal.yun.139.com"}]}}`
				case "/file/list":
					body = fmt.Sprintf(`{"success":true,"data":{"items":[{"fileId":"opaque-file","name":"film.mkv","size":%d}]}}`, len(content))
					if scenario.name == "list" {
						status, body = 503, "private upstream response"
					}
				case "/file/getDownloadUrl":
					links.Add(1)
					body = `{"success":true,"data":{"url":"https://download.test/film?sign=private-secret"}}`
					if scenario.name == "link" {
						status, body = 429, "private upstream response"
					}
				case "/film":
					reads.Add(1)
					body = content
					if scenario.name == "body" {
						status, body = 403, "private upstream response"
					} else if scenario.name == "cancel" {
						return nil, fmt.Errorf("private-secret: %w", context.Canceled)
					}
				default:
					t.Error("unexpected request", r.Method, r.URL.Path)
					status = 500
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			source := Storage{ID: "mobile", Type: "mobile", Name: "移动云盘", Enabled: true, Config: map[string]string{"mode": "native", "authorization": "cGM6YWNjb3VudDp0b2tlbg=="}}
			target := Storage{ID: "local", Type: "local", Name: "家庭影音", Enabled: true, Config: map[string]string{"root": t.TempDir()}}
			if err := a.store.update(func(st *State) error {
				st.Storages = []Storage{source, target}
				st.TransferSettings = normalizedTransferSettings(st.TransferSettings)
				st.TransferSettings.UploadDelay = 0
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			done, err := a.crossStorageFileAction(a.ctx, source, target, fileActionRequest{Action: "copy", Source: "/", Target: "/", IDs: []string{"opaque-file"}})
			if scenario.name == "success" {
				body, readErr := os.ReadFile(filepath.Join(target.Config["root"], "film.mkv"))
				if err != nil || readErr != nil || string(body) != content || done != 1 || reads.Load() != 1 {
					t.Fatal("mobile-to-local proxy copy failed", done, err, readErr, reads.Load())
				}
			} else {
				if err == nil || done != 0 || !strings.Contains(err.Error(), scenario.phase) || !strings.Contains(err.Error(), scenario.reason) {
					t.Fatal("missing mobile failure diagnosis", done, err)
				}
				if scenario.name != "list" && !strings.Contains(err.Error(), "film.mkv") {
					t.Fatal("missing selected filename", err)
				}
				if _, statErr := os.Stat(filepath.Join(target.Config["root"], "film.mkv")); !os.IsNotExist(statErr) {
					t.Fatal("failed mobile copy published target", statErr)
				}
				if scenario.name == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal("nested network cancellation lost", err)
				}
			}
			if links.Load() > 1 || reads.Load() > 1 {
				t.Fatal("unexpected retry", links.Load(), reads.Load())
			}
			for _, event := range transferEvents(a) {
				if strings.Contains(event.Message, "private") || strings.Contains(event.Message, "download.test") || strings.Contains(event.Message, source.Config["authorization"]) {
					t.Fatal("private upstream details persisted", event)
				}
			}
			if err != nil {
				events := transferEvents(a)
				last := events[len(events)-1]
				for _, want := range []string{scenario.phase, scenario.reason, "移动云盘", "家庭影音"} {
					if !strings.Contains(last.Message, want) {
						t.Error("transfer log lost mobile context", want, last)
					}
				}
				if scenario.name == "cancel" && last.Level != "warn" {
					t.Fatal("cancellation incorrectly classified", last)
				}
			}
		})
	}
}

func TestCrossStorageCopyReportsBytesBeforeFileCompletes(t *testing.T) {
	a := testApp(t)
	sourceRoot, targetRoot := t.TempDir(), t.TempDir()
	content := strings.Repeat("x", 256<<10)
	if err := os.WriteFile(filepath.Join(sourceRoot, "film.mkv"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	gate, sent := make(chan struct{}), make(chan struct{})
	handler := &webdav.Handler{FileSystem: webdav.Dir(sourceRoot), LockSystem: webdav.NewMemLS()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			handler.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(content)))
		_, _ = io.WriteString(w, content[:len(content)/2])
		w.(http.Flusher).Flush()
		close(sent)
		select {
		case <-gate:
			_, _ = io.WriteString(w, content[len(content)/2:])
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	source := Storage{ID: "dav", Type: "webdav", Enabled: true, Config: map[string]string{"address": server.URL, "root": "/"}}
	target := Storage{ID: "local", Type: "local", Enabled: true, Config: map[string]string{"root": targetRoot}}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{source, target}
		st.TransferSettings = normalizedTransferSettings(st.TransferSettings)
		st.TransferSettings.UploadDelay = 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := a.crossStorageFileAction(ctx, source, target, fileActionRequest{Action: "copy", Source: "/", Target: "/", IDs: []string{"/film.mkv"}})
		result <- err
	}()
	defer func() { cancel(); close(gate); <-result }()
	select {
	case <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("did not begin body read")
	}
	deadline, progressed := time.Now().Add(5*time.Second), false
	for time.Now().Before(deadline) {
		a.transfers.mu.Lock()
		for _, entry := range a.transfers.items {
			if entry.Kind == "copy" && entry.Status == "running" && entry.Done > 0 && entry.Done < entry.Total {
				progressed = true
			}
		}
		a.transfers.mu.Unlock()
		if progressed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !progressed {
		t.Fatal("copy stayed at zero until the complete file was verified")
	}
}
