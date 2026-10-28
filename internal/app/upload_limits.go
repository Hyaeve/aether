package app

import "math"

const defaultUploadFileLimit int64 = 100 << 30
const mobilePCUploadFileLimit int64 = 500_000_000_000

func mobilePCClient(s Storage) bool {
	return nativeMobile(s) && (s.Config["clientType"] == "" || s.Config["clientType"] == "pc")
}

func uploadFileLimit(s Storage) int64 {
	if s.Type == "local" {
		// Local streaming is bounded by the filesystem and free space, not a
		// cloud protocol limit. Leave room for limit+1 completeness checks.
		return math.MaxInt64 - 1
	}
	if mobilePCClient(s) {
		return mobilePCUploadFileLimit
	}
	return defaultUploadFileLimit
}

func uploadLimitLabel(limit int64) string {
	if limit == math.MaxInt64-1 {
		return "本地文件系统可用范围"
	}
	if limit == mobilePCUploadFileLimit {
		return "500 GB"
	}
	return "100 GiB"
}
