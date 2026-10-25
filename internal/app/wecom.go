package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const wecomDefaultAPI = "https://qyapi.weixin.qq.com"

type wecomConfig struct {
	Enabled           bool     `json:"enabled"`
	CorpID            string   `json:"corpId"`
	Secret            string   `json:"secret"`
	AgentID           string   `json:"agentId"`
	APIURL            string   `json:"apiURL"`
	UserIDs           []string `json:"userIds"`
	Token             string   `json:"token"`
	EncodingAESKey    string   `json:"encodingAESKey"`
	HasSecret         bool     `json:"hasSecret,omitempty"`
	HasToken          bool     `json:"hasToken,omitempty"`
	HasEncodingAESKey bool     `json:"hasEncodingAESKey,omitempty"`
	ClearSecrets      []string `json:"clearSecrets,omitempty"`
}

type wecomService struct {
	mu     sync.Mutex
	token  string
	stamp  string
	expiry time.Time
}

var wecomIdentity = regexp.MustCompile(`^[a-zA-Z0-9_@.-]{1,128}$`)
var wecomCallbackToken = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func visibleWeCom(value wecomConfig) wecomConfig {
	value.HasSecret, value.HasToken, value.HasEncodingAESKey = value.Secret != "", value.Token != "", value.EncodingAESKey != ""
	value.Secret, value.Token, value.EncodingAESKey, value.ClearSecrets = "", "", "", nil
	if value.APIURL == "" {
		value.APIURL = wecomDefaultAPI
	}
	if value.UserIDs == nil {
		value.UserIDs = []string{}
	}
	return value
}

func mergeWeCom(value, previous wecomConfig) (wecomConfig, error) {
	for _, field := range value.ClearSecrets {
		if field != "secret" && field != "token" && field != "encodingAESKey" {
			return value, errors.New("不支持清除此企业微信凭据字段")
		}
	}
	for field, target := range map[string]*string{"secret": &value.Secret, "token": &value.Token, "encodingAESKey": &value.EncodingAESKey} {
		*target = strings.TrimSpace(*target)
		if slices.Contains(value.ClearSecrets, field) {
			*target = ""
		} else if *target == "" || *target == "********" {
			*target = map[string]string{"secret": previous.Secret, "token": previous.Token, "encodingAESKey": previous.EncodingAESKey}[field]
		}
	}
	value.HasSecret, value.HasToken, value.HasEncodingAESKey, value.ClearSecrets = false, false, false, nil
	value.CorpID, value.AgentID, value.APIURL = strings.TrimSpace(value.CorpID), strings.TrimSpace(value.AgentID), strings.TrimRight(strings.TrimSpace(value.APIURL), "/")
	if value.APIURL == "" {
		value.APIURL = wecomDefaultAPI
	}
	users := make([]string, 0, len(value.UserIDs))
	for _, user := range value.UserIDs {
		user = strings.TrimSpace(user)
		if !slices.Contains(users, user) {
			users = append(users, user)
		}
	}
	value.UserIDs = users
	return value, validateWeCom(value)
}

func validateWeCom(value wecomConfig) error {
	if value.CorpID != "" && !wecomIdentity.MatchString(value.CorpID) {
		return errors.New("企业 ID 格式无效")
	}
	if len(value.Secret) > 512 || strings.ContainsAny(value.Secret, "\x00\r\n") {
		return errors.New("应用 Secret 格式无效")
	}
	if value.AgentID != "" {
		n, err := strconv.ParseInt(value.AgentID, 10, 32)
		if err != nil || n < 1 || strconv.FormatInt(n, 10) != value.AgentID {
			return errors.New("Agent ID 须为正整数")
		}
	}
	if value.APIURL != "" {
		u, err := url.Parse(value.APIURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(value.APIURL) > 2048 {
			return errors.New("API 地址须为不带凭据、查询参数或片段的 HTTPS 地址")
		}
		for _, segment := range strings.Split(u.Path, "/") {
			if segment == "." || segment == ".." {
				return errors.New("API 地址路径无效")
			}
		}
	}
	if len(value.UserIDs) > 100 {
		return errors.New("企业微信接收成员最多100项")
	}
	for _, user := range value.UserIDs {
		if !wecomIdentity.MatchString(user) || user == "@all" && len(value.UserIDs) != 1 {
			return errors.New("接收成员格式无效，多个成员用分号分隔，@all须单独使用")
		}
	}
	if (value.Token == "") != (value.EncodingAESKey == "") {
		return errors.New("机器人回调须同时填写 Token 和 EncodingAESKey，或同时留空")
	}
	if value.Token != "" && !wecomCallbackToken.MatchString(value.Token) {
		return errors.New("企业微信 Token 格式无效")
	}
	if value.EncodingAESKey != "" {
		key, err := base64.RawStdEncoding.DecodeString(value.EncodingAESKey)
		if len(value.EncodingAESKey) != 43 || err != nil || len(key) != 32 {
			return errors.New("EncodingAESKey 须为43位有效的企业微信编码密钥")
		}
	}
	if value.Enabled && (value.CorpID == "" || value.Secret == "" || value.AgentID == "" || len(value.UserIDs) == 0) {
		return errors.New("启用企业微信须填写企业 ID、应用 Secret、Agent ID 和接收成员")
	}
	return nil
}

func (a *App) wecomSecret(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Field        string `json:"field"`
		MetadataOnly bool   `json:"metadataOnly"`
	}
	w.Header().Set("Cache-Control", "no-store")
	if !decode(w, r, &input) {
		return
	}
	cfg := a.store.snapshotWithLogLimit(0).Notifications.WeCom
	value, ok := map[string]string{"secret": cfg.Secret, "token": cfg.Token, "encodingAESKey": cfg.EncodingAESKey}[input.Field]
	if !ok {
		fail(w, 400, errors.New("不支持的企业微信凭据字段"))
		return
	}
	if input.MetadataOnly {
		jsonResponse(w, 200, map[string]int{"length": len([]rune(value))})
		return
	}
	jsonResponse(w, 200, map[string]string{"value": value})
}

type wecomResponse struct {
	Code        *int   `json:"errcode"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	InvalidUser string `json:"invaliduser"`
	InvalidTag  string `json:"invalidtag"`
	InvalidDept string `json:"invalidparty"`
}

func wecomRequest(ctx context.Context, cfg wecomConfig, endpoint string, query url.Values, body any) (wecomResponse, error) {
	var result wecomResponse
	base := cfg.APIURL
	if base == "" {
		base = wecomDefaultAPI
	}
	u, err := url.Parse(base)
	if err != nil {
		return result, errors.New("企业微信 API 地址无效")
	}
	u.Path, u.RawPath, u.RawQuery = strings.TrimRight(u.Path, "/")+endpoint, "", query.Encode()
	method := http.MethodGet
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return result, errors.New("企业微信请求格式无效")
		}
		method, reader = http.MethodPost, bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return result, errors.New("企业微信请求创建失败")
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: apiClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return result, errors.New("企业微信连接失败，请检查服务器网络；消息不会自动重发")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return result, fmt.Errorf("企业微信接口 HTTP %d；请核对 API 地址和服务器网络", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || json.Unmarshal(raw, &result) != nil || result.Code == nil {
		return result, errors.New("企业微信未返回有效回执；消息不会自动重发")
	}
	if *result.Code != 0 {
		return result, fmt.Errorf("企业微信请求失败（错误码 %d），请核对应用权限、凭据及可信 IP", *result.Code)
	}
	return result, nil
}

func (a *App) wecomTest(w http.ResponseWriter, r *http.Request) {
	cfg := a.store.snapshotWithLogLimit(0).Notifications.WeCom
	if err := validateWeCom(cfg); err != nil {
		fail(w, 400, err)
		return
	}
	if cfg.Secret == "" || cfg.CorpID == "" || cfg.AgentID == "" || len(cfg.UserIDs) == 0 {
		fail(w, 400, errors.New("请先保存企业微信应用凭据和接收成员"))
		return
	}
	if !a.wecom.mu.TryLock() {
		fail(w, 409, errors.New("企业微信测试消息正在发送"))
		return
	}
	defer a.wecom.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	stamp := telegramDigest(cfg)
	send := func() error {
		if a.wecom.stamp != stamp || time.Now().After(a.wecom.expiry) {
			result, err := wecomRequest(ctx, cfg, "/cgi-bin/gettoken", url.Values{"corpid": {cfg.CorpID}, "corpsecret": {cfg.Secret}}, nil)
			if err != nil {
				return err
			}
			if result.AccessToken == "" || len(result.AccessToken) > 4096 || result.ExpiresIn < 1 || result.ExpiresIn > 86400 {
				return errors.New("企业微信未返回有效的应用令牌")
			}
			a.wecom.token, a.wecom.stamp = result.AccessToken, stamp
			a.wecom.expiry = time.Now().Add(time.Duration(max(1, result.ExpiresIn-60)) * time.Second)
		}
		if telegramDigest(a.store.snapshotWithLogLimit(0).Notifications.WeCom) != stamp {
			return errors.New("企业微信配置已变化，请重新测试")
		}
		agent, _ := strconv.ParseInt(cfg.AgentID, 10, 32)
		result, err := wecomRequest(ctx, cfg, "/cgi-bin/message/send", url.Values{"access_token": {a.wecom.token}}, map[string]any{
			"touser": strings.Join(cfg.UserIDs, "|"), "agentid": agent, "msgtype": "text", "text": map[string]string{"content": "Aether 企业微信通知连接测试成功"},
		})
		if err != nil {
			a.wecom.expiry = time.Time{}
			return err
		}
		if result.InvalidUser != "" || result.InvalidTag != "" || result.InvalidDept != "" {
			return errors.New("部分企业微信接收成员无效，其他成员可能已收到消息；不会自动重发")
		}
		return nil
	}
	if err := send(); err != nil {
		a.store.event("warn", "notifications", err.Error())
		fail(w, 502, err)
		return
	}
	a.store.event("info", "notifications", "企业微信测试消息已发送")
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func wecomSignature(token, timestamp, nonce, encrypted string) string {
	parts := []string{token, timestamp, nonce, encrypted}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

func wecomDecrypt(cfg wecomConfig, encrypted string) ([]byte, error) {
	key, err := base64.RawStdEncoding.DecodeString(cfg.EncodingAESKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid key")
	}
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil || len(data) < 32 || len(data)%32 != 0 {
		return nil, errors.New("invalid ciphertext")
	}
	block, _ := aes.NewCipher(key)
	cipher.NewCBCDecrypter(block, key[:16]).CryptBlocks(data, data)
	padding := int(data[len(data)-1])
	if padding < 1 || padding > 32 || padding > len(data)-20 {
		return nil, errors.New("invalid padding")
	}
	for _, value := range data[len(data)-padding:] {
		if int(value) != padding {
			return nil, errors.New("invalid padding")
		}
	}
	data = data[:len(data)-padding]
	length := uint64(binary.BigEndian.Uint32(data[16:20]))
	if length > uint64(len(data)-20) || !hmac.Equal(data[20+length:], []byte(cfg.CorpID)) {
		return nil, errors.New("invalid receiver")
	}
	return data[20 : 20+length], nil
}

func wecomEncrypt(cfg wecomConfig, plain []byte, timestamp, nonce string) ([]byte, error) {
	key, err := base64.RawStdEncoding.DecodeString(cfg.EncodingAESKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid key")
	}
	raw := make([]byte, 20, 20+len(plain)+len(cfg.CorpID)+32)
	if _, err := rand.Read(raw[:16]); err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint32(raw[16:], uint32(len(plain)))
	raw = append(raw, plain...)
	raw = append(raw, cfg.CorpID...)
	padding := 32 - len(raw)%32
	raw = append(raw, bytes.Repeat([]byte{byte(padding)}, padding)...)
	block, _ := aes.NewCipher(key)
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(raw, raw)
	encrypted := base64.StdEncoding.EncodeToString(raw)
	return xml.Marshal(struct {
		XMLName   xml.Name `xml:"xml"`
		Encrypt   string   `xml:"Encrypt"`
		Signature string   `xml:"MsgSignature"`
		Timestamp string   `xml:"TimeStamp"`
		Nonce     string   `xml:"Nonce"`
	}{Encrypt: encrypted, Signature: wecomSignature(cfg.Token, timestamp, nonce, encrypted), Timestamp: timestamp, Nonce: nonce})
}

func (a *App) wecomCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cfg := a.store.snapshotWithLogLimit(0).Notifications.WeCom
	if !cfg.Enabled || cfg.Token == "" || cfg.EncodingAESKey == "" || validateWeCom(cfg) != nil {
		http.NotFound(w, r)
		return
	}
	reject := func() { http.Error(w, "回调验证失败", http.StatusForbidden) }
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		reject()
		return
	}
	for _, field := range []string{"timestamp", "nonce", "msg_signature"} {
		if len(query[field]) != 1 || query.Get(field) == "" || len(query.Get(field)) > 128 {
			reject()
			return
		}
	}
	timestamp, nonce := query.Get("timestamp"), query.Get("nonce")
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	now := time.Now().Unix()
	if err != nil || seconds < now-300 || seconds > now+300 {
		reject()
		return
	}
	encrypted := query.Get("echostr")
	if r.Method == http.MethodGet {
		if len(query["echostr"]) != 1 || len(encrypted) > 65536 {
			reject()
			return
		}
	} else {
		var envelope struct {
			Encrypt string `xml:"Encrypt"`
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
		if err != nil || xml.Unmarshal(data, &envelope) != nil {
			reject()
			return
		}
		encrypted = envelope.Encrypt
	}
	signature := wecomSignature(cfg.Token, timestamp, nonce, encrypted)
	if !hmac.Equal([]byte(signature), []byte(query.Get("msg_signature"))) {
		reject()
		return
	}
	plain, err := wecomDecrypt(cfg, encrypted)
	if err != nil {
		reject()
		return
	}
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(plain)
		return
	}
	var message struct {
		To      string `xml:"ToUserName"`
		From    string `xml:"FromUserName"`
		AgentID string `xml:"AgentID"`
		Type    string `xml:"MsgType"`
		Content string `xml:"Content"`
	}
	if xml.Unmarshal(plain, &message) != nil || message.To != cfg.CorpID || message.AgentID != cfg.AgentID || !wecomIdentity.MatchString(message.From) || !slices.Contains(cfg.UserIDs, message.From) && !slices.Contains(cfg.UserIDs, "@all") {
		reject()
		return
	}
	if message.Type != "text" {
		io.WriteString(w, "success")
		return
	}
	content := "Aether 企业微信已连接。\n发送 /status 或“状态”查看服务状态。\n链接转存与分享STRM命令当前仅接入 TG Bot。"
	switch strings.TrimSpace(message.Content) {
	case "/status", "状态":
		st := a.store.snapshotWithLogLimit(0)
		content = fmt.Sprintf("Aether 运行正常\n存储池：%d\n任务：%d", len(st.Storages), len(st.Tasks))
	}
	reply, err := xml.Marshal(struct {
		XMLName xml.Name `xml:"xml"`
		To      string   `xml:"ToUserName"`
		From    string   `xml:"FromUserName"`
		Time    int64    `xml:"CreateTime"`
		Type    string   `xml:"MsgType"`
		Content string   `xml:"Content"`
	}{To: message.From, From: cfg.CorpID, Time: now, Type: "text", Content: content})
	if err == nil {
		reply, err = wecomEncrypt(cfg, reply, strconv.FormatInt(now, 10), id())
	}
	if err != nil || telegramDigest(a.store.snapshotWithLogLimit(0).Notifications.WeCom) != telegramDigest(cfg) {
		reject()
		return
	}
	a.store.event("info", "notifications", "企业微信已响应消息（只读）")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write(reply)
}
