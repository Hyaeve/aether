package app

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

var scrapeSeasonDir = regexp.MustCompile(`(?i)^(?:season[ ._-]*|s)(\d{1,3})$`)
var scrapeChineseSeason = regexp.MustCompile(`第([零〇一二两三四五六七八九十百\d]+)季`)
var scrapeChineseEpisode = regexp.MustCompile(`第([零〇一二两三四五六七八九十百\d]+)[集话話]`)
var scrapeEpisodeOnly = regexp.MustCompile(`(?i)(?:^|[ ._\-\[])E(?:P(?:ISODE)?)?[ ._-]*(\d{1,4})(?:$|[^a-z0-9])`)
var scrapeCrossEpisode = regexp.MustCompile(`(?i)(?:^|[ ._\-\[])(\d{1,2})x(\d{1,4})(?:$|[^a-z0-9])`)
var scrapeNumericEpisode = regexp.MustCompile(`^(\d{1,3})(?:[ ._-].*)?$`)
var scrapeTrailingEpisode = regexp.MustCompile(`^(.*?)[ ._-]+(\d{1,3})$`)

// Bare episode numbers need corroboration from siblings, not a single numeric
// movie title or an audio/resolution token.
func inferScrapeSiblings(items []scrapeItem) {
	groups := map[string][]int{}
	for i, item := range items {
		groups[path.Dir(item.Path)] = append(groups[path.Dir(item.Path)], i)
	}
	for dir, indices := range groups {
		if genericMediaDir(path.Base(dir)) {
			continue
		}
		numbers := map[int]int{}
		tv := false
		for _, i := range indices {
			item := items[i]
			tv = tv || item.Kind == "tv"
			stem := strings.TrimSuffix(path.Base(item.Path), path.Ext(item.Path))
			if isVideo(stem) {
				stem = strings.TrimSuffix(stem, path.Ext(stem))
			}
			stem = strings.Trim(scrapeSuffix.ReplaceAllString(stem, ""), " ._-")
			if m := scrapeNumericEpisode.FindStringSubmatch(stem); m != nil {
				numbers[i] = scrapeNumber(m[1])
			} else if m := scrapeTrailingEpisode.FindStringSubmatch(stem); m != nil && normalizedTitle(m[1]) == normalizedTitle(recognizeSTRM(path.Base(dir)+".strm").Title) {
				numbers[i] = scrapeNumber(m[2])
			}
		}
		if !tv && (len(numbers) < 2 || len(numbers) != len(indices)) {
			continue
		}
		parent := recognizeSTRM(path.Base(scrapeWorkDir(items[indices[0]].Path)) + ".strm")
		for i, episode := range numbers {
			if items[i].Manual {
				continue
			}
			if items[i].Kind != "tv" {
				items[i].TMDB, items[i].Poster, items[i].Status = 0, "", "pending"
			}
			items[i].Kind, items[i].Episode = "tv", episode
			if items[i].TMDB == 0 {
				items[i].Title = parent.Title
			}
			items[i].Season = 1
			if season, ok := seasonDirectory(path.Base(dir)); ok {
				items[i].Season = season
			}
			if parent.Year != "" {
				items[i].Year = parent.Year
			}
		}
	}
}

func scrapeNumber(raw string) int {
	if n, err := strconv.Atoi(raw); err == nil {
		return n
	}
	digits := map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	total, current := 0, 0
	for _, r := range raw {
		if r == '十' || r == '百' {
			unit := 10
			if r == '百' {
				unit = 100
			}
			if current == 0 {
				current = 1
			}
			total += current * unit
			current = 0
		} else {
			current = digits[r]
		}
	}
	return total + current
}

func seasonDirectory(name string) (int, bool) {
	if m := scrapeSeasonDir.FindStringSubmatch(name); m != nil {
		return scrapeNumber(m[1]), true
	}
	if m := scrapeChineseSeason.FindStringSubmatch(name); m != nil && m[0] == name {
		return scrapeNumber(m[1]), true
	}
	switch strings.ToLower(name) {
	case "specials", "special", "特别篇", "特別篇", "特别集":
		return 0, true
	}
	return 0, false
}

func scrapeWorkDir(name string) string {
	dir := path.Dir(name)
	for dir != "." {
		if _, ok := seasonDirectory(path.Base(dir)); !ok {
			break
		}
		dir = path.Dir(dir)
	}
	return dir
}

func genericMediaDir(name string) bool {
	switch strings.ToLower(name) {
	case ".", "/", "tv", "tv shows", "shows", "series", "movies", "movie", "strm", "电视剧", "剧集", "电影", "动漫", "动画", "综艺":
		return true
	}
	return false
}

func recognizeEpisode(title string, item *scrapeItem) string {
	for _, re := range []*regexp.Regexp{scrapeCrossEpisode, scrapeChineseEpisode, scrapeEpisodeOnly} {
		m := re.FindStringSubmatch(title)
		if m == nil {
			continue
		}
		item.Kind, item.Season = "tv", 1
		if re == scrapeCrossEpisode {
			item.Season, item.Episode = scrapeNumber(m[1]), scrapeNumber(m[2])
		} else {
			item.Episode = scrapeNumber(m[1])
		}
		title = title[:re.FindStringIndex(title)[0]]
		break
	}
	if m := scrapeChineseSeason.FindStringSubmatch(title); m != nil {
		item.Kind, item.Season = "tv", scrapeNumber(m[1])
		title = title[:scrapeChineseSeason.FindStringIndex(title)[0]]
	}
	return title
}

func recognizeSTRMPath(name string) scrapeItem {
	item := recognizeSTRM(path.Base(name))
	dir := scrapeWorkDir(name)
	season, seasonal := seasonDirectory(path.Base(path.Dir(name)))
	if seasonal {
		item.Kind = "tv"
		if !scrapeEpisode.MatchString(path.Base(name)) {
			item.Season = season
		}
	}
	parent := recognizeSTRM(path.Base(dir) + ".strm")
	if parent.Kind == "tv" && dir != "." {
		item.Kind = "tv"
		if !seasonal && !scrapeEpisode.MatchString(path.Base(name)) {
			item.Season = parent.Season
		}
	}
	if item.Kind == "tv" && item.Episode == 0 {
		stem := strings.TrimSuffix(path.Base(name), path.Ext(name))
		if isVideo(stem) {
			stem = strings.TrimSuffix(stem, path.Ext(stem))
		}
		if m := scrapeNumericEpisode.FindStringSubmatch(stem); m != nil {
			item.Episode = scrapeNumber(m[1])
			item.Title = ""
		}
	}
	if (item.Title == "" || item.Kind == "tv") && !genericMediaDir(path.Base(dir)) {
		item.Title = parent.Title
		if parent.Year != "" {
			item.Year = parent.Year
		}
		if item.TMDB == 0 {
			item.TMDB = parent.TMDB
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
	if item.Kind == "tv" {
		dir = scrapeWorkDir(item.Path)
		if !genericMediaDir(path.Base(dir)) {
			return strings.Join([]string{dir, "tv"}, "\x00")
		}
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
