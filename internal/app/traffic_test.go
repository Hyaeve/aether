package app

import (
	"context"
	"testing"
	"time"
)

func TestTrafficWindow(t *testing.T) {
	var m trafficMeter
	now := time.Unix(100, 0)
	m.add("upload", 4096, now)
	m.add("download", 8192, now)
	m.add("copy", 9999, now)
	if r := m.rates(now); r["uploadRate"] != 2048 || r["downloadRate"] != 4096 {
		t.Fatal(r)
	}
	if r := m.rates(now.Add(2 * time.Second)); r["uploadRate"] != 0 || r["downloadRate"] != 0 {
		t.Fatal(r)
	}
	m.add("download", 2048, now.Add(3*time.Second))
	if r := m.rates(now.Add(3 * time.Second)); r["downloadRate"] != 1024 {
		t.Fatal(r)
	}
}

func TestTrafficProgressAndAuth(t *testing.T) {
	a := testApp(t)
	p := a.beginTransfer(context.Background(), "upload", Storage{}, "file", 4096)
	p.add(4096)
	if a.traffic.rates(time.Now())["uploadRate"] != 2048 {
		t.Fatal("active upload not counted")
	}
	p.finish(nil)
	if a.traffic.rates(time.Now())["uploadRate"] != 2048 {
		t.Fatal("completion counted twice")
	}
	if w := request(t, a.Handler(t.TempDir()), "GET", "/api/traffic", nil, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
}
