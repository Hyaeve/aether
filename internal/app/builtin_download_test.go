package app

import (
	"bytes"
	"context"
	"crypto/sha1"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

func TestBuiltinHTTPDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/payload", http.StatusFound)
		case "/payload":
			w.Header().Set("Content-Disposition", `attachment; filename="book.m4a"`)
			_, _ = w.Write([]byte("audio"))
		case "/unsafe":
			w.Header().Set("Content-Disposition", `attachment; filename="../escape"`)
			_, _ = w.Write([]byte("safe"))
		case "/truncated":
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("short"))
		default:
			http.Error(w, "denied", http.StatusForbidden)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	if err := downloadBuiltinHTTP(context.Background(), dir, server.URL+"/redirect"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "book.m4a"))
	if err != nil || string(data) != "audio" {
		t.Fatalf("%q %v", data, err)
	}
	if err := downloadBuiltinHTTP(context.Background(), dir, server.URL+"/payload"); err == nil {
		t.Fatal("overwrote file")
	}
	if err := downloadBuiltinHTTP(context.Background(), dir, server.URL+"/unsafe"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "download.bin")); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/truncated", "/forbidden"} {
		temp := t.TempDir()
		if err := downloadBuiltinHTTP(context.Background(), temp, server.URL+endpoint); err == nil {
			t.Fatal("accepted", endpoint)
		}
		entries, _ := os.ReadDir(temp)
		if len(entries) != 0 {
			t.Fatal("partial file published")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := downloadBuiltinHTTP(ctx, t.TempDir(), server.URL+"/payload"); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestBuiltinTorrentWebseed(t *testing.T) {
	payload := bytes.Repeat([]byte("aether-bt-test"), 1000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "book.bin", time.Time{}, bytes.NewReader(payload))
	}))
	defer server.Close()
	hash := sha1.Sum(payload)
	info := metainfo.Info{Name: "book.bin", Length: int64(len(payload)), PieceLength: 16384, Pieces: hash[:]}
	raw, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: raw, UrlList: []string{server.URL + "/book.bin"}}
	seed := filepath.Join(t.TempDir(), "book.torrent")
	f, err := os.Create(seed)
	if err != nil {
		t.Fatal(err)
	}
	err = mi.Write(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	cfg := builtinTorrentConfig(output)
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.DisablePEX = true
	cfg.DisableUTP = true
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := downloadBuiltinTorrentWithConfig(ctx, seed, true, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(output, "book.bin"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("BT payload mismatch: %v", err)
	}
	entries, _ := os.ReadDir(output)
	if len(entries) != 1 {
		t.Fatalf("torrent internals leaked into upload tree: %v", entries)
	}
}

func TestBuiltinTorrentValidation(t *testing.T) {
	for _, raw := range []string{"ftp://example.com/a", "ed2k://a", "magnet:?xt=broken", "https:///missing-host"} {
		if validateBuiltinURL(raw) == nil {
			t.Fatal("accepted", raw)
		}
	}
	for _, info := range []metainfo.Info{
		{Name: "../escape", Length: 1},
		{Name: "book", Length: builtinDownloadLimit + 1},
		{Name: "book", Files: []metainfo.FileInfo{{Path: []string{"..", "escape"}, Length: 1}}},
		{Name: "book", Length: -1},
	} {
		if validateTorrentInfo(&info) == nil {
			t.Fatal("unsafe torrent accepted")
		}
	}
}
