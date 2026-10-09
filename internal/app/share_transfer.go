package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	driver115 "github.com/SheltonZhu/115driver/pkg/driver"
)

type shareEntry struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Token string `json:"-"`
}

type sharePreview struct {
	Owner     [32]byte
	Config    [32]byte
	Storage   string
	Code      string
	Pass      string
	Token     string
	Items     []shareEntry
	Expires   time.Time
	Used      bool
	TaskID    string
	Batches   []shareBatch
	Next      int
	BatchBusy bool
	Parent    string
}

var shareURLPattern = regexp.MustCompile(`https?://[^\s<>，。]+`)
var shareCodePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
var sharePasswordPattern = regexp.MustCompile(`(?:提取码|访问码|密码)\s*[:：]?\s*([a-zA-Z0-9]{4,8})`)

const pan115ShareUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"

type shareHTTPError struct {
	status  int
	message string
}

func (e *shareHTTPError) Error() string { return e.message }

func read115Share(ctx context.Context, s Storage, query url.Values, out any) error {
	err := shareRequest(ctx, s, http.MethodGet, driver115.ApiShareSnap+"?"+query.Encode(), nil, out)
	var failure *shareHTTPError
	if errors.As(err, &failure) && failure.status == http.StatusMethodNotAllowed && ctx.Err() == nil {
		// Only retry a read on the fixed legacy API, never replay a transfer or follow a Location.
		err = shareRequest(ctx, s, http.MethodGet, "https://webapi.115.com/share/snap?"+query.Encode(), nil, out)
		if err != nil {
			return fmt.Errorf("115 分享预览主接口返回405，备用接口也未成功：%w", err)
		}
	}
	return err
}

func parseShareLink(raw, password string) (provider, code, pass string, err error) {
	if len(raw) > 4096 || len(password) > 32 {
		return "", "", "", errors.New("分享链接或提取码过长")
	}
	links := shareURLPattern.FindAllString(raw, -1)
	if len(links) != 1 {
		return "", "", "", errors.New("请每次输入一个完整分享链接")
	}
	u, e := url.Parse(links[0])
	if e != nil || u.User != nil || u.Port() != "" {
		return "", "", "", errors.New("分享链接无效")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch strings.ToLower(u.Hostname()) {
	case "115.com", "115cdn.com", "anxia.com":
		if len(parts) == 2 && parts[0] == "s" {
			provider = "115"
			code = parts[1]
		}
	case "pan.quark.cn":
		if len(parts) == 2 && parts[0] == "s" {
			provider = "quark"
			code = parts[1]
		}
	case "yun.139.com", "caiyun.139.com":
		provider = "mobile"
		for _, key := range []string{"linkID", "linkId"} {
			if code = u.Query().Get(key); code != "" {
				break
			}
		}
		if code == "" && u.Path == "/m/i" && shareCodePattern.MatchString(u.RawQuery) {
			code = u.RawQuery
		}
		if code == "" && u.Fragment != "" {
			fragment, e := url.Parse(u.Fragment)
			if e == nil {
				code = fragment.Query().Get("linkID")
				if code == "" {
					code = fragment.Query().Get("linkId")
				}
				fragments := strings.Split(strings.Trim(fragment.Path, "/"), "/")
				if code == "" && len(fragments) == 3 && fragments[0] == "w" && (fragments[1] == "i" || fragments[1] == "r") {
					code = fragments[2]
				}
			}
		}
		if code == "" && len(parts) == 2 && parts[0] == "shareweb" {
			code = parts[1]
		}
	}
	if provider == "" || !shareCodePattern.MatchString(code) {
		return "", "", "", errors.New("仅支持 115、移动云盘和夸克的官方分享链接")
	}
	pass = strings.TrimSpace(password)
	if pass == "" {
		for _, key := range []string{"password", "pwd", "passwd", "passcode", "receive_code"} {
			if pass = u.Query().Get(key); pass != "" {
				break
			}
		}
	}
	if pass == "" {
		if match := sharePasswordPattern.FindStringSubmatch(raw); len(match) > 1 {
			pass = match[1]
		}
	}
	if pass != "" && (!shareCodePattern.MatchString(pass) || len(pass) > 32) {
		return "", "", "", errors.New("提取码格式无效")
	}
	return provider, code, pass, nil
}

func shareConfig(s Storage) [32]byte {
	b, _ := json.Marshal(s.Config)
	return sha256.Sum256(b)
}

// Only fixed provider endpoints reach this client. Never follow redirects with credentials.
func shareRequest(ctx context.Context, s Storage, method, address string, body any, out any) error {
	headers := cloudHeaders(s)
	if s.Type == "115" {
		headers.Set("User-Agent", pan115ShareUA)
		endpoint, err := url.Parse(address)
		if err != nil {
			return err
		}
		params := endpoint.Query()
		if form, ok := body.(url.Values); ok {
			params = form
		}
		headers.Set("Referer", driver115.BuildShareReferer(params.Get("share_code"), params.Get("receive_code")))
	}
	var reader io.Reader
	if nativeMobile(s) {
		_, auth, err := mobileAccount(s)
		if err != nil {
			return err
		}
		headers.Set("Authorization", "Basic "+auth)
		headers.Set("Referer", "https://yun.139.com/")
		headers.Set("caller", "web")
		headers.Set("x-m4c-caller", "PC")
		headers.Set("mcloud-client", "10701")
		headers.Set("mcloud-version", "7.17.2")
		headers.Set("mcloud-channel", "1000101")
	}
	if body != nil {
		if form, ok := body.(url.Values); ok {
			reader = strings.NewReader(form.Encode())
			headers.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			b, err := json.Marshal(body)
			if err != nil {
				return err
			}
			reader = bytes.NewReader(b)
			headers.Set("Content-Type", "application/json")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return err
	}
	req.Header = headers
	client := &http.Client{Transport: apiClient.Transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("分享服务连接失败，请检查网络后重试")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		if s.Type == "115" {
			stage := "预览"
			if method == http.MethodPost {
				stage = "转存"
			}
			return &shareHTTPError{status: res.StatusCode, message: fmt.Sprintf("115 分享%s失败（%s %s，HTTP %d）；上游未接受请求，不能据此判断CK是否失效；转存写请求不会自动重试", stage, method, req.URL.Host+req.URL.Path, res.StatusCode)}
		}
		return fmt.Errorf("分享服务 HTTP %d，请检查授权或网络", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil || len(b) > 4<<20 {
		return errors.New("分享服务响应过大或不完整")
	}
	var envelope struct {
		State *bool           `json:"state"`
		Code  json.RawMessage `json:"code"`
	}
	if json.Unmarshal(b, &envelope) != nil {
		return errors.New("分享服务响应格式异常")
	}
	code := strings.Trim(string(envelope.Code), `"`)
	if s.Type == "115" {
		if envelope.State == nil || !*envelope.State {
			return errors.New("115 拒绝分享请求，请检查提取码、链接有效期和账号权限")
		}
	} else if code != "0" && code != "0000" {
		return errors.New("网盘拒绝分享请求，请检查提取码、链接有效期和账号权限")
	}
	if err := json.Unmarshal(b, out); err != nil {
		return errors.New("分享服务响应格式异常")
	}
	return nil
}

func (a *App) readShare(ctx context.Context, s Storage, code, pass string) (string, []shareEntry, error) {
	return a.readShareAt(ctx, s, code, pass, "root")
}
func (a *App) readShareAt(ctx context.Context, s Storage, code, pass, parent string) (string, []shareEntry, error) {
	items := []shareEntry{}
	token := ""
	if s.Type == "quark" {
		var result struct {
			Data struct {
				Token string `json:"stoken"`
			} `json:"data"`
		}
		err := shareRequest(ctx, s, "POST", "https://drive.quark.cn/1/clouddrive/share/sharepage/token?pr=ucpro&fr=pc", map[string]string{"pwd_id": code, "passcode": pass}, &result)
		if err != nil {
			return "", nil, err
		}
		token = result.Data.Token
		if token == "" {
			return "", nil, errors.New("未取得夸克分享令牌")
		}
	}
	seen := map[string]bool{}
	for page := 0; page < 50; page++ {
		if err := a.waitAPI(ctx, s.ID); err != nil {
			return "", nil, err
		}
		batch := []shareEntry{}
		total := 0
		switch s.Type {
		case "115":
			var result struct {
				Data struct {
					Count int `json:"count"`
					List  []struct {
						ID   string `json:"fid"`
						CID  string `json:"cid"`
						Name string `json:"n"`
					} `json:"list"`
				} `json:"data"`
			}
			q := url.Values{"share_code": {code}, "receive_code": {pass}, "cid": {"0"}, "offset": {strconv.Itoa(page * 100)}, "limit": {"100"}, "asc": {"0"}, "format": {"json"}}
			if err := read115Share(ctx, s, q, &result); err != nil {
				return "", nil, err
			}
			total = result.Data.Count
			for _, f := range result.Data.List {
				id := f.ID
				if id == "" {
					id = f.CID
				}
				batch = append(batch, shareEntry{ID: id, Name: f.Name, IsDir: f.ID == ""})
			}
		case "quark":
			var result struct {
				Data struct {
					List []struct {
						ID    string `json:"fid"`
						Name  string `json:"file_name"`
						Dir   bool   `json:"dir"`
						Token string `json:"share_fid_token"`
					} `json:"list"`
				} `json:"data"`
				Metadata struct {
					Total int `json:"_total"`
				} `json:"metadata"`
			}
			q := url.Values{"pr": {"ucpro"}, "fr": {"pc"}, "pwd_id": {code}, "stoken": {token}, "pdir_fid": {"0"}, "_page": {strconv.Itoa(page + 1)}, "_size": {"100"}, "_fetch_total": {"1"}}
			if err := shareRequest(ctx, s, "GET", "https://drive.quark.cn/1/clouddrive/share/sharepage/detail?"+q.Encode(), nil, &result); err != nil {
				return "", nil, err
			}
			total = result.Metadata.Total
			for _, f := range result.Data.List {
				batch = append(batch, shareEntry{ID: f.ID, Name: f.Name, IsDir: f.Dir, Token: f.Token})
			}
		case "mobile":
			account, _, err := mobileAccount(s)
			if err != nil {
				return "", nil, err
			}
			type entry struct {
				ID     string `json:"coID"`
				CID    string `json:"caID"`
				Name   string `json:"coName"`
				CName  string `json:"caName"`
				Path   string `json:"path"`
				CoPath string `json:"coPath"`
				CaPath string `json:"caPath"`
			}
			var result struct {
				Data struct {
					Total int     `json:"nodNum"`
					Files []entry `json:"coLst"`
					Dirs  []entry `json:"caLst"`
				} `json:"data"`
			}
			body := map[string]any{"getOutLinkInfoReq": map[string]any{"account": account, "linkID": code, "passwd": pass, "pCaID": parent, "caSrt": 0, "coSrt": 0, "srtDr": 1, "bNum": page*100 + 1, "eNum": (page + 1) * 100}}
			if err := shareRequest(ctx, s, "POST", mobileShareBase+"IOutLink/getOutLinkInfoV6", body, &result); err != nil {
				return "", nil, err
			}
			total = result.Data.Total
			for _, f := range result.Data.Dirs {
				p := f.Path
				if p == "" {
					p = f.CaPath
				}
				batch = append(batch, shareEntry{ID: f.CID, Name: f.CName, IsDir: true, Token: p})
			}
			for _, f := range result.Data.Files {
				p := f.Path
				if p == "" {
					p = f.CoPath
				}
				batch = append(batch, shareEntry{ID: f.ID, Name: f.Name, Token: p})
			}
		}
		for _, item := range batch {
			if item.ID == "" || item.Name == "" || seen[item.ID] {
				return "", nil, errors.New("分享列表不完整或重复，请重新解析")
			}
			seen[item.ID] = true
			items = append(items, item)
		}
		if total > 5000 {
			return "", nil, errors.New("分享根目录最多支持 5000 项")
		}
		if len(batch) == 0 && total > len(items) {
			return "", nil, errors.New("分享列表分页不完整，请重新解析")
		}
		if len(batch) == 0 || (total > 0 && len(items) >= total) || (total == 0 && len(batch) < 100) {
			return token, items, nil
		}
	}
	return "", nil, errors.New("分享目录过大，请缩小分享范围")
}

const mobileShareBase = "https://share-kd-njs.yun.139.com/yun-share/richlifeApp/devapp/"

func (a *App) shareTransfer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		StorageID string   `json:"storageId"`
		URL       string   `json:"url"`
		Password  string   `json:"password"`
		Preview   string   `json:"preview"`
		Parent    string   `json:"parent"`
		IDs       []string `json:"ids"`
		Batch     int      `json:"batch"`
	}
	if !decode(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	s, err := a.store.storage(in.StorageID)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if s.Type != "115" && s.Type != "quark" && !nativeMobile(s) {
		fail(w, 400, errors.New("请选择 115、夸克或原生移动个人云存储"))
		return
	}
	if r.PathValue("action") == "batch" {
		a.saveShareBatch(w, r, ctx, s, in.Preview, in.Parent, in.Batch)
		return
	}
	if r.PathValue("action") == "preview" {
		provider, code, pass, err := parseShareLink(in.URL, in.Password)
		if err != nil {
			fail(w, 400, err)
			return
		}
		if provider != s.Type {
			fail(w, 400, errors.New("分享链接与目标存储类型不一致，不支持跨网盘转存"))
			return
		}
		token, items, err := a.readShare(ctx, s, code, pass)
		if err != nil {
			fail(w, 400, err)
			return
		}
		batches, err := a.planShare(ctx, s, code, pass, items)
		if err != nil {
			fail(w, 400, err)
			return
		}
		key := id()
		a.shareMu.Lock()
		if a.sharePreviews == nil {
			a.sharePreviews = map[string]*sharePreview{}
		}
		for key, p := range a.sharePreviews {
			if time.Now().After(p.Expires) {
				delete(a.sharePreviews, key)
			}
		}
		if len(a.sharePreviews) >= 64 {
			a.shareMu.Unlock()
			fail(w, 429, errors.New("分享预览过多，请稍后再试"))
			return
		}
		a.sharePreviews[key] = &sharePreview{Owner: authorizationOwner(r), Config: shareConfig(s), Storage: s.ID, Code: code, Pass: pass, Token: token, Items: items, Batches: batches, Expires: time.Now().Add(10 * time.Minute)}
		a.shareMu.Unlock()
		jsonResponse(w, 200, map[string]any{"preview": key, "items": items, "batches": len(batches)})
		return
	}
	if r.PathValue("action") != "save" {
		if r.PathValue("action") == "status" {
			a.shareMu.Lock()
			p := a.sharePreviews[in.Preview]
			if p == nil || p.Owner != authorizationOwner(r) || p.Storage != s.ID || p.Config != shareConfig(s) || time.Now().After(p.Expires) || p.TaskID == "" || s.Type != "quark" {
				a.shareMu.Unlock()
				fail(w, 409, errors.New("转存状态会话已失效，请直接检查网盘目标目录"))
				return
			}
			task := p.TaskID
			a.shareMu.Unlock()
			var result struct {
				Data struct {
					Status int `json:"status"`
				} `json:"data"`
			}
			err := shareRequest(ctx, s, "GET", "https://drive.quark.cn/1/clouddrive/task?pr=ucpro&fr=pc&retry_index=0&task_id="+url.QueryEscape(task), nil, &result)
			if err != nil {
				fail(w, 502, err)
				return
			}
			status, message := "submitted", "网盘正在处理"
			if result.Data.Status == 2 {
				status, message = "completed", "网盘已完成转存"
				a.cache.clear()
				a.shareMu.Lock()
				if p.TaskID == task {
					p.TaskID = ""
				}
				a.shareMu.Unlock()
			}
			if result.Data.Status == 3 {
				status, message = "failed", "网盘转存失败，请检查账号权限、容量及分享有效性"
			}
			jsonResponse(w, 200, map[string]string{"status": status, "message": message, "taskId": task})
			return
		}
		http.NotFound(w, r)
		return
	}
	a.shareMu.Lock()
	p := a.sharePreviews[in.Preview]
	if p == nil || p.Used || p.Next > 0 || p.BatchBusy || p.Owner != authorizationOwner(r) || p.Storage != s.ID || p.Config != shareConfig(s) || time.Now().After(p.Expires) {
		a.shareMu.Unlock()
		fail(w, 409, errors.New("预览已失效或已提交，请重新解析"))
		return
	}
	preview := *p
	a.shareMu.Unlock()
	if len(in.IDs) == 0 || len(in.IDs) > 100 {
		fail(w, 400, errors.New("每次选择 1–100 项"))
		return
	}
	chosen := []shareEntry{}
	known := map[string]shareEntry{}
	for _, item := range preview.Items {
		known[item.ID] = item
	}
	for _, id := range in.IDs {
		item, ok := known[id]
		if !ok {
			fail(w, 400, errors.New("选择项目无效或重复"))
			return
		}
		delete(known, id)
		chosen = append(chosen, item)
	}
	if in.Parent == "" || in.Parent == "/" {
		in.Parent = rootOf(s)
	}
	existing, err := a.rawList(ctx, s, in.Parent)
	if err != nil {
		fail(w, 400, errors.New("目标目录不可访问"))
		return
	}
	names := map[string]bool{}
	for _, item := range existing {
		names[item.Name] = true
	}
	for _, item := range chosen {
		if nativeMobile(s) && item.IsDir {
			fail(w, 400, errors.New("移动分享目录须使用分批转存"))
			return
		}
		if names[item.Name] {
			fail(w, 409, errors.New("目标目录或选择中有同名项目，请更换目录或调整选择"))
			return
		}
		names[item.Name] = true
		if s.Type != "115" && item.Token == "" {
			fail(w, 400, errors.New("分享缺少转存凭据或路径，请重新解析"))
			return
		}
	}
	current, err := a.store.storage(s.ID)
	if err != nil || shareConfig(current) != preview.Config {
		fail(w, 409, errors.New("存储配置已变化，请重新解析"))
		return
	}
	a.shareMu.Lock()
	if p.Used || p.Next > 0 || p.BatchBusy {
		a.shareMu.Unlock()
		fail(w, 409, errors.New("此预览已经提交"))
		return
	}
	p.Used = true
	a.shareMu.Unlock()
	// A lost response may still have created an upstream job. Do not automatically retry.
	taskID, err := submitShare(ctx, s, preview, in.Parent, chosen)
	a.cache.clear()
	if err != nil {
		fail(w, 502, fmt.Errorf("%s；如已提交请求，请先检查目标网盘，勿直接重复转存", err))
		return
	}
	a.shareMu.Lock()
	p.TaskID = taskID
	a.shareMu.Unlock()
	a.store.event("info", "files", fmt.Sprintf("%s 分享转存已提交，共 %d 项", s.Name, len(chosen)))
	jsonResponse(w, 200, map[string]string{"status": "submitted", "taskId": taskID, "message": "转存已提交，请刷新目标目录确认结果"})
}

func submitShare(ctx context.Context, s Storage, p sharePreview, parent string, items []shareEntry) (string, error) {
	ids, tokens := []string{}, []string{}
	for _, item := range items {
		ids = append(ids, item.ID)
		tokens = append(tokens, item.Token)
	}
	var result struct {
		Data struct {
			TaskID   string `json:"task_id"`
			MobileID string `json:"taskID"`
			Nested   struct {
				ID string `json:"taskID"`
			} `json:"createOuterLinkBatchOprTaskRes"`
		} `json:"data"`
	}
	var err error
	switch s.Type {
	case "115":
		err = shareRequest(ctx, s, "POST", "https://webapi.115.com/share/receive", url.Values{"share_code": {p.Code}, "receive_code": {p.Pass}, "file_id": {strings.Join(ids, ",")}, "cid": {parent}}, &result)
	case "quark":
		err = shareRequest(ctx, s, "POST", "https://drive.quark.cn/1/clouddrive/share/sharepage/save?pr=ucpro&fr=pc", map[string]any{"fid_list": ids, "fid_token_list": tokens, "to_pdir_fid": parent, "pwd_id": p.Code, "stoken": p.Token, "pdir_fid": "0", "scene": "link"}, &result)
	case "mobile":
		account, _, e := mobileAccount(s)
		if e != nil {
			return "", e
		}
		files, dirs := []string{}, []string{}
		for _, item := range items {
			if item.IsDir {
				dirs = append(dirs, item.Token)
			} else {
				files = append(files, item.Token)
			}
		}
		err = shareRequest(ctx, s, "POST", mobileShareBase+"IBatchOprTask/createOuterLinkBatchOprTask", map[string]any{"createOuterLinkBatchOprTaskReq": map[string]any{"msisdn": account, "ownerAccount": "", "taskType": 1, "linkID": p.Code, "needPassword": p.Pass != "", "taskInfo": map[string]any{"linkID": p.Code, "needPassword": p.Pass != "", "contentInfoList": files, "catalogInfoList": dirs, "newCatalogID": parent}}}, &result)
	}
	if err != nil {
		return "", err
	}
	task := result.Data.TaskID
	if s.Type == "mobile" {
		task = result.Data.MobileID
		if task == "" {
			task = result.Data.Nested.ID
		}
	}
	if s.Type != "115" && task == "" {
		return "", errors.New("网盘未返回转存任务编号，结果尚未确认")
	}
	return task, nil
}
