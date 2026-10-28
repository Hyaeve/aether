package resolver

import (
	"testing"
	"time"

	"aether/internal/linkcore/strm"
)

func TestSharePlaybackRejectsOldPlaceholderAndLongLivedCache(t *testing.T) {
	for _, prefix := range []string{"quark-share", "115-share"} {
		value := &Resolution{Target: &strm.Target{Type: strm.TargetRemote, URL: "https://example.test/" + prefix + "/sample/Movie.mkv"}, ResolvedAt: time.Now()}
		if shareCacheRemaining(value, time.Hour) != 0 {
			t.Fatal("unprepared service URL remained a cache hit")
		}
		value.FinalURL = "https://cdn.test/video"
		if remaining := shareCacheRemaining(value, time.Hour); remaining <= 0 || remaining > 5*time.Minute {
			t.Fatal("share CDN cache exceeded lifetime", remaining)
		}
		value.ResolvedAt = time.Now().Add(-6 * time.Minute)
		if shareCacheRemaining(value, time.Hour) > 0 {
			t.Fatal("restored old CDN ticket remained usable")
		}
	}
}

func TestRejectedPlaybackCannotInvalidateANewerResolution(t *testing.T) {
	cache := newLRUCache(time.Hour, 4)
	before := &Resolution{FinalURL: "https://cdn.test/old"}
	after := &Resolution{FinalURL: "https://cdn.test/new"}
	cache.put("key", before, time.Hour)
	cache.put("key", after, time.Hour)
	cache.removeIf("key", before)
	if value, _, ok := cache.get("key"); !ok || value != after {
		t.Fatal("late rejection removed a newer ticket")
	}
	cache.removeIf("key", after)
	if _, _, ok := cache.get("key"); ok {
		t.Fatal("rejected ticket retained")
	}
}
