package app

import (
	"errors"
	"time"
)

// Only Cron and display-name changes may bypass generation. Normalize legacy
// defaults and share URL spelling, but compare every output-affecting field.
func shareLibrarySameGeneration(provider string, before, after quarkLibraryCard) bool {
	before.Cron, after.Cron = "", ""
	before.Name, after.Name, before.ShareName, after.ShareName = "", "", "", ""
	for _, c := range []*quarkLibraryCard{&before, &after} {
		var err error
		switch provider {
		case "quark":
			if c.CascadeCleanup == "" {
				c.CascadeCleanup = "on"
			}
			err = validateQuarkLibraryCard(c)
		case "mobile", "cas":
			if c.CascadeCleanup == "" {
				c.CascadeCleanup = "off"
			}
			err = validateMobileLibraryCard(c)
		default:
			return false
		}
		if err != nil {
			return false
		}
	}
	return before == after
}

func (a *App) saveShareLibraryMetadata(provider string, old, card quarkLibraryCard) (bool, error) {
	if !shareLibrarySameGeneration(provider, old, card) {
		return false, nil
	}
	card.ShareName = old.ShareName
	if card.Name == "" {
		card.Name = old.ShareName
		if card.Name == "" {
			card.Name = old.Name
		}
	}
	// Keep the exact generation fields, including legacy URL/default spellings,
	// so existing file grants and ownership stamps remain valid.
	updated := old
	updated.Cron, updated.Name = card.Cron, card.Name
	card = updated
	err := a.store.update(func(st *State) error {
		var cards *[]quarkLibraryCard
		var results map[string]quarkLibraryResult
		var validate func(State, ...bool) error
		switch provider {
		case "quark":
			initQuarkLibraryRuntime(st)
			cards = &st.QuarkLibraryCards
			results = st.QuarkLibraryRuntime.Results
			validate = validateQuarkLibrary
		case "mobile":
			initMobileLibrary(st)
			cards = &st.MobileLibraryCards
			results = st.MobileLibraryRuntime.Results
			validate = validateMobileLibrary
		case "cas":
			initCASLibrary(st)
			cards = &st.CASLibraryCards
			results = st.CASLibraryRuntime.Results
		}
		found := false
		for i, current := range *cards {
			if current.ID == old.ID {
				if current != old {
					return errors.New("分享记录已改变，请重新打开")
				}
				(*cards)[i] = card
				found = true
				break
			}
		}
		if !found {
			return errors.New("分享记录已移除，请重新打开")
		}
		if err := shareLibraryNamesAvailable(*st, card); err != nil {
			return err
		}
		if old.Cron != card.Cron {
			result := results[card.ID]
			result.NextRun = quarkLibraryNext(card, time.Now())
			results[card.ID] = result
		}
		if provider == "cas" {
			return validateCASLibrary(*st)
		}
		return validate(*st)
	})
	return true, err
}
