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
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
			if params.Get("sliceSize") != "10485760" || params.Get("parentFolderId") != "folder" {
				t.Fatal(params)
			}
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
	info := CASInfo{Name: "movie.mp4", Size: 123, MD5: strings.Repeat("a", 32), SliceMD5: strings.Repeat("b", 32), SliceSize: 10 << 20}
	fid, err := a.tianyiRestore(context.Background(), s, "folder", info.Name, info)
	if err != nil || fid != "restored" || !committed {
		t.Fatal(fid, err)
	}
}
