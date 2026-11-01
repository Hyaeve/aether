package app

import (
	"path"
	"strings"
)

const shareSTRMSuffix = ".share.strm"

func isShareSTRM(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), shareSTRMSuffix)
}

func shareSTRMSourceName(name string) string {
	if isShareSTRM(name) {
		return name[:len(name)-len(shareSTRMSuffix)]
	}
	return name
}

func quarkLibraryExcluded(card quarkLibraryCard, directory, name string) bool {
	return excluded(directory, card.ExcludeDirs) || excluded(name, card.ExcludeFiles) || excludedType(name, card.ExcludeTypes)
}

func scrapeMediaStem(name string) string {
	title := strings.TrimSuffix(name, path.Ext(name))
	if ext := strings.ToLower(path.Ext(title)); ext == ".cas" || ext == ".ed2k" || ext == ".share" {
		title = strings.TrimSuffix(title, path.Ext(title))
	}
	if isVideo(title) {
		title = strings.TrimSuffix(title, path.Ext(title))
	}
	return title
}
