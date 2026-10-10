package app

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// Compare origins, not raw Host strings: default ports and DNS case are equivalent.
func normalizedOrigin(raw string, allowPath bool) string {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || (!allowPath && u.Path != "") {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || u.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if strings.ContainsAny(host, " ,/\\\t\r\n") {
		return ""
	}
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return ""
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(n))
}

func trustedOriginPeer(remote string, trusted []string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, raw := range trusted {
		prefix, err := netip.ParsePrefix(raw)
		if err == nil && prefix.Contains(addr.Unmap()) {
			return true
		}
	}
	return false
}

func (a *App) acceptRequestOrigin(r *http.Request, trusted []string) bool {
	if len(r.Header.Values("Origin")) != 1 {
		return false
	}
	origin := normalizedOrigin(r.Header.Get("Origin"), false)
	if origin == "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if origin == normalizedOrigin(scheme+"://"+r.Host, false) {
		return true
	}
	// A configured public address is an explicit allow-list, not a client header.
	a.store.mu.RLock()
	publicURL := a.store.state.Settings.PublicURL
	a.store.mu.RUnlock()
	if origin == normalizedOrigin(publicURL, true) {
		return true
	}
	if !trustedOriginPeer(r.RemoteAddr, trusted) {
		return false
	}
	hosts, protos := r.Header.Values("X-Forwarded-Host"), r.Header.Values("X-Forwarded-Proto")
	if len(hosts) > 1 || len(protos) != 1 {
		return false
	}
	host, proto := r.Host, strings.TrimSpace(protos[0])
	if len(hosts) == 1 {
		host = strings.TrimSpace(hosts[0])
	}
	if strings.ContainsAny(host+proto, ", \t\r\n") {
		return false
	}
	return origin == normalizedOrigin(proto+"://"+host, false)
}
