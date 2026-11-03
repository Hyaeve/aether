package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	for _, group := range [][]quarkLibraryCard{st.QuarkLibraryCards, st.MobileLibraryCards, st.CASLibraryCards} {
		for _, other := range group {
			if other.ID != card.ID && strings.EqualFold(filepath.Clean(other.Directory), filepath.Clean(card.Directory)) &&
				strings.EqualFold(other.DirectoryName, card.DirectoryName) {
				return errShareLibraryDirectory
			}
		}
	}
	return nil
}

func telegramLibraryDirectory(st State, card quarkLibraryCard, now time.Time) (string, error) {
	if shareLibraryDirectoryAvailable(st, card, quarkLibraryCard{}) == nil {
		return card.DirectoryName, nil
	}
	// ASCII slashes and colons are not valid directory-name components.
	suffix := "（" + now.Format("01／02 15：04") + "）"
	for index := 1; index <= 201; index++ {
		tail := suffix
		if index > 1 {
			tail += fmt.Sprintf("-%d", index)
		}
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
