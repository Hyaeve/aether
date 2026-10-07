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

var scrapeEpisode = regexp.MustCompile(`(?i)S(\d{1,3})[ ._-]*E(\d{1,4})\b`)
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
	}
	if m := scrapeYear.FindStringSubmatch(title); len(m) > 0 {
		item.Year = m[1]
		title = title[:scrapeYear.FindStringIndex(title)[0]]
	}
	title = scrapeSuffix.ReplaceAllString(title, "")
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
		if t.ID == taskID && t.Kind == "strm" {
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
			return nil
		}
		if !entry.Type().IsRegular() || !strings.EqualFold(path.Ext(name), ".strm") {
			return nil
		}
		if len(items) >= 10000 {
			return errors.New("单次刮削最多扫描 10000 个 STRM")
		}
		item := recognizeSTRM(entry.Name())
		item.Path = name
		if saved, ok := old[name]; ok {
			item = saved
		}
		items = append(items, item)
		return nil
	})
	return items, err
}
func (a *App) strmScrape(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
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
		jsonResponse(w, 200, a.loadScrapeIndex(task, root).Items)
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
	if action != "scan" && action != "run" && action != "match" {
		w.WriteHeader(404)
		return
	}
	if a.scrapeProgress.Running {
		fail(w, 409, errors.New("刮削正在执行，请先停止"))
		return
	}
	var in struct {
		TaskID string `json:"taskId"`
		Path   string `json:"path"`
		TMDB   int    `json:"tmdb"`
		Kind   string `json:"kind"`
	}
	if !decode(w, r, &in) {
		return
	}
	rootName, err := a.scrapeRoot(in.TaskID)
	if err != nil {
		fail(w, 400, err)
		return
	}
	index := a.loadScrapeIndex(in.TaskID, rootName)
	if action == "match" {
		if in.TMDB < 1 || (in.Kind != "movie" && in.Kind != "tv") {
			fail(w, 400, errors.New("请输入有效的 TMDB ID 和媒体类型"))
			return
		}
		found := false
		for i := range index.Items {
			if index.Items[i].Path == in.Path {
				index.Items[i].TMDB = in.TMDB
				index.Items[i].Kind = in.Kind
				index.Items[i].Status = "pending"
				index.Items[i].Message = ""
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
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	root, err := os.OpenRoot(rootName)
	if err != nil {
		fail(w, 400, errors.New("生成目录不存在，请先生成 STRM"))
		return
	}
	st := a.store.snapshotWithLogLimit(0)
	cfg, tmdb := a.scrapeConfig(), pluginDefaults("tmdb", st.Plugins["tmdb"])
	if action == "run" && (!tmdb.Enabled || tmdb.APIKey == "") {
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
			a.scrapeMu.Lock()
			a.scrapeProgress.Total = len(items)
			if in.Path != "" {
				a.scrapeProgress.Total = 0
				for _, item := range items {
					if item.Path == in.Path {
						a.scrapeProgress.Total++
					}
				}
			}
			a.scrapeMu.Unlock()
			if action == "run" {
				client, closeIdle, clientErr := pluginClient(st.Plugins["proxy"])
				if clientErr != nil {
					runErr = clientErr
				} else {
					defer closeIdle()
					for i := range index.Items {
						if ctx.Err() != nil {
							runErr = ctx.Err()
							break
						}
						item := &index.Items[i]
						if in.Path != "" && item.Path != in.Path {
							continue
						}
						item.Message = ""
						e := scrapeOne(ctx, root, client, tmdb, cfg, item)
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

func scrapeOne(ctx context.Context, root *os.Root, client *http.Client, tmdb PluginConfig, cfg scrapeSettings, item *scrapeItem) error {
	if item.TMDB == 0 {
		var result struct {
			Results []tmdbMedia `json:"results"`
		}
		if err := tmdbGet(ctx, client, tmdb, "search/"+item.Kind, url.Values{"query": {item.Title}}, &result); err != nil {
			return err
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
			return nil
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
		return err
	}
	title := media.Title
	if item.Kind == "tv" {
		title = media.Name
	}
	if title == "" || media.ID != item.TMDB {
		return errors.New("TMDB 详情无效")
	}
	item.Title = title
	release := media.Release
	if item.Kind == "tv" {
		release = media.AirDate
	}
	if len(release) >= 4 {
		item.Year = release[:4]
	}
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
		dir := path.Dir(item.Path)
		if strings.HasPrefix(strings.ToLower(path.Base(dir)), "season") || regexp.MustCompile(`(?i)^s\d+$`).MatchString(path.Base(dir)) {
			dir = path.Dir(dir)
		}
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
