package app

import (
	"net"
	"net/netip"
	"os"
	"strings"
)

// Trust only this host by default, not every address in its private subnet.
func linkTrustedProxies() []string {
	result := []string{"127.0.0.1/32", "::1/128"}
	addresses, _ := net.InterfaceAddrs()
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil {
			addr := prefix.Addr().Unmap()
			result = append(result, netip.PrefixFrom(addr, addr.BitLen()).String())
		}
	}
	for _, value := range strings.Split(os.Getenv("AETHER_TRUSTED_PROXIES"), ",") {
		if prefix, err := netip.ParsePrefix(strings.TrimSpace(value)); err == nil && prefix.Bits() > 0 {
			result = append(result, prefix.String())
		}
	}
	return result
}
