package app

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

func TestShareTransportFailureClassificationAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"dns", &net.DNSError{Err: "private", Name: "private"}, "DNS"},
		{"certificate", x509.UnknownAuthorityError{}, "TLS"},
		{"deadline", context.DeadlineExceeded, "超时"},
		{"reset", syscall.ECONNRESET, "对端或代理关闭"},
		{"refused", syscall.ECONNREFUSED, "被拒绝"},
		{"unreachable", syscall.ENETUNREACH, "不可达"},
		{"cancelled", context.Canceled, "已取消"},
		{"other", errors.New("private token"), "连接失败"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := shareTransportFailure(context.Background(), &url.Error{Op: "Post", URL: "https://private.test/?token=private", Err: tc.err}, "移动分享读取")
			if !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
		})
	}
}
