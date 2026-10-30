package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCASFormatAliasesRequirementsAndPlaceholders(t *testing.T) {
	sha, md, slice := strings.Repeat("a", 64), strings.Repeat("b", 32), strings.Repeat("c", 32)
	for _, tc := range []struct {
		name, fields string
		valid        bool
		size         int64
	}{
		{"sha", `"sha256":"` + sha + `"`, true, 0},
		{"md5 alone", `"md5":"` + md + `"`, false, 0},
		{"slice alone", `"sliceMd5":"` + slice + `"`, false, 0},
		{"no hashes", `"md5":"","sha256":"","slice_md5":""`, false, 0},
		{"camel", `"md5":"` + md + `","sliceMd5":"` + slice + `"`, true, 10 << 20},
		{"snake", `"md5":"` + md + `","slice_md5":"` + slice + `","slice_size":5242880`, true, 5 << 20},
		{"string size", `"md5":"` + md + `","sliceMd5":"` + slice + `","sliceSize":"5242880"`, true, 5 << 20},
		{"all three", `"sha256":"` + sha + `","md5":"` + md + `","sliceMd5":"` + slice + `"`, true, 10 << 20},
		{"hash conflict", `"md5":"` + md + `","sliceMd5":"` + slice + `","slice_md5":"` + md + `"`, false, 0},
		{"size conflict", `"md5":"` + md + `","sliceMd5":"` + slice + `","sliceSize":20,"slice_size":10`, false, 0},
		{"malformed slice", `"sha256":"` + sha + `","sliceMd5":"bad"`, false, 0},
		{"negative size", `"md5":"` + md + `","sliceMd5":"` + slice + `","slice_size":-1`, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"name":"original.mkv","size":123,` + tc.fields + `}`
			info, err := decodeCAS([]byte(base64.StdEncoding.EncodeToString([]byte(raw))), "renamed.mkv.cas")
			if (err == nil) != tc.valid {
				t.Fatal(info, err)
			}
			if !tc.valid {
				return
			}
			if info.SliceSize != tc.size || info.Name != "renamed.mkv" {
				t.Fatal(info)
			}
			encoded, _ := json.Marshal(info)
			var fields map[string]json.RawMessage
			json.Unmarshal(encoded, &fields)
			for _, key := range []string{"sha256", "md5", "slice_md5", "slice_size"} {
				if _, ok := fields[key]; !ok {
					t.Fatal("missing placeholder", key, string(encoded))
				}
			}
			if tc.name == "sha" && (string(fields["md5"]) != `""` || string(fields["slice_md5"]) != `""`) {
				t.Fatal("not empty placeholders")
			}
		})
	}
}

func TestCASFormatUserSampleAndPointerPreserveSlice(t *testing.T) {
	sample := `eyJuYW1lIjoi5q2k5aSE6YCa5b6A57mB5pifIC0gUzAxRTAyIC0g56ysIDIg6ZuGIC0gMjE2MHAubWt2Iiwic2l6ZSI6MTE2MDM3NTc2NiwibWQ1IjoiMjk2NzE5NUU2OTg0MDEwNDM1Mjk0M0Y2NEQ3ODVBQUUiLCJzbGljZU1kNSI6IkM1OEUxOUQ0Qzk0OTJENzJFQ0IwQkZBQ0Q4RUMyMUFEIiwiY3JlYXRlX3RpbWUiOiIxNzkxNzgxMTA5Iiwic2hhMjU2IjoiOUFDN0QzMEJBODRGODdDQTEyM0YxMEJGNzJGNjRDRDUzQkUyNTBEQ0M5QzgxMzFFQjlDRjg4NDUxNTE4QTBBNiJ9`
	info, err := decodeCAS([]byte(sample), "sample.mkv.cas")
	if err != nil || info.SliceMD5 != "C58E19D4C9492D72ECB0BFACD8EC21AD" || info.SliceSize != 10<<20 || info.MD5 != "2967195e69840104352943f64d785aae" {
		t.Fatal(info, err)
	}
	raw, _ := base64.StdEncoding.DecodeString(sample)
	pointerRaw := []byte(`{"s":"old-account","f":"old-source","cas":` + string(raw) + `}`)
	pointer := "http://localhost/cas/" + base64.RawURLEncoding.EncodeToString(pointerRaw) + "?sign=old"
	got, err := decodeCAS([]byte(pointer), "sample.mkv.cas.strm")
	if err != nil || got.SliceMD5 != info.SliceMD5 || got.SHA256 != info.SHA256 {
		t.Fatal(got, err)
	}
}

func TestCASFormatTianyiMissingHashesRejectBeforeUpstream(t *testing.T) {
	a := testApp(t)
	s := Storage{Type: "tianyi", Config: map[string]string{"username": "test", "password": "test"}}
	old := apiClient
	defer func() { apiClient = old }()
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		t.Fatal("incomplete feature contacted upstream", r.URL.Path)
		return nil, nil
	})}
	for _, info := range []CASInfo{
		{Name: "film.mp4", Size: 12, MD5: strings.Repeat("b", 32)},
		{Name: "film.mp4", Size: 12, SHA256: strings.Repeat("a", 64), MD5: strings.Repeat("b", 32)},
		{Name: "film.mp4", Size: 12, SliceMD5: strings.Repeat("c", 32), SliceSize: 10 << 20},
		{Name: "film.mp4", Size: 12, MD5: strings.Repeat("b", 32), SliceMD5: strings.Repeat("c", 32), SliceSize: -1},
	} {
		if _, err := a.tianyiRestore(context.Background(), s, "folder", info.Name, info); err == nil {
			t.Fatal("incomplete Tianyi feature accepted", info)
		}
	}
	legacy := CASInfo{Name: "film.mp4", Size: 12, MD5: strings.Repeat("b", 32), SliceMD5: strings.Repeat("c", 32)}
	if err := validateCASFor(s, legacy); err != nil || legacy.SliceSize != 0 {
		t.Fatal("missing size should normalize without mutating the source", legacy, err)
	}
}

func TestCASFormatTaskTreeSkipsIncompleteAndKeepsSources(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	sha, md, slice := strings.Repeat("a", 64), strings.Repeat("b", 32), strings.Repeat("c", 32)
	files := map[string]string{
		"A/Season 1/sha.mkv.cas":   `{"name":"sha.mkv","size":12,"sha256":"` + sha + `"}`,
		"A/Season 1/pair.mkv.cas":  `{"name":"pair.mkv","size":12,"md5":"` + md + `","sliceMd5":"` + slice + `"}`,
		"A/Season 1/md.mkv.cas":    `{"name":"md.mkv","size":12,"md5":"` + md + `"}`,
		"A/Season 1/slice.mkv.cas": `{"name":"slice.mkv","size":12,"sliceMd5":"` + slice + `"}`,
	}
	for name, raw := range files {
		writeTest(t, filepath.Join(s.Config["root"], filepath.FromSlash(name)), raw)
	}
	valid := CASInfo{Name: "pointer.mkv", Size: 12, MD5: md, SliceMD5: slice, SliceSize: 10 << 20}
	writeTest(t, filepath.Join(s.Config["root"], "A", "Season 1", "pointer.mkv.cas.strm"), a.signedStreamURL(streamClaim{Storage: "old", File: "old-file", CAS: &valid}))
	invalid := CASInfo{Name: "old.mkv", Size: 12, MD5: md}
	writeTest(t, filepath.Join(s.Config["root"], "A", "Season 1", "old.mkv.cas.strm"), a.signedStreamURL(streamClaim{CAS: &invalid}))
	task := Task{ID: "format-task", Name: "格式任务", Kind: "cas", StorageID: s.ID, Source: "/A", Mode: "full", Target: "format", CompletionAction: "delete"}
	a.store.update(func(st *State) error { st.Tasks = append(st.Tasks, task); return nil })
	n, err := a.executeTask(context.Background(), task, s)
	if err != nil || n != 3 {
		t.Fatal(n, err)
	}
	for _, name := range []string{"sha", "pair", "pointer"} {
		raw, err := os.ReadFile(filepath.Join(a.outputDir, "format", "A", "Season 1", name+".mkv.cas.strm"))
		if err != nil {
			t.Fatal(err)
		}
		info := generatedCASClaim(t, raw)
		if info.Name != name+".mkv" {
			t.Fatal(info)
		}
	}
	for _, name := range []string{"md", "slice", "old"} {
		if _, err := os.Stat(filepath.Join(a.outputDir, "format", "A", "Season 1", name+".mkv.cas.strm")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("incomplete output generated", name, err)
		}
		suffix := ".cas"
		if name == "old" {
			suffix = ".cas.strm"
		}
		if _, err := os.Stat(filepath.Join(s.Config["root"], "A", "Season 1", name+".mkv"+suffix)); err != nil {
			t.Fatal("incomplete source deleted", name, err)
		}
	}
	logs := ""
	for _, log := range a.store.logSnapshot() {
		logs += log.Message
	}
	for _, name := range []string{"md.mkv.cas", "slice.mkv.cas", "old.mkv.cas.strm"} {
		if !strings.Contains(logs, "/A/Season 1/"+name) || !strings.Contains(logs, "CAS信息缺失") {
			t.Fatal("missing diagnostic", logs)
		}
	}
	if strings.Contains(logs, "/A/A/") {
		t.Fatal("output wrapper duplicated in source diagnostic", logs)
	}
	if a.store.snapshot().Tasks[0].CASFailed != 3 {
		t.Fatal("missing failed counters")
	}
}

func TestCASFormatMobileTaskWritesAllHashPlaceholders(t *testing.T) {
	a := testApp(t)
	s := addCASBindings(t, a)[0]
	cfg := a.store.snapshot().Settings
	cfg.CacheEnabled = true
	cfg.CacheMemoryMB = 128
	cfg.CacheMaxItems = 10000
	a.store.update(func(st *State) error { st.Settings = cfg; return nil })
	a.cache.put(s.ID+":"+rootOf(s), []File{{ID: "cloud-file", Name: "mobile.mkv", Size: 42, SHA256: strings.Repeat("a", 64)}}, 30, cfg)
	task := Task{Name: "mobile placeholders", Kind: "cas", StorageID: s.ID, Source: rootOf(s), Target: "mobile-format", Mode: "full"}
	if n, err := a.executeTask(context.Background(), task, s); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	raw, err := os.ReadFile(filepath.Join(a.outputDir, "mobile-format", "mobile.mkv.cas.strm"))
	if err != nil {
		t.Fatal(err)
	}
	info := generatedCASClaim(t, raw)
	if info.MD5 != "" || info.SliceMD5 != "" || info.SliceSize != 0 || info.SHA256 == "" {
		t.Fatal(info)
	}
	link := strings.TrimSpace(string(raw))
	token := strings.Split(strings.Split(link, "/cas/")[1], "?")[0]
	claimRaw, _ := base64.RawURLEncoding.DecodeString(token)
	var claim struct {
		CAS map[string]json.RawMessage `json:"cas"`
	}
	json.Unmarshal(claimRaw, &claim)
	for _, key := range []string{"sha256", "md5", "slice_md5", "slice_size"} {
		if _, ok := claim.CAS[key]; !ok {
			t.Fatal("missing in STRM", key, string(claimRaw))
		}
	}
}
