package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTelegramLibraryExtensionDefaultsAreCopiedOnlyForNewCards(t *testing.T) {
	for _, kind := range []string{"quark", "mobile", "cas"} {
		t.Run(kind, func(t *testing.T) {
			a, bot := telegramFixture(t)
			command, link := "/share", "https://yun.139.com/shareweb/one?pwd=Ab12"
			switch kind {
			case "quark":
				mockQuarkLibrary(t, a)
				link = "https://pan.quark.cn/s/extensions"
			case "mobile":
				mockMobileLibrary(t, a)
			case "cas":
				mockMobileGeneration(t, "valid")
				command = "/cas"
			}
			captureTelegramResults(t)
			defaults := func(media, metadata string) {
				t.Helper()
				if err := a.store.update(func(st *State) error {
					switch kind {
					case "quark":
						cfg := effectiveQuarkLibrary(*st)
						cfg.MediaExtensions, cfg.MetadataExtensions = media, metadata
						st.QuarkLibrary = &cfg
					case "mobile":
						cfg := effectiveMobileLibrary(*st)
						cfg.MediaExtensions, cfg.MetadataExtensions = media, metadata
						st.MobileLibrary = &cfg
					case "cas":
						cfg := effectiveCASLibrary(*st)
						cfg.MediaExtensions = media
						st.CASLibrary = &cfg
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			cards := func(st *State) *[]quarkLibraryCard {
				switch kind {
				case "quark":
					return &st.QuarkLibraryCards
				case "mobile":
					return &st.MobileLibraryCards
				default:
					return &st.CASLibraryCards
				}
			}
			if err := a.store.update(func(st *State) error {
				*cards(st) = nil
				st.Notifications.Targets = nil
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			defaults("mkv;mp4", "nfo;ass")
			send := func(update int64) quarkLibraryCard {
				t.Helper()
				if err := a.handleTelegramUpdate(context.Background(), bot, telegramTextUpdate(update, 123, 123, command+" "+link)); err != nil {
					t.Fatal(err)
				}
				st := a.store.snapshot()
				a.executeTelegramTransfer(context.Background(), st.TelegramRuntime.Jobs[len(st.TelegramRuntime.Jobs)-1])
				st = a.store.snapshot()
				if len(*cards(&st)) != 1 || st.TelegramRuntime.Jobs[len(st.TelegramRuntime.Jobs)-1].Status != "completed" {
					t.Fatal("expected one completed draft", *cards(&st), st.TelegramRuntime.Jobs)
				}
				return (*cards(&st))[0]
			}
			first := send(1)
			wantMetadata := "nfo;ass"
			if kind == "cas" {
				wantMetadata = ""
			}
			if first.MediaExtensions != "mkv;mp4" || first.MetadataExtensions != wantMetadata || first.Enabled {
				t.Fatal("new bot card did not copy its library extension defaults", first)
			}
			reopened, err := NewStore(a.store.dir)
			if err != nil {
				t.Fatal(err)
			}
			persisted := reopened.snapshot()
			if got := (*cards(&persisted))[0]; got.MediaExtensions != first.MediaExtensions || got.MetadataExtensions != first.MetadataExtensions {
				t.Fatal("extension defaults did not survive reopening", got)
			}
			defaults("pdf", "jpg")
			if got := send(2); got.ID != first.ID || got.MediaExtensions != first.MediaExtensions || got.MetadataExtensions != first.MetadataExtensions {
				t.Fatal("later defaults changed an existing bot card", got)
			}
			for _, values := range [][2]string{{"avi", "srt"}, {"", ""}} {
				if kind == "cas" {
					values[1] = ""
				}
				if err := a.store.update(func(st *State) error {
					card := &(*cards(st))[0]
					card.MediaExtensions, card.MetadataExtensions = values[0], values[1]
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				update := int64(3)
				if values[0] == "" {
					update++
				}
				if got := send(update); got.ID != first.ID || got.MediaExtensions != values[0] || got.MetadataExtensions != values[1] {
					t.Fatal("repeated share replaced custom or deliberately empty card fields", got)
				}
			}
		})
	}
}

func TestShareLibraryExtensionPriorityAndIndependentFallback(t *testing.T) {
	for _, kind := range []string{"quark", "mobile"} {
		for _, values := range [][2]string{{"", ""}, {"mp4", ""}, {"", "srt"}, {"mp4", "srt"}} {
			t.Run(kind+"/media="+values[0]+"/metadata="+values[1], func(t *testing.T) {
				a := testApp(t)
				var card quarkLibraryCard
				if kind == "quark" {
					m := mockQuarkLibrary(t, a)
					m.files = nil
					for _, ext := range []string{"mkv", "mp4", "nfo", "srt"} {
						m.files = append(m.files, shareEntry{ID: ext, Name: "Movie." + ext, Size: 10, Token: "file-token"})
					}
					card = newQuarkLibraryCard(t, a, values[0])
				} else {
					m, initial := mockMobileLibrary(t, a)
					card = initial
					m.list["root"] = nil
					for _, ext := range []string{"mkv", "mp4", "nfo", "srt"} {
						m.list["root"] = append(m.list["root"], map[string]any{"coID": ext, "coName": "Movie." + ext, "coPath": "root/" + ext, "coSize": 10})
					}
				}
				card.MediaExtensions, card.MetadataExtensions, card.CascadeCleanup = values[0], values[1], "off"
				if err := a.store.update(func(st *State) error {
					if kind == "quark" {
						cfg := effectiveQuarkLibrary(*st)
						cfg.MediaExtensions, cfg.MetadataExtensions = "mkv", "nfo"
						st.QuarkLibrary, st.QuarkLibraryCards = &cfg, []quarkLibraryCard{card}
					} else {
						cfg := effectiveMobileLibrary(*st)
						cfg.MediaExtensions, cfg.MetadataExtensions = "mkv", "nfo"
						st.MobileLibrary, st.MobileLibraryCards = &cfg, []quarkLibraryCard{card}
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				var result quarkLibraryResult
				var err error
				if kind == "quark" {
					result, err = a.runQuarkLibrary(context.Background(), card)
				} else {
					result, err = a.runMobileLibrary(context.Background(), card)
				}
				if err != nil || result.Generated != 1 || result.Metadata != 1 {
					t.Fatal("wrong extension selection", result, err)
				}
				media, metadata := values[0], values[1]
				if media == "" {
					media = "mkv"
				}
				if metadata == "" {
					metadata = "nfo"
				}
				for _, ext := range []string{"mkv", "mp4", "nfo", "srt"} {
					name := "Movie." + ext
					if ext == "mkv" || ext == "mp4" {
						name += shareSTRMSuffix
					}
					_, err := os.Stat(filepath.Join(quarkLibraryRoot(card), name))
					if ext == media || ext == metadata {
						if err != nil {
							t.Fatal("selected file missing", name, err)
						}
					} else if !os.IsNotExist(err) {
						t.Fatal("library defaults merged into card override", name, err)
					}
				}
			})
		}
	}
}
