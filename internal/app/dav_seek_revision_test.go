package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
)

type seekRevisionBody struct {
	io.Reader
	closed bool
}

func (b *seekRevisionBody) Close() error { b.closed = true; return nil }

func TestDAVCloudSeekReopensRangeForwardAndBackward(t *testing.T) {
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	const content = "0123456789abcdef"
	var bodies []*seekRevisionBody
	var ranges []string
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		ranges = append(ranges, r.Header.Get("Range"))
		offset := 0
		status := 200
		if value := r.Header.Get("Range"); value != "" {
			var err error
			offset, err = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(value, "bytes="), "-"))
			if err != nil {
				t.Fatal(err)
			}
			status = 206
		}
		body := &seekRevisionBody{Reader: strings.NewReader(content[offset:])}
		bodies = append(bodies, body)
		return &http.Response{StatusCode: status, Body: body, Header: http.Header{}, Request: r}, nil
	})}
	f := &davFile{ctx: context.Background(), info: davInfo{File{Name: "book.epub", Size: int64(len(content))}}, download: Download{URL: "https://cdn.invalid/book?token=secret"}}
	defer f.Close()
	read := func(want string) {
		t.Helper()
		buf := make([]byte, len(want))
		n, err := f.Read(buf)
		if err != nil || n != len(want) || string(buf) != want {
			t.Fatal(string(buf), n, err)
		}
	}
	read("01")
	for _, tc := range []struct {
		offset int64
		whence int
		want   string
	}{{8, io.SeekStart, "89"}, {2, io.SeekStart, "23"}, {2, io.SeekCurrent, "67"}, {-2, io.SeekEnd, "ef"}, {0, io.SeekStart, "01"}} {
		previous := bodies[len(bodies)-1]
		if _, err := f.Seek(tc.offset, tc.whence); err != nil {
			t.Fatal(err)
		}
		if !previous.closed || f.body != nil {
			t.Fatal("seek retained old body")
		}
		read(tc.want)
	}
	if got := strings.Join(ranges, ","); got != ",bytes=8-,bytes=2-,bytes=6-,bytes=14-," {
		t.Fatal(got)
	}
	previous, offset := f.body, f.offset
	if _, err := f.Seek(-1, io.SeekStart); err == nil || f.body != previous || f.offset != offset {
		t.Fatal("invalid seek changed stream")
	}
}

func TestDAVReadZeroLengthAndDirectories(t *testing.T) {
	opens := 0
	f := &davFile{info: davInfo{File{Name: "book.epub"}}, open: func() error { opens++; return errors.New("must not open") }}
	if n, err := f.Read(nil); n != 0 || err != nil || opens != 0 {
		t.Fatal(n, err, opens)
	}
	dir := &davFile{info: davInfo{File{IsDir: true}}, open: f.open}
	for _, buf := range [][]byte{nil, make([]byte, 1)} {
		if _, err := dir.Read(buf); !errors.Is(err, os.ErrInvalid) || opens != 0 {
			t.Fatal(err, opens)
		}
	}
	if _, err := dir.Seek(0, io.SeekStart); !errors.Is(err, os.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestDAVRangeFailureHasSafeContext(t *testing.T) {
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	for _, status := range []int{200, 403, 416, 503} {
		body := &seekRevisionBody{Reader: strings.NewReader("error")}
		apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Range") != "bytes=7-" {
				t.Fatal("missing range")
			}
			return &http.Response{StatusCode: status, Body: body, Header: http.Header{}, Request: r}, nil
		})}
		f := &davFile{ctx: context.Background(), info: davInfo{File{Size: 100}}, offset: 7, download: Download{URL: "https://cdn.invalid/book?token=secret"}}
		_, err := f.Read(make([]byte, 1))
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status)) || !strings.Contains(err.Error(), "7") || strings.Contains(err.Error(), "secret") || !body.closed {
			t.Fatal(err)
		}
	}
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) { return nil, errors.New("network secret") })}
	f := &davFile{ctx: context.Background(), info: davInfo{File{Size: 100}}, offset: 7, download: Download{URL: "https://cdn.invalid/book?token=secret"}}
	if _, err := f.Read(make([]byte, 1)); err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "7") {
		t.Fatal(err)
	}
}
