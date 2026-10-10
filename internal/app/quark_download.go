package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const quarkClientUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) quark-cloud-drive/2.5.20 Chrome/100.0.4896.160 Electron/18.3.5.4-b478491100 Safari/537.36 Channel/pckk_other_ch"

// Classify transport failures without exposing signed URLs, proxy credentials or cookies.
func upstreamConnectionError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var dns *net.DNSError
	var timeout net.Error
	var certificate *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var operation *net.OpError
	reason := "网络连接或重定向失败"
	switch {
	case errors.As(err, &dns):
		reason = "DNS 解析失败"
	case errors.As(err, &timeout) && timeout.Timeout():
		reason = "连接或响应超时"
	case errors.As(err, &certificate), errors.As(err, &authority):
		reason = "TLS 证书校验失败"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		reason = "上游提前断开连接"
	case errors.As(err, &operation):
		reason = "上游连接建立或传输失败"
	}
	return fmt.Errorf("上游连接失败（%s），请检查服务器网络", reason)
}

func (a *App) quarkJSON(ctx context.Context, s Storage, method, address string, body, out any) (Storage, error) {
	headers, err := requestJSONResponse(ctx, method, address, cloudHeaders(s), body, out)
	updated := mergeQuarkCookies(s, headers)
	if updated.Config["cookie"] == s.Config["cookie"] {
		return s, err
	}
	// Do not let a late response overwrite an edited or concurrently refreshed account.
	persistErr := a.store.update(func(st *State) error {
		for i := range st.Storages {
			current := &st.Storages[i]
			if current.ID == s.ID && current.Type == "quark" && storageRevision(*current) == storageRevision(s) {
				current.Config["cookie"] = updated.Config["cookie"]
				break
			}
		}
		return nil
	})
	if persistErr != nil {
		return updated, errors.New("夸克更新凭据保存失败，请检查配置目录写入权限")
	}
	return updated, err
}

func mergeQuarkCookies(s Storage, headers http.Header) Storage {
	incoming := map[string]string{}
	for _, cookie := range (&http.Response{Header: headers}).Cookies() {
		if (cookie.Name == "__puus" || cookie.Name == "__pus") && cookie.Value != "" && cookie.MaxAge >= 0 && (cookie.Expires.IsZero() || cookie.Expires.After(time.Now())) {
			incoming[cookie.Name] = cookie.Value
		}
	}
	if len(incoming) == 0 {
		return s
	}
	var parts []string
	for _, part := range strings.Split(s.Config["cookie"], ";") {
		name, _, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		if value, exists := incoming[name]; exists {
			parts = append(parts, name+"="+value)
			delete(incoming, name)
		} else {
			parts = append(parts, strings.TrimSpace(part))
		}
	}
	for _, name := range []string{"__pus", "__puus"} {
		if value, exists := incoming[name]; exists {
			parts = append(parts, name+"="+value)
		}
	}
	config := make(map[string]string, len(s.Config))
	for key, value := range s.Config {
		config[key] = value
	}
	s.Config = config
	s.Config["cookie"] = strings.Join(parts, "; ")
	return s
}
