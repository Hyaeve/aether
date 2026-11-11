package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

func TestVirtual115FirstPlaybackReconcilesWithoutResubmitting(t *testing.T) {
	for _, mode := range []string{"delayed-file", "lost-submit-response", "submit-502", "empty-ticket", "list-disconnected"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: "115", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
			if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
				t.Fatal(err)
			}
			source := shareEntry{ID: "101", Name: "episode.mkv", Size: 42, SHA1: strings.Repeat("a", 40)}
			copy := &driver.File{FileID: "201", ParentID: "99", Name: source.Name, Size: source.Size, Sha1: source.SHA1, PickCode: "validpickcode"}
			folders, submits, lists, tickets := 0, 0, 0, 0
			ops := virtualPlaybackOps{
				folder: func(context.Context) (string, error) { folders++; return "99", nil },
				submit: func(context.Context, string) error {
					submits++
					if mode == "lost-submit-response" {
						return io.ErrUnexpectedEOF
					}
					if mode == "submit-502" {
						return &shareHTTPError{status: 502, message: "网关未确认响应"}
					}
					return nil
				},
				files: func(_ context.Context, folder string) ([]*driver.File, error) {
					lists++
					if folder != "99" {
						t.Fatal("looked outside the isolated destination", folder)
					}
					if mode == "delayed-file" && lists == 1 {
						return nil, nil
					}
					if mode == "list-disconnected" && lists == 1 {
						return []*driver.File{copy}, io.ErrUnexpectedEOF
					}
					return []*driver.File{copy}, nil
				},
				download: func(_ context.Context, file *driver.File) (Download, error) {
					tickets++
					if file != copy || mode == "list-disconnected" && lists == 1 {
						t.Fatal("ticket requested without a complete verified listing")
					}
					if mode == "empty-ticket" && tickets == 1 {
						return Download{}, &pan115DownloadError{message: "新文件下载记录尚未就绪", temporary: true}
					}
					return Download{URL: "https://cdn.example/episode.mkv"}, nil
				},
				interval: time.Millisecond,
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			download, err := a.virtualShareDownload(ctx, s, normalizedED2KPool(ED2KPoolConfig{}), "first-play", source, ops)
			if err != nil || download.URL != "https://cdn.example/episode.mkv" || submits != 1 || folders != 1 {
				t.Fatal("first request did not finish preparation", download, submits, folders, err)
			}
			wantTickets := 1
			if mode == "empty-ticket" {
				wantTickets = 2
			}
			if tickets != wantTickets || mode != "lost-submit-response" && mode != "submit-502" && lists != 2 {
				t.Fatal("unexpected read attempts", lists, tickets)
			}
			if _, err = a.virtualShareDownload(ctx, s, normalizedED2KPool(ED2KPoolConfig{}), "first-play", source, ops); err != nil || folders != 1 || submits != 1 {
				t.Fatal("warm request repeated a write", folders, submits, err)
			}
			state := a.store.snapshotWithLogLimit(0)
			if len(state.ED2KTemporary) != 1 || state.ED2KTemporary[0].FileID != "201" || state.ED2KTemporary[0].SHA1 != source.SHA1 {
				t.Fatal("verified copy was not retained", state.ED2KTemporary)
			}
		})
	}
}

func TestVirtual115FirstPlaybackRecoveryLimits(t *testing.T) {
	for _, mode := range []string{"submit-rejected", "submit-forbidden", "submit-auth", "ticket-rejected", "ticket-auth", "ticket-transient-limit", "list-transient-limit", "identity-changed", "credentials-changed", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: "115", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
			if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
				t.Fatal(err)
			}
			source := shareEntry{ID: "101", Name: "episode.mkv", Size: 42, SHA1: strings.Repeat("a", 40)}
			copy := &driver.File{FileID: "201", ParentID: "99", Name: source.Name, Size: source.Size, Sha1: source.SHA1, PickCode: "validpickcode"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			submits, lists, tickets := 0, 0, 0
			ops := virtualPlaybackOps{
				folder: func(context.Context) (string, error) { return "99", nil },
				submit: func(context.Context, string) error {
					submits++
					switch mode {
					case "submit-rejected":
						return &shareRejectedError{message: "115 拒绝分享请求"}
					case "submit-forbidden":
						return &shareHTTPError{status: http.StatusForbidden, message: "115 分享转存被拒绝"}
					case "submit-auth":
						return authStorageError(errors.New("115 CK已失效"), true)
					case "canceled":
						cancel()
						return io.EOF
					}
					return nil
				},
				files: func(context.Context, string) ([]*driver.File, error) {
					lists++
					if mode == "list-transient-limit" {
						return nil, io.ErrUnexpectedEOF
					}
					if mode == "identity-changed" && lists > 1 {
						copy.FileID, copy.Sha1, copy.ParentID = "999", strings.Repeat("b", 40), "other"
						cancel()
					}
					if mode == "credentials-changed" && lists > 1 {
						if err := a.store.update(func(st *State) error { st.Storages[0].Config = map[string]string{"cookie": "changed"}; return nil }); err != nil {
							t.Fatal(err)
						}
					}
					return []*driver.File{copy}, nil
				},
				download: func(context.Context, *driver.File) (Download, error) {
					tickets++
					if mode == "ticket-auth" {
						return Download{}, authStorageError(errors.New("115 CK已失效"), true)
					}
					return Download{}, &pan115DownloadError{message: "取链失败", temporary: mode != "ticket-rejected"}
				},
				interval: time.Millisecond,
			}
			_, err := a.virtualShareDownload(ctx, s, normalizedED2KPool(ED2KPoolConfig{}), "limits", source, ops)
			if err == nil || submits != 1 || len(a.store.snapshotWithLogLimit(0).ED2KTemporary) != 0 {
				t.Fatal("unsafe recovery accepted", submits, err)
			}
			wantLists, wantTickets := 1, 1
			switch mode {
			case "submit-rejected", "submit-forbidden", "submit-auth", "canceled":
				wantLists, wantTickets = 0, 0
			case "ticket-transient-limit":
				wantLists, wantTickets = 3, 3
			case "list-transient-limit":
				wantLists, wantTickets = 3, 0
			case "identity-changed", "credentials-changed":
				wantLists = 2
			}
			if lists != wantLists || tickets != wantTickets {
				t.Fatal("recovery exceeded its identity or retry bounds", lists, tickets, err)
			}
			record, readErr := a.readED2KRecord("limits")
			if readErr != nil || !record.Attempted || record.Folder != "99" {
				t.Fatal("write-ahead intent was lost", record, readErr)
			}
		})
	}
}

func Test115PlaybackDownloadRetryClassification(t *testing.T) {
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	for _, test := range []struct {
		name      string
		status    int
		body      string
		retryable bool
	}{
		{"gateway", 503, `{"state":false}`, true},
		{"refused-code", 200, `{"state":false,"errno":10008}`, false},
		{"refused-no-code", 200, `{"state":false}`, false},
		{"bad-response", 200, `not-json`, false},
		{"forbidden", 403, `{"state":false}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: test.status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(test.body)), Request: r}, nil
			})}
			_, err := download115(context.Background(), Storage{Type: "115", Config: map[string]string{"cookie": test115Cookie}}, "validpickcode", "Hills/1.11.0")
			if err == nil || retryable115Read(err) != test.retryable || calls != 1 {
				t.Fatal("ticket classification or driver replay", err, calls)
			}
		})
	}
}
