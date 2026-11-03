package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func tempBudgetFixture(t *testing.T, remaining int64) (*App, string) {
	t.Helper()
	a := testApp(t)
	root := t.TempDir()
	a.store.update(func(st *State) error {
		st.TransferSettings = normalizedTransferSettings(TransferSettings{})
		st.TransferSettings.TempDirectory = root
		st.TransferSettings.TempMaxGiB = 1
		return nil
	})
	f, err := os.Create(filepath.Join(root, "retained-upload"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate((1 << 30) - remaining); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return a, root
}
func TestTransferTempDefaultsValidationAndPersistence(t *testing.T) {
	a := testApp(t)
	s := a.transferConfig()
	if s.TempMaxGiB != 100 {
		t.Fatal(s)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	for _, v := range []any{-1, 1.5, 1048577} {
		w := request(t, h, "PUT", "/api/transfer/settings", map[string]any{"version": 1, "tempMaxGiB": v}, cookie)
		if w.Code != 400 {
			t.Fatal(v, w.Code)
		}
	}
	s.TempMaxGiB = 23
	if w := request(t, h, "PUT", "/api/transfer/settings", s, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || reopened.state.TransferSettings.TempMaxGiB != 23 {
		t.Fatal(err)
	}
}
func TestTransferTempConcurrentReservationsAndRetainedFiles(t *testing.T) {
	a, root := tempBudgetFixture(t, 10)
	var wg sync.WaitGroup
	files := make(chan *transferTempFile, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if f, e := a.newTransferTemp(root, "job-*", 6); e == nil {
				files <- f
			}
		}()
	}
	wg.Wait()
	close(files)
	var accepted []*transferTempFile
	for f := range files {
		accepted = append(accepted, f)
	}
	if len(accepted) != 1 {
		t.Fatal("concurrent reservations exceeded aggregate cap", len(accepted))
	}
	f := accepted[0]
	if _, err := f.WriteString("123456"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := a.newTransferTemp(root, "job-*", 5); err == nil {
		t.Fatal("closed retained file escaped quota")
	}
	os.Remove(f.Name())
	next, err := a.newTransferTemp(root, "job-*", 10)
	if err != nil {
		t.Fatal("removed file not released", err)
	}
	next.Close()
}
func TestTransferTempAllGrowthPathsRejectBeforeWriting(t *testing.T) {
	for _, method := range []string{"write", "writeAt", "truncate", "string", "readFrom"} {
		t.Run(method, func(t *testing.T) {
			a, root := tempBudgetFixture(t, 8)
			f, err := a.newTransferTemp(root, "stream-*", -1)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			switch method {
			case "write":
				_, err = f.Write([]byte("123456789"))
			case "writeAt":
				_, err = f.WriteAt([]byte("x"), 8)
			case "truncate":
				err = f.Truncate(9)
			case "string":
				_, err = f.WriteString("123456789")
			case "readFrom":
				_, err = f.ReadFrom(bytes.NewBufferString("123456789"))
			}
			if err == nil || !strings.Contains(err.Error(), "上限") {
				t.Fatal("growth bypass", err)
			}
			info, _ := f.Stat()
			if info.Size() != 0 {
				t.Fatal("over-limit write mutated file", info.Size())
			}
			if _, err = io.Copy(f, strings.NewReader("12345678")); err != nil {
				t.Fatal("exact boundary rejected", err)
			}
		})
	}
}

func TestTransferTempReducedLimitBlocksReservedGrowth(t *testing.T) {
	a, root := tempBudgetFixture(t, 0)
	a.store.update(func(st *State) error { st.TransferSettings.TempMaxGiB = 2; return nil })
	f, err := a.newTransferTemp(root, "reserved-*", 10)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	a.store.update(func(st *State) error { st.TransferSettings.TempMaxGiB = 1; return nil })
	if _, err = f.WriteString("x"); err == nil {
		t.Fatal("old reservation bypassed lower limit")
	}
	info, _ := f.Stat()
	if info.Size() != 0 {
		t.Fatal("over-limit file grew")
	}
}
