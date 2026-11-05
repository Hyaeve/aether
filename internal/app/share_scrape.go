package app

import (
	"encoding/json"
	"errors"
	"path"
	"strings"
)

const quarkScrapePrefix = "quark-share:"
const mobileScrapePrefix = "mobile-share:"
const library115ScrapePrefix = "115-share:"
const casScrapePrefix = "cas-share:"

func shareScrapeProvider(taskID string) string {
	for _, item := range []struct{ prefix, provider string }{{quarkScrapePrefix, "quark"}, {mobileScrapePrefix, "mobile"}, {library115ScrapePrefix, "115"}, {casScrapePrefix, "cas"}} {
		if strings.HasPrefix(taskID, item.prefix) {
			return item.provider
		}
	}
	return ""
}

func isShareScrapeTask(taskID string) bool {
	return shareScrapeProvider(taskID) != ""
}

func shareScrapeCard(st State, taskID string) (quarkLibraryCard, bool) {
	if strings.HasPrefix(taskID, library115ScrapePrefix) {
		return library115CardAt(st, strings.TrimPrefix(taskID, library115ScrapePrefix))
	}
	if strings.HasPrefix(taskID, casScrapePrefix) {
		return casLibraryCardAt(st, strings.TrimPrefix(taskID, casScrapePrefix))
	}
	if strings.HasPrefix(taskID, mobileScrapePrefix) {
		return mobileLibraryCardAt(st, strings.TrimPrefix(taskID, mobileScrapePrefix))
	}
	if !strings.HasPrefix(taskID, quarkScrapePrefix) {
		return quarkLibraryCard{}, false
	}
	return quarkLibraryCardAt(st, strings.TrimPrefix(taskID, quarkScrapePrefix))
}

func shareScrapeResult(st State, taskID string, card quarkLibraryCard) quarkLibraryResult {
	if strings.HasPrefix(taskID, library115ScrapePrefix) {
		return st.Library115Runtime.Results[card.ID]
	}
	if strings.HasPrefix(taskID, casScrapePrefix) {
		return st.CASLibraryRuntime.Results[card.ID]
	}
	if strings.HasPrefix(taskID, mobileScrapePrefix) {
		return st.MobileLibraryRuntime.Results[card.ID]
	}
	return st.QuarkLibraryRuntime.Results[card.ID]
}

func (a *App) checkShareScrapeCard(taskID string, card quarkLibraryCard) error {
	if provider := shareScrapeProvider(taskID); provider == "115" || provider == "cas" {
		current, ok := shareScrapeCard(a.store.snapshotWithLogLimit(0), taskID)
		if !ok || current != card {
			return errors.New("链库配置已变化，刮削停止")
		}
		return nil
	}
	if strings.HasPrefix(taskID, mobileScrapePrefix) {
		return a.mobileLibraryCheck(card, nil)
	}
	return a.quarkLibraryCheckCard(card)
}

func shareScrapeSources(st State) []map[string]any {
	sources := []map[string]any{}
	for _, source := range []struct {
		provider, prefix string
		cards            []quarkLibraryCard
	}{{"115", library115ScrapePrefix, st.Library115Cards}, {"cas", casScrapePrefix, st.CASLibraryCards}} {
		for _, card := range source.cards {
			if card.Enabled && card.ScrapeEnabled && card.Directory != "" {
				result := shareScrapeResult(st, source.prefix+card.ID, card)
				sources = append(sources, map[string]any{"id": source.prefix + card.ID, "provider": source.provider, "name": card.Name, "status": result.Status, "lastRun": result.LastRun})
			}
		}
	}
	for _, card := range st.QuarkLibraryCards {
		if card.Enabled && card.ScrapeEnabled && card.Directory != "" {
			result := st.QuarkLibraryRuntime.Results[card.ID]
			sources = append(sources, map[string]any{"id": quarkScrapePrefix + card.ID, "provider": "quark", "name": card.Name,
				"status": result.Status, "lastRun": result.LastRun})
		}
	}
	for _, card := range st.MobileLibraryCards {
		if card.Enabled && card.ScrapeEnabled && card.Directory != "" {
			result := st.MobileLibraryRuntime.Results[card.ID]
			sources = append(sources, map[string]any{"id": mobileScrapePrefix + card.ID, "provider": "mobile", "name": card.Name,
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
		cfg.LibraryName = card.ShareName
		if cfg.LibraryName == "" {
			cfg.LibraryName = card.Name
		}
		if shareScrapeProvider(taskID) == "cas" {
			cfg.CASOnly = true
		}
		cfg.Excluded += ";" + card.ExcludeDirs
		cfg.ExcludeFiles, cfg.ExcludeTypes = card.ExcludeFiles, card.ExcludeTypes
	}
	return cfg
}

func scrapeIncludes(name string, cfg scrapeSettings) bool {
	source := shareSTRMSourceName(name)
	if cfg.CASOnly && strings.HasSuffix(strings.ToLower(source), ".cas.strm") {
		source = source[:len(source)-len(".cas.strm")]
	}
	return (!cfg.ShareOnly || !cfg.CASOnly && isShareSTRM(name) || cfg.CASOnly && strings.HasSuffix(strings.ToLower(name), ".cas.strm")) && !excluded(path.Dir(name), cfg.Excluded) &&
		!excluded(path.Base(source), cfg.ExcludeFiles) && !excludedType(source, cfg.ExcludeTypes)
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
	if provider := shareScrapeProvider(taskID); provider == "115" || provider == "cas" {
		lock := &a.library115Mu
		if provider == "cas" {
			lock = &a.casLibraryMu
		}
		if !lock.TryLock() {
			return nil, errors.New("链库正在生成或刮削，请稍后重试")
		}
		return lock.Unlock, nil
	}
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
