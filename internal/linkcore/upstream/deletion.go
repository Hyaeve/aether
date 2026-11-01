package upstream

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"aether/internal/linkcore/config"
)

// DeletionPaths reads the filesystem pointers before an explicit user deletion.
// An inventory refresh or a removed-item notification is never deletion authority.
func DeletionPaths(ctx context.Context, provider Provider, id string) ([]string, error) {
	identity := contextClientIdentity(ctx)
	if identity.Token == "" && identity.Session == "" {
		return nil, errors.New("删除请求缺少媒体服务身份")
	}
	// Read with the deleting user's identity only. A denied user must never fall
	// back to the configured service account while preparing a destructive plan.
	reader, err := New(config.Upstream{Name: provider.Name(), Type: provider.Type(), BaseURL: provider.BaseURL().String(), APIKey: identity.Token})
	if err != nil {
		return nil, err
	}
	var client *apiClient
	switch p := reader.(type) {
	case *embyProvider:
		client = p.client
	case *fnosProvider:
		client = p.client
	case *absProvider:
		client = p.client
	}
	if client == nil {
		return nil, errors.New("媒体服务不支持读取删除范围")
	}
	client.http.Transport = provider.Transport()
	client.apiKeyOptional = true
	provider = reader
	switch p := provider.(type) {
	case *absProvider:
		_, files, err := p.ItemFiles(ctx, id)
		if err != nil {
			return nil, err
		}
		paths := []string{}
		for _, f := range files {
			if f.IsStrm {
				paths = append(paths, f.Path)
			}
		}
		return paths, nil
	case *fnosProvider:
		return p.embyProvider.deletionPaths(ctx, id)
	case *embyProvider:
		return p.deletionPaths(ctx, id)
	default:
		return nil, errors.New("媒体服务不支持读取删除范围")
	}
}

func WithDeletionCredentials(ctx context.Context, r *http.Request) context.Context {
	ctx = WithClientCredentials(ctx, r)
	identity := contextClientIdentity(ctx)
	if identity.Token == "" {
		identity.Token = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	return context.WithValue(ctx, clientCredentialsContextKey{}, identity)
}

func embyPointerPaths(item embyItem) []string {
	paths := []string{}
	if strings.EqualFold(path.Ext(strings.ReplaceAll(item.Path, "\\", "/")), ".strm") {
		paths = append(paths, item.Path)
	}
	for _, source := range item.MediaSources {
		if strings.EqualFold(path.Ext(strings.ReplaceAll(source.Path, "\\", "/")), ".strm") {
			paths = append(paths, source.Path)
		}
	}
	return paths
}

func (p *embyProvider) deletionPaths(ctx context.Context, id string) ([]string, error) {
	// Deletion needs the original filesystem path and folder type, not only a
	// playback URL. Do not let PlaybackInfo replace valid Series/Season metadata.
	var item embyItem
	found := false
	for _, lookup := range []embyItemLookup{embyLookupByID, embyLookupByIDs} {
		loaded, err := p.fetchItemBy(ctx, id, lookup)
		if err == nil && loaded.ID != "" && (loaded.Type != "" || loaded.Path != "") {
			item = loaded
			found = true
			break
		}
	}
	if !found {
		var err error
		item, err = p.fetchItem(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	paths := embyPointerPaths(item)
	if len(paths) == 0 && !slicesContainsFolder(item.Type) {
		for _, source := range item.MediaSources {
			if strings.EqualFold(source.Container, "strm") {
				return nil, errors.New("媒体服务只返回播放URL，缺少原始STRM路径")
			}
		}
	}
	switch strings.ToLower(item.Type) {
	case "series", "season", "folder", "musicalbum":
		seen := map[string]bool{}
		total := -1
		for offset := 0; ; {
			var page embyItemsResponse
			query := url.Values{"ParentId": {id}, "Recursive": {"true"}, "Fields": {"Path,MediaSources"}, "StartIndex": {strconv.Itoa(offset)}, "Limit": {"500"}, "EnableTotalRecordCount": {"true"}}
			if err := p.client.getJSON(ctx, "/Items", query, &page); err != nil {
				return nil, err
			}
			if total < 0 {
				total = page.TotalRecordCount
			}
			if page.TotalRecordCount != total || total < offset+len(page.Items) || page.TotalRecordCount < 0 || page.TotalRecordCount > 100000 || len(page.Items) == 0 && offset < page.TotalRecordCount {
				return nil, errors.New("作品删除范围不完整")
			}
			for _, child := range page.Items {
				if child.ID == "" || seen[child.ID] {
					return nil, errors.New("作品删除分页重复或缺少标识")
				}
				seen[child.ID] = true
				childPaths := embyPointerPaths(child)
				if len(childPaths) == 0 && !slicesContainsFolder(child.Type) {
					for _, source := range child.MediaSources {
						if strings.EqualFold(source.Container, "strm") {
							return nil, errors.New("作品中有STRM缺少原始路径，范围不完整")
						}
					}
				}
				paths = append(paths, childPaths...)
			}
			offset += len(page.Items)
			if offset > 100000 {
				return nil, errors.New("作品删除范围超过十万项")
			}
			if offset >= page.TotalRecordCount {
				break
			}
		}
	}
	return paths, nil
}

func slicesContainsFolder(kind string) bool {
	switch strings.ToLower(kind) {
	case "series", "season", "folder", "musicalbum":
		return true
	}
	return false
}
