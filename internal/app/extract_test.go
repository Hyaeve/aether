package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
	zip "github.com/yeka/zip"
)

func extractFixture(t *testing.T, name string, enc zip.EncryptionMethod, mode os.FileMode) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	var dst io.Writer
	var err error
	if enc != 0 {
		dst, err = w.Encrypt(name, "secret-password", enc)
	} else {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(mode)
		dst, err = w.CreateHeader(h)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dst.Write([]byte("hello from archive")); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func stageFixture(t *testing.T, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "archive")
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractZIPPasswordsAndPaths(t *testing.T) {
	for _, enc := range []zip.EncryptionMethod{0, zip.StandardEncryption, zip.AES128Encryption, zip.AES256Encryption} {
		p := stageFixture(t, extractFixture(t, "目录/音频.txt", enc, 0600))
		count, size, err := extractArchive(context.Background(), p, t.TempDir(), ".zip", "secret-password")
		if err != nil || count != 1 || size != 18 {
			t.Fatal(enc, count, size, err)
		}
		if enc != 0 {
			for _, password := range []string{"", "incorrect"} {
				if _, _, err := extractArchive(context.Background(), p, t.TempDir(), ".zip", password); err == nil {
					t.Fatal("accepted wrong password", enc)
				}
			}
		}
	}
	for _, name := range []string{"../outside", "/outside", "C:/escape", "dir\\escape", "dir/../escape", "dir//escape", "NUL.txt", "folder/file.", "folder/ bad", "bad\x00name", "bad\tname"} {
		if _, _, err := extractArchive(context.Background(), stageFixture(t, extractFixture(t, name, 0, 0600)), t.TempDir(), ".zip", ""); err == nil {
			t.Fatal("accepted unsafe path", name)
		}
	}
	if _, _, err := extractArchive(context.Background(), stageFixture(t, extractFixture(t, "link", 0, os.ModeSymlink|0777)), t.TempDir(), ".zip", ""); err == nil {
		t.Fatal("accepted symlink")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := extractArchive(ctx, stageFixture(t, extractFixture(t, "file", 0, 0600)), t.TempDir(), ".zip", ""); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestExtractTARCompressionAndSpecialFiles(t *testing.T) {
	var raw, gz, xb bytes.Buffer
	w := tar.NewWriter(&raw)
	w.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0755})
	w.WriteHeader(&tar.Header{Name: "./folder/", Typeflag: tar.TypeDir, Mode: 0755})
	w.WriteHeader(&tar.Header{Name: "./folder/test.txt", Typeflag: tar.TypeReg, Mode: 0644, Size: 5})
	w.Write([]byte("hello"))
	w.Close()
	g := gzip.NewWriter(&gz)
	g.Write(raw.Bytes())
	g.Close()
	x, _ := xz.NewWriter(&xb)
	x.Write(raw.Bytes())
	x.Close()
	for format, b := range map[string][]byte{".tar": raw.Bytes(), ".tar.gz": gz.Bytes(), ".txz": xb.Bytes()} {
		out := t.TempDir()
		count, size, err := extractArchive(context.Background(), stageFixture(t, b), out, format, "")
		if err != nil || count != 1 || size != 5 {
			t.Fatal(format, count, size, err)
		}
		contents, err := os.ReadFile(filepath.Join(out, "folder", "test.txt"))
		if err != nil || string(contents) != "hello" {
			t.Fatal(err)
		}
	}
	corrupt := append([]byte{}, gz.Bytes()...)
	corrupt[len(corrupt)-8] ^= 0xff
	if _, _, err := extractArchive(context.Background(), stageFixture(t, corrupt), t.TempDir(), ".tar.gz", ""); err == nil {
		t.Fatal("accepted corrupt checksum")
	}
	for _, kind := range []byte{tar.TypeSymlink, tar.TypeLink, tar.TypeFifo} {
		var b bytes.Buffer
		w := tar.NewWriter(&b)
		w.WriteHeader(&tar.Header{Name: "link", Linkname: "../outside", Typeflag: kind, Mode: 0777})
		w.Close()
		if _, _, err := extractArchive(context.Background(), stageFixture(t, b.Bytes()), t.TempDir(), ".tar", ""); err == nil {
			t.Fatal("accepted special entry")
		}
	}
}

func TestExtractLimitsAndDuplicates(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, e := range []extraction{{ctx: context.Background(), root: root, names: map[string]bool{}, count: extractEntryLimit}, {ctx: context.Background(), root: root, names: map[string]bool{}, bytes: extractOutputLimit}} {
		if err := e.entry("file", 0600, 1, strings.NewReader("a")); err == nil {
			t.Fatal("limit ignored")
		}
	}
	e := extraction{ctx: context.Background(), root: root, names: map[string]bool{}}
	if err := e.entry("file.txt", 0600, 1, strings.NewReader("a")); err != nil {
		t.Fatal(err)
	}
	if err := e.entry("FILE.TXT", 0600, 1, strings.NewReader("b")); err == nil {
		t.Fatal("case duplicate ignored")
	}
	if err := e.entry("short", 0600, 3, strings.NewReader("ab")); err == nil {
		t.Fatal("truncation ignored")
	}
}

func TestExtractAPIEncryptedZIPNoPartialPublish(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "encrypted.zip"), extractFixture(t, "nested/file.txt", zip.AES256Encryption, 0600), 0600); err != nil {
		t.Fatal(err)
	}
	s := Storage{ID: "s", Type: "local", Name: "本地", Enabled: true, Config: map[string]string{"root": root}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	in := map[string]any{"storageId": "s", "parent": "/", "id": "/encrypted.zip", "name": "output", "password": "incorrect"}
	if w := request(t, h, "POST", "/api/files/extract", in, nil); w.Code != 401 {
		t.Fatal("unprotected")
	}
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/files/extract", in, cookie)
	if w.Code != 400 || strings.Contains(w.Body.String(), "incorrect") {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "output")); !os.IsNotExist(err) {
		t.Fatal("published wrong password output", err)
	}
	in["password"] = "secret-password"
	w = request(t, h, "POST", "/api/files/extract", in, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	b, err := os.ReadFile(filepath.Join(root, "output", "nested", "file.txt"))
	if err != nil || string(b) != "hello from archive" {
		t.Fatal(err)
	}
	if w = request(t, h, "POST", "/api/files/extract", in, cookie); w.Code != 409 {
		t.Fatal("overwrote directory", w.Code)
	}
	files, err := os.ReadDir(filepath.Join(a.dataDir, "cache", "extract"))
	if err != nil || len(files) != 0 {
		t.Fatal("temporary plaintext not cleaned", err)
	}
}

func TestExtractRARAndInvalidSevenZip(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{'R', 'a', 'r', '!', 0x1a, 0x07, 0x00})
	header := func(kind byte, flags uint16, body []byte) {
		v := []byte{0, 0, kind, 0, 0, 0, 0}
		binary.LittleEndian.PutUint16(v[3:], flags)
		binary.LittleEndian.PutUint16(v[5:], uint16(len(v)+len(body)))
		v = append(v, body...)
		binary.LittleEndian.PutUint16(v, uint16(crc32.ChecksumIEEE(v[2:])))
		b.Write(v)
	}
	header(0x73, 0, make([]byte, 6))
	data := []byte("hello")
	name := []byte("hello.txt")
	body := make([]byte, 25)
	binary.LittleEndian.PutUint32(body, uint32(len(data)))
	binary.LittleEndian.PutUint32(body[4:], uint32(len(data)))
	body[8] = 2
	binary.LittleEndian.PutUint32(body[9:], crc32.ChecksumIEEE(data))
	body[17] = 20
	body[18] = 0x30
	binary.LittleEndian.PutUint16(body[19:], uint16(len(name)))
	binary.LittleEndian.PutUint32(body[21:], 0x20)
	header(0x74, 0x8000, append(body, name...))
	b.Write(data)
	header(0x7b, 0, nil)
	count, size, err := extractArchive(context.Background(), stageFixture(t, b.Bytes()), t.TempDir(), ".rar", "")
	if err != nil || count != 1 || size != 5 {
		t.Fatal(count, size, err)
	}
	if _, _, err := extractArchive(context.Background(), stageFixture(t, []byte("not 7z")), t.TempDir(), ".7z", "secret"); err == nil {
		t.Fatal("accepted invalid 7z")
	}
}

func TestExtractSevenZipEncryptedHeadersAndFiles(t *testing.T) {
	// Tiny t2/t4 fixtures from bodgit/sevenzip v1.6.5 (BSD-3-Clause, see THIRD_PARTY_NOTICES.md).
	for _, encoded := range []string{
		"N3q8ryccAAQfQXHHwAAAAAAAAAAoAAAAAAAAALn7J1qgRMFFatX0e5vxiYNkCc0bOOTUmvi+KatqrtUARthD6ik2mQ2RgdM8A3HxtXiuzmUYq53Om8X6sE3kZ+A1br2Ylv2nvh3rUMcWgf1i+MC8UXkcAiQZme6XopM71m+OeNlu8lfFYkKp/EA4SKNOVdtinaYnj0Y6pRJQJhRTVRWX9XgSnN3fd0sFwKmndH7i0WMdM0gRC4Y6c4wSthZkV1kllHvYvgcInoSnXefRgKBVaQ34kCZCq0xo2g90+JSboO8XBiABCYCgAAcLAQABJAbxBwEKUwf0wep1D5nnYwyAlgoB8PBMOwAA",
		"N3q8ryccAAQZPwq/fQAAAAAAAAAgAAAAAAAAADfp0uy/yFjkFdadbTq6EDSfSOaTAACBMweuD87ysgwHsMPa919FipdTgilRmAEQAhLTPSSWedwNTLs1pIFAusGbXPphDu/LJSNENG45EQ990gX2H8aWbJqUvttxWjo0kngSGUx1uxodfPul72dl4+qZuzA/mOdWwQ4P3fgEgQAAABcGEAEJbQAHCwEAASMDAQEFXQAQAAAMdgoBVofKcwAA",
	} {
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		input := stageFixture(t, data)
		if count, _, err := extractArchive(context.Background(), input, t.TempDir(), ".7z", "password"); err != nil || count == 0 {
			t.Fatal(count, err)
		}
		for _, password := range []string{"", "wrong"} {
			if _, _, err := extractArchive(context.Background(), input, t.TempDir(), ".7z", password); err == nil {
				t.Fatal("accepted wrong 7z password")
			}
		}
	}
}
