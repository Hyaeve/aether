package app

import (
	"context"
	"sync"
	"time"
)

var tmdbGate = make(chan struct{}, 1)
var tmdbLast struct {
	sync.Mutex
	at time.Time
}

// Serial reservation keeps concurrent searches and scrape requests spaced.
func waitTMDB(ctx context.Context, cfg PluginConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case tmdbGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-tmdbGate }()
	interval := pluginDefaults("tmdb", cfg).RequestInterval
	tmdbLast.Lock()
	delay := time.Until(tmdbLast.at.Add(time.Duration(interval) * time.Millisecond))
	tmdbLast.Unlock()
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	tmdbLast.Lock()
	tmdbLast.at = time.Now()
	tmdbLast.Unlock()
	return nil
}
