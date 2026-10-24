package app

import (
	"context"
	"path"
	"strings"
)

type transferLocation struct {
	StorageID string `json:"storageId,omitempty"`
	Storage   string `json:"storage"`
	Type      string `json:"type"`
	Directory string `json:"directory"`
	Path      string `json:"path,omitempty"`
}

type transferLocations struct {
	From transferLocation
	To   transferLocation
}

type transferPathHint struct {
	parent string
	name   string
	dir    bool
}

func pathBasedTransferStorage(s Storage) bool {
	return s.Type == "local" || s.Type == "openlist" || s.Type == "webdav" || s.Type == "mobile" && !nativeMobile(s)
}

func transferHintKey(s Storage, fid string) string {
	return s.ID + "\x00" + storageRevision(s) + "\x00" + fid
}

func (a *App) transferPathGeneration() uint64 {
	a.transfers.mu.Lock()
	defer a.transfers.mu.Unlock()
	return a.transfers.pathGeneration
}

func (a *App) invalidateTransferPaths(s Storage) {
	a.transfers.mu.Lock()
	defer a.transfers.mu.Unlock()
	a.transfers.pathGeneration++
	for key := range a.transfers.paths {
		if strings.HasPrefix(key, s.ID+"\x00") {
			delete(a.transfers.paths, key)
		}
	}
}

// Keep only metadata already returned by directory reads. Transfer presentation
// must never add cloud requests or depend on the directory-cache setting.
func (a *App) rememberTransferPaths(s Storage, parent string, files []File, generation uint64) {
	if pathBasedTransferStorage(s) || s.ID == "" {
		return
	}
	if parent == "" || parent == "/" {
		parent = rootOf(s)
	}
	prefix := transferHintKey(s, "")
	a.transfers.mu.Lock()
	defer a.transfers.mu.Unlock()
	if a.transfers.pathGeneration != generation {
		return
	}
	if a.transfers.paths == nil {
		a.transfers.paths = map[string]transferPathHint{}
	}
	for _, file := range files {
		if file.ID == "" || file.ID == parent || !safeName(file.Name) {
			continue
		}
		key := prefix + file.ID
		if _, exists := a.transfers.paths[key]; !exists && len(a.transfers.paths) >= 8192 {
			for key := range a.transfers.paths {
				delete(a.transfers.paths, key)
				break
			}
		}
		a.transfers.paths[key] = transferPathHint{parent: parent, name: file.Name, dir: file.IsDir}
	}
}

func (a *App) transferHint(s Storage, fid string) (transferPathHint, bool) {
	a.transfers.mu.Lock()
	defer a.transfers.mu.Unlock()
	hint, ok := a.transfers.paths[transferHintKey(s, fid)]
	return hint, ok
}

func (a *App) transferDirectory(s Storage, directory string) transferLocation {
	result := transferLocation{StorageID: s.ID, Storage: s.Name, Type: s.Type}
	if directory == "" {
		return result
	}
	if directory == "/" || directory == "." || directory == rootOf(s) {
		result.Directory, result.Path = "根目录", "/"
		return result
	}
	if pathBasedTransferStorage(s) {
		relative, err := relative(directory)
		if err == nil {
			result.Path = "/" + relative
			result.Directory = path.Base(relative)
		}
		return result
	}
	current := directory
	names := []string{}
	seen := map[string]bool{}
	for range 128 {
		if current == "/" || current == rootOf(s) {
			for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
				names[i], names[j] = names[j], names[i]
			}
			result.Path = "/" + strings.Join(names, "/")
			return result
		}
		if seen[current] {
			break
		}
		seen[current] = true
		hint, ok := a.transferHint(s, current)
		if !ok || !hint.dir {
			break
		}
		if result.Directory == "" {
			result.Directory = hint.name
		}
		names = append(names, hint.name)
		current = hint.parent
	}
	return result
}

func (a *App) transferFileLocation(s Storage, fid string) transferLocation {
	if pathBasedTransferStorage(s) {
		return a.transferDirectory(s, path.Dir(strings.ReplaceAll(fid, "\\", "/")))
	}
	if hint, ok := a.transferHint(s, fid); ok {
		return a.transferDirectory(s, hint.parent)
	}
	return a.transferDirectory(s, "")
}

func (a *App) transferUploadLocation(s Storage, parent, name string) transferLocation {
	result := a.transferDirectory(s, parent)
	subdirectory := path.Dir(strings.ReplaceAll(name, "\\", "/"))
	if subdirectory != "." {
		if value, err := relative(subdirectory); err == nil {
			result.Directory = path.Base(value)
			if result.Path != "" {
				result.Path = path.Join(result.Path, value)
			}
		}
	}
	return result
}

func transferLocalLocation(name, directory string) transferLocation {
	directory = strings.ReplaceAll(directory, "\\", "/")
	return transferLocation{Storage: name, Type: "local", Directory: path.Base(directory), Path: directory}
}

func transferClientLocation(ctx context.Context) transferLocation {
	source, _ := ctx.Value(transferSourceKey{}).(string)
	switch source {
	case "FUSE":
		return transferLocation{Storage: "本地挂载", Type: "client"}
	case "WebDAV":
		return transferLocation{Storage: "WebDAV 客户端", Type: "client"}
	case "":
		return transferLocation{Storage: "浏览器", Type: "browser"}
	default:
		return transferLocation{Storage: source, Type: "client"}
	}
}
