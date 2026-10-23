package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type pluginRepository struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Ref     string `json:"ref"`
	Enabled bool   `json:"enabled"`
}

type marketPlugin struct {
	ID          string `json:"id"`
	Repository  string `json:"repository"`
	Source      string `json:"source"`
	Name        string `json:"name"`
	Subtitle    string `json:"subtitle"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Labels      string `json:"labels"`
	Icon        string `json:"icon,omitempty"`
	Aether      bool   `json:"aether"`
}

type pluginMarketCache struct {
	Plugins []marketPlugin
	Updated time.Time
	Attempt time.Time
	Done    chan struct{}
	Error   string
}

var marketPart = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
var marketPluginID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

func normalizePluginRepository(repo pluginRepository) (pluginRepository, error) {
	repo.Name = strings.TrimSpace(repo.Name)
	u, err := url.Parse(strings.TrimSpace(repo.URL))
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return repo, errors.New("仓库地址须为不含凭据和查询参数的 HTTPS GitHub 仓库地址")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 4 && parts[2] == "tree" {
		if repo.Ref == "" {
			repo.Ref = parts[3]
		} else if repo.Ref != parts[3] {
			return repo, errors.New("仓库地址与所填分支不一致")
		}
		parts = parts[:2]
	}
	if len(parts) != 2 {
		return repo, errors.New("请填写 GitHub 仓库地址，不要填写文件或子目录地址")
	}
	parts[1] = strings.TrimSuffix(parts[1], ".git")
	for _, part := range parts {
		if !marketPart.MatchString(part) || part == "." || part == ".." {
			return repo, errors.New("仓库名称或作者无效")
		}
	}
	repo.URL = "https://github.com/" + parts[0] + "/" + parts[1]
	repo.Ref = strings.TrimSpace(repo.Ref)
	if repo.Ref == "" {
		repo.Ref = "main"
	}
	if len(repo.Ref) > 128 || strings.HasPrefix(repo.Ref, "/") || strings.HasSuffix(repo.Ref, "/") || strings.Contains(repo.Ref, "..") {
		return repo, errors.New("仓库分支或提交无效")
	}
	for _, part := range strings.Split(repo.Ref, "/") {
		if !marketPart.MatchString(part) {
			return repo, errors.New("仓库分支或提交无效")
		}
	}
	if repo.Name == "" {
		repo.Name = parts[1]
	}
	if len([]rune(repo.Name)) > 80 || strings.ContainsAny(repo.Name, "\x00\r\n") {
		return repo, errors.New("仓库名称须为1至80个字符")
	}
	return repo, nil
}

func validatePluginRepositories(repos []pluginRepository) error {
	if len(repos) > 20 {
		return errors.New("最多添加20个插件仓库")
	}
	ids, sources := map[string]bool{}, map[string]bool{}
	for _, repo := range repos {
		normal, err := normalizePluginRepository(repo)
		if err != nil {
			return err
		}
		key := strings.ToLower(normal.URL) + "\x00" + normal.Ref
		if !shareCodePattern.MatchString(repo.ID) || ids[repo.ID] || sources[key] || normal != repo {
			return errors.New("插件仓库重复或配置格式无效")
		}
		ids[repo.ID], sources[key] = true, true
	}
	return nil
}

func marketRevision(repos []pluginRepository) string {
	if repos == nil {
		repos = []pluginRepository{}
	}
	raw, _ := json.Marshal(repos)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func marketRawBase(repo pluginRepository) string {
	return "https://raw.githubusercontent.com/" + strings.TrimPrefix(repo.URL, "https://github.com/") + "/" + url.PathEscape(repo.Ref) + "/"
}

func marketIcon(repo pluginRepository, value string) string {
	if value == "" || len(value) > 512 || strings.ContainsAny(value, "\\?#\x00\r\n") {
		return ""
	}
	base := marketRawBase(repo)
	if strings.HasPrefix(value, base) {
		value = strings.TrimPrefix(value, base)
	} else if strings.Contains(value, ":") || strings.HasPrefix(value, "/") {
		return ""
	}
	if path.Clean(value) != value || value == "." || strings.HasPrefix(value, "../") {
		return ""
	}
	if !strings.Contains(value, "/") {
		value = "icons/" + value
	}
	for _, segment := range strings.Split(value, "/") {
		if !marketPart.MatchString(segment) {
			return ""
		}
	}
	return base + value
}

func decodeMarketIndex(raw []byte, repo pluginRepository) ([]marketPlugin, error) {
	if len(raw) > 1<<20 {
		return nil, errors.New("插件索引超过1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	depth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("插件索引不是合法JSON")
		}
		if d, ok := token.(json.Delim); ok {
			if d == '{' || d == '[' {
				depth++
			} else {
				depth--
			}
			if depth > 64 {
				return nil, errors.New("插件索引嵌套超过64层")
			}
		}
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, errors.New("插件索引须为以插件ID为键的JSON对象")
	}
	plugins := []marketPlugin{}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		id, ok := key.(string)
		if err != nil || !ok || !marketPluginID.MatchString(id) || seen[strings.ToLower(id)] || len(plugins) >= 4096 {
			return nil, errors.New("插件索引ID无效、重复或超过4096项")
		}
		seen[strings.ToLower(id)] = true
		var item struct {
			ID          string          `json:"id"`
			Name        string          `json:"name"`
			Subtitle    string          `json:"subtitle"`
			Description string          `json:"description"`
			Version     string          `json:"version"`
			Author      string          `json:"author"`
			Icon        string          `json:"icon"`
			Labels      json.RawMessage `json:"labels"`
			Aether      json.RawMessage `json:"aether"`
		}
		if decoder.Decode(&item) != nil || item.ID != "" && !strings.EqualFold(item.ID, id) || strings.TrimSpace(item.Name) == "" || len([]rune(item.Name)) > 128 || item.Version == "" || len(item.Version) > 64 || len(item.Description) > 8000 || len(item.Subtitle) > 640 || len(item.Author) > 512 || strings.ContainsAny(item.Name+item.Version+item.Subtitle+item.Author, "\x00\r\n") {
			return nil, errors.New("插件索引元数据无效，请检查名称、版本及文本长度")
		}
		labels := ""
		if len(item.Labels) != 0 && string(item.Labels) != "null" {
			if json.Unmarshal(item.Labels, &labels) != nil {
				var values []string
				if json.Unmarshal(item.Labels, &values) != nil || len(values) > 64 {
					return nil, errors.New("插件标签无效")
				}
				labels = strings.Join(values, ",")
			}
			if len(labels) > 1024 {
				return nil, errors.New("插件标签过长")
			}
		}
		var host struct {
			API int `json:"api"`
		}
		compatible := len(item.Aether) > 0 && json.Unmarshal(item.Aether, &host) == nil && host.API == 1
		plugins = append(plugins, marketPlugin{ID: id, Repository: repo.ID, Source: repo.Name, Name: item.Name, Subtitle: item.Subtitle, Description: item.Description, Version: item.Version, Author: item.Author, Labels: labels, Icon: marketIcon(repo, item.Icon), Aether: compatible})
	}
	if _, err := decoder.Token(); err != nil {
		return nil, errors.New("插件索引不完整")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("插件索引包含多段内容")
	}
	sort.SliceStable(plugins, func(i, j int) bool { return strings.ToLower(plugins[i].ID) < strings.ToLower(plugins[j].ID) })
	return plugins, nil
}

func fetchMarketIndex(ctx context.Context, repo pluginRepository) ([]marketPlugin, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client := &http.Client{Transport: apiClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, name := range []string{"package.json", "package.aether.json", "package.v3.json"} {
		r, err := http.NewRequestWithContext(ctx, "GET", marketRawBase(repo)+name, nil)
		if err != nil {
			return nil, errors.New("仓库请求无效")
		}
		response, err := client.Do(r)
		if err != nil {
			return nil, errors.New("插件仓库连接失败，请检查服务器网络")
		}
		if response.StatusCode == 404 {
			response.Body.Close()
			continue
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			return nil, fmt.Errorf("插件仓库 HTTP %d，未读取新索引", response.StatusCode)
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		response.Body.Close()
		if readErr != nil {
			return nil, errors.New("插件索引读取失败")
		}
		return decodeMarketIndex(raw, repo)
	}
	return nil, errors.New("仓库未提供package.json、package.aether.json或package.v3.json索引")
}

func (a *App) marketIndex(ctx context.Context, repo pluginRepository, force bool) pluginMarketCache {
	key := marketRevision([]pluginRepository{repo})
	for {
		a.marketMu.Lock()
		if a.marketCache == nil {
			a.marketCache = map[string]*pluginMarketCache{}
		}
		cached := a.marketCache[key]
		if cached != nil && cached.Done != nil {
			done := cached.Done
			a.marketMu.Unlock()
			select {
			case <-ctx.Done():
				return pluginMarketCache{Error: "仓库读取已取消"}
			case <-done:
				force = false
				continue
			}
		}
		if cached != nil && (!force && (cached.Error == "" && time.Since(cached.Updated) < 30*time.Minute || cached.Error != "" && time.Since(cached.Attempt) < time.Minute)) {
			value := *cached
			a.marketMu.Unlock()
			return value
		}
		if cached == nil && len(a.marketCache) >= 64 {
			oldest, stamp := "", time.Now()
			for candidate, value := range a.marketCache {
				if value.Done == nil && value.Attempt.Before(stamp) {
					oldest, stamp = candidate, value.Attempt
				}
			}
			if oldest == "" {
				a.marketMu.Unlock()
				return pluginMarketCache{Error: "仓库读取繁忙，请稍后重试"}
			}
			delete(a.marketCache, oldest)
		}
		if a.marketGate == nil {
			a.marketGate = make(chan struct{}, 4)
		}
		gate := a.marketGate
		next := &pluginMarketCache{Done: make(chan struct{}), Attempt: time.Now()}
		if cached != nil {
			next.Plugins, next.Updated = cached.Plugins, cached.Updated
		}
		a.marketCache[key] = next
		a.marketMu.Unlock()
		var plugins []marketPlugin
		var err error
		select {
		case gate <- struct{}{}:
			plugins, err = fetchMarketIndex(ctx, repo)
			<-gate
		case <-ctx.Done():
			err = errors.New("仓库读取已取消")
		}
		a.marketMu.Lock()
		if err == nil {
			next.Plugins, next.Updated, next.Error = plugins, time.Now(), ""
		} else {
			next.Error = err.Error()
		}
		close(next.Done)
		next.Done = nil
		result := *next
		a.marketMu.Unlock()
		return result
	}
}

func (a *App) pluginRepositories(w http.ResponseWriter, r *http.Request) {
	current := a.store.snapshotWithLogLimit(0).PluginRepositories
	if r.Method == "GET" {
		revision := marketRevision(current)
		if current == nil {
			current = []pluginRepository{}
		}
		jsonResponse(w, 200, map[string]any{"repositories": current, "revision": revision})
		return
	}
	if r.Method != "PUT" {
		w.WriteHeader(405)
		return
	}
	var input struct {
		Repositories []pluginRepository `json:"repositories"`
		Revision     string             `json:"revision"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Revision != marketRevision(current) {
		fail(w, 409, errors.New("仓库配置已变化，请重新打开后编辑"))
		return
	}
	for i := range input.Repositories {
		value, err := normalizePluginRepository(input.Repositories[i])
		if err != nil {
			fail(w, 400, err)
			return
		}
		if value.ID == "" {
			value.ID = id()
		}
		input.Repositories[i] = value
	}
	if err := validatePluginRepositories(input.Repositories); err != nil {
		fail(w, 400, err)
		return
	}
	for _, repo := range input.Repositories {
		unchanged := false
		for _, old := range current {
			if old == repo {
				unchanged = true
			}
		}
		if repo.Enabled && !unchanged {
			if result := a.marketIndex(r.Context(), repo, false); result.Error != "" {
				fail(w, 400, errors.New(repo.Name+"："+result.Error))
				return
			}
		}
	}
	err := a.store.update(func(st *State) error {
		if marketRevision(st.PluginRepositories) != input.Revision {
			return errors.New("仓库配置已变化，请重新打开后编辑")
		}
		st.PluginRepositories = input.Repositories
		return nil
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	// Bound obsolete snapshots after repository edits; keep active fetches alive.
	a.marketMu.Lock()
	keep := map[string]bool{}
	for _, repo := range input.Repositories {
		keep[marketRevision([]pluginRepository{repo})] = true
	}
	for key, value := range a.marketCache {
		if !keep[key] && value.Done == nil {
			delete(a.marketCache, key)
		}
	}
	a.marketMu.Unlock()
	a.store.event("info", "system", "插件仓库配置已保存")
	jsonResponse(w, 200, map[string]any{"repositories": input.Repositories, "revision": marketRevision(input.Repositories)})
}

func (a *App) pluginMarketCatalog(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query().Get("q")) > 512 {
		fail(w, 400, errors.New("搜索关键词过长"))
		return
	}
	repos := a.store.snapshotWithLogLimit(0).PluginRepositories
	results := make([]pluginMarketCache, len(repos))
	gate := make(chan struct{}, 4)
	var group sync.WaitGroup
	for i, repo := range repos {
		if !repo.Enabled {
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			select {
			case gate <- struct{}{}:
				defer func() { <-gate }()
			case <-r.Context().Done():
				return
			}
			results[i] = a.marketIndex(r.Context(), repo, r.URL.Query().Get("refresh") == "1")
		}()
	}
	group.Wait()
	plugins, failures := []marketPlugin{}, []map[string]string{}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	for i, result := range results {
		if result.Error != "" {
			failures = append(failures, map[string]string{"id": repos[i].ID, "name": repos[i].Name, "message": result.Error})
		}
		for _, plugin := range result.Plugins {
			if query == "" || strings.Contains(strings.ToLower(plugin.ID+" "+plugin.Name+" "+plugin.Subtitle+" "+plugin.Description+" "+plugin.Labels), query) {
				plugins = append(plugins, plugin)
			}
		}
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	page, count = max(1, min(page, 100000)), max(1, min(count, 100))
	if r.URL.Query().Get("count") == "" {
		count = 48
	}
	start := min((page-1)*count, len(plugins))
	jsonResponse(w, 200, map[string]any{"plugins": plugins[start:min(start+count, len(plugins))], "total": len(plugins), "page": page, "failures": failures, "runtimeAvailable": false})
}
