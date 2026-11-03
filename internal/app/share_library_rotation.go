package app

import "time"

// Rotate the eligible pool, retaining the remaining accounts as ordered fallbacks.
func rotateLibraryStorages(storages []Storage, hours int, now time.Time) []Storage {
	if len(storages) < 2 {
		return storages
	}
	if hours < 1 {
		hours = 24
	}
	start := int(now.Unix() / int64(hours*3600) % int64(len(storages)))
	if start < 0 {
		start += len(storages)
	}
	result := make([]Storage, 0, len(storages))
	result = append(result, storages[start:]...)
	return append(result, storages[:start]...)
}
