package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestShareLibraryRenameKeepsDirectoryIdentityAndAllContents(t *testing.T) {
	for _, provider := range []string{"quark", "mobile", "cas", "115"} {
		t.Run(provider, func(t *testing.T) {
			a := testApp(t)
			old := quarkLibraryCard{ID: "rename", Secret: "https://pan.quark.cn/s/abcdef", Name: "Series", ShareName: "Series", Directory: t.TempDir(), DirectoryName: "Old", MediaExtensions: "mkv", CascadeCleanup: "off"}
			if provider == "mobile" || provider == "cas" {
				old.Secret = mobileLibrarySecret("abcdef", "")
			}
			if provider == "115" {
				old.Secret, old.StorageID = "https://115.com/s/abcdef", "pan"
			}
			writeTest(t, filepath.Join(quarkLibraryRoot(old), "Season 1", "movie.mkv.share.strm"), "stream")
			writeTest(t, filepath.Join(quarkLibraryRoot(old), "poster.jpg"), "artwork")
			writeTest(t, filepath.Join(quarkLibraryRoot(old), "custom.txt"), "keep")
			root, err := openStrmReplaceRoot(quarkLibraryRoot(old))
			if err != nil {
				t.Fatal(err)
			}
			identity, err := strmRootIdentity(root)
			root.Close()
			if err != nil {
				t.Fatal(err)
			}
			owner := quarkLibraryOwnership{Directory: quarkLibraryRoot(old), Identity: identity}
			result := quarkLibraryResult{Status: "completed", Message: "unchanged", Generated: 1}
			if err := a.store.update(func(st *State) error {
				switch provider {
				case "quark":
					initQuarkLibraryRuntime(st)
					st.QuarkLibraryCards = []quarkLibraryCard{old}
					st.QuarkLibraryRuntime.Roots[old.ID] = owner
					st.QuarkLibraryRuntime.Results[old.ID] = result
				case "mobile":
					initMobileLibrary(st)
					st.MobileLibraryCards = []quarkLibraryCard{old}
					st.MobileLibraryRuntime.Roots[old.ID] = owner
					st.MobileLibraryRuntime.Results[old.ID] = result
				case "cas":
					initCASLibrary(st)
					st.CASLibraryCards = []quarkLibraryCard{old}
					st.CASLibraryRuntime.Roots[old.ID] = owner
					st.CASLibraryRuntime.Results[old.ID] = result
				case "115":
					initLibrary115(st)
					st.Library115Cards = []quarkLibraryCard{old}
					st.Library115Runtime.Roots[old.ID] = owner
					st.Library115Runtime.Results[old.ID] = result
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			next := old
			next.DirectoryName = "Renamed"
			prior := apiClient.Transport
			apiClient.Transport = casTransport(func(*http.Request) (*http.Response, error) {
				t.Error("rename must not contact cloud")
				return nil, errors.New("unexpected cloud read")
			})
			defer func() { apiClient.Transport = prior }()
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			body := map[string]any{"url": old.Secret, "name": old.Name, "directory": old.Directory, "directoryName": next.DirectoryName, "mediaExtensions": old.MediaExtensions, "cascadeCleanup": old.CascadeCleanup, "enabled": false}
			if provider == "115" {
				body["storageId"] = old.StorageID
			}
			w := request(t, h, "PUT", "/api/virtual-library/"+provider+"/"+old.ID, body, cookie)
			if w.Code != 200 {
				t.Fatal("directory rename did not use local edit path", w.Code, w.Body.String())
			}
			if _, err := os.Stat(quarkLibraryRoot(old)); !os.IsNotExist(err) {
				t.Fatal("old directory remains", err)
			}
			renamed, err := openStrmReplaceRoot(quarkLibraryRoot(next))
			if err != nil {
				t.Fatal(err)
			}
			afterIdentity, err := strmRootIdentity(renamed)
			renamed.Close()
			if err != nil || afterIdentity != identity {
				t.Fatal("directory was copied instead of renamed", err)
			}
			for file, want := range map[string]string{"Season 1/movie.mkv.share.strm": "stream", "poster.jpg": "artwork", "custom.txt": "keep"} {
				got, err := os.ReadFile(filepath.Join(quarkLibraryRoot(next), filepath.FromSlash(file)))
				if err != nil || string(got) != want {
					t.Fatal(file, err, string(got))
				}
			}
			st := a.store.snapshot()
			var gotOwner quarkLibraryOwnership
			var gotResult quarkLibraryResult
			switch provider {
			case "quark":
				gotOwner = st.QuarkLibraryRuntime.Roots[old.ID]
				gotResult = st.QuarkLibraryRuntime.Results[old.ID]
			case "mobile":
				gotOwner = st.MobileLibraryRuntime.Roots[old.ID]
				gotResult = st.MobileLibraryRuntime.Results[old.ID]
			case "cas":
				gotOwner = st.CASLibraryRuntime.Roots[old.ID]
				gotResult = st.CASLibraryRuntime.Results[old.ID]
			case "115":
				gotOwner = st.Library115Runtime.Roots[old.ID]
				gotResult = st.Library115Runtime.Results[old.ID]
			}
			if gotOwner.Directory != quarkLibraryRoot(next) || gotOwner.Identity != identity || !reflect.DeepEqual(gotResult, result) {
				t.Fatal("rename lost ownership or result", gotOwner, gotResult)
			}
			reopened, err := NewStore(a.store.dir)
			if err != nil {
				t.Fatal(err)
			}
			if err := reopened.initRuntime(a.store.runtimeDir); err != nil {
				t.Fatal(err)
			}
			restored := reopened.snapshot()
			var persisted quarkLibraryOwnership
			switch provider {
			case "quark":
				persisted = restored.QuarkLibraryRuntime.Roots[old.ID]
			case "mobile":
				persisted = restored.MobileLibraryRuntime.Roots[old.ID]
			case "cas":
				persisted = restored.CASLibraryRuntime.Roots[old.ID]
			case "115":
				persisted = restored.Library115Runtime.Roots[old.ID]
			}
			if persisted != gotOwner {
				t.Fatal("renamed directory identity did not survive reopen", persisted, gotOwner)
			}
		})
	}
}

func TestShareLibraryRenameFailedPersistenceRestoresOriginalDirectory(t *testing.T) {
	a := testApp(t)
	old := quarkLibraryCard{ID: "rename", Secret: "https://pan.quark.cn/s/abcdef", Name: "Series", Directory: t.TempDir(), DirectoryName: "Old", CascadeCleanup: "off"}
	writeTest(t, filepath.Join(quarkLibraryRoot(old), "keep.nfo"), "keep")
	if err := a.store.update(func(st *State) error {
		st.QuarkLibraryCards = []quarkLibraryCard{old}
		initQuarkLibraryRuntime(st)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	writeTest(t, blocked, "keep")
	a.store.mu.Lock()
	previous := a.store.runtimeDir
	a.store.runtimeDir = blocked
	a.store.mu.Unlock()
	defer func() { a.store.mu.Lock(); a.store.runtimeDir = previous; a.store.mu.Unlock() }()
	next := old
	next.DirectoryName = "New"
	saved, err := a.saveShareLibraryMetadata(context.Background(), "quark", old, next)
	if !saved || err == nil {
		t.Fatal("failed persistence accepted", saved, err)
	}
	if got := a.store.snapshot().QuarkLibraryCards[0]; got != old {
		t.Fatal("failed save changed card", got)
	}
	if _, err := os.Stat(quarkLibraryRoot(next)); !os.IsNotExist(err) {
		t.Fatal("failed save left renamed directory", err)
	}
	content, err := os.ReadFile(filepath.Join(quarkLibraryRoot(old), "keep.nfo"))
	if err != nil || string(content) != "keep" {
		t.Fatal("failed save lost original files", err)
	}
}

func TestShareLibraryRenameRollbackConflictAndReplacedRoot(t *testing.T) {
	for _, scenario := range []string{"rollback", "conflict", "replaced", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			a := testApp(t)
			old := quarkLibraryCard{ID: "rename", Directory: t.TempDir(), DirectoryName: "Old"}
			next := old
			next.DirectoryName = "New"
			writeTest(t, filepath.Join(quarkLibraryRoot(old), "Season 1", "keep.nfo"), "keep")
			root, err := openStrmReplaceRoot(quarkLibraryRoot(old))
			if err != nil {
				t.Fatal(err)
			}
			identity, err := strmRootIdentity(root)
			root.Close()
			if err != nil {
				t.Fatal(err)
			}
			owner := quarkLibraryOwnership{Directory: quarkLibraryRoot(old), Identity: identity}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "conflict":
				writeTest(t, filepath.Join(quarkLibraryRoot(next), "unrelated.txt"), "foreign")
			case "replaced":
				if err := os.Rename(quarkLibraryRoot(old), quarkLibraryRoot(old)+"-original"); err != nil {
					t.Fatal(err)
				}
				writeTest(t, filepath.Join(quarkLibraryRoot(old), "foreign.txt"), "foreign")
			case "cancelled":
				cancel()
			}
			finish, _, err := a.prepareShareLibraryRename(ctx, old, next, owner)
			if scenario != "rollback" {
				if err == nil {
					finish(false)
					t.Fatal("unsafe rename accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := finish(false); err != nil {
				t.Fatal("rollback failed", err)
			}
			if _, err := os.Stat(quarkLibraryRoot(next)); !os.IsNotExist(err) {
				t.Fatal("rolled-back directory remains", err)
			}
			content, err := os.ReadFile(filepath.Join(quarkLibraryRoot(old), "Season 1", "keep.nfo"))
			if err != nil || string(content) != "keep" {
				t.Fatal("rollback lost contents", err)
			}
		})
	}
}

func TestShareLibraryRenameWithGenerationChangesUsesNativeDirectory(t *testing.T) {
	a := testApp(t)
	old := quarkLibraryCard{ID: "rename", Directory: t.TempDir(), DirectoryName: "Old"}
	next := old
	next.DirectoryName = "New"
	next.ExcludeFiles = "sample"
	writeTest(t, filepath.Join(quarkLibraryRoot(old), "artwork", "poster.jpg"), "keep")
	root, err := openStrmReplaceRoot(quarkLibraryRoot(old))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := strmRootIdentity(root)
	root.Close()
	if err != nil {
		t.Fatal(err)
	}
	owner := quarkLibraryOwnership{Directory: quarkLibraryRoot(old), Identity: identity}
	finish, err := a.prepareShareLibraryMove(context.Background(), old, next, nil, owner)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if _, err := os.Stat(quarkLibraryRoot(old)); !os.IsNotExist(err) {
		t.Fatal("old library remains", err)
	}
	content, err := os.ReadFile(filepath.Join(quarkLibraryRoot(next), "artwork", "poster.jpg"))
	if err != nil || string(content) != "keep" {
		t.Fatal("nested artwork was not renamed", err)
	}
	roots := map[string]quarkLibraryOwnership{old.ID: owner}
	updateShareLibraryRenamedRoot(roots, old, next)
	if roots[old.ID].Directory != quarkLibraryRoot(next) || roots[old.ID].Identity != identity {
		t.Fatal("ownership identity was lost", roots)
	}
}
