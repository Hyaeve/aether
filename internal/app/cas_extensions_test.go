package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func TestCASTaskCustomExtensions(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	content := "custom extension content"
	for _, name := range []string{"nested/report.PDF", "archive.zip", "payload.pkg_7-z", "ignored.mkv", "readme.txt", "blocked.pdf", "excluded/report.pdf"} {
		writeTest(t, filepath.Join(s.Config["root"], filepath.FromSlash(name)), content)
	}
	imported := CASInfo{Name: "original.pdf", Size: 42, SHA256: strings.Repeat("a", 64)}
	raw, err := marshalCASContent(imported)
	if err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(s.Config["root"], "features", "imported.pdf.cas"), string(raw))
	imported.Name = "original.pkg_7-z"
	writeTest(t, filepath.Join(s.Config["root"], "features", "pointer.PKG_7-Z.cas.strm"), a.generatedStreamURL(streamClaim{CAS: &imported, Redirect: true}))
	imported.Name = "ignored.mp4"
	raw, err = marshalCASContent(imported)
	if err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(s.Config["root"], "features", "ignored.mp4.cas"), string(raw))
	task := Task{Name: "custom extensions", Kind: "cas", StorageID: s.ID, Source: "/", Target: "custom", Mode: "incremental", MediaExtensions: "PDF;zip;PKG_7-Z", ExcludeFiles: "blocked", ExcludeDirs: "excluded"}
	if err := a.validateTask(&task); err != nil {
		t.Fatal(err)
	}
	if task.MediaExtensions != "pdf;zip;pkg_7-z" {
		t.Fatal("custom extension normalization changed", task.MediaExtensions)
	}
	if count, err := a.executeTask(context.Background(), task, s); err != nil || count != 5 {
		t.Fatalf("custom generation: %d %v", count, err)
	}
	expected := map[string]bool{
		"nested/report.PDF.cas.strm":        false,
		"archive.zip.cas.strm":              false,
		"payload.pkg_7-z.cas.strm":          false,
		"features/imported.pdf.cas.strm":    true,
		"features/pointer.PKG_7-Z.cas.strm": true,
	}
	sha := sha256.Sum256([]byte(content))
	err = filepath.WalkDir(filepath.Join(a.outputDir, "custom"), func(name string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(filepath.Join(a.outputDir, "custom"), name)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		featureSource, ok := expected[rel]
		if !ok {
			t.Fatalf("generated unconfigured/excluded file: %s", rel)
		}
		delete(expected, rel)
		raw, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		info := generatedCASClaim(t, raw)
		wantName := strings.TrimSuffix(path.Base(rel), ".cas.strm")
		if info.Name != wantName {
			t.Fatal("custom file name changed", info.Name, wantName)
		}
		wantHash, wantSize := hex.EncodeToString(sha[:]), int64(len(content))
		if featureSource {
			wantHash, wantSize = imported.SHA256, imported.Size
		}
		if info.SHA256 != wantHash || info.Size != wantSize {
			t.Fatal("custom file features changed", info)
		}
		decoded, err := decodeCAS(raw, path.Base(rel))
		if err != nil || decoded.Name != wantName || decoded.SHA256 != wantHash {
			t.Fatal("custom CAS STRM cannot be reimported", decoded, err)
		}
		return nil
	})
	if err != nil || len(expected) != 0 {
		t.Fatal("missing output", expected, err)
	}
	if count, err := a.executeTask(context.Background(), task, s); err != nil || count != 0 {
		t.Fatalf("incremental custom generation: %d %v", count, err)
	}
	if !taskMedia(Task{}, "default.mkv") || taskMedia(Task{}, "unconfigured.pdf") {
		t.Fatal("empty extension settings should retain the existing defaults")
	}
	if _, err := normalizeExtensions("pdf;中文"); err == nil {
		t.Fatal("custom extensions bypassed syntax validation")
	}
}

func TestCASCustomExtensionImportNames(t *testing.T) {
	a := testApp(t)
	for _, tc := range []struct{ source, original, want string }{
		{"renamed.PDF", "original.pdf", "renamed.PDF"},
		{"archive.pkg_7-z", "original.pkg_7-z", "archive.pkg_7-z"},
		{"renamed", "original.zip", "renamed.zip"},
		{"renamed.custom", "original.pdf", "renamed.custom"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			info := CASInfo{Name: tc.original, Size: 42, SHA256: strings.Repeat("a", 64)}
			raw, err := marshalCASContent(info)
			if err != nil {
				t.Fatal(err)
			}
			for _, content := range [][]byte{raw, []byte(base64.StdEncoding.EncodeToString(raw))} {
				got, err := decodeCAS(content, tc.source+".cas")
				if err != nil || got.Name != tc.want {
					t.Fatal("CAS custom extension changed", got, err)
				}
			}
			got, err := decodeCAS([]byte(a.generatedStreamURL(streamClaim{CAS: &info, Redirect: true})), tc.source+".cas.strm")
			if err != nil || got.Name != tc.want {
				t.Fatal("CAS STRM custom extension changed", got, err)
			}
			got, err = parseCASContent(raw)
			if err != nil || got.Name != tc.original {
				t.Fatal("manual CAS parsing rejected custom extension", got, err)
			}
		})
	}
}

func TestCASCustomExtensionCloudGeneration(t *testing.T) {
	a := testApp(t)
	s := addCASBindings(t, a)[0]
	cfg := a.store.snapshot().Settings
	cfg.CacheEnabled, cfg.CacheMemoryMB, cfg.CacheMaxItems = true, 128, 10000
	if err := a.store.update(func(st *State) error { st.Settings = cfg; return nil }); err != nil {
		t.Fatal(err)
	}
	a.cache.put(s.ID+":"+rootOf(s), []File{
		{ID: "custom", Name: "cloud.PDF", Size: 42, SHA256: strings.Repeat("a", 64)},
		{ID: "excluded", Name: "cloud.mkv", Size: 42, SHA256: strings.Repeat("b", 64)},
	}, 30, cfg)
	task := Task{Name: "cloud custom extensions", Kind: "cas", StorageID: s.ID, Source: rootOf(s), Target: "cloud-custom", Mode: "full", MediaExtensions: "pdf"}
	if err := a.validateTask(&task); err != nil {
		t.Fatal(err)
	}
	if count, err := a.executeTask(context.Background(), task, s); err != nil || count != 1 {
		t.Fatalf("cloud custom generation: %d %v", count, err)
	}
	raw, err := os.ReadFile(filepath.Join(a.outputDir, "cloud-custom", "cloud.PDF.cas.strm"))
	if err != nil {
		t.Fatal(err)
	}
	info := generatedCASClaim(t, raw)
	if info.Name != "cloud.PDF" || info.SHA256 != strings.Repeat("a", 64) || info.MD5 != "" || info.SliceMD5 != "" || info.SliceSize != 0 {
		t.Fatal("cloud custom CAS features changed", info)
	}
}

func TestCASCustomExtensionValidationSafety(t *testing.T) {
	valid := CASInfo{Name: "document.pdf", Size: 42, SHA256: strings.Repeat("a", 64)}
	if err := validateCAS(valid); err != nil {
		t.Fatal("custom extension rejected", err)
	}
	for _, change := range []func(*CASInfo){
		func(i *CASInfo) { i.Name = "../document.pdf" },
		func(i *CASInfo) { i.Name = `folder\document.pdf` },
		func(i *CASInfo) { i.Name = "C:document.pdf" },
		func(i *CASInfo) { i.Name = "" },
		func(i *CASInfo) { i.Size = 0 },
		func(i *CASInfo) { i.Size = 1<<40 + 1 },
		func(i *CASInfo) { i.SHA256 = "invalid" },
		func(i *CASInfo) { i.SHA256 = ""; i.MD5 = strings.Repeat("a", 32) },
		func(i *CASInfo) { i.SliceSize = -1 },
	} {
		info := valid
		change(&info)
		if err := validateCAS(info); err == nil {
			encoded, _ := json.Marshal(info)
			t.Fatal("invalid custom CAS accepted", string(encoded))
		}
	}
}
