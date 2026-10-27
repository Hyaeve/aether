package app

import (
	"errors"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

var rapid115CronChecks = struct {
	sync.Mutex
	items map[string]error
}{items: map[string]error{}}

func validateRapid115RetryCron(expression string) error {
	key := time.Local.String() + "\x00" + expression
	rapid115CronChecks.Lock()
	result, found := rapid115CronChecks.items[key]
	rapid115CronChecks.Unlock()
	if found {
		return result
	}
	result = checkRapid115RetryCron(expression)
	rapid115CronChecks.Lock()
	defer rapid115CronChecks.Unlock()
	if len(rapid115CronChecks.items) >= 256 {
		clear(rapid115CronChecks.items)
	}
	rapid115CronChecks.items[key] = result
	return result
}

func checkRapid115RetryCron(expression string) error {
	schedule, err := cronParser.Parse(expression)
	if err != nil {
		return errors.New("Cron表达式无效")
	}
	spec, ok := schedule.(*cron.SpecSchedule)
	if !ok {
		return errors.New("不支持此Cron计划")
	}
	const minimum = 120 * time.Minute
	tooShort := errors.New("带重试的Cron计划相邻执行间隔必须严格大于120min")
	loc := spec.Location
	if loc == time.Local {
		loc = time.Local
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	first := schedule.Next(start.Add(-time.Second))
	if first.IsZero() {
		return errors.New("Cron计划没有可执行时间")
	}
	slots := []int{}
	for hour := 0; hour < 24; hour++ {
		for minute := 0; minute < 60; minute++ {
			if spec.Hour&(1<<hour) != 0 && spec.Minute&(1<<minute) != 0 {
				value := hour*60 + minute
				if len(slots) > 0 && value-slots[len(slots)-1] <= 120 {
					return tooShort
				}
				slots = append(slots, value)
			}
		}
	}
	_, zoneEnd := start.ZoneBounds()
	fixed := zoneEnd.IsZero()
	wrap := 1440 - slots[len(slots)-1] + slots[0]
	if fixed && wrap > 120 {
		return nil
	}
	// A full Gregorian cycle catches weekday/month boundaries; the library
	// decides actual dates and DST. Fixed zones need only inter-day pairs.
	end := start.AddDate(400, 0, 0)
	for previous := first; previous.Before(end); {
		if fixed {
			last := slots[len(slots)-1]
			previous = time.Date(previous.Year(), previous.Month(), previous.Day(), last/60, last%60, 0, 0, loc)
		}
		next := schedule.Next(previous)
		if next.IsZero() {
			return nil
		}
		if next.Sub(previous) <= minimum {
			return tooShort
		}
		previous = next
	}
	return nil
}
