package app

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func wecomFixture(t *testing.T) (*App, http.Handler, *http.Cookie, map[string]any) {
	t.Helper()
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	cfg := map[string]any{"enabled": true, "corpId": "ww-test", "secret": "test-corp-secret", "agentId": "1000002", "apiURL": "https://qyapi.weixin.qq.com", "userIds": []string{"alice"}, "token": "callback-token", "encodingAESKey": base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32))}
	w := request(t, h, "PUT", "/api/notification-service/wecom", map[string]any{"wecom": cfg}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return a, h, cookie, cfg
}

func TestWeComConfigPersistenceAndSecrets(t *testing.T) {
	a, h, cookie, cfg := wecomFixture(t)
	for _, endpoint := range []string{"/api/notification-service", "/api/state"} {
		w := request(t, h, "GET", endpoint, nil, cookie)
		for _, key := range []string{"secret", "token", "encodingAESKey"} {
			if strings.Contains(w.Body.String(), cfg[key].(string)) {
				t.Fatal("secret exposed", endpoint, key)
			}
		}
	}
	for _, key := range []string{"secret", "token", "encodingAESKey"} {
		if w := request(t, h, "POST", "/api/notification-service/wecom/secret", map[string]string{"field": key}, nil); w.Code != 401 {
			t.Fatal("unauthorized reveal", key, w.Code)
		}
		w := request(t, h, "POST", "/api/notification-service/wecom/secret", map[string]any{"field": key, "metadataOnly": true}, cookie)
		if w.Code != 200 || strings.Contains(w.Body.String(), cfg[key].(string)) {
			t.Fatal("unsafe secret metadata", key, w.Code)
		}
		w = request(t, h, "POST", "/api/notification-service/wecom/secret", map[string]string{"field": key}, cookie)
		if w.Code != 200 || !strings.Contains(w.Body.String(), cfg[key].(string)) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("explicit reveal", key, w.Code)
		}
	}
	raw, err := os.ReadFile(filepath.Join(a.store.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"secret", "token", "encodingAESKey"} {
		if strings.Contains(string(raw), cfg[key].(string)) {
			t.Fatal("plaintext credential", key)
		}
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := json.Marshal(a.store.snapshot().Notifications)
	restored, _ := json.Marshal(reopened.snapshot().Notifications)
	if !bytes.Equal(original, restored) {
		t.Fatal("configuration lost across restart")
	}
	cfg["secret"], cfg["token"], cfg["encodingAESKey"] = "", "", ""
	if w := request(t, h, "PUT", "/api/notification-service/wecom", map[string]any{"wecom": cfg}, cookie); w.Code != 200 {
		t.Fatal("unchanged secrets should be retained", w.Code)
	}
	if w := request(t, h, "PUT", "/api/notification-service", map[string]any{"telegram": map[string]any{}}, cookie); w.Code != 200 {
		t.Fatal("saving TG damaged WeCom", w.Code)
	}
	w := request(t, h, "GET", "/api/notification-service", nil, cookie)
	if !strings.Contains(w.Body.String(), `"hasSecret":true`) || !strings.Contains(w.Body.String(), `"hasEncodingAESKey":true`) {
		t.Fatal("another notification form erased credentials", w.Body.String())
	}
	cfg["clearSecrets"] = []string{"token", "encodingAESKey"}
	if w := request(t, h, "PUT", "/api/notification-service/wecom", map[string]any{"wecom": cfg}, cookie); w.Code != 200 {
		t.Fatal("explicit callback credentials clear", w.Code)
	}
	w = request(t, h, "GET", "/api/notification-service", nil, cookie)
	var visible struct {
		WeCom map[string]any `json:"wecom"`
	}
	if json.Unmarshal(w.Body.Bytes(), &visible) != nil || visible.WeCom["hasToken"] == true || visible.WeCom["hasEncodingAESKey"] == true {
		t.Fatal("callback credentials were not cleared")
	}
}

func TestWeComValidationAndMessageResult(t *testing.T) {
	a, h, cookie, cfg := wecomFixture(t)
	for key, invalid := range map[string]any{"apiURL": "http://example.com", "agentId": "0", "encodingAESKey": "wrong", "userIds": []string{"alice|bob"}} {
		old := cfg[key]
		cfg[key] = invalid
		if w := request(t, h, "PUT", "/api/notification-service/wecom", map[string]any{"wecom": cfg}, cookie); w.Code != 400 {
			t.Fatal("invalid field accepted", key, w.Code)
		}
		cfg[key] = old
	}
	calls := 0
	partial := false
	shareMock(t, func(r *http.Request) string {
		if r.URL.Host != "qyapi.weixin.qq.com" {
			t.Fatal("unexpected destination", r.URL.Host)
		}
		calls++
		if r.URL.Path == "/cgi-bin/gettoken" {
			if r.URL.Query().Get("corpsecret") != "test-corp-secret" {
				t.Fatal("missing credentials")
			}
			return `{"errcode":0,"access_token":"test-access-token","expires_in":7200}`
		}
		if r.URL.Path != "/cgi-bin/message/send" || r.Method != "POST" || r.URL.Query().Get("access_token") != "test-access-token" {
			t.Fatal("unexpected request", r.Method, r.URL.Path)
		}
		var body struct {
			ToUser  string `json:"touser"`
			AgentID int64  `json:"agentid"`
			MsgType string `json:"msgtype"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.ToUser != "alice" || body.AgentID != 1000002 || body.MsgType != "text" {
			t.Fatal("wrong message envelope", body)
		}
		if partial {
			return `{"errcode":0,"invaliduser":"alice","errmsg":"test-corp-secret private upstream"}`
		}
		return `{"errcode":0,"msgid":"ok"}`
	})
	if w := request(t, h, "POST", "/api/notification-service/wecom/test", map[string]any{}, nil); w.Code != 401 || calls != 0 {
		t.Fatal("unauthorized message sent")
	}
	if w := request(t, h, "POST", "/api/notification-service/wecom/test", map[string]any{}, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	partial = true
	w := request(t, h, "POST", "/api/notification-service/wecom/test", map[string]any{}, cookie)
	if w.Code != 502 || calls != 3 || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "test-corp-secret") {
		t.Fatal("partial failure, token cache or privacy", calls, w.Code, w.Body.String())
	}
	logs, _ := json.Marshal(a.store.snapshot().Logs)
	if strings.Contains(string(logs), "test-access-token") || strings.Contains(string(logs), "test-corp-secret") {
		t.Fatal("message credentials leaked to logs")
	}
}

// This fixture constructs the wire envelope independently of the application codec.
func wecomWire(t *testing.T, cfg map[string]any, payload, receiver string) string {
	t.Helper()
	key, _ := base64.RawStdEncoding.DecodeString(cfg["encodingAESKey"].(string))
	raw := make([]byte, 20)
	copy(raw, "0123456789abcdef")
	binary.BigEndian.PutUint32(raw[16:], uint32(len(payload)))
	raw = append(append(raw, payload...), receiver...)
	padding := 32 - len(raw)%32
	raw = append(raw, bytes.Repeat([]byte{byte(padding)}, padding)...)
	block, _ := aes.NewCipher(key)
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(raw, raw)
	return base64.StdEncoding.EncodeToString(raw)
}

func wecomQuery(cfg map[string]any, encrypted, timestamp string) string {
	values := []string{cfg["token"].(string), timestamp, "test-nonce", encrypted}
	sort.Strings(values)
	sum := sha1.Sum([]byte(strings.Join(values, "")))
	return url.Values{"timestamp": {timestamp}, "nonce": {"test-nonce"}, "msg_signature": {hex.EncodeToString(sum[:])}}.Encode()
}

func TestWeComCallbackAuthenticationAndReadOnlyReplies(t *testing.T) {
	_, h, _, cfg := wecomFixture(t)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	echo := wecomWire(t, cfg, "hello", "ww-test")
	w := request(t, h, "GET", "/notify/wecom?"+wecomQuery(cfg, echo, stamp)+"&echostr="+url.QueryEscape(echo), nil, nil)
	if w.Code != 200 || w.Body.String() != "hello" {
		t.Fatal("URL verification", w.Code, w.Body.String())
	}
	for _, receiver := range []string{"wrong-company", "ww-test"} {
		encrypted := wecomWire(t, cfg, "hello", receiver)
		query := wecomQuery(cfg, encrypted, stamp)
		if receiver == "ww-test" {
			query += "&msg_signature=duplicate"
		}
		w = request(t, h, "GET", "/notify/wecom?"+query+"&echostr="+url.QueryEscape(encrypted), nil, nil)
		if w.Code != 403 {
			t.Fatal("unauthorized verification accepted", receiver, w.Code)
		}
	}
	for _, tc := range []struct {
		user, agent, stamp string
		want               int
	}{
		{"alice", "1000002", stamp, 200}, {"mallory", "1000002", stamp, 403},
		{"alice", "1000003", stamp, 403}, {"alice", "1000002", "1", 403},
	} {
		payload := fmt.Sprintf("<xml><ToUserName>ww-test</ToUserName><FromUserName>%s</FromUserName><CreateTime>%s</CreateTime><MsgType>text</MsgType><Content>/status</Content><MsgId>123</MsgId><AgentID>%s</AgentID></xml>", tc.user, tc.stamp, tc.agent)
		encrypted := wecomWire(t, cfg, payload, "ww-test")
		r := httptest.NewRequest("POST", "/notify/wecom?"+wecomQuery(cfg, encrypted, tc.stamp), strings.NewReader("<xml><Encrypt>"+encrypted+"</Encrypt></xml>"))
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal(tc.user, tc.agent, w.Code, w.Body.String())
		}
		if tc.want == 200 {
			var reply struct {
				Encrypt   string `xml:"Encrypt"`
				Signature string `xml:"MsgSignature"`
				Timestamp string `xml:"TimeStamp"`
				Nonce     string `xml:"Nonce"`
			}
			if xml.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Encrypt == "" || strings.Contains(w.Body.String(), "Aether") {
				t.Fatal("expected encrypted passive reply", w.Body.String())
			}
			parts := []string{cfg["token"].(string), reply.Timestamp, reply.Nonce, reply.Encrypt}
			sort.Strings(parts)
			sum := sha1.Sum([]byte(strings.Join(parts, "")))
			if reply.Signature != hex.EncodeToString(sum[:]) {
				t.Fatal("invalid response signature")
			}
			key, _ := base64.RawStdEncoding.DecodeString(cfg["encodingAESKey"].(string))
			raw, err := base64.StdEncoding.DecodeString(reply.Encrypt)
			if err != nil || len(raw)%16 != 0 {
				t.Fatal("invalid reply envelope")
			}
			block, _ := aes.NewCipher(key)
			cipher.NewCBCDecrypter(block, key[:16]).CryptBlocks(raw, raw)
			length := int(binary.BigEndian.Uint32(raw[16:20]))
			if length > len(raw)-20 || !strings.Contains(string(raw[20:20+length]), "<ToUserName>alice</ToUserName>") || !strings.Contains(string(raw[20:20+length]), "Aether 运行正常") {
				t.Fatal("incorrect passive reply")
			}
		}
	}
}

func TestWeComTransportNeverRedirectsOrReplays(t *testing.T) {
	for _, mode := range []string{"redirect", "invalid-token", "unknown-write", "api-error"} {
		t.Run(mode, func(t *testing.T) {
			_, h, cookie, _ := wecomFixture(t)
			old := apiClient
			t.Cleanup(func() { apiClient = old })
			calls := 0
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "qyapi.weixin.qq.com" {
					t.Fatal("credential redirect followed")
				}
				body, status := `{"errcode":0,"access_token":"private-token","expires_in":7200}`, 200
				header := http.Header{}
				if mode == "redirect" {
					status = 302
					header.Set("Location", "https://foreign.invalid/steal")
				} else if mode == "invalid-token" {
					body = `{"access_token":"private-token","expires_in":7200}`
				} else if r.URL.Path == "/cgi-bin/message/send" {
					body = `{"private":"test-corp-secret private-token"}`
					if mode == "api-error" {
						body = `{"errcode":60020,"errmsg":"test-corp-secret private-token"}`
					}
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			w := request(t, h, "POST", "/api/notification-service/wecom/test", map[string]any{}, cookie)
			want := 2
			if mode == "redirect" || mode == "invalid-token" {
				want = 1
			}
			if w.Code != 502 || calls != want || strings.Contains(w.Body.String(), "private-token") || strings.Contains(w.Body.String(), "test-corp-secret") {
				t.Fatal("unsafe retry or error output", calls, w.Code, w.Body.String())
			}
		})
	}
}

func TestWeComConfigurationChangeStopsMessage(t *testing.T) {
	_, h, cookie, cfg := wecomFixture(t)
	calls := 0
	shareMock(t, func(r *http.Request) string {
		calls++
		if r.URL.Path != "/cgi-bin/gettoken" {
			t.Fatal("message sent after configuration changed")
		}
		cfg["enabled"] = false
		if w := request(t, h, "PUT", "/api/notification-service/wecom", map[string]any{"wecom": cfg}, cookie); w.Code != 200 {
			t.Fatal(w.Code)
		}
		return `{"errcode":0,"access_token":"private-token","expires_in":7200}`
	})
	w := request(t, h, "POST", "/api/notification-service/wecom/test", map[string]any{}, cookie)
	if w.Code != 502 || calls != 1 || !strings.Contains(w.Body.String(), "配置已变化") {
		t.Fatal("configuration guard", calls, w.Code)
	}
}

func TestWeComMalformedCiphertextAndDisabledCallback(t *testing.T) {
	_, h, cookie, cfg := wecomFixture(t)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	for _, encrypted := range []string{"", "invalid-base64", base64.StdEncoding.EncodeToString(make([]byte, 32)), base64.StdEncoding.EncodeToString(make([]byte, 17))} {
		w := request(t, h, "GET", "/notify/wecom?"+wecomQuery(cfg, encrypted, stamp)+"&echostr="+url.QueryEscape(encrypted), nil, nil)
		if w.Code != 403 {
			t.Fatal("malformed ciphertext accepted", w.Code)
		}
	}
	cfg["enabled"] = false
	if w := request(t, h, "PUT", "/api/notification-service/wecom", map[string]any{"wecom": cfg}, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	echo := wecomWire(t, cfg, "hello", "ww-test")
	w := request(t, h, "GET", "/notify/wecom?"+wecomQuery(cfg, echo, stamp)+"&echostr="+url.QueryEscape(echo), nil, nil)
	if w.Code != 404 {
		t.Fatal("disabled callback remained active")
	}
	if logModule("/api/notification-service/wecom/test") != "notifications" {
		t.Fatal("notification audit classification")
	}
}
