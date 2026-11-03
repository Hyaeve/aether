package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMountRecoveryExactKernelIdentity(t *testing.T) {
	m := MountConfig{Name: "media", MountPoint: "/NetDisk"}
	line := "52 40 0:91 / /NetDisk/AetherDrive rw,nosuid shared:19 - fuse.aether aether:media rw,user_id=0,group_id=0"
	entry, ok, err := parseFuseMountIdentity(line, mountTarget(m))
	// Windows filesystem paths use a different separator; the parser is tested
	// with the Linux kernel path, while ownership uses this platform's target.
	if !ok || err != nil {
		entry, ok, err = parseFuseMountIdentity(line, "/NetDisk/AetherDrive")
	}
	if !ok || err != nil || entry.ID != "52" || entry.Kind != "fuse.aether" {
		t.Fatal(entry, ok, err)
	}
	entry.Point = mountTarget(m)
	if !ownFuseMount(entry, m) {
		t.Fatal("own mount rejected")
	}
	other := entry
	other.Source = "aether:other"
	if ownFuseMount(other, m) {
		t.Fatal("another task mount accepted")
	}
	other = entry
	other.Kind = "fuse.rclone"
	if ownFuseMount(other, m) {
		t.Fatal("another fuse provider accepted")
	}
	if _, _, err := parseFuseMountIdentity(line+"\n"+strings.Replace(line, "52 40", "53 40", 1), "/NetDisk/AetherDrive"); err == nil {
		t.Fatal("stacked mounts accepted")
	}
	escaped := strings.ReplaceAll(line, "/NetDisk/AetherDrive", `/Net\040Disk/AetherDrive`)
	if _, ok, err := parseFuseMountIdentity(escaped, "/Net Disk/AetherDrive"); !ok || err != nil {
		t.Fatal(ok, err)
	}
}

type exitFuseServer struct{ exit chan struct{} }

func (s *exitFuseServer) Wait()          { <-s.exit }
func (s *exitFuseServer) Unmount() error { return nil }

func TestMountRecoveryUnexpectedExitIsNotReportedMounted(t *testing.T) {
	a := testApp(t)
	mount := MountConfig{ID: "exited", Name: "断连测试", MountPoint: t.TempDir()}
	_ = a.store.update(func(st *State) error { st.Mounts = []MountConfig{mount}; return nil })
	server := &exitFuseServer{exit: make(chan struct{})}
	p := &mountProcess{server: server, done: make(chan struct{}), point: mountTarget(mount)}
	a.mounts.active[mount.ID] = p
	go a.mounts.waitMount(mount.ID, mount, p)
	close(server.exit)
	select {
	case <-p.done:
	case <-time.After(time.Second):
		t.Fatal("exit was not observed")
	}
	deadline := time.Now().Add(time.Second)
	for {
		a.mounts.mu.Lock()
		message := a.mounts.failures[mount.ID]
		a.mounts.mu.Unlock()
		if message != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("missing disconnected diagnostic")
		}
		time.Sleep(time.Millisecond)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "GET", "/api/mounts", nil, cookie)
	var got []MountConfig
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if len(got) != 1 || got[0].Status != "error" || !strings.Contains(got[0].LastError, "FUSE连接已断开") {
		t.Fatal(w.Code, w.Body.String())
	}
	a.mounts.mu.Lock()
	err := a.mounts.stopLocked(mount.ID)
	a.mounts.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if a.mounts.active[mount.ID] != nil {
		t.Fatal("dead mount cannot be released")
	}
}
