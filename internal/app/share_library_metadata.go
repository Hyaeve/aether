package app

import (
	"context"
	"errors"
	"time"
)

func (a *App) saveShareLibraryMetadata(ctx context.Context, provider string, old, card quarkLibraryCard) (bool, error) {
	rename := old.DirectoryName != card.DirectoryName
	// Saving filters, extensions, scraping or scheduling does not synchronize the
	// library. Only a new source/output location needs a fresh generation pass.
	if old.Secret != card.Secret || old.Directory != card.Directory || old.StorageID != card.StorageID || rename && (card.DirectoryName == "" || !validShareDirectory(card.DirectoryName)) {
		return false, nil
	}
	card.ShareName = old.ShareName
	if card.Name == "" {
		card.Name = old.ShareName
		if card.Name == "" {
			card.Name = old.Name
		}
	}
	finish := func(bool) error { return nil }
	err := a.store.update(func(st *State) error {
		var cards *[]quarkLibraryCard
		var results map[string]quarkLibraryResult
		var roots map[string]quarkLibraryOwnership
		var validate func(State, ...bool) error
		switch provider {
		case "115":
			initLibrary115(st)
			cards = &st.Library115Cards
			results = st.Library115Runtime.Results
			roots = st.Library115Runtime.Roots
			validate = func(st State, _ ...bool) error { return validateLibrary115(st) }
		case "quark":
			initQuarkLibraryRuntime(st)
			cards = &st.QuarkLibraryCards
			results = st.QuarkLibraryRuntime.Results
			roots = st.QuarkLibraryRuntime.Roots
			validate = validateQuarkLibrary
		case "mobile":
			initMobileLibrary(st)
			cards = &st.MobileLibraryCards
			results = st.MobileLibraryRuntime.Results
			roots = st.MobileLibraryRuntime.Roots
			validate = validateMobileLibrary
		case "cas":
			initCASLibrary(st)
			cards = &st.CASLibraryCards
			results = st.CASLibraryRuntime.Results
			roots = st.CASLibraryRuntime.Roots
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
		if old.Cron != card.Cron || old.Enabled != card.Enabled {
			result := results[card.ID]
			result.NextRun = quarkLibraryNext(card, time.Now())
			results[card.ID] = result
		}
		if provider == "cas" {
			validate = func(st State, _ ...bool) error { return validateCASLibrary(st) }
		}
		if err := validate(*st); err != nil {
			return err
		}
		if rename {
			if err := shareLibraryDirectoryAvailable(*st, card, old); err != nil {
				return err
			}
			var owner quarkLibraryOwnership
			var err error
			finish, owner, err = a.prepareShareLibraryRename(ctx, old, card, roots[old.ID])
			if err != nil {
				return err
			}
			if owner.Directory != "" {
				roots[old.ID] = owner
			}
			if provider == "cas" {
				for i := range st.GenerationJobs {
					if st.GenerationJobs[i].Target == quarkLibraryRoot(old) && st.GenerationJobs[i].LibrarySource == casLibrarySourceStamp(old.Secret) {
						st.GenerationJobs[i].Target = quarkLibraryRoot(card)
					}
				}
			}
		}
		return nil
	})
	err = errors.Join(err, finish(err == nil))
	if err == nil && rename {
		a.store.event("info", "virtual", "分享链库目录已改名："+old.DirectoryName+" → "+card.DirectoryName)
	}
	return true, err
}
