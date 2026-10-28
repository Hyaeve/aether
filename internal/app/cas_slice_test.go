package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTianyiCASDynamicPartSizeAndLegacyImport(t *testing.T) {
	const mib = int64(1 << 20)
	for _, tc := range []struct{ size, part int64 }{
		{1, 10 * mib}, {1160375766, 10 * mib},
		{9990*mib - 1, 10 * mib}, {9990 * mib, 10 * mib}, {9990*mib + 1, 20 * mib},
		{19980*mib - 1, 20 * mib}, {19980 * mib, 20 * mib}, {19980*mib + 1, 50 * mib},
		{50 * mib * 1999, 50 * mib}, {50*mib*1999 + 1, 60 * mib},
		{60 * mib * 1999, 60 * mib}, {60*mib*1999 + 1, 70 * mib},
		{1 << 40, 530 * mib},
	} {
		t.Run(fmt.Sprint(tc.size), func(t *testing.T) {
			if got := tianyiCASPartSize(tc.size); got != tc.part {
				t.Fatal("boundary", got, tc.part)
			}
			for _, field := range []string{"", `,"sliceSize":0`, `,"slice_size":0`, `,"sliceSize":5242880`} {
				raw := fmt.Sprintf(`{"name":"video.mkv","size":%d,"md5":"%s","sliceMd5":"%s"%s}`, tc.size, strings.Repeat("a", 32), strings.Repeat("b", 32), field)
				for _, pointer := range []bool{false, true} {
					content, name := raw, "video.mkv.cas"
					if pointer {
						content = "http://localhost/cas/" + base64.RawURLEncoding.EncodeToString([]byte(`{"cas":`+raw+`}`))
						name = "video.mkv.cas.strm"
					}
					info, err := decodeCAS([]byte(content), name)
					want := tc.part
					if field == `,"sliceSize":5242880` {
						want = 5 * mib
					}
					if err != nil || info.SliceSize != want || info.MD5 != strings.Repeat("a", 32) || info.SliceMD5 != strings.Repeat("B", 32) {
						t.Fatal(info, err, want)
					}
				}
			}
			shaRaw := fmt.Sprintf(`{"name":"video.mkv","size":%d,"sha256":"%s"}`, tc.size, strings.Repeat("a", 64))
			if info, err := decodeCAS([]byte(shaRaw), "video.mkv.cas"); err != nil || info.SliceSize != 0 || info.SliceMD5 != "" {
				t.Fatal("SHA-only gained slice metadata", info, err)
			}
		})
	}
}

func TestTianyiCASUserSampleUsesOpenListPartRule(t *testing.T) {
	const sample = "eyJuYW1lIjoiRG9jdG9yIFjvvJrnmb3oibLpu5HmiYvlhZrml7bku6MgLSBTMDFFMDEgLSDnrKwgMSDpm4YgLSAxMDgwcC5ta3YiLCJzaXplIjoyNTM5OTk0NTQzLCJtZDUiOiIwMThDRkIwODJGNDQ1NjAxMDUwRjc3OUYxRUU3RjlDQyIsInNsaWNlTWQ1IjoiQjU4QjNGNUYyREI1NUJBM0FFNjI2MUFEQkFEODAyMjYiLCJjcmVhdGVfdGltZSI6IjE3OTIwNjI4ODAiLCJzaGEyNTYiOiIyRjNFRjBBNzlCMEY3QUE2OThBNTk5RDBCMjkxMzdDNDFFQkQ4NjZGMzlGREFENEE5OTgyQUFGQjk3QTlFRDY0In0="
	info, err := decodeCAS([]byte(sample), "Doctor X.mkv.cas")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != 2539994543 || info.SliceSize != 10<<20 ||
		info.MD5 != "018cfb082f445601050f779f1ee7f9cc" ||
		info.SliceMD5 != "B58B3F5F2DB55BA3AE6261ADBAD80226" ||
		info.SHA256 != "2f3ef0a79b0f7aa698a599d0b29137c41ebd866f39fdad4a9982aafb97a9ed64" {
		t.Fatalf("unexpected decoded OpenList CAS: %+v", info)
	}
}

func tianyiTestUploadParams(t *testing.T, r *http.Request, secret string) url.Values {
	t.Helper()
	if r.URL.Query().Get("rand") == "" {
		t.Fatal("missing OpenList-compatible random query parameter")
	}
	ciphertext, err := hex.DecodeString(r.URL.Query().Get("params"))
	if err != nil || len(ciphertext) == 0 || len(ciphertext)%16 != 0 {
		t.Fatal("invalid encrypted request")
	}
	block, err := aes.NewCipher([]byte(secret[:16]))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(ciphertext); i += 16 {
		block.Decrypt(ciphertext[i:i+16], ciphertext[i:i+16])
	}
	plain := ciphertext[:len(ciphertext)-int(ciphertext[len(ciphertext)-1])]
	params, err := url.ParseQuery(string(plain))
	if err != nil {
		t.Fatal(err)
	}
	return params
}

func TestLocalCASSliceHashesAndCompatibility(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	for _, size := range []int{5, 10 << 20, (10 << 20) + 3} {
		data := bytes.Repeat([]byte{42}, size)
		os.WriteFile(filepath.Join(s.Config["root"], "slice.mp4"), data, 0600)
		info, err := a.generateCASInfo(context.Background(), s, File{ID: "/slice.mp4", Name: "slice.mp4"})
		if err != nil {
			t.Fatal(err)
		}
		hash := md5.Sum(data)
		expected := strings.ToUpper(hex.EncodeToString(hash[:]))
		if size > 10<<20 {
			first, last := md5.Sum(data[:10<<20]), md5.Sum(data[10<<20:])
			sum := md5.Sum([]byte(strings.ToUpper(hex.EncodeToString(first[:]) + "\n" + hex.EncodeToString(last[:]))))
			expected = strings.ToUpper(hex.EncodeToString(sum[:]))
		}
		if info.SliceMD5 != expected || info.SliceSize != 10<<20 {
			t.Fatal(info)
		}
		raw, _ := json.Marshal(info)
		decoded, err := decodeCAS([]byte(base64.StdEncoding.EncodeToString(raw)), "slice.mp4.cas")
		if err != nil || decoded.SliceMD5 != expected {
			t.Fatal(decoded, err)
		}
		mobile, err := casForBinding(info, Storage{Type: "mobile", Config: map[string]string{"mode": "native"}})
		if err != nil || mobile.SliceMD5 != expected || mobile.SliceSize != info.SliceSize || mobile.MD5 != info.MD5 || mobile.SHA256 != info.SHA256 {
			t.Fatal("binding must retain portable hashes", mobile, err)
		}
	}
}

func TestTianyiCASSliceRestore(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "slice-cloud", Type: "tianyi", Config: map[string]string{"username": "account", "password": "secret"}}
	a.tianyiSessions[s.ID] = tianyiSession{Key: "session", Secret: "0123456789abcdef", Credentials: sha256.Sum256([]byte("account\x00secret")), Expires: time.Now().Add(time.Hour)}
	old := apiClient
	defer func() { apiClient = old }()
	committed := false
	expectedSlice := int64(10 << 20)
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		ciphertext, _ := hex.DecodeString(r.URL.Query().Get("params"))
		if len(ciphertext) == 0 || len(ciphertext)%16 != 0 {
			t.Fatal("invalid encrypted request")
		}
		block, _ := aes.NewCipher([]byte("0123456789abcdef"))
		for i := 0; i < len(ciphertext); i += 16 {
			block.Decrypt(ciphertext[i:i+16], ciphertext[i:i+16])
		}
		plain := ciphertext[:len(ciphertext)-int(ciphertext[len(ciphertext)-1])]
		params, _ := url.ParseQuery(string(plain))
		raw := `{"uploadFileId":"upload"}`
		switch r.URL.Path {
		case "/person/initMultiUpload":
			if params.Get("sliceSize") != fmt.Sprint(expectedSlice) || params.Get("parentFolderId") != "folder" {
				t.Fatal(params)
			}
		case "/person/checkTransSecond":
			if params.Get("uploadFileId") != "upload" || params.Get("fileMd5") != strings.Repeat("A", 32) || params.Get("sliceMd5") != strings.Repeat("B", 32) {
				t.Fatal(params)
			}
			raw = `{"fileDataExists":1}`
		case "/person/commitMultiUploadFile":
			if params.Get("sliceMd5") != strings.Repeat("B", 32) || params.Get("fileMd5") != strings.Repeat("A", 32) || params.Get("opertype") != "3" {
				t.Fatal(params)
			}
			committed = true
			raw = `{"file":{"userFileId":"restored"}}`
		default:
			t.Fatal("CAS must not upload content", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	for _, size := range []int64{123, (9990 << 20) + 1, (19980 << 20) + 1, (50<<20)*1999 + 1} {
		expectedSlice = tianyiCASPartSize(size)
		committed = false
		raw := fmt.Sprintf(`{"name":"movie.mp4","size":%d,"md5":"%s","sliceMd5":"%s"}`, size, strings.Repeat("a", 32), strings.Repeat("b", 32))
		info, err := decodeCAS([]byte(raw), "movie.mp4.cas")
		if err != nil {
			t.Fatal(err)
		}
		fid, err := a.tianyiRestore(context.Background(), s, "folder", info.Name, info)
		if err != nil || fid != "restored" || !committed {
			t.Fatal(fid, err)
		}
	}
}
