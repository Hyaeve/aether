//go:build !linux

package app

import "errors"

func recoverDisconnectedFuse(_ MountConfig) error {
	return errors.New("FUSE挂载恢复仅适用于Linux")
}

func startNativeFuse(_ *App, _ MountConfig) (fuseMountServer, error) {
	return nil, errors.New("FUSE 挂载仅在 Linux 容器中运行")
}
