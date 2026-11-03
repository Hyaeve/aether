package app

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// Only ENOTCONN on this exact configured Aether mount authorizes a detach.
// A healthy mount, another FUSE provider, stacked mount or changed ID is retained.
func recoverDisconnectedFuse(mount MountConfig) error {
	point := mountTarget(mount)
	read := func() (fuseMountIdentity, bool, error) {
		raw, err := os.ReadFile("/proc/self/mountinfo")
		if err != nil {
			return fuseMountIdentity{}, false, err
		}
		return parseFuseMountIdentity(string(raw), point)
	}
	entry, exists, err := read()
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if !ownFuseMount(entry, mount) {
		return errors.New("挂载点由其他挂载占用，未执行恢复")
	}
	var info syscall.Statfs_t
	if err = syscall.Statfs(point, &info); !errors.Is(err, syscall.ENOTCONN) {
		if err == nil {
			return errors.New("挂载仍在运行，未卸载")
		}
		return fmt.Errorf("挂载状态未确认，未卸载：%w", err)
	}
	current, exists, err := read()
	if err != nil || !exists || current != entry {
		return errors.New("挂载身份已变化，未卸载")
	}
	if err = syscall.Unmount(point, syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("清除已断连的自身FUSE挂载失败：%w", err)
	}
	_, exists, err = read()
	if err != nil || exists {
		return errors.New("旧FUSE挂载移除未确认，未重新挂载")
	}
	return nil
}
