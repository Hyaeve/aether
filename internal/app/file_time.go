package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Providers mix Unix seconds/milliseconds with formatted local timestamps.
type fileTimestamp struct{ time.Time }

func (t *fileTimestamp) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		value = string(data)
	}
	t.Time = parseFileTime(value)
	return nil
}

func parseFileTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if len(value) == 14 && strings.HasPrefix(value, "20") {
		if parsed, err := time.ParseInLocation("20060102150405", value, time.FixedZone("CST", 8*60*60)); err == nil {
			return parsed
		}
	}
	if number, err := strconv.ParseInt(value, 10, 64); err == nil && number > 0 {
		if number > 100000000000 {
			return time.UnixMilli(number)
		}
		return time.Unix(number, 0)
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006/01/02 15:04:05", "20060102150405"} {
		if parsed, err := time.ParseInLocation(layout, value, time.FixedZone("CST", 8*60*60)); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func firstFileTime(values ...fileTimestamp) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.Time
		}
	}
	return time.Time{}
}
