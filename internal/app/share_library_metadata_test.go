package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestShareLibraryCronSavePreservesOutputsAndRuntimeWithoutCloud(t *testing.T) {
	for _, provider := range []string{"quark", "mobile", "cas"} {
		t.Run(provider, func(t *testing.T) {
			a := testApp(t)
			root := t.TempDir()
			old := quarkLibraryCard{ID: "saved-share", Secret: "https://pan.quark.cn/s/abcdef", Name: "分享别名", ShareName: "原名称", Cron: "0 0 * * *", Directory: root, DirectoryName: "Share", MediaExtensions: "mkv", Enabled: true, CascadeCleanup: "on"}
			if provider != "quark" {
				old.Secret = mobileLibrarySecret("abcdef", "")
				old.CascadeCleanup = "off"
			} else {
				old.CascadeCleanup = ""
			}
			before := quarkLibraryResult{Status: "completed", Message: "原始结果", Generated: 7, Skipped: 3, LastRun: time.Now().Add(-time.Hour), InvalidSince: time.Now().Add(-24 * time.Hour)}
			a.store.update(func(st *State) error {
				switch provider {
				case "quark":
					st.QuarkLibraryCards = []quarkLibraryCard{old}
					initQuarkLibraryRuntime(st)
					st.QuarkLibraryRuntime.Results[old.ID] = before
				case "mobile":
					st.MobileLibraryCards = []quarkLibraryCard{old}
					initMobileLibrary(st)
					st.MobileLibraryRuntime.Results[old.ID] = before
				case "cas":
					st.CASLibraryCards = []quarkLibraryCard{old}
					initCASLibrary(st)
					st.CASLibraryRuntime.Results[old.ID] = before
				}
				return nil
			})
			file := filepath.Join(root, "Share", "keep.share.strm")
			writeTest(t, file, "preserve existing output")
			info, _ := os.Stat(file)
			var calls atomic.Int32
			prior := apiClient.Transport
			apiClient.Transport = casTransport(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return nil, errors.New("must not contact cloud for metadata edits")
			})
			defer func() { apiClient.Transport = prior }()
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			raw, _ := json.Marshal(old)
			var body map[string]any
			json.Unmarshal(raw, &body)
			delete(body, "secret")
			delete(body, "shareName")
			delete(body, "scheduleAnchor")
			body["url"] = old.Secret
			body["cron"] = "0 12 * * *"
			body["name"] = "新别名"
			if provider == "quark" {
				body["cascadeCleanup"] = "on"
			}
			w := request(t, h, "PUT", "/api/virtual-library/"+provider+"/"+old.ID, body, cookie)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			st := a.store.snapshot()
			var got quarkLibraryResult
			var card quarkLibraryCard
			switch provider {
			case "quark":
				got = st.QuarkLibraryRuntime.Results[old.ID]
				card = st.QuarkLibraryCards[0]
			case "mobile":
				got = st.MobileLibraryRuntime.Results[old.ID]
				card = st.MobileLibraryCards[0]
			case "cas":
				got = st.CASLibraryRuntime.Results[old.ID]
				card = st.CASLibraryCards[0]
			}
			if calls.Load() != 0 || got.Generated != 7 || got.Skipped != 3 || got.Status != before.Status || got.Message != before.Message || !got.LastRun.Equal(before.LastRun) || !got.InvalidSince.Equal(before.InvalidSince) || got.NextRun.IsZero() {
				t.Fatal("metadata save changed results or made cloud requests", got, calls.Load())
			}
			wantCascade := old.CascadeCleanup
			if provider == "quark" {
				wantCascade = "on"
			}
			if card.Cron != "0 12 * * *" || card.Name != "新别名" || card.ShareName != old.ShareName || card.Secret != old.Secret || card.CascadeCleanup != wantCascade {
				t.Fatal(card)
			}
			after, _ := os.Stat(file)
			content, _ := os.ReadFile(file)
			if !after.ModTime().Equal(info.ModTime()) || string(content) != "preserve existing output" {
				t.Fatal("cron-only save touched output")
			}
			body["cron"] = "invalid"
			if w := request(t, h, "PUT", "/api/virtual-library/"+provider+"/"+old.ID, body, cookie); w.Code != 400 {
				t.Fatal("invalid Cron accepted", w.Code)
			}
		})
	}
}

func TestShareLibraryMetadataGateRejectsSourceAndOutputChanges(t *testing.T) {
	a := testApp(t)
	old := quarkLibraryCard{ID: "a", Secret: "https://pan.quark.cn/s/abcde", Name: "显示名", Directory: filepath.Join(t.TempDir(), "out"), DirectoryName: "Title", Enabled: true}
	for _, field := range []string{"url", "directory", "storageId"} {
		changed := old
		switch field {
		case "url":
			changed.Secret = "https://pan.quark.cn/s/other"
		case "directory":
			changed.Directory += "2"
		case "storageId":
			changed.StorageID = "other"
		}
		if handled, err := a.saveShareLibraryMetadata(a.ctx, "quark", old, changed); handled || err != nil {
			t.Fatal("source/output change bypassed generation path", field, handled, err)
		}
	}
}
