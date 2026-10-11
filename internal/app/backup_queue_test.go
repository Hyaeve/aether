package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func backupQueueEntries(count int) []backupEntry {
	items := make([]backupEntry, count)
	for i := range items {
		items[i] = backupEntry{source: Storage{ID: "pool", Config: map[string]string{"token": "private-token", "cookie": "private-cookie"}}, file: File{ID: strings.Repeat("x", i+1), Name: "movie.mkv", Size: int64(i)}, relative: "Movies/movie.mkv", parent: "/Movies"}
	}
	return items
}

func TestBackupManifestFIFOUpperLowerAndRepeatedRun(t *testing.T) {
	for _, tc := range []struct{ upper, lower, peak, refills int }{{3, 1, 3, 5}, {4, 2, 4, 5}, {4, 1, 4, 4}, {0, 0, 11, 1}, {1, 0, 1, 11}} {
		t.Run(strings.Repeat("x", tc.upper)+string(rune('0'+tc.lower)), func(t *testing.T) {
			a := testApp(t)
			items := backupQueueEntries(11)
			m, err := newBackupManifest(context.Background(), a, items)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			for round := range 2 {
				var got []backupEntry
				err = m.Run(context.Background(), BackupResources{QueueUpper: tc.upper, QueueLower: tc.lower}, func(item backupEntry) error {
					pending := 0
					for _, slot := range m.pending {
						if slot.source.Config != nil {
							pending++
						}
					}
					if pending >= tc.peak {
						t.Fatal("consumed descriptor was not released")
					}
					got = append(got, item)
					return nil
				})
				if err != nil || !reflect.DeepEqual(got, items) {
					t.Fatalf("round %d FIFO: %v", round, err)
				}
				if m.peak != tc.peak || m.refills != tc.refills {
					t.Fatalf("bounds peak/refills=%d/%d expected %d/%d", m.peak, m.refills, tc.peak, tc.refills)
				}
				if m.pending != nil {
					t.Fatal("payload queue retained after run")
				}
			}
		})
	}
}

func TestBackupManifestSyncCodecTwoPassesAndEncryption(t *testing.T) {
	a := testApp(t)
	entry := backupQueueEntries(1)[0]
	items := []syncOperation{{from: 1, to: 2, name: "new.mkv", file: syncFile{backupEntry: entry, digest: "private-sha256"}}, {from: 2, to: 0, name: "old-dir", file: syncFile{backupEntry: entry, digest: "other-digest"}, remove: true, directory: true}}
	m, err := newBackupManifest(context.Background(), a, items)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	raw, err := os.ReadFile(filepath.Join(a.dataDir, m.name))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-token", "private-cookie", "private-sha256", "movie.mkv", "new.mkv"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("plaintext leaked: %s", secret)
		}
	}
	info, err := m.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 && os.PathSeparator != '\\' {
		t.Fatal("queue file permissions too open")
	}
	for range 2 {
		var got []syncOperation
		err = m.Run(context.Background(), BackupResources{QueueUpper: 1}, func(item syncOperation) error { got = append(got, item); return nil })
		if err != nil || !reflect.DeepEqual(items, got) {
			t.Fatalf("sync codec: %v %+v", err, got)
		}
	}
	other, err := newBackupManifest(context.Background(), a, items)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	rawOther, err := os.ReadFile(filepath.Join(a.dataDir, other.name))
	if err != nil || bytes.Equal(raw, rawOther) {
		t.Fatal("random nonce/manifest identity missing", err)
	}
}

func TestBackupManifestReleasesInputSliceAndCleanup(t *testing.T) {
	a := testApp(t)
	items := backupQueueEntries(3)
	m, err := newBackupManifest(context.Background(), a, items)
	if err != nil {
		t.Fatal(err)
	}
	items[0].source.Config["token"] = "changed-input"
	clear(items)
	items = nil
	seen := 0
	err = m.Run(context.Background(), BackupResources{QueueUpper: 2}, func(item backupEntry) error {
		if item.source.Config["token"] != "private-token" {
			t.Fatal("retained input payload")
		}
		seen++
		return nil
	})
	if err != nil || seen != 3 {
		t.Fatal(seen, err)
	}
	filename := filepath.Join(a.dataDir, m.name)
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal("idempotent close", err)
	}
	if _, err = os.Stat(filename); !os.IsNotExist(err) {
		t.Fatal("manifest not removed", err)
	}
	if err = m.Run(context.Background(), BackupResources{}, func(backupEntry) error { return nil }); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed manifest accepted", err)
	}
}

func TestBackupManifestTamperRejectedBeforeConsume(t *testing.T) {
	for _, kind := range []string{"header", "ciphertext", "length", "truncate", "append", "swap"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			items := backupQueueEntries(2)
			items[1] = items[0]
			m, err := newBackupManifest(context.Background(), a, items)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			filename := filepath.Join(a.dataDir, m.name)
			raw, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "header":
				raw[8] ^= 1
			case "ciphertext":
				raw[len(raw)-1] ^= 1
			case "length":
				binary.BigEndian.PutUint32(raw[len(m.header):], ^uint32(0))
			case "truncate":
				raw = raw[:len(raw)-1]
			case "append":
				raw = append(raw, 0)
			case "swap":
				start := len(m.header)
				size := 4 + int(binary.BigEndian.Uint32(raw[start:start+4]))
				a, b := append([]byte{}, raw[start:start+size]...), append([]byte{}, raw[start+size:]...)
				copy(raw[start:], b)
				copy(raw[start+len(b):], a)
			}
			if err = os.WriteFile(filename, raw, 0600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			err = m.Run(context.Background(), BackupResources{QueueUpper: 1}, func(backupEntry) error { calls++; return nil })
			if err == nil || calls != 0 {
				t.Fatal("tampered manifest consumed", kind, calls, err)
			}
		})
	}
}

func TestBackupManifestEmptyAndCancelledRunWait(t *testing.T) {
	a := testApp(t)
	empty, err := newBackupManifest[backupEntry](context.Background(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	for range 2 {
		if err := empty.Run(context.Background(), BackupResources{QueueUpper: 2}, func(backupEntry) error { t.Fatal("empty consumed"); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	m, err := newBackupManifest(context.Background(), a, backupQueueEntries(3))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		first := true
		done <- m.Run(context.Background(), BackupResources{QueueUpper: 1}, func(backupEntry) error {
			if first {
				first = false
				close(started)
				<-release
			}
			return nil
		})
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Run(ctx, BackupResources{}, func(backupEntry) error { t.Fatal("cancelled waiter consumed"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("active run did not finish")
	}
}

func TestBackupManifestCancellationFailureAndLimits(t *testing.T) {
	a := testApp(t)
	before, err := os.ReadDir(a.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = newBackupManifest(ctx, a, backupQueueEntries(1)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled construction", err)
	}
	oversized := backupQueueEntries(1)
	oversized[0].relative = strings.Repeat("a", backupManifestRecordLimit+1)
	if _, err = newBackupManifest(context.Background(), a, oversized); err == nil {
		t.Fatal("oversized record accepted")
	}
	if _, err = newBackupManifest(context.Background(), a, []int{1}); err == nil {
		t.Fatal("unsupported type accepted")
	}
	after, err := os.ReadDir(a.dataDir)
	if err != nil || len(before) != len(after) {
		t.Fatal("failed construction leaked file", err)
	}
	m, err := newBackupManifest(context.Background(), a, backupQueueEntries(5))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err = m.Run(ctx, BackupResources{}, func(backupEntry) error { t.Fatal("cancelled consumer"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, config := range []BackupResources{{QueueUpper: -1}, {QueueUpper: 2, QueueLower: 2}, {QueueUpper: 0, QueueLower: 1}} {
		if err = m.Run(context.Background(), config, func(backupEntry) error { t.Fatal("invalid limit consumed"); return nil }); err == nil {
			t.Fatal("invalid limits accepted")
		}
	}
	ctx, cancel = context.WithCancel(context.Background())
	calls := 0
	err = m.Run(ctx, BackupResources{QueueUpper: 3}, func(backupEntry) error { calls++; cancel(); return nil })
	if !errors.Is(err, context.Canceled) || calls != 1 || m.pending != nil {
		t.Fatal("cancellation retained payload", calls, err)
	}
	want := errors.New("consumer failed")
	if err = m.Run(context.Background(), BackupResources{QueueUpper: 2}, func(backupEntry) error { return want }); !errors.Is(err, want) || m.pending != nil {
		t.Fatal("consumer error retained payload", err)
	}
}
