package app

const defaultUploadFileLimit int64 = 100 << 30
const mobilePCUploadFileLimit int64 = 500_000_000_000

func mobilePCClient(s Storage) bool {
	return nativeMobile(s) && (s.Config["clientType"] == "" || s.Config["clientType"] == "pc")
}

func uploadFileLimit(s Storage) int64 {
	if mobilePCClient(s) {
		return mobilePCUploadFileLimit
	}
	return defaultUploadFileLimit
}

func uploadLimitLabel(limit int64) string {
	if limit == mobilePCUploadFileLimit {
		return "500 GB"
	}
	return "100 GiB"
}
