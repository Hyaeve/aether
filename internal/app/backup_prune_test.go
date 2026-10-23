package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupNameBlacklistPrunesBeforeListingAndHashing(t *testing.T) {
	for _, sync := range []bool{false, true} {
		t.Run(fmt.Sprint(sync), func(t *testing.T) {
			a, rule, _, target := backupFixture(t)
			rule.Filters = []BackupFilter{{Type: "name", Mode: "exclude", Value: "CACHE;secret*", MatchDir: true, MatchFile: true}}
			s := Storage{ID: "source", Name: "Remote", Type: "openlist", Enabled: true, Config: map[string]string{"address": "https://prune.invalid", "token": "test"}}
			if err := a.store.update(func(st *State) error { st.Storages[0] = s; return nil }); err != nil {
				t.Fatal(err)
			}
			original := apiClient
			t.Cleanup(func() { apiClient = original })
			calls := []string{}
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				var body struct {
					Path string `json:"path"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				calls = append(calls, r.URL.Path+":"+body.Path)
				if r.URL.Path != "/api/fs/list" || body.Path != "/" {
					return nil, fmt.Errorf("blacklisted branch or file accessed: %s", calls[len(calls)-1])
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(`{"code":200,"data":{"content":[{"name":"Cache","is_dir":true},{"name":"secret.bin","size":1}],"total":2}}`))}, nil
			})}
			if sync {
				visited := 0
				rep, err := a.scanSyncReplica(context.Background(), BackupLocation{"source", "/", ""}, s, rule, map[string]bool{}, &visited)
				if err != nil || len(rep.files) != 0 || len(rep.dirs) != 0 {
					t.Fatal("blacklist not pruned", rep, err)
				}
			} else {
				if err := a.executeBackup(context.Background(), &rule); err != nil {
					t.Fatal("excluded directories must not be listed", err)
				}
				if rule.Scanned != 0 || rule.Copied != 0 {
					t.Fatal("blacklisted files counted as scanned/copied", rule)
				}
			}
			if len(calls) != 1 {
				t.Fatal("unexpected requests", calls)
			}
			if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
				t.Fatal("blacklisted content published", entries, err)
			}
		})
	}
}

func TestBackupNameBlacklistDoesNotTriggerMonitor(t *testing.T) {
	a, rule, source, _ := backupFixture(t)
	rule.Filters = []BackupFilter{{Type: "name", Mode: "exclude", Value: "Cache;secret*", MatchDir: true, MatchFile: true}}
	if err := os.MkdirAll(filepath.Join(source, "Cache", "deep"), 0700); err != nil {
		t.Fatal(err)
	}
	before, err := backupFingerprint(context.Background(), rule, a.store.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Cache/deep/new.bin", "secret.bin"} {
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), []byte("excluded"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	after, err := backupFingerprint(context.Background(), rule, a.store.snapshot())
	if err != nil || before != after {
		t.Fatal("excluded changes triggered monitor", err)
	}
	if err := os.WriteFile(filepath.Join(source, "visible.txt"), []byte("visible"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := backupFingerprint(context.Background(), rule, a.store.snapshot())
	if err != nil || changed == after {
		t.Fatal("allowed change did not trigger monitor", err)
	}
}

func TestBackupNameBlacklistScopeAndCase(t *testing.T) {
	for _, tc := range []struct {
		dir, file, sensitive, isDir, want bool
		name                              string
	}{
		{true, false, false, true, true, "CACHE"},
		{true, false, false, false, false, "cache"},
		{false, true, false, true, false, "cache"},
		{false, true, false, false, true, "CACHE"},
		{true, true, true, true, false, "CACHE"},
		{true, true, true, true, true, "cache"},
	} {
		filters, err := compileBackupFilters([]BackupFilter{{Type: "name", Mode: "exclude", Value: "cache", MatchDir: tc.dir, MatchFile: tc.file, CaseSensitive: tc.sensitive}})
		if err != nil {
			t.Fatal(err)
		}
		if got := backupNameExcluded(filters, File{Name: tc.name, IsDir: tc.isDir}, "parent/"+tc.name); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}

func TestBackupPruningKeepsOverlapProtection(t *testing.T) {
	remote := Storage{Type: "openlist"}
	for _, peer := range []string{"/Cache", "/Cache/deep", "/"} {
		if backupPrunedOverlap(remote, "/Cache", peer) == nil {
			t.Fatal("overlap accepted", peer)
		}
	}
	if err := backupPrunedOverlap(remote, "/Cache", "/Cache-other"); err != nil {
		t.Fatal("sibling rejected", err)
	}
	if backupPrunedOverlap(Storage{Type: "115"}, "opaque-excluded", "opaque-peer") == nil {
		t.Fatal("unknown opaque relationship accepted")
	}
	if err := backupPrunedOverlap(Storage{Type: "local"}, "/Cache", "/Target"); err != nil {
		t.Fatal("validated local roots rejected", err)
	}
	if got := backupScopePath(Storage{Type: "openlist", Config: map[string]string{"root": "/account/media"}}, "/Cache"); got != "/account/media/Cache" {
		t.Fatal(got)
	}
}
