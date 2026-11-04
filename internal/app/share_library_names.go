package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errShareLibraryDirectory = errors.New("保存目录名称已存在，请修改目录名称")

func shareLibraryDirectoryAvailable(st State, card, old quarkLibraryCard) error {
	if err := shareLibraryNamesAvailable(st, card); err != nil {
		return err
	}
	if card.Directory != "" && card.DirectoryName != "" && (old.ID == "" || quarkLibraryRoot(old) != quarkLibraryRoot(card)) {
		if _, err := os.Lstat(quarkLibraryRoot(card)); err == nil {
			return errShareLibraryDirectory
		} else if !os.IsNotExist(err) {
			return errors.New("保存目录不可核验，请检查生成位置")
		}
	}
	return nil
}

func shareLibraryNamesAvailable(st State, card quarkLibraryCard) error {
	for _, group := range [][]quarkLibraryCard{st.QuarkLibraryCards, st.MobileLibraryCards, st.CASLibraryCards, st.Library115Cards} {
		for _, other := range group {
			if other.ID != card.ID && strings.EqualFold(filepath.Clean(other.Directory), filepath.Clean(card.Directory)) &&
				strings.EqualFold(other.DirectoryName, card.DirectoryName) {
				return errShareLibraryDirectory
			}
		}
	}
	return nil
}

func telegramLibraryDirectory(st State, card quarkLibraryCard) (string, error) {
	if err := shareLibraryDirectoryAvailable(st, card, quarkLibraryCard{}); err == nil {
		return card.DirectoryName, nil
	} else if !errors.Is(err, errShareLibraryDirectory) {
		return "", err
	}
	// Use the first free numeric suffix across every library and existing disk
	// entries. Keep the original share title, including any literal numeric tail.
	for index := 1; index <= 10000; index++ {
		tail := fmt.Sprintf("_%d", index)
		stem := []rune(card.DirectoryName)
		for len(stem) > 0 && len(string(stem))+len(tail) > 255 {
			stem = stem[:len(stem)-1]
		}
		candidate := card
		candidate.DirectoryName = string(stem) + tail
		if err := shareLibraryDirectoryAvailable(st, candidate, quarkLibraryCard{}); err == nil {
			return candidate.DirectoryName, nil
		} else if !errors.Is(err, errShareLibraryDirectory) {
			return "", err
		}
	}
	return "", errors.New("无法分配不重名的保存目录，请调整链库默认位置")
}
