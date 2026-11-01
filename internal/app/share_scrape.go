package app

import (
	"encoding/json"
	"errors"
	"path"
	"strings"
)

const quarkScrapePrefix = "quark-share:"
const mobileScrapePrefix = "mobile-share:"

func isShareScrapeTask(taskID string) bool {
	return strings.HasPrefix(taskID, quarkScrapePrefix) || strings.HasPrefix(taskID, mobileScrapePrefix)
}

func shareScrapeCard(st State, taskID string) (quarkLibraryCard, bool) {
	if strings.HasPrefix(taskID, mobileScrapePrefix) {
		return mobileLibraryCardAt(st, strings.TrimPrefix(taskID, mobileScrapePrefix))
	}
	if !strings.HasPrefix(taskID, quarkScrapePrefix) {
		return quarkLibraryCard{}, false
	}
	return quarkLibraryCardAt(st, strings.TrimPrefix(taskID, quarkScrapePrefix))
}

func shareScrapeResult(st State, taskID string, card quarkLibraryCard) quarkLibraryResult {
	if strings.HasPrefix(taskID, mobileScrapePrefix) {
		return st.MobileLibraryRuntime.Results[card.ID]
	}
	return st.QuarkLibraryRuntime.Results[card.ID]
}

func (a *App) checkShareScrapeCard(taskID string, card quarkLibraryCard) error {
	if strings.HasPrefix(taskID, mobileScrapePrefix) {
		return a.mobileLibraryCheck(card, nil)
	}
	return a.quarkLibraryCheckCard(card)
}

func shareScrapeSources(st State) []map[string]any {
	sources := []map[string]any{}
	for _, card := range st.QuarkLibraryCards {
		if card.Enabled && card.ScrapeEnabled && card.Directory != "" {
			result := st.QuarkLibraryRuntime.Results[card.ID]
			sources = append(sources, map[string]any{"id": quarkScrapePrefix + card.ID, "name": card.Name,
				"status": result.Status, "lastRun": result.LastRun})
		}
	}
	for _, card := range st.MobileLibraryCards {
		if card.Enabled && card.ScrapeEnabled && card.Directory != "" {
			result := st.MobileLibraryRuntime.Results[card.ID]
			sources = append(sources, map[string]any{"id": mobileScrapePrefix + card.ID, "name": card.Name,
				"status": result.Status, "lastRun": result.LastRun})
		}
	}
	return sources
}

func (a *App) scrapeSourceConfig(taskID string) scrapeSettings {
	cfg := a.scrapeConfig()
	if isShareScrapeTask(taskID) {
		card, _ := shareScrapeCard(a.store.snapshotWithLogLimit(0), taskID)
		cfg.ShareOnly = true
		cfg.Excluded += ";" + card.ExcludeDirs
		cfg.ExcludeFiles, cfg.ExcludeTypes = card.ExcludeFiles, card.ExcludeTypes
	}
	return cfg
}

func scrapeIncludes(name string, cfg scrapeSettings) bool {
	return (!cfg.ShareOnly || isShareSTRM(name)) && !excluded(path.Dir(name), cfg.Excluded) &&
		!excluded(path.Base(shareSTRMSourceName(name)), cfg.ExcludeFiles) &&
		!excludedType(shareSTRMSourceName(name), cfg.ExcludeTypes)
}

func shareScrapeRevision(card quarkLibraryCard, result quarkLibraryResult) string {
	b, _ := json.Marshal(struct {
		Card quarkLibraryCard
		Run  quarkLibraryResult
	}{card, result})
	return quarkLibraryStamp(string(b))
}

// Share generation, card edits and scraping use the same non-queuing gate.
func (a *App) lockShareScrape(taskID string) (func(), error) {
	if strings.HasPrefix(taskID, mobileScrapePrefix) {
		if !a.mobileLibraryMu.TryLock() {
			return nil, errors.New("移动链库正在生成或刮削，请稍后重试")
		}
		return a.mobileLibraryMu.Unlock, nil
	}
	if !strings.HasPrefix(taskID, quarkScrapePrefix) {
		return func() {}, nil
	}
	if !a.quarkLibraryMu.TryLock() {
		return nil, errors.New("夸克链库正在生成或刮削，请稍后重试")
	}
	return a.quarkLibraryMu.Unlock, nil
}
