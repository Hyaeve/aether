package app

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
)

// Classify transport failures without exposing URLs, credentials or raw upstream errors.
func shareTransportFailure(ctx context.Context, err error, stage string) error {
	reason := "连接失败，请检查服务器网络或代理"
	var dns *net.DNSError
	var network net.Error
	var authority x509.UnknownAuthorityError
	var certificate x509.CertificateInvalidError
	var hostname x509.HostnameError
	switch {
	case errors.Is(ctx.Err(), context.Canceled), errors.Is(err, context.Canceled):
		reason = "请求已取消"
	case errors.As(err, &dns):
		reason = "DNS解析失败，请检查服务器DNS和网络"
	case errors.As(err, &authority), errors.As(err, &certificate), errors.As(err, &hostname):
		reason = "TLS证书校验失败，请检查服务器时间、证书和代理"
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded), errors.As(err, &network) && network.Timeout():
		reason = "连接或读取超时，请检查服务器网络"
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		reason = "连接被对端或代理关闭，请检查服务器网络出口"
	case errors.Is(err, syscall.ECONNREFUSED):
		reason = "连接被拒绝，请检查服务器网络或代理"
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
		reason = "网络不可达，请检查服务器路由或代理"
	}
	return fmt.Errorf("%s：%s", stage, reason)
}
