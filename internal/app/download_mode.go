package app

import (
	"strconv"
	"time"
)

func davTimeout(s Storage) time.Duration {
	n, err := strconv.Atoi(s.Config["timeoutSeconds"])
	if err != nil || n < 1 || n > 600 {
		n = 60
	}
	return time.Duration(n) * time.Second
}

// FUSE/DAV read bytes through download directly; only browser/STRM routes apply
// this policy. Quark TV is a separate playback-only override.
func storageRedirect(s Storage) bool {
	switch s.Type {
	case "115", "mobile", "tianyi", "openlist", "webdav":
		return s.Config["downloadMode"] != "proxy"
	default:
		return false
	}
}
