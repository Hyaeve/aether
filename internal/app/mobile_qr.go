package app

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// These keys are the public web protocol's transport constants, not storage credentials.
const mobileQRTransportKey = "UqEZkrjCKfa02pP6jntzFmkzOz86zHUC"
const mobileQRDataKey = "qPqDw263XgFgL3u8"

func (a *App) mobileQRStart(w http.ResponseWriter, r *http.Request) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		fail(w, 500, errors.New("无法创建扫码会话"))
		return
	}
	session := authorizationSession{Provider: "mobile", Token: hex.EncodeToString(random[:8]), Visitor: hex.EncodeToString(random[8:]), Expires: time.Now().Add(5 * time.Minute).Unix(), Owner: authorizationOwner(r)}
	address := "https://yun.139.com/w/#/qrcLogin?" + url.Values{"sID": {session.Token}, "dID": {session.Visitor}, "cType": {"9"}}.Encode()
	png, err := qrcode.Encode(address, qrcode.Medium, 256)
	if err != nil {
		fail(w, 500, errors.New("无法生成移动云盘二维码"))
		return
	}
	token, err := a.sealAuthorization(session)
	if err != nil {
		fail(w, 500, errors.New("无法创建扫码会话"))
		return
	}
	jsonResponse(w, 200, map[string]any{"token": token, "image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), "expiresIn": 300})
}

func mobileQRPad(raw []byte) []byte {
	n := aes.BlockSize - len(raw)%aes.BlockSize
	return append(append([]byte{}, raw...), bytes.Repeat([]byte{byte(n)}, n)...)
}

func mobileQRUnpad(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errors.New("移动扫码响应解密失败")
	}
	n := int(raw[len(raw)-1])
	if n == 0 || n > aes.BlockSize || n > len(raw) || !bytes.Equal(raw[len(raw)-n:], bytes.Repeat([]byte{byte(n)}, n)) {
		return nil, errors.New("移动扫码响应解密失败")
	}
	return raw[:len(raw)-n], nil
}

func mobileQREncrypt(raw []byte) (string, error) {
	block, err := aes.NewCipher([]byte(mobileQRTransportKey))
	if err != nil {
		return "", err
	}
	iv := make([]byte, aes.BlockSize)
	if _, err = rand.Read(iv); err != nil {
		return "", err
	}
	padded := mobileQRPad(raw)
	out := make([]byte, aes.BlockSize+len(padded))
	copy(out, iv)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out[aes.BlockSize:], padded)
	return base64.StdEncoding.EncodeToString(out), nil
}

func mobileQRDecrypt(raw []byte, inner bool) ([]byte, error) {
	if json.Valid(raw) && bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return raw, nil
	}
	value := strings.TrimSpace(string(raw))
	if strings.HasPrefix(value, `"`) {
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, errors.New("移动扫码响应格式异常")
		}
	}
	if strings.HasPrefix(strings.TrimSpace(value), "{") && json.Valid([]byte(value)) {
		return []byte(value), nil
	}
	key := mobileQRTransportKey
	var encrypted []byte
	var err error
	if inner {
		key = mobileQRDataKey
		encrypted, err = hex.DecodeString(value)
	}
	if !inner || err != nil {
		encrypted, err = base64.StdEncoding.DecodeString(value)
	}
	if err != nil || len(encrypted) == 0 || len(encrypted)%aes.BlockSize != 0 {
		return nil, errors.New("移动扫码响应密文无效")
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	var plain []byte
	if inner {
		plain = make([]byte, len(encrypted))
		for i := 0; i < len(encrypted); i += aes.BlockSize {
			block.Decrypt(plain[i:i+aes.BlockSize], encrypted[i:i+aes.BlockSize])
		}
	} else {
		if len(encrypted) < 2*aes.BlockSize {
			return nil, errors.New("移动扫码响应密文无效")
		}
		plain = make([]byte, len(encrypted)-aes.BlockSize)
		cipher.NewCBCDecrypter(block, encrypted[:aes.BlockSize]).CryptBlocks(plain, encrypted[aes.BlockSize:])
	}
	return mobileQRUnpad(plain)
}

func mobileQRString(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(string(raw))
}

func mobileQRField(data map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		if value := mobileQRString(data[key]); value != "" && value != "null" {
			return value
		}
	}
	for _, key := range []string{"data", "userInfo", "loginInfo", "result"} {
		var child map[string]json.RawMessage
		if json.Unmarshal(data[key], &child) == nil {
			if value := mobileQRField(child, keys...); value != "" {
				return value
			}
		}
	}
	return ""
}

func (a *App) mobileQRPoll(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &input) {
		return
	}
	session, err := a.openAuthorization(input.Token, r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if len(session.Token) != 16 || len(session.Visitor) != 32 {
		fail(w, 400, errors.New("移动扫码会话无效"))
		return
	}
	sum := sha1.Sum([]byte("fetion.com.cn:" + session.Token))
	body := map[string]any{"msisdn": "", "random": "", "dycpwd": session.Token, "cpid": 292, "clienttype": 670, "version": "mCloud_4.3.0_536", "pintype": 21, "secinfo": strings.ToUpper(hex.EncodeToString(sum[:])), "loginMode": "0", "extInfo": map[string]any{}}
	plain, _ := json.Marshal(body)
	encrypted, err := mobileQREncrypt(plain)
	if err != nil {
		fail(w, 500, errors.New("移动扫码请求加密失败"))
		return
	}
	payload, _ := json.Marshal(encrypted)
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://user-njs.yun.139.com/user/thirdlogin", bytes.NewReader(payload))
	ts, nonce := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05"), id()[:16]
	device := "||9|7.17.9|chrome|120.0.0.0|" + session.Visitor + "||windows 10||zh-CN|||"
	for name, value := range map[string]string{
		"Accept": "application/json, text/plain, */*", "Content-Type": "application/json;charset=UTF-8", "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36",
		"Origin": "https://yun.139.com", "Referer": "https://yun.139.com/w/", "Caller": "web", "CMS-DEVICE": "default", "Inner-Hcy-Router-Https": "1",
		"mcloud-channel": "1000101", "mcloud-client": "10701", "mcloud-route": "001", "mcloud-version": "7.17.9", "mcloud-sign": ts + "," + nonce + "," + mobileSign(string(plain), ts, nonce), "hcy-cool-flag": "1",
		"x-DeviceInfo": device, "x-yun-client-info": device + "dW5kZWZpbmVk||", "x-huawei-channelSrc": "10000034", "x-inner-ntwk": "2", "x-m4c-caller": "PC", "x-m4c-src": "10002", "x-SvcType": "1",
		"x-yun-api-version": "v1", "x-yun-app-channel": "10000034", "x-yun-channel-source": "10000034", "x-yun-module-type": "100", "x-yun-svc-type": "1",
	} {
		req.Header.Set(name, value)
	}
	client := &http.Client{Transport: apiClient.Transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		fail(w, 502, errors.New("移动扫码服务连接失败，请重新获取二维码"))
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		fail(w, 502, fmt.Errorf("移动扫码服务 HTTP %d", res.StatusCode))
		return
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		fail(w, 502, errors.New("移动扫码响应过大或不完整"))
		return
	}
	raw, err = mobileQRDecrypt(raw, false)
	var envelope map[string]json.RawMessage
	if err != nil || json.Unmarshal(raw, &envelope) != nil {
		fail(w, 502, errors.New("移动扫码响应无法解析，请重新获取二维码"))
		return
	}
	code := mobileQRField(envelope, "code")
	status := ""
	switch code {
	case "200059542":
		status = "expired"
	case "200059549":
		status = "cancelled"
	case "200059541", "200059548", "9999":
		status = "waiting"
	case "200059543", "200059545", "200059546", "200059547", "01000001", "9101":
		status = "error"
	}
	if status != "" {
		jsonResponse(w, 200, map[string]string{"status": status, "error": map[string]string{"error": "移动扫码授权失败，请重新获取二维码"}[status]})
		return
	}
	data, err := mobileQRDecrypt(envelope["data"], true)
	var credentials map[string]json.RawMessage
	if err != nil || json.Unmarshal(data, &credentials) != nil {
		fail(w, 502, errors.New("移动扫码未返回有效账号与令牌"))
		return
	}
	account, token := mobileQRField(credentials, "account", "msisdn", "phoneNumber"), mobileQRField(credentials, "token", "authToken", "accessToken")
	if account == "" {
		if decoded, e := base64.StdEncoding.DecodeString(mobileQRField(credentials, "encryptAccount")); e == nil {
			account = strings.TrimSpace(string(decoded))
		}
	}
	if code != "" && code != "0" && code != "0000" || account == "" || token == "" || strings.Contains(account, ":") || len(account) > 128 || len(token) > 16384 {
		fail(w, 502, errors.New("移动扫码未返回有效账号与令牌"))
		return
	}
	auth := base64.StdEncoding.EncodeToString([]byte("pc:" + account + ":" + token))
	jsonResponse(w, 200, map[string]string{"status": "success", "authorization": auth, "userDomainId": mobileQRField(credentials, "userDomainId", "ud_id")})
}
