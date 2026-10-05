package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const tianyiAPI = "https://api.cloud.189.cn"
const tianyiAuth = "https://open.e.189.cn"
const tianyiAppID = "9317140619"
const tianyiReturn = "https://m.cloud.189.cn/zhuanti/2020/loginErrorPc/index.html"

type tianyiSession struct {
	Key         string `json:"sessionKey"`
	Secret      string `json:"sessionSecret"`
	Credentials [32]byte
	Expires     time.Time
}

func nativeTianyi(s Storage) bool {
	return s.Type == "tianyi" && s.Config["username"] != "" && s.Config["password"] != ""
}

func casStorage(s Storage) bool { return nativeMobile(s) || nativeTianyi(s) }

func casAccount(s Storage) (string, error) {
	if nativeTianyi(s) {
		return "189:" + s.Config["username"], nil
	}
	account, _, err := mobileAccount(s)
	return "139:" + account, err
}

func trustedTianyi(address string) bool {
	u, err := url.Parse(address)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" &&
		(u.Hostname() == "189.cn" || strings.HasSuffix(u.Hostname(), ".189.cn"))
}

func tianyiClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Transport: apiClient.Transport, Jar: jar, Timeout: 20 * time.Second,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 8 || !trustedTianyi(r.URL.String()) {
				return errors.New("天翼登录返回非可信跳转")
			}
			return nil
		}}
}

func tianyiHTTP(ctx context.Context, client *http.Client, method, address string, values url.Values, headers http.Header) ([]byte, *url.URL, error) {
	if !trustedTianyi(address) {
		return nil, nil, errors.New("天翼返回非可信接口地址")
	}
	var body io.Reader
	if method == "GET" {
		u, _ := url.Parse(address)
		q := u.Query()
		for k, vs := range values {
			q[k] = vs
		}
		u.RawQuery = q.Encode()
		address = u.String()
	} else if values != nil {
		body = strings.NewReader(values.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return nil, nil, err
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = http.Header{}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, errors.New("天翼连接失败，请检查网络")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, nil, fmt.Errorf("天翼接口 HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20+1))
	if err != nil || len(raw) > 8<<20 {
		return nil, nil, errors.New("天翼响应读取失败或过大")
	}
	return raw, res.Request.URL, nil
}

func tianyiDecode(raw []byte, out any) error {
	var status struct {
		XMLName   xml.Name
		Code      string      `json:"code" xml:"code"`
		ErrorCode string      `json:"errorCode"`
		ResCode   json.Number `json:"res_code"`
	}
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "<") {
		if xml.Unmarshal(raw, &status) != nil || status.XMLName.Local == "error" {
			return errors.New("天翼拒绝请求，请检查账户、权限或会话")
		}
		return xml.Unmarshal(raw, out)
	}
	if json.Unmarshal(raw, &status) != nil {
		return errors.New("天翼响应格式异常")
	}
	if (status.Code != "" && status.Code != "SUCCESS" && status.Code != "0") || status.ErrorCode != "" || (status.ResCode != "" && status.ResCode != "0") {
		return errors.New("天翼拒绝请求，请检查账户、权限或会话")
	}
	return json.Unmarshal(raw, out)
}

func (a *App) tianyiLogin(ctx context.Context, s Storage) (tianyiSession, error) {
	empty := tianyiSession{}
	if !nativeTianyi(s) {
		return empty, errors.New("天翼存储需配置账户和密码；旧网关池请重新配置")
	}
	client := tianyiClient()
	raw, final, err := tianyiHTTP(ctx, client, "GET", "https://cloud.189.cn/api/portal/unifyLoginForPC.action",
		url.Values{"appId": {tianyiAppID}, "clientType": {"10020"}, "returnURL": {tianyiReturn}, "timeStamp": {strconv.FormatInt(time.Now().UnixMilli(), 10)}}, nil)
	if err != nil {
		return empty, err
	}
	lt, reqID, paramID := final.Query().Get("lt"), final.Query().Get("reqId"), ""
	if lt == "" || reqID == "" {
		read := func(key string) string {
			match := regexp.MustCompile(`\b` + key + `\s*=\s*"([^"]+)"`).FindSubmatch(raw)
			if len(match) == 2 {
				return string(match[1])
			}
			return ""
		}
		lt, reqID, paramID = read("lt"), read("reqId"), read("paramId")
	}
	if lt == "" || reqID == "" {
		return empty, errors.New("未获取天翼登录参数")
	}
	headers := http.Header{"Lt": {lt}, "Reqid": {reqID}, "Referer": {final.String()}, "Origin": {tianyiAuth}}
	post := func(endpoint string, values url.Values, out any) error {
		raw, _, err := tianyiHTTP(ctx, client, "POST", tianyiAuth+endpoint, values, headers)
		if err != nil {
			return err
		}
		if json.Unmarshal(raw, out) != nil {
			return errors.New("天翼登录响应格式异常")
		}
		return nil
	}
	if paramID == "" {
		var conf struct {
			Data struct {
				ParamID string `json:"paramId"`
			} `json:"data"`
		}
		if err := post("/api/logbox/oauth2/appConf.do", url.Values{"version": {"2.0"}, "appKey": {tianyiAppID}}, &conf); err != nil {
			return empty, err
		}
		paramID = conf.Data.ParamID
	}
	if paramID == "" {
		return empty, errors.New("天翼登录缺少 paramId")
	}
	var encryption struct{ Data struct{ PubKey, Pre string } }
	if err := post("/api/logbox/config/encryptConf.do", url.Values{"appId": {tianyiAppID}}, &encryption); err != nil {
		return empty, err
	}
	der, err := base64.StdEncoding.DecodeString(encryption.Data.PubKey)
	if err != nil {
		return empty, errors.New("天翼登录公钥无效")
	}
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return empty, errors.New("天翼登录公钥无效")
	}
	pub, ok := key.(*rsa.PublicKey)
	if !ok || pub.N.BitLen() < 1024 {
		return empty, errors.New("天翼登录公钥类型无效")
	}
	encrypt := func(value string) (string, error) {
		b, err := rsa.EncryptPKCS1v15(rand.Reader, pub, []byte(value))
		return encryption.Data.Pre + strings.ToUpper(hex.EncodeToString(b)), err
	}
	username, err := encrypt(s.Config["username"])
	if err != nil {
		return empty, errors.New("天翼账户加密失败")
	}
	password, err := encrypt(s.Config["password"])
	if err != nil {
		return empty, errors.New("天翼密码加密失败")
	}
	raw, _, err = tianyiHTTP(ctx, client, "POST", tianyiAuth+"/api/logbox/oauth2/needcaptcha.do",
		url.Values{"appKey": {tianyiAppID}, "accountType": {"02"}, "userName": {username}}, headers)
	if err != nil {
		return empty, err
	}
	if strings.TrimSpace(string(raw)) != "0" {
		return empty, errors.New("天翼要求图形验证码，当前暂不支持交互验证；请在官方客户端完成验证后重试")
	}
	var login struct {
		Result int
		ToURL  string `json:"toUrl"`
	}
	err = post("/api/logbox/oauth2/loginSubmit.do", url.Values{
		"version": {"v2.0"}, "appKey": {tianyiAppID}, "pageKey": {"normal"}, "accountType": {"02"},
		"userName": {username}, "password": {password}, "epd": {password}, "returnUrl": {tianyiReturn},
		"dynamicCheck": {"FALSE"}, "clientType": {"10020"}, "cb_SaveName": {"1"}, "isOauth2": {"false"},
		"paramId": {paramID}, "apToken": {""}, "state": {""}, "validateCode": {""}, "captchaToken": {""},
	}, &login)
	if err != nil {
		return empty, err
	}
	if login.Result == -133 {
		return empty, errors.New("天翼要求设备短信二次验证，当前暂不支持交互验证；请在官方客户端完成验证后重试")
	}
	if login.Result != 0 || !trustedTianyi(login.ToURL) {
		return empty, errors.New("天翼账户登录失败，请检查账户密码或风控验证")
	}
	address := tianyiAPI + "/getSessionForPC.action?" + url.Values{
		"redirectURL": {login.ToURL}, "clientType": {"TELEPC"}, "version": {"7.2.4.0"}, "channelId": {"web_cloud.189.cn"},
	}.Encode()
	raw, _, err = tianyiHTTP(ctx, client, "POST", address, nil, nil)
	if err != nil {
		return empty, err
	}
	var session tianyiSession
	if err := tianyiDecode(raw, &session); err != nil {
		return empty, err
	}
	if session.Key == "" || session.Secret == "" {
		return empty, errors.New("天翼登录未返回有效会话")
	}
	return session, nil
}

func (a *App) tianyiSessionFor(ctx context.Context, s Storage) (tianyiSession, error) {
	a.tianyiMu.Lock()
	defer a.tianyiMu.Unlock()
	digest := sha256.Sum256([]byte(s.Config["username"] + "\x00" + s.Config["password"]))
	cached := a.tianyiSessions[s.ID]
	if cached.Credentials == digest && time.Now().Before(cached.Expires) {
		return cached, nil
	}
	session, err := a.tianyiLogin(ctx, s)
	if err != nil {
		return tianyiSession{}, err
	}
	session.Credentials, session.Expires = digest, time.Now().Add(time.Hour)
	a.tianyiSessions[s.ID] = session
	return session, nil
}

func (a *App) tianyiRequest(ctx context.Context, s Storage, method, address string, values url.Values, out any) error {
	if err := a.waitAPI(ctx, s.ID); err != nil {
		return err
	}
	session, err := a.tianyiSessionFor(ctx, s)
	if err != nil {
		return err
	}
	u, err := url.Parse(address)
	if err != nil || !trustedTianyi(address) {
		return errors.New("天翼接口地址无效")
	}
	date := time.Now().UTC().Format(http.TimeFormat)
	mac := hmac.New(sha1.New, []byte(session.Secret))
	fmt.Fprintf(mac, "SessionKey=%s&Operate=%s&RequestURI=%s&Date=%s", session.Key, method, u.Path, date)
	headers := http.Header{"Sessionkey": {session.Key}, "Date": {date}, "Signature": {strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))}, "X-Request-Id": {id()}}
	q := u.Query()
	q.Set("clientType", "TELEPC")
	q.Set("version", "7.2.4.0")
	q.Set("channelId", "web_cloud.189.cn")
	u.RawQuery = q.Encode()
	client := tianyiClient()
	// Session-bearing requests must not forward signatures through redirects.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	raw, _, err := tianyiHTTP(ctx, client, method, u.String(), values, headers)
	if err != nil {
		return err
	}
	if strings.Contains(string(raw), "InvalidSessionKey") || strings.Contains(string(raw), "userSessionBO is null") {
		a.tianyiMu.Lock()
		if a.tianyiSessions[s.ID].Key == session.Key {
			delete(a.tianyiSessions, s.ID)
		}
		a.tianyiMu.Unlock()
		return errors.New("天翼会话已过期，请重试以重新登录")
	}
	return tianyiDecode(raw, out)
}

func (a *App) tianyiList(ctx context.Context, s Storage, dir string) ([]File, error) {
	if dir == "/" || dir == "" {
		dir = rootOf(s)
	}
	files := []File{}
	seen := map[string]bool{}
	type item struct {
		MD5  string `json:"md5"`
		ID   json.Number
		Name string
		Size int64
	}
	for page := 1; page <= 10000; page++ {
		var data struct {
			FileListAO struct{ FileList, FolderList []item }
		}
		err := a.tianyiRequest(ctx, s, "GET", tianyiAPI+"/listFiles.action", url.Values{
			"folderId": {dir}, "fileType": {"0"}, "mediaAttr": {"0"}, "iconOption": {"5"}, "pageNum": {strconv.Itoa(page)},
			"pageSize": {"100"}, "recursive": {"0"}, "orderBy": {"filename"}, "descending": {"false"},
		}, &data)
		if err != nil {
			return nil, err
		}
		added := 0
		for index, group := range [][]item{data.FileListAO.FolderList, data.FileListAO.FileList} {
			for _, f := range group {
				if f.ID == "" || !safeName(f.Name) || seen[string(f.ID)] {
					continue
				}
				seen[string(f.ID)] = true
				files = append(files, File{ID: string(f.ID), Name: f.Name, Size: f.Size, MD5: f.MD5, IsDir: index == 0})
				added++
			}
		}
		if len(data.FileListAO.FileList)+len(data.FileListAO.FolderList) < 100 {
			return files, nil
		}
		if added == 0 {
			return nil, errors.New("天翼目录分页重复")
		}
	}
	return nil, errors.New("天翼目录条目过多")
}

func (a *App) tianyiLink(ctx context.Context, s Storage, fid string) (Download, error) {
	var result struct {
		URL string `json:"fileDownloadUrl"`
	}
	if err := a.tianyiRequest(ctx, s, "GET", tianyiAPI+"/getFileDownloadUrl.action", url.Values{"fileId": {fid}, "dt": {"3"}, "flag": {"1"}}, &result); err != nil {
		return Download{}, err
	}
	result.URL = strings.ReplaceAll(result.URL, "&amp;", "&")
	u, err := url.Parse(result.URL)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return Download{}, errors.New("天翼返回无效下载地址")
	}
	return Download{URL: result.URL, Headers: http.Header{}}, nil
}

func (a *App) tianyiCASFolder(ctx context.Context, s Storage) (string, error) {
	name := "Aether"
	files, err := a.tianyiList(ctx, s, "-11")
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if f.IsDir && f.Name == name {
			return f.ID, nil
		}
	}
	var folder struct{ ID json.Number }
	// The PC API expects folder creation parameters in the query string.
	address := tianyiAPI + "/createFolder.action?" + url.Values{"parentFolderId": {"-11"}, "folderName": {name}, "relativePath": {""}}.Encode()
	err = a.tianyiRequest(ctx, s, "POST", address, nil, &folder)
	if err == nil && folder.ID == "" {
		err = errors.New("天翼临时目录创建失败")
	}
	return string(folder.ID), err
}

func (a *App) tianyiRestore(ctx context.Context, s Storage, folder, name string, info CASInfo) (string, error) {
	var upload struct {
		UploadFileID   json.Number
		FileCommitURL  string
		FileDataExists json.Number
	}
	err := a.tianyiRequest(ctx, s, "POST", tianyiAPI+"/createUploadFile.action", url.Values{
		"parentFolderId": {folder}, "fileName": {name}, "size": {strconv.FormatInt(info.Size, 10)},
		"md5": {strings.ToUpper(info.MD5)}, "opertype": {"3"}, "flag": {"1"}, "resumePolicy": {"1"}, "isLog": {"0"},
	}, &upload)
	if err != nil {
		return "", err
	}
	if upload.FileDataExists != "1" {
		return "", errors.New("天翼 CAS 未命中秒传，不会上传媒体内容")
	}
	if upload.UploadFileID == "" || !trustedTianyi(upload.FileCommitURL) {
		return "", errors.New("天翼秒传返回无效提交信息")
	}
	var committed struct {
		ID string `xml:"id"`
	}
	err = a.tianyiRequest(ctx, s, "POST", upload.FileCommitURL, url.Values{
		"uploadFileId": {string(upload.UploadFileID)}, "opertype": {"1"}, "resumePolicy": {"1"}, "isLog": {"0"},
	}, &committed)
	if err == nil && committed.ID == "" {
		err = errors.New("天翼秒传未返回文件 ID")
	}
	return committed.ID, err
}

func (a *App) tianyiRemove(ctx context.Context, s Storage, fid string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	name := ""
	trashed := false
	for _, entry := range a.store.snapshot().CASTemporary {
		if entry.StorageID == s.ID && entry.FileID == fid {
			name = entry.Name
			trashed = entry.Trashed
			break
		}
	}
	tasks, _ := json.Marshal([]map[string]any{{"fileId": fid, "fileName": name, "isFolder": 0}})
	types := []string{"DELETE"}
	if s.Config["deleteMode"] == "permanent" {
		types = append(types, "CLEAR_RECYCLE")
	}
	for _, kind := range types {
		if kind == "DELETE" && trashed {
			continue
		}
		var created struct{ TaskID string }
		if err := a.tianyiRequest(ctx, s, "POST", tianyiAPI+"/batch/createBatchTask.action",
			url.Values{"type": {kind}, "taskInfos": {string(tasks)}}, &created); err != nil {
			return err
		}
		if created.TaskID == "" {
			return errors.New("天翼清理未返回任务 ID")
		}
		for {
			var state struct{ TaskStatus, FailedCount, SkipCount, SuccessedCount int }
			if err := a.tianyiRequest(ctx, s, "POST", tianyiAPI+"/batch/checkBatchTask.action",
				url.Values{"type": {kind}, "taskId": {created.TaskID}}, &state); err != nil {
				return err
			}
			if state.FailedCount > 0 || state.SkipCount > 0 || state.TaskStatus == 2 {
				return errors.New("天翼临时文件清理失败或冲突，保留记录待重试")
			}
			if state.TaskStatus == 4 {
				if state.SuccessedCount < 1 {
					return errors.New("天翼清理未确认文件删除，保留记录")
				}
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
		if kind == "DELETE" {
			if err := a.store.update(func(st *State) error {
				for i := range st.CASTemporary {
					if st.CASTemporary[i].StorageID == s.ID && st.CASTemporary[i].FileID == fid {
						st.CASTemporary[i].Trashed = true
						st.CASTemporary[i].Ready = false
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
