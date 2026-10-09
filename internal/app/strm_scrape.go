package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type scrapeSettings struct {
	WriteMode string `json:"writeMode"`
	Episodes  bool   `json:"episodes"`
	Fanart    bool   `json:"fanart"`
	Actors    bool   `json:"actors"`
	Excluded  string `json:"excluded"`
}
type scrapeItem struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Year    string `json:"year"`
	Kind    string `json:"kind"`
	TMDB    int    `json:"tmdb"`
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Poster  string `json:"poster,omitempty"`
	Manual  bool   `json:"manual,omitempty"`
}
type scrapeProgress struct {
	Running   bool   `json:"running"`
	TaskID    string `json:"taskId"`
	Done      int    `json:"done"`
	Total     int    `json:"total"`
	Failed    int    `json:"failed"`
	Unmatched int    `json:"unmatched"`
	Message   string `json:"message"`
}
type scrapeIndex struct {
	Root  string       `json:"root"`
	Items []scrapeItem `json:"items"`
}

var scrapeEpisode = regexp.MustCompile(`(?i)S(\d{1,3})[ ._-]*E(\d{1,4})(?:$|[^0-9])`)
var scrapeYear = regexp.MustCompile(`(?:^|[ ._(\[])((?:19|20)\d{2})(?:$|[ ._)\]])`)
var scrapeSuffix = regexp.MustCompile(`(?i)\b(?:2160p|1080p|720p|4k|web[ .-]?dl|bluray|remux|h26[45]|x26[45])\b.*$`)
var scrapeID = regexp.MustCompile(`(?i)[{\[]tmdb[-=](\d+)[}\]]`)

func recognizeSTRM(name string) scrapeItem {
	title := strings.TrimSuffix(name, path.Ext(name))
	if isVideo(title) {
		title = strings.TrimSuffix(title, path.Ext(title))
	}
	item := scrapeItem{Kind: "movie", Status: "pending"}
	if m := scrapeID.FindStringSubmatch(title); len(m) > 0 {
		item.TMDB, _ = strconv.Atoi(m[1])
		title = scrapeID.ReplaceAllString(title, "")
	}
	if m := scrapeEpisode.FindStringSubmatch(title); len(m) > 0 {
		item.Kind = "tv"
		item.Season, _ = strconv.Atoi(m[1])
		item.Episode, _ = strconv.Atoi(m[2])
		title = title[:scrapeEpisode.FindStringIndex(title)[0]]
	} else {
		title = recognizeEpisode(title, &item)
	}
	yearStart := 0
	if m := scrapeYear.FindStringIndex(title); m != nil && m[0] == 0 {
		yearStart = 4
	}
	if m := scrapeYear.FindStringSubmatch(title[yearStart:]); len(m) > 0 {
		item.Year = m[1]
		title = title[:yearStart+scrapeYear.FindStringIndex(title[yearStart:])[0]]
	}
	// A quality token preceding a Chinese title is not a release suffix.
	if suffix := scrapeSuffix.FindStringIndex(title); suffix != nil && !strings.ContainsFunc(title[suffix[0]:], func(r rune) bool { return r >= '\u3400' && r <= '\u9fff' }) {
		title = title[:suffix[0]]
	}
	item.Title = strings.Trim(strings.NewReplacer(".", " ", "_", " ").Replace(title), " -()[]")
	return item
}
func (a *App) scrapeConfig() scrapeSettings {
	cfg := scrapeSettings{WriteMode: "missing", Episodes: true}
	b, err := os.ReadFile(filepath.Join(a.store.dir, "organize", "strm-scrape.json"))
	if err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	return cfg
}
func (a *App) scrapeIndexPath(task string) string {
	sum := sha256.Sum256([]byte(task))
	return filepath.Join(a.dataDir, "cache", "scrape", hex.EncodeToString(sum[:])+".json")
}
func (a *App) scrapeRoot(taskID string) (string, error) {
	for _, t := range a.store.snapshotWithLogLimit(0).Tasks {
		if t.ID == taskID && t.Kind == "strm" && !t.ScrapeExcluded {
			base, rel, err := a.outputLocation(t.Target)
			if err != nil {
				return "", err
			}
			if t.Source != "" && t.Source != "/" {
				var s Storage
				for _, pool := range a.store.snapshotWithLogLimit(0).Storages {
					if pool.ID == t.StorageID {
						s = pool
						break
					}
				}
				if s.ID == "" {
					return "", errors.New("任务源存储不存在")
				}
				if t.Source != rootOf(s) {
					name := t.SourceLabel
					if s.Type == "local" || s.Type == "openlist" || s.Type == "webdav" {
						name = path.Base(strings.TrimRight(t.Source, "/"))
					}
					if !safeName(name) {
						return "", errors.New("源目录名称无效，请重新选择任务源目录")
					}
					rel = path.Join(rel, name)
				}
			}
			return filepath.Abs(filepath.Join(base, filepath.FromSlash(rel)))
		}
	}
	return "", errors.New("请选择 STRM 任务")
}
func (a *App) loadScrapeIndex(task, root string) scrapeIndex {
	index := scrapeIndex{Root: root, Items: []scrapeItem{}}
	b, err := os.ReadFile(a.scrapeIndexPath(task))
	var old scrapeIndex
	if err == nil && json.Unmarshal(b, &old) == nil && old.Root == root {
		filtered := make([]scrapeItem, 0, len(old.Items))
		for _, item := range old.Items {
			if fs.ValidPath(item.Path) && strings.EqualFold(path.Ext(item.Path), ".strm") {
				filtered = append(filtered, item)
			}
		}
		old.Items = filtered
		return old
	}
	return index
}
func (a *App) saveScrapeIndex(task string, index scrapeIndex) error {
	name := a.scrapeIndexPath(task)
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(index)
	if err != nil {
		return err
	}
	return atomicWrite(name, b)
}
func scanSTRM(ctx context.Context, root *os.Root, previous scrapeIndex, cfg scrapeSettings) ([]scrapeItem, error) {
	items := []scrapeItem{}
	old := map[string]scrapeItem{}
	tvDirs := map[string]bool{}
	for _, item := range previous.Items {
		old[item.Path] = item
	}
	err := fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			if name != "." && excluded(name, cfg.Excluded) {
				return fs.SkipDir
			}
			if strings.Count(name, "/") > 128 {
				return errors.New("目录层级过深")
			}
			if _, seasonal := seasonDirectory(path.Base(name)); seasonal && name != "." {
				tvDirs[scrapeWorkDir(path.Join(name, "episode.strm"))] = true
			}
			return nil
		}
		if !entry.Type().IsRegular() || !strings.EqualFold(path.Ext(name), ".strm") {
			if entry.Type().IsRegular() && strings.EqualFold(path.Base(name), "tvshow.nfo") {
				tvDirs[path.Dir(name)] = true
			}
			return nil
		}
		item := recognizeSTRMPath(name)
		item.Path = name
		items = append(items, item)
		return nil
	})
	if err == nil {
		inferScrapeWorks(items, tvDirs)
		for i := range items {
			item := &items[i]
			if saved, ok := old[item.Path]; ok && saved.TMDB > 0 {
				if saved.Manual || saved.Kind == item.Kind {
					title, year, season, episode := item.Title, item.Year, item.Season, item.Episode
					*item = saved
					if !saved.Manual {
						item.Title = title
						if year != "" {
							item.Year = year
						}
					}
					if item.Kind == "tv" && episode > 0 {
						item.Season, item.Episode = season, episode
					}
				}
			}
		}
	}
	return items, err
}
func (a *App) strmScrape(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	if action == "directories" && r.Method == "GET" {
		a.scrapeDirectories(w, r)
		return
	}
	if action == "cover" && (r.Method == "GET" || r.Method == "HEAD") {
		a.scrapeCover(w, r)
		return
	}
	if action == "settings" {
		if r.Method == "GET" {
			jsonResponse(w, 200, a.scrapeConfig())
			return
		}
		if r.Method != "PUT" {
			w.WriteHeader(405)
			return
		}
		var cfg scrapeSettings
		if !decode(w, r, &cfg) {
			return
		}
		if (cfg.WriteMode != "missing" && cfg.WriteMode != "overwrite") || len(cfg.Excluded) > 4096 {
			fail(w, 400, errors.New("刮削设置无效"))
			return
		}
		b, _ := json.Marshal(cfg)
		if err := atomicWrite(filepath.Join(a.store.dir, "organize", "strm-scrape.json"), b); err != nil {
			fail(w, 500, err)
			return
		}
		jsonResponse(w, 200, cfg)
		return
	}
	if action == "candidates" && r.Method == "POST" {
		var in struct {
			Query string `json:"query"`
			Kind  string `json:"kind"`
		}
		if !decode(w, r, &in) {
			return
		}
		if strings.TrimSpace(in.Query) == "" || len(in.Query) > 300 || (in.Kind != "movie" && in.Kind != "tv") {
			fail(w, 400, errors.New("候选搜索参数无效"))
			return
		}
		st := a.store.snapshotWithLogLimit(0)
		cfg := pluginDefaults("tmdb", st.Plugins["tmdb"])
		if !cfg.Enabled || cfg.APIKey == "" {
			fail(w, 400, errors.New("请配置并启用 TMDB 插件"))
			return
		}
		client, closeIdle, err := pluginClient(st.Plugins["proxy"])
		if err != nil {
			fail(w, 400, err)
			return
		}
		defer closeIdle()
		var result struct {
			Results []tmdbMedia `json:"results"`
		}
		if err := tmdbGet(r.Context(), client, cfg, "search/"+in.Kind, url.Values{"query": {in.Query}}, &result); err != nil {
			fail(w, 502, err)
			return
		}
		type candidate struct {
			ID       int    `json:"id"`
			Title    string `json:"title"`
			Name     string `json:"name"`
			Year     string `json:"year"`
			Overview string `json:"overview"`
			Poster   string `json:"poster"`
		}
		out := make([]candidate, 0, len(result.Results))
		for _, item := range result.Results {
			year := item.Release
			if year == "" {
				year = item.AirDate
			}
			poster := item.Poster
			if poster != "" && cfg.ImageURL != "" {
				poster = strings.TrimRight(cfg.ImageURL, "/") + "/t/p/w342" + poster
			}
			out = append(out, candidate{ID: item.ID, Title: item.Title, Name: item.Name, Year: year, Overview: item.Overview, Poster: poster})
		}
		jsonResponse(w, 200, out)
		return
	}
	a.scrapeMu.Lock()
	defer a.scrapeMu.Unlock()
	if action == "status" && r.Method == "GET" {
		jsonResponse(w, 200, a.scrapeProgress)
		return
	}
	if action == "items" && r.Method == "GET" {
		task := r.URL.Query().Get("taskId")
		root, err := a.scrapeRoot(task)
		if err != nil {
			fail(w, 400, err)
			return
		}
		index := a.loadScrapeIndex(task, root)
		// Discover generated files without requiring TMDB or overwriting an active scrape.
		if !a.scrapeProgress.Running {
			dir, openErr := os.OpenRoot(root)
			if openErr != nil && !errors.Is(openErr, os.ErrNotExist) {
				fail(w, 500, errors.New("无法读取 STRM 生成目录"))
				return
			}
			if openErr == nil {
				items, scanErr := scanSTRM(r.Context(), dir, index, a.scrapeConfig())
				dir.Close()
				if scanErr != nil {
					fail(w, 500, scanErr)
					return
				}
				index.Items = items
				if err := a.saveScrapeIndex(task, index); err != nil {
					fail(w, 500, err)
					return
				}
			} else {
				index.Items = []scrapeItem{}
			}
		}
		items := append([]scrapeItem(nil), index.Items...)
		if dir, err := os.OpenRoot(root); err == nil {
			localScrapePosters(dir, task, items)
			dir.Close()
		}
		if r.URL.Query().Get("group") == "true" {
			jsonResponse(w, 200, scrapeWorks(items))
		} else {
			jsonResponse(w, 200, items)
		}
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	if action == "stop" {
		if a.scrapeCancel != nil {
			a.scrapeCancel()
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	if action != "scan" && action != "run" && action != "identify" && action != "match" && action != "reset" {
		w.WriteHeader(404)
		return
	}
	if a.scrapeProgress.Running {
		fail(w, 409, errors.New("刮削正在执行，请先停止"))
		return
	}
	var in struct {
		TaskID         string   `json:"taskId"`
		Path           string   `json:"path"`
		TMDB           int      `json:"tmdb"`
		Kind           string   `json:"kind"`
		Group          bool     `json:"group"`
		Confirmed      bool     `json:"confirmed"`
		Scope          string   `json:"scope"`
		Scopes         []string `json:"scopes"`
		ExcludedScopes []string `json:"excludedScopes"`
		Paths          []string `json:"paths"`
		DeleteFiles    bool     `json:"deleteFiles"`
		Scrape         bool     `json:"scrape"`
		Reidentify     bool     `json:"reidentify"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Scope != "" && (!fs.ValidPath(in.Scope) || in.Scope == ".") {
		fail(w, 400, errors.New("无效的库目录"))
		return
	}
	if len(in.Scopes) > 1000 || len(in.ExcludedScopes) > 1000 || len(in.Paths) > 10000 {
		fail(w, 400, errors.New("目录范围过多"))
		return
	}
	for _, scope := range in.Scopes {
		if !fs.ValidPath(scope) || scope == "." {
			fail(w, 400, errors.New("无效的库目录"))
			return
		}
	}
	for _, scope := range in.ExcludedScopes {
		if !fs.ValidPath(scope) {
			fail(w, 400, errors.New("无效的排除目录"))
			return
		}
	}
	rootName, err := a.scrapeRoot(in.TaskID)
	if err != nil {
		fail(w, 400, err)
		return
	}
	index := a.loadScrapeIndex(in.TaskID, rootName)
	selectedWork := ""
	selectedWorks := map[string]bool{}
	requestedPaths := map[string]bool{}
	for _, p := range in.Paths {
		requestedPaths[p] = false
	}
	for _, item := range index.Items {
		if _, ok := requestedPaths[item.Path]; ok {
			requestedPaths[item.Path] = true
			selectedWorks[scrapeWorkKey(item)] = true
		}
		if item.Path == in.Path {
			selectedWork = scrapeWorkKey(item)
		}
	}
	for _, found := range requestedPaths {
		if !found {
			fail(w, 400, errors.New("所选作品已变化，请刷新后重试"))
			return
		}
	}
	selected := func(item scrapeItem) bool {
		for _, excluded := range in.ExcludedScopes {
			if excluded == "." || strings.HasPrefix(item.Path, excluded+"/") {
				return false
			}
		}
		if len(in.Scopes) > 0 {
			matched := false
			for _, scope := range in.Scopes {
				if strings.HasPrefix(item.Path, scope+"/") {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
		if in.Scope != "" && !strings.HasPrefix(item.Path, in.Scope+"/") {
			return false
		}
		if len(in.Paths) > 0 {
			return selectedWorks[scrapeWorkKey(item)]
		}
		return in.Path == "" || item.Path == in.Path || (in.Group && selectedWork != "" && scrapeWorkKey(item) == selectedWork)
	}
	if action == "reset" {
		if !in.Confirmed || (selectedWork == "" && len(selectedWorks) == 0) {
			fail(w, 400, errors.New("请确认要重置的作品"))
			return
		}
		if in.DeleteFiles {
			a.runMu.Lock()
			if len(a.running) != 0 {
				a.runMu.Unlock()
				fail(w, 409, errors.New("有任务正在执行，请等待完成后重置"))
				return
			}
			removed, err := resetScrapeFiles(r.Context(), rootName, index.Items, selected)
			a.runMu.Unlock()
			if err != nil {
				fail(w, 400, err)
				return
			}
			a.store.event("info", "tasks", fmt.Sprintf("STRM作品重置：删除%d个非STRM文件", removed))
		}
		for i := range index.Items {
			if selected(index.Items[i]) {
				index.Items[i].TMDB = 0
				index.Items[i].Manual = false
				index.Items[i].Poster, index.Items[i].Message = "", ""
				index.Items[i].Status = "pending"
			}
		}
		if err := a.saveScrapeIndex(in.TaskID, index); err != nil {
			fail(w, 500, err)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	if action == "match" {
		if in.TMDB < 1 || (in.Kind != "movie" && in.Kind != "tv") {
			fail(w, 400, errors.New("请输入有效的 TMDB ID 和媒体类型"))
			return
		}
		found := false
		for i := range index.Items {
			if in.Path != "" && selected(index.Items[i]) {
				index.Items[i].TMDB = in.TMDB
				index.Items[i].Kind = in.Kind
				index.Items[i].Manual = true
				index.Items[i].Status = "pending"
				index.Items[i].Message = ""
				index.Items[i].Poster = ""
				found = true
			}
		}
		if !found {
			http.NotFound(w, r)
			return
		}
		if err := a.saveScrapeIndex(in.TaskID, index); err != nil {
			fail(w, 500, err)
			return
		}
		if !in.Scrape {
			jsonResponse(w, 200, map[string]bool{"ok": true})
			return
		}
		action = "run"
		for _, item := range index.Items {
			if item.Path == in.Path {
				selectedWork = scrapeWorkKey(item)
				break
			}
		}
	}
	root, err := os.OpenRoot(rootName)
	if err != nil {
		fail(w, 400, errors.New("生成目录不存在，请先生成 STRM"))
		return
	}
	st := a.store.snapshotWithLogLimit(0)
	cfg, tmdb := a.scrapeConfig(), pluginDefaults("tmdb", st.Plugins["tmdb"])
	if (action == "run" || action == "identify") && (!tmdb.Enabled || tmdb.APIKey == "") {
		root.Close()
		fail(w, 400, errors.New("请配置并启用 TMDB 插件"))
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.scrapeCancel = cancel
	a.scrapeProgress = scrapeProgress{Running: true, TaskID: in.TaskID, Message: "正在扫描"}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer root.Close()
		defer cancel()
		items, runErr := scanSTRM(ctx, root, index, cfg)
		if runErr == nil {
			index.Items = items
			if in.Reidentify {
				for i := range index.Items {
					if selected(index.Items[i]) {
						index.Items[i].TMDB = 0
						index.Items[i].Manual = false
						index.Items[i].Status = "pending"
						index.Items[i].Poster = ""
					}
				}
			}
			a.scrapeMu.Lock()
			a.scrapeProgress.Total = len(items)
			if in.Path != "" || in.Scope != "" || len(in.Scopes) > 0 || len(in.ExcludedScopes) > 0 || len(in.Paths) > 0 {
				a.scrapeProgress.Total = 0
				for _, item := range items {
					if selected(item) {
						a.scrapeProgress.Total++
					}
				}
			}
			selectedCount := a.scrapeProgress.Total
			a.scrapeMu.Unlock()
			if (action == "run" || action == "identify") && selectedCount > 10000 {
				runErr = errors.New("索引已完整刷新；单次自动刮削最多处理 10000 个 STRM，请选择子目录分批处理")
			}
			if runErr == nil && (action == "run" || action == "identify") {
				client, closeIdle, clientErr := pluginClient(st.Plugins["proxy"])
				if clientErr != nil {
					runErr = clientErr
				} else {
					defer closeIdle()
					identified := map[string]scrapeItem{}
					for i := range index.Items {
						if ctx.Err() != nil {
							runErr = ctx.Err()
							break
						}
						item := &index.Items[i]
						if !selected(*item) {
							continue
						}
						item.Message = ""
						var e error
						if action == "identify" {
							key := scrapeWorkKey(*item)
							if known, ok := identified[key]; ok {
								item.TMDB, item.Title, item.Year, item.Poster, item.Status, item.Message = known.TMDB, known.Title, known.Year, known.Poster, known.Status, known.Message
							} else {
								_, e = resolveScrapeMedia(ctx, client, tmdb, cfg, item)
								if e == nil && item.TMDB > 0 && item.Status != "ok" {
									item.Status = "pending"
								}
								if e == nil {
									identified[key] = *item
								}
							}
						} else {
							e = scrapeOne(ctx, root, client, tmdb, cfg, item)
						}
						if action == "identify" && in.Scrape && e == nil && item.TMDB > 0 {
							e = scrapeOne(ctx, root, client, tmdb, cfg, item)
						}
						if e != nil {
							item.Status = "error"
							item.Message = e.Error()
						}
						a.scrapeMu.Lock()
						a.scrapeProgress.Done++
						if e != nil {
							a.scrapeProgress.Failed++
						}
						if item.Status == "miss" || item.Status == "doubt" {
							a.scrapeProgress.Unmatched++
						}
						a.scrapeProgress.Message = item.Title
						a.scrapeMu.Unlock()
						select {
						case <-ctx.Done():
							runErr = ctx.Err()
						case <-time.After(200 * time.Millisecond):
						}
					}
				}
			}
			if e := a.saveScrapeIndex(in.TaskID, index); e != nil {
				runErr = e
			}
		}
		a.scrapeMu.Lock()
		message := fmt.Sprintf("刮削结束：已处理 %d，失败 %d，待匹配 %d", a.scrapeProgress.Done, a.scrapeProgress.Failed, a.scrapeProgress.Unmatched)
		if action == "identify" {
			message = fmt.Sprintf("识别结束：%d 个文件，%d 个待匹配", a.scrapeProgress.Done, a.scrapeProgress.Unmatched)
		}
		if action == "scan" {
			message = "索引已刷新"
			a.scrapeProgress.Done = a.scrapeProgress.Total
		}
		if runErr != nil {
			message = runErr.Error()
			if errors.Is(runErr, context.Canceled) {
				message = "刮削已停止"
			}
		}
		a.scrapeProgress.Running = false
		a.scrapeProgress.Message = message
		a.scrapeCancel = nil
		a.scrapeMu.Unlock()
		level := "info"
		if runErr != nil {
			level = "warn"
		}
		a.store.event(level, "tasks", "STRM "+message)
	}()
	jsonResponse(w, 202, a.scrapeProgress)
}

type tmdbMedia struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	Name          string  `json:"name"`
	OriginalTitle string  `json:"original_title"`
	Overview      string  `json:"overview"`
	Release       string  `json:"release_date"`
	AirDate       string  `json:"first_air_date"`
	Poster        string  `json:"poster_path"`
	Backdrop      string  `json:"backdrop_path"`
	Still         string  `json:"still_path"`
	Rating        float64 `json:"vote_average"`
	Runtime       int     `json:"runtime"`
	Credits       struct {
		Cast []struct {
			Name      string `json:"name"`
			Character string `json:"character"`
		} `json:"cast"`
	} `json:"credits"`
}

func tmdbGet(ctx context.Context, client *http.Client, cfg PluginConfig, endpoint string, q url.Values, out any) error {
	if err := waitTMDB(ctx, cfg); err != nil {
		return err
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("language", cfg.Language)
	key := cfg.APIKey
	if len(key) == 32 {
		q.Set("api_key", key)
		key = ""
	}
	return pluginJSON(ctx, client, "GET", strings.TrimRight(cfg.APIURL, "/")+"/3/"+endpoint+"?"+q.Encode(), key, nil, out)
}
func normalizedTitle(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(s)), " "))
}

type nfoActor struct {
	Name string `xml:"name"`
	Role string `xml:"role"`
}
type scrapeNFO struct {
	XMLName  xml.Name
	Title    string     `xml:"title"`
	Original string     `xml:"originaltitle,omitempty"`
	Plot     string     `xml:"plot,omitempty"`
	TMDB     int        `xml:"tmdbid"`
	Year     string     `xml:"year,omitempty"`
	Rating   float64    `xml:"rating,omitempty"`
	Runtime  int        `xml:"runtime,omitempty"`
	Season   int        `xml:"season,omitempty"`
	Episode  int        `xml:"episode,omitempty"`
	Actors   []nfoActor `xml:"actor,omitempty"`
}

func resolveScrapeMedia(ctx context.Context, client *http.Client, tmdb PluginConfig, cfg scrapeSettings, item *scrapeItem) (tmdbMedia, error) {
	if item.TMDB == 0 {
		var result struct {
			Results []tmdbMedia `json:"results"`
		}
		if err := tmdbGet(ctx, client, tmdb, "search/"+item.Kind, url.Values{"query": {item.Title}}, &result); err != nil {
			return tmdbMedia{}, err
		}
		matches := []tmdbMedia{}
		for _, m := range result.Results {
			title, date := m.Title, m.Release
			if item.Kind == "tv" {
				title, date = m.Name, m.AirDate
			}
			if normalizedTitle(title) == normalizedTitle(item.Title) && (item.Year == "" || strings.HasPrefix(date, item.Year)) {
				matches = append(matches, m)
			}
		}
		if len(matches) != 1 {
			item.Status = "miss"
			item.Message = "未找到唯一匹配，请手动指定 TMDB ID"
			if len(matches) > 1 {
				item.Status = "doubt"
				item.Message = "存在多个同名匹配，请手动确认"
			}
			for _, candidate := range result.Results {
				if candidate.Poster != "" {
					item.Poster = strings.TrimRight(tmdb.ImageURL, "/") + "/t/p/w342" + candidate.Poster
					break
				}
			}
			return tmdbMedia{}, nil
		}
		item.TMDB = matches[0].ID
	}
	var media tmdbMedia
	endpoint := fmt.Sprintf("%s/%d", item.Kind, item.TMDB)
	q := url.Values{}
	if cfg.Actors {
		q.Set("append_to_response", "credits")
	}
	if err := tmdbGet(ctx, client, tmdb, endpoint, q, &media); err != nil {
		return tmdbMedia{}, err
	}
	title := media.Title
	if item.Kind == "tv" {
		title = media.Name
	}
	if title == "" || media.ID != item.TMDB {
		return tmdbMedia{}, errors.New("TMDB 详情无效")
	}
	item.Title = title
	if media.Poster != "" && tmdb.ImageURL != "" {
		item.Poster = strings.TrimRight(tmdb.ImageURL, "/") + "/t/p/w342" + media.Poster
	}
	release := media.Release
	if item.Kind == "tv" {
		release = media.AirDate
	}
	if len(release) >= 4 {
		item.Year = release[:4]
	}
	return media, nil
}

func scrapeOne(ctx context.Context, root *os.Root, client *http.Client, tmdb PluginConfig, cfg scrapeSettings, item *scrapeItem) error {
	media, err := resolveScrapeMedia(ctx, client, tmdb, cfg, item)
	if err != nil || media.ID == 0 {
		return err
	}
	title := item.Title
	nfo := scrapeNFO{XMLName: xml.Name{Local: "movie"}, Title: title, Original: media.OriginalTitle, Plot: media.Overview, TMDB: item.TMDB, Rating: media.Rating, Runtime: media.Runtime, Year: item.Year}
	if cfg.Actors {
		for i, actor := range media.Credits.Cast {
			if i >= 20 {
				break
			}
			nfo.Actors = append(nfo.Actors, nfoActor{actor.Name, actor.Character})
		}
	}
	stem := strings.TrimSuffix(item.Path, path.Ext(item.Path))
	incremental := cfg.WriteMode != "overwrite"
	writeNFO := func(name string, value scrapeNFO) error {
		b, err := xml.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		return writeCASOutput(root, name, append([]byte(xml.Header), b...), incremental)
	}
	posterPath, fanartPath := stem+"-poster.jpg", stem+"-fanart.jpg"
	if item.Kind == "tv" {
		nfo.XMLName.Local = "tvshow"
		dir := scrapeWorkDir(item.Path)
		if err := writeNFO(path.Join(dir, "tvshow.nfo"), nfo); err != nil {
			return err
		}
		posterPath, fanartPath = path.Join(dir, "poster.jpg"), path.Join(dir, "fanart.jpg")
		if cfg.Episodes && item.Episode > 0 {
			var episode tmdbMedia
			if err := tmdbGet(ctx, client, tmdb, fmt.Sprintf("tv/%d/season/%d/episode/%d", item.TMDB, item.Season, item.Episode), nil, &episode); err != nil {
				return err
			}
			if episode.Name == "" {
				return errors.New("TMDB 分集详情无效")
			}
			nfo.XMLName.Local = "episodedetails"
			nfo.Title = episode.Name
			nfo.Plot = episode.Overview
			nfo.Season = item.Season
			nfo.Episode = item.Episode
			if err := writeNFO(stem+".nfo", nfo); err != nil {
				return err
			}
			if err := scrapeImage(ctx, root, client, tmdb, episode.Still, stem+"-thumb.jpg", incremental); err != nil {
				return err
			}
		}
	} else if err := writeNFO(stem+".nfo", nfo); err != nil {
		return err
	}
	if err := scrapeImage(ctx, root, client, tmdb, media.Poster, posterPath, incremental); err != nil {
		return err
	}
	if cfg.Fanart {
		if err := scrapeImage(ctx, root, client, tmdb, media.Backdrop, fanartPath, incremental); err != nil {
			return err
		}
	}
	item.Status = "ok"
	item.Message = "元数据已写入"
	return nil
}
func scrapeImage(ctx context.Context, root *os.Root, client *http.Client, cfg PluginConfig, image, target string, incremental bool) error {
	if image == "" {
		return nil
	}
	if incremental {
		if _, err := root.Stat(target); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if !strings.HasPrefix(image, "/") || strings.ContainsAny(image, `?#\`) || strings.Contains(image, "..") {
		return errors.New("TMDB 图片路径无效")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(cfg.ImageURL, "/")+"/t/p/w780"+image, nil)
	if err != nil {
		return errors.New("图片地址无效")
	}
	if err := waitTMDB(ctx, cfg); err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("图片获取失败")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("图片返回 HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, (20<<20)+1))
	if err != nil || len(b) > 20<<20 {
		return errors.New("图片过大或读取失败")
	}
	if !strings.HasPrefix(http.DetectContentType(b), "image/") {
		return errors.New("图片响应内容无效")
	}
	return writeCASOutput(root, target, b, incremental)
}
