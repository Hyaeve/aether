package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func upload115API(ctx context.Context, s Storage, method, endpoint string, body, out any) error {
	var result struct {
		State bool
		Data  json.RawMessage
	}
	if err := uploadAPI(ctx, s, method, "https://proapi.115.com/open/upload/"+endpoint, body, &result); err != nil {
		return err
	}
	if !result.State {
		return errors.New("115 拒绝上传操作")
	}
	return json.Unmarshal(result.Data, out)
}

func (a *App) upload115(ctx context.Context, s Storage, parent, name string, f *os.File, size int64) error {
	digest, err := uploadHash(ctx, f, 0, size, sha1.New())
	if err != nil {
		return err
	}
	digest = strings.ToUpper(digest)
	preHash, err := uploadHash(ctx, f, 0, min(size, 128<<10), sha1.New())
	if err != nil {
		return err
	}
	form := url.Values{"file_name": {name}, "file_size": {strconv.FormatInt(size, 10)}, "target": {"U_1_" + parent}, "fileid": {digest}, "preid": {strings.ToUpper(preHash)}, "topupload": {"0"}}
	var init struct {
		Status      json.RawMessage `json:"status"`
		SignKey     string          `json:"sign_key"`
		SignCheck   string          `json:"sign_check"`
		PickCode    string          `json:"pick_code"`
		Bucket      string          `json:"bucket"`
		Object      string          `json:"object"`
		Callback    json.RawMessage `json:"callback"`
		CallbackVar json.RawMessage `json:"callback_var"`
	}
	if err := upload115API(ctx, s, "POST", "init", form, &init); err != nil {
		return err
	}
	if status := rawText(init.Status); status == "6" || status == "7" || status == "8" {
		left, right, ok := strings.Cut(init.SignCheck, "-")
		start, e1 := strconv.ParseInt(left, 10, 64)
		end, e2 := strconv.ParseInt(right, 10, 64)
		if !ok || e1 != nil || e2 != nil || start < 0 || end < start || end >= size || init.SignKey == "" {
			return errors.New("115 二次校验范围无效")
		}
		proof, err := uploadHash(ctx, f, start, end-start+1, sha1.New())
		if err != nil {
			return err
		}
		form.Set("sign_key", init.SignKey)
		form.Set("sign_val", strings.ToUpper(proof))
		form.Set("pick_code", init.PickCode)
		if err := upload115API(ctx, s, "POST", "init", form, &init); err != nil {
			return err
		}
	}
	if rawText(init.Status) == "2" {
		return nil
	}
	var token map[string]json.RawMessage
	if err := upload115API(ctx, s, "GET", "get_token", nil, &token); err != nil {
		return err
	}
	value := func(names ...string) string {
		for _, name := range names {
			if v := rawText(token[name]); v != "" {
				return v
			}
		}
		return ""
	}
	key := value("AccessKeyId", "accessKeyId", "access_key_id")
	secret := value("AccessKeySecret", "accessKeySecret", "access_key_secret")
	security := value("SecurityToken", "securityToken", "security_token")
	endpoint := value("Endpoint", "endpoint", "endPoint", "EndPoint")
	if key == "" || secret == "" || security == "" || endpoint == "" || init.Bucket == "" || init.Object == "" {
		return errors.New("115 上传凭据不完整")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("115 上传地址无效")
	}
	u.Host = init.Bucket + "." + u.Host
	u.Path = "/" + init.Object
	callback, callbackVar := init.Callback, init.CallbackVar
	var envelope map[string]json.RawMessage
	if json.Unmarshal(callback, &envelope) == nil {
		if nested := envelope["value"]; len(nested) > 0 {
			_ = json.Unmarshal(nested, &envelope)
		}
		if cb := envelope["callback"]; len(cb) > 0 {
			callback = cb
		}
		if cv := envelope["callback_var"]; len(cv) > 0 {
			callbackVar = cv
		}
	}
	encodeCallback := func(raw json.RawMessage) string {
		if len(raw) == 0 || string(raw) == "null" {
			return ""
		}
		var text string
		if json.Unmarshal(raw, &text) != nil {
			text = string(raw)
		}
		return base64.StdEncoding.EncodeToString([]byte(strings.ReplaceAll(text, "${sha1}", digest)))
	}
	send := func(method string, q url.Values, body io.Reader, length int64, complete bool) ([]byte, http.Header, error) {
		h := http.Header{"Content-Type": {"application/octet-stream"}}
		if method == "POST" && complete {
			h.Set("Content-Type", "application/xml")
		}
		h.Set("x-oss-security-token", security)
		if complete {
			if cb := encodeCallback(callback); cb != "" {
				h.Set("x-oss-callback", cb)
			}
			if cv := encodeCallback(callbackVar); cv != "" {
				h.Set("x-oss-callback-var", cv)
			}
		}
		date := time.Now().UTC().Format(http.TimeFormat)
		h.Set("Date", date)
		keys := []string{}
		for k := range h {
			if strings.HasPrefix(strings.ToLower(k), "x-oss-") {
				keys = append(keys, strings.ToLower(k))
			}
		}
		sort.Strings(keys)
		sign := method + "\n\n" + h.Get("Content-Type") + "\n" + date + "\n"
		for _, k := range keys {
			sign += k + ":" + h.Get(k) + "\n"
		}
		sign += "/" + init.Bucket + "/" + init.Object
		queryKeys := []string{}
		for k := range q {
			queryKeys = append(queryKeys, k)
		}
		sort.Strings(queryKeys)
		query := []string{}
		for _, k := range queryKeys {
			v := k
			if q.Get(k) != "" {
				v += "=" + q.Get(k)
			}
			query = append(query, v)
		}
		if len(query) > 0 {
			sign += "?" + strings.Join(query, "&")
		}
		mac := hmac.New(sha1.New, []byte(secret))
		_, _ = mac.Write([]byte(sign))
		h.Set("Authorization", "OSS "+key+":"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		address := *u
		address.RawQuery = q.Encode()
		return writeHTTP(ctx, method, address.String(), h, body, length)
	}
	if size <= 16<<20 {
		raw, _, err := send("PUT", nil, io.NewSectionReader(f, 0, size), size, true)
		if err != nil {
			return err
		}
		return check115Callback(raw)
	}
	raw, _, err := send("POST", url.Values{"uploads": {""}, "sequential": {""}}, nil, 0, false)
	if err != nil {
		return err
	}
	var created struct {
		ID string `xml:"UploadId"`
	}
	if xml.Unmarshal(raw, &created) != nil || created.ID == "" {
		return errors.New("115 未返回分片会话")
	}
	completed := false
	defer func() {
		if !completed {
			a.store.event("warn", "storage", "115 分片上传未完成，暂存文件已保留；上游可能残留未完成分片")
		}
	}()
	parts := completedParts{}
	partSize := max(int64(16<<20), (size+9998)/9999)
	for offset := int64(0); offset < size; offset += partSize {
		number := len(parts.Parts) + 1
		length := min(partSize, size-offset)
		_, h, err := send("PUT", url.Values{"uploadId": {created.ID}, "partNumber": {strconv.Itoa(number)}}, io.NewSectionReader(f, offset, length), length, false)
		if err != nil {
			return err
		}
		if h.Get("ETag") == "" {
			return errors.New("115 未确认上传分片")
		}
		parts.Parts = append(parts.Parts, uploadedPart{number, h.Get("ETag")})
	}
	body, err := xml.Marshal(parts)
	if err != nil {
		return err
	}
	raw, _, err = send("POST", url.Values{"uploadId": {created.ID}}, bytes.NewReader(body), int64(len(body)), true)
	if err != nil {
		return err
	}
	if err := check115Callback(raw); err != nil {
		return err
	}
	completed = true
	return nil
}

func check115Callback(raw []byte) error {
	var result struct{ State bool }
	if json.Unmarshal(raw, &result) != nil || !result.State {
		return errors.New("115 未确认上传回调成功")
	}
	return nil
}
