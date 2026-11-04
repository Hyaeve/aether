package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

func libraryUpdateDays(days *int) int {
	if days == nil {
		return 7
	}
	return *days
}
func validateLibraryUpdateDays(days *int) error {
	if n := libraryUpdateDays(days); n < 1 || n > 106751 {
		return errors.New("更新周期须为正整数，单位天")
	}
	return nil
}

// Interval descriptors are library-only. Five-field tasks keep their existing policy.
func parseLibraryCron(expression string, legacy ...bool) (cron.Schedule, error) {
	if strings.HasPrefix(expression, "@every ") {
		d, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(expression, "@every ")))
		if err != nil || d < 15*time.Minute || d%time.Minute != 0 {
			return nil, errors.New("链库更新间隔须至少15分钟且为整分钟")
		}
		return cron.Every(d), nil
	}
	if len(legacy) > 0 && legacy[0] {
		return rawCronParser.Parse(expression)
	}
	return cronParser.Parse(expression)
}

func setLibrarySchedule(card *quarkLibraryCard, old quarkLibraryCard, now time.Time) {
	card.ScheduleAnchor = old.ScheduleAnchor
	if card.Cron != old.Cron || card.ScheduleAnchor.IsZero() {
		card.ScheduleAnchor = now.Truncate(time.Hour).UTC()
	}
	if card.Cron == "" {
		card.ScheduleAnchor = time.Time{}
	}
}

func botLibrarySchedule(card *quarkLibraryCard, days *int, received time.Time) {
	card.Cron = fmt.Sprintf("@every %dh", libraryUpdateDays(days)*24)
	card.ScheduleAnchor = received.Truncate(time.Hour).UTC()
}
