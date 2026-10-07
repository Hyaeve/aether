package app

import (
	"path"
	"regexp"
	"strings"
)

var scrapeSeasonDir = regexp.MustCompile(`(?i)^(?:season[ ._-]*|s)\d+$`)

func recognizeSTRMPath(name string) scrapeItem {
	item := recognizeSTRM(path.Base(name))
	if item.Title == "" {
		dir := path.Dir(name)
		if scrapeSeasonDir.MatchString(path.Base(dir)) {
			dir = path.Dir(dir)
		}
		if dir != "." {
			parent := recognizeSTRM(path.Base(dir) + ".strm")
			item.Title, item.Year = parent.Title, parent.Year
		}
	}
	return item
}

type scrapeWork struct {
	scrapeItem
	Count int `json:"count"`
}

func scrapeWorkKey(item scrapeItem) string {
	dir := path.Dir(item.Path)
	if item.Kind == "tv" && scrapeSeasonDir.MatchString(path.Base(dir)) {
		dir = path.Dir(dir)
	}
	original := recognizeSTRMPath(item.Path)
	title := normalizedTitle(original.Title)
	if title == "" {
		return item.Path
	}
	return strings.Join([]string{dir, item.Kind, title, original.Year}, "\x00")
}

func scrapeWorks(items []scrapeItem) []scrapeWork {
	result := []scrapeWork{}
	indices := map[string]int{}
	priority := map[string]int{"ok": 0, "pending": 1, "miss": 2, "doubt": 3, "error": 4}
	for _, item := range items {
		key := scrapeWorkKey(item)
		if i, ok := indices[key]; ok {
			result[i].Count++
			if priority[item.Status] > priority[result[i].Status] {
				result[i].Status, result[i].Message = item.Status, item.Message
			}
			if result[i].Poster == "" {
				result[i].Poster = item.Poster
			}
		} else {
			indices[key] = len(result)
			result = append(result, scrapeWork{item, 1})
		}
	}
	return result
}
