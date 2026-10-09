package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

func libraryNoticeDescription(n LibraryNotice) string {
	if n.MediaType == "movie" {
		return "电影 · " + n.Name
	}
	if n.MediaType != "episode" {
		return n.Name
	}
	name := n.Series
	if name == "" {
		name = n.Name
	}
	season := "季数未知"
	if n.Season != nil {
		season = fmt.Sprintf("第%d季", *n.Season)
		if *n.Season == 0 {
			season = "特别篇"
		}
	}
	values := slices.Clone(n.Episodes)
	slices.Sort(values)
	values = slices.Compact(values)
	ranges := []string{}
	for i := 0; i < len(values); i++ {
		start, end := values[i], values[i]
		for i+1 < len(values) && values[i+1] == end+1 {
			i++
			end = values[i]
		}
		part := strconv.Itoa(start)
		if end != start {
			part += "-" + strconv.Itoa(end)
		}
		ranges = append(ranges, part)
	}
	episodes := "集数未知"
	if len(ranges) > 0 {
		episodes = strings.Join(ranges, "、") + "集"
	}
	return "电视剧 · " + name + " · " + season + " · " + episodes
}

func mergeLibraryNotice(st *State, incoming LibraryNotice) bool {
	for i := len(st.LibraryNotices) - 1; i >= 0; i-- {
		n := &st.LibraryNotices[i]
		if n.LibraryName != incoming.LibraryName || n.ServerName != incoming.ServerName || normalizedNoticeEvent(n.Event) != normalizedNoticeEvent(incoming.Event) {
			continue
		}
		if incoming.Time.Sub(n.Time) > 10*time.Minute || incoming.Time.Before(n.Time) || n.ServerID != incoming.ServerID {
			continue
		}
		if len(incoming.ItemIDs) > 0 && slices.Contains(n.ItemIDs, incoming.ItemIDs[0]) {
			return false
		}
		if normalizedNoticeEvent(incoming.Event) == "library.new" && incoming.MediaType == "episode" && n.MediaType == "episode" && incoming.Series != "" && n.Series == incoming.Series && n.SeriesID == incoming.SeriesID && n.Season != nil && incoming.Season != nil && *n.Season == *incoming.Season && len(n.Episodes) > 0 && len(incoming.Episodes) > 0 && len(n.ItemIDs) < 1000 && len(n.Episodes)+len(incoming.Episodes) <= 1000 {
			merged := append(slices.Clone(n.Episodes), incoming.Episodes...)
			slices.Sort(merged)
			merged = slices.Compact(merged)
			changed := !slices.Equal(n.Episodes, merged)
			n.Episodes = merged
			n.ItemIDs = append(n.ItemIDs, incoming.ItemIDs...)
			if changed {
				n.Time = incoming.Time
			}
			return changed
		}
		if len(incoming.ItemIDs) == 0 && len(n.ItemIDs) == 0 && incoming.MediaType != "episode" && n.MediaType == incoming.MediaType && n.Name == incoming.Name && incoming.Time.Sub(n.Time) < time.Minute {
			return false
		}
	}
	st.LibraryNotices = append(st.LibraryNotices, incoming)
	return true
}

func normalizedNoticeEvent(event string) string {
	if event == "" {
		return "library.new"
	}
	return event
}
