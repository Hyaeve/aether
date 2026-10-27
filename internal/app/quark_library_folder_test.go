package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQuarkLibraryFixedFolderVisibilityAndCookie(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		t.Run(fmtBool(rotate), func(t *testing.T) {
			a := testApp(t)
			m := mockQuarkLibrary(t, a)
			card := newQuarkLibraryCard(t, a, "mkv")
			card.MetadataExtensions = "nfo"
			if err := a.store.update(func(st *State) error { st.QuarkLibraryCards[0] = card; return nil }); err != nil {
				t.Fatal(err)
			}
			m.files = append(m.files, shareEntry{ID: "meta", Name: "Movie.nfo", Size: 10, Token: "file-token"})
			s := a.store.snapshot().Storages[0]
			s.Config["root"] = "selected-media-root"
			if err := a.store.update(func(st *State) error { st.Storages[0] = s; return nil }); err != nil {
				t.Fatal(err)
			}
			transport := apiClient.Transport
			creates, reads := map[string]int{}, map[string]int{}
			requiredCookie := ""
			apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
				parent := r.URL.Query().Get("pdir_fid")
				if requiredCookie != "" && !strings.Contains(r.Header.Get("Cookie"), requiredCookie) {
					return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
				}
				if strings.HasSuffix(r.URL.Path, "/sort") && len(m.folders[parent]) > 0 && m.folders[parent][0].IsDir && reads[parent] == 0 {
					reads[parent]++
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"list":[]}}`)), Request: r}, nil
				}
				if r.URL.Path == "/1/clouddrive/file" {
					raw, _ := io.ReadAll(r.Body)
					r.Body = io.NopCloser(strings.NewReader(string(raw)))
					var in struct {
						Parent string `json:"pdir_fid"`
						Name   string `json:"file_name"`
					}
					if err := json.Unmarshal(raw, &in); err != nil {
						t.Fatal(err)
					}
					creates[in.Name]++
					if in.Name == "Aether" && in.Parent != "0" || in.Name == "Share" && in.Parent != "folder-Aether" ||
						strings.HasPrefix(in.Name, "q-") && in.Parent != "folder-Share" {
						t.Fatal("temporary directory escaped fixed cloud root /Aether/Share", in)
					}
				}
				res, err := transport.RoundTrip(r)
				if err == nil && rotate && r.URL.Path == "/1/clouddrive/file" {
					requiredCookie = "__puus=rotated-" + string(rune('a'+len(creates)))
					res.Header.Set("Set-Cookie", requiredCookie+"; Path=/; Secure")
				}
				return res, err
			})
			result, err := a.runQuarkLibrary(context.Background(), card)
			if err != nil || result.Generated != 1 || result.Metadata != 1 || m.saves != 1 {
				t.Fatal("one-time generation must confirm newly created fixed folders", result, err, m.saves)
			}
			if creates["Aether"] != 1 || creates["Share"] != 1 || len(creates) != 3 {
				t.Fatal("folder creation was repeated", creates)
			}
			for _, n := range creates {
				if n != 1 {
					t.Fatal("uncertain creation replayed", creates)
				}
			}
			body, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), "Movie.nfo"))
			if err != nil || string(body) != "0123456789" {
				t.Fatal("metadata not published", err)
			}
			if rotate && !strings.Contains(a.store.snapshot().Storages[0].Config["cookie"], requiredCookie) {
				t.Fatal("trusted folder-session Cookie rotation was lost")
			}
		})
	}
}

func TestQuarkLibraryFolderConfirmationStopsOnConflictAndCancellation(t *testing.T) {
	for _, mode := range []string{"file-conflict", "cancel", "unknown-create", "wrong-id"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			m := mockQuarkLibrary(t, a)
			s := a.store.snapshot().Storages[0]
			if mode == "file-conflict" {
				m.folders["0"] = []File{{ID: "user-file", Name: "Aether", Size: 20}}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			transport, writes := apiClient.Transport, 0
			apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/1/clouddrive/file" {
					writes++
					if mode == "unknown-create" {
						return nil, io.ErrUnexpectedEOF
					}
					if mode == "cancel" {
						cancel()
					}
					if mode == "wrong-id" {
						m.folders["0"] = []File{{ID: "unrelated", Name: "Aether", IsDir: true}}
						return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"fid":"expected"}}`)), Request: r}, nil
					}
				}
				return transport.RoundTrip(r)
			})
			_, err := a.quarkLibraryFolder(ctx, &s, func() error { return ctx.Err() }, "0", "Aether")
			if err == nil || writes > 1 || m.saves != 0 {
				t.Fatal("unconfirmed folder was accepted or write was replayed", err, writes)
			}
			if mode == "file-conflict" && writes != 0 {
				t.Fatal("user file was overwritten")
			}
			if mode != "file-conflict" && writes != 1 {
				t.Fatal("scenario did not reach the single directory creation", writes)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "https://") {
				t.Fatal("private upstream response leaked", err)
			}
		})
	}
}

func TestQuarkLibraryFixedFolderReusesExistingAndNeverGuessesID(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	s := a.store.snapshot().Storages[0]
	m.folders["0"] = []File{{ID: "existing-aether", Name: "Aether", IsDir: true}}
	m.folders["existing-aether"] = []File{{ID: "existing-share", Name: "Share", IsDir: true}}
	transport := apiClient.Transport
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Fatal("existing fixed cloud directory must not be created again", r.Method)
		}
		return transport.RoundTrip(r)
	})
	ctx := context.Background()
	parent, err := a.quarkLibraryFolder(ctx, &s, func() error { return nil }, "0", "Aether")
	if err != nil || parent != "existing-aether" {
		t.Fatal(parent, err)
	}
	folder, err := a.quarkLibraryFolder(ctx, &s, func() error { return nil }, parent, "Share")
	if err != nil || folder != "existing-share" {
		t.Fatal(folder, err)
	}
}

func TestQuarkLibraryFolderConfirmationHasBoundedReadsAndOneWrite(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	s := a.store.snapshot().Storages[0]
	transport := apiClient.Transport
	reads, writes := 0, 0
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/sort") {
			reads++
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"list":[]}}`)), Request: r}, nil
		}
		if r.URL.Path == "/1/clouddrive/file" {
			writes++
		}
		return transport.RoundTrip(r)
	})
	_, err := a.quarkLibraryFolder(context.Background(), &s, func() error { return nil }, "0", "Aether")
	if err == nil || writes != 1 || reads != 8 || !strings.Contains(err.Error(), "/Aether/Share") || !strings.Contains(err.Error(), s.Name) {
		t.Fatal("unconfirmed directory must stop after bounded reads and one create", err, reads, writes)
	}
}
