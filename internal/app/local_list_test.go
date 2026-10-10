package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

// Keep the pre-optimization local branch as a parity and benchmark baseline.
func legacyLocalList(s Storage, dir string) ([]File, error) {
	name, err := relative(dir)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.Config["root"])
	if err != nil {
		return nil, errors.New("无法打开本地根目录")
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := []File{}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, File{ID: path.Join("/", dir, entry.Name()), Name: entry.Name(), IsDir: entry.IsDir(), Size: info.Size(), Modified: info.ModTime()})
	}
	return out, nil
}

func TestLocalRawListMetadataParity(t *testing.T) {
	root := t.TempDir()
	s := Storage{Type: "local", Config: map[string]string{"root": root}}
	for _, dir := range []string{"empty", "nested", "nested/folder"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < localListBatchSize*2+17; i++ {
		name := filepath.Join(root, "nested", fmt.Sprintf("file-%04d.txt", i))
		if err := os.WriteFile(name, []byte("metadata"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Date(2020, 4, 5, 6, 7, 8, 0, time.UTC)
	for _, name := range []string{"nested/folder", "nested/file-0000.txt"} {
		if err := os.Chtimes(filepath.Join(root, name), stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"/", "/empty", "/nested", "nested/", "nested\\folder"} {
		t.Run(dir, func(t *testing.T) {
			want, err := legacyLocalList(s, dir)
			if err != nil {
				t.Fatal(err)
			}
			got, err := localRawList(context.Background(), s, dir)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("changed IDs, order, directory/file metadata, or empty slice semantics")
			}
			for _, file := range got {
				if file.Name == "folder" || file.Name == "file-0000.txt" {
					if !file.Modified.Equal(stamp) {
						t.Fatalf("lost modification time: %+v", file)
					}
				}
			}
		})
	}
}

func TestLocalRawListBoundaries(t *testing.T) {
	root := t.TempDir()
	s := Storage{Type: "local", Config: map[string]string{"root": root}}
	if err := os.WriteFile(filepath.Join(root, "plain.txt"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"../", "/../outside", "..\\outside", "C:/Windows", "/missing", "/plain.txt"} {
		if got, err := localRawList(context.Background(), s, dir); err == nil || got != nil {
			t.Fatalf("accepted invalid directory %q: %v, %v", dir, got, err)
		}
	}
	s.Config["root"] = filepath.Join(root, "missing")
	if _, err := localRawList(context.Background(), s, "/"); err == nil || err.Error() != "无法打开本地根目录" {
		t.Fatalf("changed root error: %v", err)
	}
}

func TestLocalRawListSymlinkSafety(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	s := Storage{Type: "local", Config: map[string]string{"root": root}}
	if err := os.Mkdir(filepath.Join(root, "inside"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inside", "safe.txt"), []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, link := range []struct{ name, target string }{
		{"escape", outside}, {"internal", "inside"}, {"broken", "absent"}, {"file-link", "inside/safe.txt"},
	} {
		if err := os.Symlink(link.target, filepath.Join(root, link.name)); err != nil {
			t.Skipf("platform cannot create symlinks: %v", err)
		}
	}
	got, err := localRawList(context.Background(), s, "/")
	if err != nil || len(got) != 1 || got[0].Name != "inside" {
		t.Fatalf("listed symlink: %v, %v", got, err)
	}
	if _, err := localRawList(context.Background(), s, "/escape"); err == nil {
		t.Fatal("escaped os.Root")
	}
	got, err = localRawList(context.Background(), s, "/internal")
	if err != nil || len(got) != 1 || got[0].ID != "/internal/safe.txt" {
		t.Fatalf("changed safe internal symlink traversal: %v, %v", got, err)
	}
}

type localListCancelContext struct {
	context.Context
	checks int
}

func (c *localListCancelContext) Err() error {
	c.checks++
	if c.checks >= 4 {
		return context.Canceled
	}
	return nil
}

func TestLocalRawListCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if files, err := localRawList(ctx, Storage{}, "/"); !errors.Is(err, context.Canceled) || files != nil {
		t.Fatalf("ignored initial cancellation: %v, %v", files, err)
	}
	root := t.TempDir()
	for i := 0; i < localListBatchSize+1; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%04d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	c := &localListCancelContext{Context: context.Background()}
	s := Storage{Type: "local", Config: map[string]string{"root": root}}
	if files, err := localRawList(c, s, "/"); !errors.Is(err, context.Canceled) || files != nil {
		t.Fatalf("returned partial list after cancellation: %v, %v", files, err)
	}
	if _, err := localRawList(context.Background(), s, "/"); err != nil {
		t.Fatalf("subsequent listing failed: %v", err)
	}
}

func TestLocalRawListFreshMetadata(t *testing.T) {
	root := t.TempDir()
	s := Storage{Type: "local", Config: map[string]string{"root": root}}
	name := filepath.Join(root, "file.txt")
	if err := os.WriteFile(name, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := localRawList(context.Background(), s, "/"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(name, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	got, err := localRawList(context.Background(), s, "/")
	if err != nil || len(got) != 1 || got[0].Size != 7 || !got[0].Modified.Equal(stamp) {
		t.Fatalf("stale metadata: %v, %v", got, err)
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	got, err = localRawList(context.Background(), s, "/")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("stale deleted entry: %v, %v", got, err)
	}
}

func BenchmarkLocalRawList(b *testing.B) {
	for _, count := range []int{256, 4096} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			root := b.TempDir()
			s := Storage{Type: "local", Config: map[string]string{"root": root}}
			for i := 0; i < count; i++ {
				if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%05d.txt", i)), []byte("test"), 0600); err != nil {
					b.Fatal(err)
				}
			}
			variants := []struct {
				name string
				list func() ([]File, error)
			}{
				{"legacy", func() ([]File, error) { return legacyLocalList(s, "/") }},
				{"batched", func() ([]File, error) { return localRawList(context.Background(), s, "/") }},
			}
			// Ensure both variants return the same data before timing them.
			want, _ := variants[0].list()
			got, _ := variants[1].list()
			sort.Slice(want, func(i, j int) bool { return want[i].ID < want[j].ID })
			sort.Slice(got, func(i, j int) bool { return got[i].ID < got[j].ID })
			if !reflect.DeepEqual(got, want) || len(got) != count {
				b.Fatal("benchmark outputs differ")
			}
			for _, v := range variants {
				b.Run(v.name, func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						files, err := v.list()
						if err != nil || len(files) != count {
							b.Fatalf("list failed: %d, %v", len(files), err)
						}
					}
				})
			}
		})
	}
}
