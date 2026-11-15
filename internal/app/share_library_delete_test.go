package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func libraryDeleteFixture(t *testing.T, provider string) (*App, quarkLibraryCard, quarkLibraryCard) {
	t.Helper()
	a := testApp(t)
	card := quarkLibraryCard{ID: "delete-card", Name: "待删除链库", ShareName: "作品", Secret: "https://pan.quark.cn/s/abcdef", Directory: t.TempDir(), DirectoryName: "Library", CascadeCleanup: "off"}
	other := card
	other.ID, other.Name, other.Secret, other.DirectoryName = "other-card", "保留链库", "https://pan.quark.cn/s/ghijkl", "Other"
	if provider == "cas" || provider == "mobile" {
		card.Secret, other.Secret = mobileLibrarySecret("abcdef", ""), mobileLibrarySecret("ghijkl", "")
	}
	if provider == "115" {
		card.Secret, other.Secret, card.StorageID, other.StorageID = "https://115.com/s/abcdef", "https://115.com/s/ghijkl", "pan", "pan"
	}
	media := "Season 1/Film.mkv.share.strm"
	if provider == "cas" {
		media = "Season 1/Film.mkv.cas.strm"
	}
	writeTest(t, filepath.Join(quarkLibraryRoot(card), filepath.FromSlash(media)), "generated media pointer")
	writeTest(t, filepath.Join(quarkLibraryRoot(card), "Season 1", "Film.nfo"), "scraped episode")
	writeTest(t, filepath.Join(quarkLibraryRoot(card), "Season 1", "poster.jpg"), "season artwork")
	writeTest(t, filepath.Join(quarkLibraryRoot(card), "tvshow.nfo"), "series metadata")
	writeTest(t, filepath.Join(quarkLibraryRoot(card), "credits.txt"), "generated metadata")
	writeTest(t, filepath.Join(quarkLibraryRoot(other), "keep.txt"), "other library")
	root, err := openStrmReplaceRoot(quarkLibraryRoot(card))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := strmRootIdentity(root)
	root.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.update(func(st *State) error {
		cards, roots, files, results := shareLibraryRecords(st, provider)
		*cards = []quarkLibraryCard{card, other}
		roots[card.ID] = quarkLibraryOwnership{Directory: quarkLibraryRoot(card), Identity: identity}
		results[card.ID] = quarkLibraryResult{Status: "completed"}
		for _, name := range []string{media, "credits.txt"} {
			raw, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), filepath.FromSlash(name)))
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			files[name] = quarkLibraryFile{Key: name, CardID: card.ID, Relative: name, Digest: hex.EncodeToString(sum[:]), Media: name == media, Published: true}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return a, card, other
}

func TestShareLibraryDeleteActionsAndNameReuse(t *testing.T) {
	for _, provider := range []string{"quark", "mobile", "115", "cas"} {
		for _, deleteFiles := range []bool{false, true} {
			mode := "record"
			if deleteFiles {
				mode = "files"
			}
			t.Run(provider+"/"+mode, func(t *testing.T) {
				a, card, other := libraryDeleteFixture(t, provider)
				h := a.Handler(t.TempDir())
				cookie := setupTest(t, a, h)
				body := map[string]bool{"deleteFiles": deleteFiles, "confirm": deleteFiles}
				endpoint := "/api/virtual-library/" + provider + "/"
				if w := request(t, h, "DELETE", endpoint+card.ID, body, nil); w.Code != 401 {
					t.Fatal("unauthenticated deletion accepted", w.Code)
				}
				w := request(t, h, "DELETE", endpoint+card.ID, body, cookie)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				st := a.store.snapshot()
				cards, roots, files, results := shareLibraryRecords(&st, provider)
				if len(*cards) != 1 || (*cards)[0] != other || len(roots) != 0 || len(files) != 0 || len(results) != 0 {
					t.Fatal("card/grant/ownership cleanup mismatch", *cards, roots, files, results)
				}
				_, err := os.Stat(quarkLibraryRoot(card))
				if deleteFiles && !os.IsNotExist(err) || !deleteFiles && err != nil {
					t.Fatal("wrong output deletion mode", deleteFiles, err)
				}
				if raw, err := os.ReadFile(filepath.Join(quarkLibraryRoot(other), "keep.txt")); err != nil || string(raw) != "other library" {
					t.Fatal("other library changed", string(raw), err)
				}
				if deleteFiles {
					body := map[string]any{"url": other.Secret, "name": other.Name, "directory": other.Directory, "directoryName": card.DirectoryName, "enabled": false, "cascadeCleanup": "off", "storageId": other.StorageID}
					if provider == "quark" || provider == "mobile" {
						delete(body, "storageId")
					}
					if w := request(t, h, "PUT", endpoint+other.ID, body, cookie); w.Code != 200 {
						t.Fatal("purged directory name cannot be reused", w.Code, w.Body.String())
					}
				}
			})
		}
	}
}

func TestShareLibraryDeletePreflightPreservesCardAndAllFiles(t *testing.T) {
	for _, provider := range []string{"quark", "mobile", "115", "cas"} {
		for _, scenario := range []string{"unknown", "modified", "identity", "marker", "confirmation", "persistence"} {
			t.Run(provider+"/"+scenario, func(t *testing.T) {
				a, card, _ := libraryDeleteFixture(t, provider)
				h := a.Handler(t.TempDir())
				cookie := setupTest(t, a, h)
				body := map[string]bool{"deleteFiles": true, "confirm": true}
				switch scenario {
				case "unknown":
					writeTest(t, filepath.Join(quarkLibraryRoot(card), "private.txt"), "unrelated")
				case "modified":
					writeTest(t, filepath.Join(quarkLibraryRoot(card), "credits.txt"), "changed externally")
				case "identity":
					if err := a.store.update(func(st *State) error {
						_, roots, _, _ := shareLibraryRecords(st, provider)
						owner := roots[card.ID]
						owner.Identity = "other-volume"
						roots[card.ID] = owner
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				case "marker":
					writeTest(t, filepath.Join(quarkLibraryRoot(card), quarkLibraryMarker), "foreign")
				case "confirmation":
					body["confirm"] = false
				case "persistence":
					blocked := filepath.Join(t.TempDir(), "not-a-directory")
					writeTest(t, blocked, "block write")
					a.store.mu.Lock()
					previous := a.store.runtimeDir
					a.store.runtimeDir = blocked
					a.store.mu.Unlock()
					defer func() { a.store.mu.Lock(); a.store.runtimeDir = previous; a.store.mu.Unlock() }()
				}
				w := request(t, h, "DELETE", "/api/virtual-library/"+provider+"/"+card.ID, body, cookie)
				if scenario == "confirmation" && w.Code != 400 || scenario != "confirmation" && w.Code != 409 {
					t.Fatal("unsafe deletion accepted", w.Code, w.Body.String())
				}
				st := a.store.snapshot()
				if !shareLibraryCardUnchanged(&st, provider, card) {
					t.Fatal("failed deletion lost card")
				}
				for _, name := range []string{"Season 1/Film.nfo", "Season 1/poster.jpg", "tvshow.nfo", "credits.txt"} {
					if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), filepath.FromSlash(name))); err != nil {
						t.Fatal("preflight failure partially deleted files", name, err)
					}
				}
			})
		}
	}
}

func TestShareLibraryDeleteCancellationAndMissingOutput(t *testing.T) {
	a, card, _ := libraryDeleteFixture(t, "quark")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if count, err := a.purgeShareLibrary(ctx, "quark", card); err == nil || count != 0 {
		t.Fatal("cancelled deletion accepted", count, err)
	}
	if err := os.Rename(quarkLibraryRoot(card), filepath.Join(card.Directory, "moved-aside")); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	if w := request(t, h, http.MethodDelete, "/api/virtual-library/quark/"+card.ID, map[string]bool{"deleteFiles": true, "confirm": true}, cookie); w.Code != 200 {
		t.Fatal("missing output cannot retire record", w.Code, w.Body.String())
	}
}

func TestCASLibraryOutputDigestsPersistAndLegacySkipCanRegister(t *testing.T) {
	a := testApp(t)
	card := casLibraryFixture(t, a)
	mockMobileGeneration(t, "valid")
	if _, err := a.runCASLibrary(context.Background(), card, nil); err != nil {
		t.Fatal(err)
	}
	if len(a.store.snapshot().CASLibraryRuntime.Files) != 1 {
		t.Fatal("CAS publish did not persist its digest")
	}
	if err := a.store.update(func(st *State) error { st.CASLibraryRuntime.Files = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if result, err := a.runCASLibrary(context.Background(), card, nil); err != nil || result.Skipped != 1 || result.Generated != 0 {
		t.Fatal("legacy CAS skip failed", result, err)
	}
	reopened, err := NewStore(a.store.dir)
	if err == nil {
		err = reopened.initRuntime(a.store.runtimeDir)
	}
	if err != nil {
		t.Fatal(err)
	}
	files := reopened.snapshot().CASLibraryRuntime.Files
	if len(files) != 1 {
		t.Fatal("CAS output digest did not survive restart", files)
	}
	for _, f := range files {
		if f.CardID != card.ID || len(f.Digest) != 64 || !strings.HasSuffix(f.Relative, ".cas.strm") {
			t.Fatal("invalid persisted output", f)
		}
	}
	if count, err := a.purgeShareLibrary(context.Background(), "cas", card); err != nil || count != 1 {
		t.Fatal("legacy CAS cannot be safely removed after verification", count, err)
	}
}

func TestShareLibraryDeleteProtectsOtherLibraryAndSystemScope(t *testing.T) {
	a, card, _ := libraryDeleteFixture(t, "quark")
	st := a.store.snapshot()
	st.MobileLibraryCards = []quarkLibraryCard{{ID: card.ID, Directory: card.Directory, DirectoryName: card.DirectoryName}}
	if err := libraryDeleteScopeAvailable(st, "quark", card); err == nil {
		t.Fatal("same id in another provider bypassed overlap check")
	}
	st.MobileLibraryCards[0].DirectoryName = "another-path"
	initMobileLibrary(&st)
	st.MobileLibraryRuntime.Roots[card.ID] = st.QuarkLibraryRuntime.Roots[card.ID]
	if err := libraryDeleteScopeAvailable(st, "quark", card); err == nil {
		t.Fatal("same native directory identity bypassed overlap check")
	}
	unsafe := card
	unsafe.Directory, unsafe.DirectoryName = filepath.Dir(a.store.dir), filepath.Base(a.store.dir)
	if count, err := a.purgeShareLibrary(context.Background(), "quark", unsafe); err == nil || count != 0 || !strings.Contains(err.Error(), "配置") {
		t.Fatal("system scope was not protected", count, err)
	}
	if err := a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, Storage{ID: "local-source", Type: "local", Name: "本地", Enabled: true, Config: map[string]string{"root": card.Directory}})
		st.Tasks = append(st.Tasks, Task{ID: "running-source", Kind: "cache", Name: "保护源目录", StorageID: "local-source", Source: "/" + card.DirectoryName})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.runMu.Lock()
	a.running["running-source"] = func() {}
	a.runMu.Unlock()
	defer func() { a.runMu.Lock(); delete(a.running, "running-source"); a.runMu.Unlock() }()
	if count, err := a.purgeShareLibrary(context.Background(), "quark", card); err == nil || count != 0 || !strings.Contains(err.Error(), "保护源目录") {
		t.Fatal("running task scope was not protected", count, err)
	}
}

func TestCASLibraryDeletePreservesUnknownCloudReceipt(t *testing.T) {
	a, card, _ := libraryDeleteFixture(t, "cas")
	job := GenerationJob{ID: "uncertain", Status: "error", LibrarySource: casLibrarySourceStamp(card.Secret), Temporary: []GenerationTemporary{{Phase: "submitting"}}}
	if err := a.store.update(func(st *State) error { st.GenerationJobs = []GenerationJob{job}; return nil }); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	endpoint := "/api/virtual-library/cas/" + card.ID
	if w := request(t, h, "DELETE", endpoint, map[string]bool{"deleteFiles": true, "confirm": true}, cookie); w.Code != 409 {
		t.Fatal("uncertain CAS deletion accepted", w.Code, w.Body.String())
	}
	if w := request(t, h, "DELETE", endpoint, nil, cookie); w.Code != 200 {
		t.Fatal("record-only deletion blocked", w.Code, w.Body.String())
	}
	st := a.store.snapshot()
	if len(st.GenerationJobs) != 1 || st.GenerationJobs[0].ID != job.ID || st.GenerationJobs[0].Temporary[0].Phase != "submitting" {
		t.Fatal("deleting a card erased the cloud receipt", st.GenerationJobs)
	}
	if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), "credits.txt")); err != nil {
		t.Fatal("record-only recovery deleted output", err)
	}
}
