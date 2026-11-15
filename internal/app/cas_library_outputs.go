package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
)

type generationOutputKey struct{}

func generationOutputPublished(ctx context.Context, name string, content []byte) error {
	if record, ok := ctx.Value(generationOutputKey{}).(func(string, []byte) error); ok {
		return record(name, content)
	}
	return nil
}

func (a *App) recordCASLibraryOutput(ctx context.Context, card quarkLibraryCard, name string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !quarkLibraryRelative(name) {
		return errors.New("CAS生成文件路径无效，未登记")
	}
	digest := sha256.Sum256(content)
	return a.store.update(func(st *State) error {
		current, ok := casLibraryCardAt(*st, card.ID)
		if !ok || current != card {
			return errors.New("CAS链库已改变，未登记生成文件")
		}
		initCASLibrary(st)
		key := card.ID + "/" + name
		if _, exists := st.CASLibraryRuntime.Files[key]; !exists && len(st.CASLibraryRuntime.Files) >= 20000 {
			return errors.New("CAS生成文件登记已满")
		}
		st.CASLibraryRuntime.Files[key] = quarkLibraryFile{Key: key, CardID: card.ID, Relative: name, Name: path.Base(name), Digest: hex.EncodeToString(digest[:]), Media: true, Published: true}
		return nil
	})
}
