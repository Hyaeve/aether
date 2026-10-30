package app

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

func Test115ExtensionRenameDoesNotUseNativeRename(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "s", Type: "115", Name: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	native := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"state":false,"errno":500}`
		switch r.URL.Path {
		case "/files":
			body = fmt.Sprintf(`{"state":true,"cid":"0","count":1,"offset":0,"data":[{"fid":"123","cid":"0","n":"movie.mkv","s":5,"sha":%q,"pc":"sourcepick"}]}`, strings.Repeat("a", 40))
		case "/files/batch_rename":
			native++
			body = `{"state":true}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	err := a.cloudFileAction(context.Background(), s, s, fileActionRequest{Source: "0", Action: "rename", IDs: []string{"123"}, Name: "movie.mp4"})
	if err == nil || native != 0 {
		raw, _ := json.Marshal(map[string]any{"nativeRenames": native, "error": fmt.Sprint(err)})
		t.Fatalf("extension change must use verified instant upload, not native rename: %s", raw)
	}
}

type rename115Fixture struct {
	a       *App
	storage Storage
	files   map[string]map[string]any
	reads   int
	deletes int
	mode    string
}

func newRename115Fixture(t *testing.T, mode string) *rename115Fixture {
	t.Helper()
	f := &rename115Fixture{a: testApp(t), mode: mode}
	f.storage = Storage{ID: "115", Type: "115", Name: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	f.files = map[string]map[string]any{"123": f.file("123", "movie.mkv")}
	if mode == "conflict" {
		f.files["777"] = f.file("777", "movie.mp4")
	}
	stored := f.storage
	stored.Config = map[string]string{"cookie": test115Cookie}
	if err := f.a.store.update(func(st *State) error { st.Storages = []Storage{stored}; return nil }); err != nil {
		t.Fatal(err)
	}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		var body any
		switch r.URL.Path {
		case "/files":
			f.reads++
			if r.URL.Query().Get("cid") != "0" {
				t.Fatal("wrong parent directory")
			}
			files := []any{}
			for _, value := range f.files {
				files = append(files, value)
			}
			body = map[string]any{"state": true, "cid": "0", "count": len(files), "offset": 0, "data": files}
		case "/files/get_info":
			fid := r.URL.Query().Get("file_id")
			if f.mode == "changed-source" && fid == "123" && f.files["456"] != nil {
				f.files[fid]["n"] = "someone-renamed.mkv"
			}
			if f.mode == "changed-copy" && fid == "456" {
				f.files[fid]["sha"] = strings.Repeat("b", 40)
			}
			files := []any{}
			if file := f.files[fid]; file != nil {
				files = append(files, file)
			}
			body = map[string]any{"state": true, "data": files}
		case "/rb/delete":
			f.deletes++
			_ = r.ParseForm()
			if r.Form.Get("fid[0]") != "123" || len(r.PostForm) != 1 || f.files["456"] == nil {
				t.Fatal("recycled wrong or unconfirmed file")
			}
			if mode == "recycle-unknown" {
				return nil, errors.New("private-signed-url-and-cookie")
			}
			if mode != "recycle-no-op" {
				delete(f.files, "123")
			}
			body = map[string]any{"state": true}
		default:
			t.Fatalf("unexpected upstream request: %s", r.URL.Path)
		}
		raw, _ := json.Marshal(body)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})}
	return f
}

func (f *rename115Fixture) file(id, name string) map[string]any {
	return map[string]any{"fid": id, "cid": "0", "n": name, "s": "42", "sha": strings.Repeat("a", 40), "pc": "pick" + id}
}

func (f *rename115Fixture) run(ctx context.Context, calls *int) (File, error) {
	c, _ := client115(ctx, f.storage)
	return f.a.rename115Recreate(ctx, f.storage, "0", File{ID: "123", Name: "movie.mkv", Size: 42, PickCode: "pick123"}, "movie.mp4", c, func(original *driver.File) (string, bool, error) {
		*calls++
		if original.FileID != "123" || original.Name != "movie.mkv" || original.Size != 42 || original.ParentID != "0" || len(f.a.store.snapshot().Rename115Attempts) != 1 {
			return "", true, errors.New("wrong upload metadata or missing durable intent")
		}
		if f.mode == "uncertain" {
			return "", false, errors.New("115秒传提交未确认")
		}
		if f.mode == "no-match" {
			return "", true, errors.New("115未命中秒传")
		}
		f.files["456"] = f.file("456", "movie.mp4")
		if f.mode == "hash" {
			f.files["456"]["sha"] = strings.Repeat("b", 40)
		}
		if f.mode == "duplicate" {
			f.files["789"] = f.file("789", "movie.mp4")
		}
		if f.mode == "config-change" {
			_ = f.a.store.update(func(st *State) error { st.Storages[0].Config["cookie"] = "changed"; return nil })
		}
		if f.mode == "missing-pick" {
			return "", false, nil
		}
		return "pick456", false, nil
	})
}

func Test115ExtensionRenameVerifiedRecreationAndSafety(t *testing.T) {
	for _, mode := range []string{"success", "conflict", "hash", "duplicate", "changed-source", "changed-copy", "config-change", "uncertain", "no-match", "missing-pick", "recycle-unknown", "recycle-no-op"} {
		t.Run(mode, func(t *testing.T) {
			f := newRename115Fixture(t, mode)
			calls := 0
			out, err := f.run(f.a.ctx, &calls)
			if mode == "success" {
				if err != nil || out.ID != "456" || out.Name != "movie.mp4" || out.PickCode != "pick456" || f.deletes != 1 || f.files["123"] != nil || len(f.a.store.snapshot().Rename115Attempts) != 0 {
					t.Fatalf("rename did not finish safely: %+v %v deletes=%d", out, err, f.deletes)
				}
				result := f.a.fileActionResult(f.storage, fileActionRequest{Action: "rename", IDs: []string{"123"}, Name: out.Name, renamed: &out}, 1)["renamed"].(map[string]string)
				if result["id"] != "456" || result["pickCode"] != "pick456" {
					t.Fatal(result)
				}
				address, _ := url.Parse(result["url"])
				raw, decodeErr := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(address.Path, "/stream/"))
				var claim streamClaim
				if decodeErr != nil || json.Unmarshal(raw, &claim) != nil || claim.File != "456" || claim.Pick != "pick456" || address.Query().Get("sign") == "" {
					t.Fatal("new signed playback address lost the replacement identity", result)
				}
				return
			}
			if err == nil || f.files["123"] == nil {
				t.Fatalf("failure discarded original: %v %+v", err, f.files)
			}
			expectedDeletes := 0
			if mode == "recycle-unknown" || mode == "recycle-no-op" {
				expectedDeletes = 1
			}
			if f.deletes != expectedDeletes {
				t.Fatal("unexpected destructive write", mode, f.deletes)
			}
			if mode == "conflict" && calls != 0 {
				t.Fatal("submitted into an occupied name")
			}
			if mode == "uncertain" || mode == "recycle-unknown" || mode == "recycle-no-op" {
				initial := calls
				_, again := f.run(f.a.ctx, &calls)
				if again == nil || calls != initial || f.deletes != expectedDeletes {
					t.Fatal("unknown write replayed", mode, again)
				}
				reopened, openErr := newWithDirectories(f.a.ctx, f.a.store.dir, f.a.dataDir, f.a.outputDir)
				if openErr != nil || len(reopened.store.snapshot().Rename115Attempts) != 1 {
					t.Fatal("unknown write lost across restart", openErr)
				}
				if mode == "uncertain" {
					// The same account rotating its login CK must not bypass the receipt.
					f.storage.Config["cookie"] = test115Cookie + ";KID=rotated"
					stored := f.storage
					stored.Config = map[string]string{"cookie": f.storage.Config["cookie"]}
					_ = f.a.store.update(func(st *State) error { st.Storages = []Storage{stored}; return nil })
					_, rotated := f.run(f.a.ctx, &calls)
					if rotated == nil || calls != initial {
						t.Fatal("cookie rotation replayed an unknown write", rotated)
					}
				}
				for _, log := range f.a.store.snapshot().Logs {
					if strings.Contains(log.Message, "private-signed") {
						t.Fatal("upstream secret leaked to log")
					}
				}
			}
			if mode == "no-match" && len(f.a.store.snapshot().Rename115Attempts) != 0 {
				t.Fatal("known no-write result blocked a future explicit attempt")
			}
		})
	}
}

func Test115RenameRangeReadsOnlyBoundedChallenge(t *testing.T) {
	for _, mode := range []string{"ok", "range-ignored", "range-mismatch", "truncated", "forbidden", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			content := strings.Repeat("abcd", 100)
			const start, end = 23, 67
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Header.Get("Range") != "bytes=23-67" || r.Method != "GET" {
					t.Error("missing exact range", r.Header)
				}
				if mode == "range-ignored" {
					w.Write([]byte(content))
					return
				}
				if mode == "forbidden" {
					w.WriteHeader(403)
					return
				}
				rangeHeader := fmt.Sprintf("bytes %d-%d/%d", start, end, len(content))
				if mode == "range-mismatch" {
					rangeHeader = "bytes 0-44/400"
				}
				w.Header().Set("Content-Range", rangeHeader)
				w.WriteHeader(http.StatusPartialContent)
				value := content[start : end+1]
				if mode == "truncated" {
					value = value[:10]
				}
				w.Write([]byte(value))
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			hash, err := read115RenameRange(ctx, Download{URL: server.URL, Headers: http.Header{}}, int64(len(content)), start, end)
			if mode == "ok" {
				sum := sha1.Sum([]byte(content[start : end+1]))
				if err != nil || hash != strings.ToUpper(hex.EncodeToString(sum[:])) || requests != 1 {
					t.Fatal("wrong bounded SHA1", hash, err, requests)
				}
			} else if err == nil || hash != "" {
				t.Fatal("unsafe range accepted", mode, hash)
			}
		})
	}
	for _, spec := range []string{"0-1048576", "-1-2", "1-0", "0-9999999999", "invalid", "0-1-2", "99999999999999999999-99999999999999999999"} {
		a := testApp(t)
		_, err := a.rename115RangeSHA1(a.ctx, Storage{}, &driver.File{Size: 1 << 30}, spec)
		if err == nil {
			t.Fatal("unsafe challenge accepted", spec)
		}
	}
}

func Test115RenameSubmitUsesPinnedProtocolAndNeverFallsBackToUpload(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "115", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	_ = a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	initCalls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme+"://"+r.URL.Host+r.URL.Path != driver.ApiUploadInit && r.URL.Path != "/app/uploadinfo" {
			t.Fatalf("unexpected request or upload fallback: %s", r.URL.Path)
		}
		code, body := 200, `{"state":true,"user_id":123,"userkey":"private-upload-key","upload_allowed":true,"size_limit":100000000000}`
		if r.URL.String() != "https://proapi.115.com/app/uploadinfo" && r.URL.Path == "/4.0/initupload.php" {
			initCalls++
			code, body = 503, "private-cookie-and-response"
			if r.Method != http.MethodPost || r.URL.Query().Get("k_ec") == "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Fatal("native encrypted upload protocol missing")
			}
			raw, _ := io.ReadAll(r.Body)
			if len(raw) == 0 || strings.Contains(string(raw), "movie.mp4") {
				t.Fatal("unencrypted upload payload")
			}
		}
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	c, _ := client115(a.ctx, s)
	_, noWrite, err := a.submit115Rename(a.ctx, s, c, &driver.File{FileID: "123", ParentID: "0", Name: "movie.mkv", Size: 42, Sha1: strings.Repeat("a", 40)}, "movie.mp4")
	if err == nil || noWrite || initCalls != 1 || strings.Contains(err.Error(), "private") {
		t.Fatal("upload uncertainty replayed or leaked secrets", noWrite, initCalls, err)
	}
}

func Test115RenameUploadChallengeHandshakeIsBoundedAndFailClosed(t *testing.T) {
	for _, mode := range []string{"direct", "challenge", "no-match", "repeat-challenge", "invalid-key", "unknown", "nil", "read-error", "network"} {
		t.Run(mode, func(t *testing.T) {
			submits, reads := 0, 0
			pick, noWrite, err := complete115RenameUpload(func(key, value string) (*driver.UploadInitResp, bool, error) {
				submits++
				if submits == 1 && (key != "" || value != "") {
					t.Fatal("initial submit contained a stale challenge")
				}
				if submits == 2 && (key != "challenge-key" || value != strings.Repeat("A", 40)) {
					t.Fatal("challenge fields not passed to the second and final request")
				}
				result := &driver.UploadInitResp{Status: 2, PickCode: "newpick"}
				switch mode {
				case "challenge", "read-error", "repeat-challenge", "invalid-key":
					if submits == 1 || mode == "repeat-challenge" {
						result = &driver.UploadInitResp{Status: 7, SignKey: "challenge-key", SignCheck: "23-67"}
						if mode == "invalid-key" {
							result.SignKey = ""
						}
					}
				case "no-match":
					result.Status = 1
				case "unknown":
					result.Status = 8
				case "nil":
					return nil, false, nil
				case "network":
					return nil, false, errors.New("request uncertain")
				}
				return result, false, nil
			}, func(spec string) (string, error) {
				reads++
				if spec != "23-67" {
					t.Fatal("wrong challenge range")
				}
				if mode == "read-error" {
					return "", io.ErrUnexpectedEOF
				}
				return strings.Repeat("A", 40), nil
			})
			if mode == "direct" || mode == "challenge" {
				if err != nil || pick != "newpick" || noWrite {
					t.Fatal(pick, noWrite, err)
				}
			} else {
				expectedNoWrite := mode == "no-match" || mode == "repeat-challenge" || mode == "invalid-key" || mode == "read-error"
				if err == nil || pick != "" || noWrite != expectedNoWrite {
					t.Fatal(mode, pick, noWrite, err)
				}
			}
			if submits > 2 || reads > 1 || (mode == "direct" && (reads != 0 || submits != 1)) || mode == "challenge" && (reads != 1 || submits != 2) {
				t.Fatal("unbounded submission or unnecessary media reads", submits, reads)
			}
		})
	}
}

func Test115RenameHistoryTracksReplacementFileAndDropsPreviousIdentity(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "115", Type: "115", Config: map[string]string{}}
	input := renameRequest{StorageID: s.ID, Source: "0"}
	original := makeRenameRecord(s, renameItem{ID: "123", Name: "old.mkv", NewName: "movie.mkv"}, "")
	if err := a.rememberRenames(s, input, []RenameRecord{original}); err != nil {
		t.Fatal(err)
	}
	replacement := makeRenameRecord(s, renameItem{ID: "123", Name: "movie.mkv", NewName: "movie.mp4"}, "")
	replacement.FileID, replacement.previousID = "456", "123"
	if err := a.rememberRenames(s, input, []RenameRecord{replacement}); err != nil {
		t.Fatal(err)
	}
	history := a.store.snapshot().RenameHistory
	if len(history) != 1 || len(history[0].Items) != 1 || history[0].Items[0].FileID != "456" {
		t.Fatal(history)
	}
	for _, file := range []string{filepath.Join(a.store.dir, "state.json"), filepath.Join(a.dataDir, "runtime", "state.json")} {
		raw, err := os.ReadFile(file)
		if err != nil || strings.Contains(string(raw), "previousID") {
			t.Fatal("transient identity leaked or runtime absent", file, err)
		}
	}
}
