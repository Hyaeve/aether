package app

import (
	"crypto/aes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMobileQRCodec(t *testing.T) {
	plain := []byte(`{"account":"13900000000","token":"private-token"}`)
	encoded, err := mobileQREncrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	quoted, _ := json.Marshal(encoded)
	for _, raw := range [][]byte{[]byte(encoded), quoted, plain} {
		decoded, err := mobileQRDecrypt(raw, false)
		if err != nil || string(decoded) != string(plain) {
			t.Fatal(err)
		}
	}
	block, _ := aes.NewCipher([]byte(mobileQRDataKey))
	padded := mobileQRPad(plain)
	encrypted := make([]byte, len(padded))
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(encrypted[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
	}
	for _, value := range []string{hex.EncodeToString(encrypted), base64.StdEncoding.EncodeToString(encrypted), string(plain)} {
		raw, _ := json.Marshal(value)
		decoded, err := mobileQRDecrypt(raw, true)
		if err != nil || string(decoded) != string(plain) {
			t.Fatal(err)
		}
	}
	for _, raw := range [][]byte{nil, []byte(`"bad"`), []byte(`"AA=="`)} {
		if _, err := mobileQRDecrypt(raw, false); err == nil {
			t.Fatal("accepted malformed ciphertext")
		}
	}
	if _, err := mobileQRUnpad([]byte{1, 2}); err == nil {
		t.Fatal("accepted invalid padding")
	}
}

func TestMobileQRAuthorizationProtocolAndScope(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	if w := request(t, h, "POST", "/api/authorization/mobile/start", nil, nil); w.Code != 401 {
		t.Fatal("unprotected")
	}
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "qr-owner", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/authorization/mobile/start", nil, cookie)
	var start struct {
		Token string `json:"token"`
		Image string `json:"image"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &start) != nil || !strings.HasPrefix(start.Image, "data:image/png;base64,") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	r, _ := http.NewRequest("POST", "/", nil)
	r.AddCookie(cookie)
	r.SetPathValue("provider", "mobile")
	session, err := a.openAuthorization(start.Token, r)
	if err != nil || len(session.Token) != 16 || len(session.Visitor) != 32 {
		t.Fatal(session, err)
	}
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	responses := []string{
		`{"code":"200059541"}`, `{"code":"200059548"}`, `{"code":"200059542"}`, `{"code":"200059549"}`, `{"code":"9101"}`,
		`{"code":"0","data":{"account":"13900000000","token":"private-token","userDomainId":"domain-123"}}`,
		`{"code":"0","data":{"account":"13900000000"}}`,
		`{"code":"unknown","message":"private-token","data":{"account":"13900000000","token":"private-token"}}`,
	}
	for i, response := range responses {
		calls := 0
		apiClient.Transport = casTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Method != "POST" || req.URL.String() != "https://user-njs.yun.139.com/user/thirdlogin" || req.Header.Get("Authorization") != "" || req.Header.Get("Mcloud-Version") != "7.17.9" || !strings.Contains(req.Header.Get("X-DeviceInfo"), session.Visitor) {
				t.Error("incorrect QR protocol")
			}
			body, _ := io.ReadAll(req.Body)
			plain, err := mobileQRDecrypt(body, false)
			if err != nil {
				t.Error(err)
			}
			var payload map[string]any
			json.Unmarshal(plain, &payload)
			signParts := strings.Split(req.Header.Get("Mcloud-Sign"), ",")
			if len(signParts) != 3 || signParts[2] != mobileSign(string(plain), signParts[0], signParts[1]) || payload["dycpwd"] != session.Token || payload["clienttype"] != float64(670) {
				t.Error("incorrect QR signature or payload")
			}
			ciphertext, _ := mobileQREncrypt([]byte(response))
			raw, _ := json.Marshal(ciphertext)
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: req}, nil
		})
		w = request(t, h, "POST", "/api/authorization/mobile/poll", map[string]string{"token": start.Token}, cookie)
		if calls != 1 {
			t.Fatal(calls)
		}
		if i >= 6 {
			if w.Code != 502 || strings.Contains(w.Body.String(), "private-token") {
				t.Fatal(w.Code, w.Body.String())
			}
			continue
		}
		want := []string{"waiting", "waiting", "expired", "cancelled", "error", "success"}[i]
		var result map[string]string
		json.Unmarshal(w.Body.Bytes(), &result)
		if w.Code != 200 || result["status"] != want || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
		if want == "success" {
			if result["userDomainId"] != "domain-123" {
				t.Fatal("missing quota account domain")
			}
			_, auth, err := mobileAccount(Storage{Config: map[string]string{"authorization": result["authorization"]}})
			if err != nil || auth != base64.StdEncoding.EncodeToString([]byte("pc:13900000000:private-token")) {
				t.Fatal("invalid authorization")
			}
		}
	}
	for _, bad := range []authorizationSession{{Provider: "mobile", Token: session.Token, Visitor: session.Visitor, Owner: session.Owner, Expires: time.Now().Add(-time.Minute).Unix()}, {Provider: "quark", Owner: session.Owner, Expires: time.Now().Add(time.Minute).Unix()}} {
		token, _ := a.sealAuthorization(bad)
		if w = request(t, h, "POST", "/api/authorization/mobile/poll", map[string]string{"token": token}, cookie); w.Code != 400 {
			t.Fatal("accepted invalid session", w.Code)
		}
	}
	other := &http.Cookie{Name: cookie.Name, Value: "other-session"}
	foreign, _ := http.NewRequest("POST", "/", nil)
	foreign.AddCookie(other)
	foreign.SetPathValue("provider", "mobile")
	if _, err := a.openAuthorization(start.Token, foreign); err == nil {
		t.Fatal("cross-session authorization accepted")
	}
	calls := 0
	apiClient.Transport = casTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://attacker.example/"}}, Body: io.NopCloser(strings.NewReader("private-token")), Request: req}, nil
	})
	w = request(t, h, "POST", "/api/authorization/mobile/poll", map[string]string{"token": start.Token}, cookie)
	if calls != 1 || w.Code != 502 || strings.Contains(w.Body.String(), "private-token") || strings.Contains(w.Body.String(), "attacker") {
		t.Fatal("redirect or upstream secret escaped", calls, w.Code, w.Body.String())
	}
}
