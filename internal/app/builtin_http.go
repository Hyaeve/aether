package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const builtinDownloadLimit int64 = 100 << 30

func downloadBuiltinHTTP(ctx context.Context, output, address string) error {
	if err := validateBuiltinURL(address); err != nil {
		return err
	}
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
		DisableCompression: true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("下载重定向过多")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("不支持的重定向协议")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			req.Header.Del("Authorization")
			req.Header.Del("Cookie")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return errors.New("下载链接无效")
	}
	req.Header.Set("User-Agent", "Aether/"+Version)
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("下载连接失败")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("下载返回 HTTP %d", res.StatusCode)
	}
	if res.ContentLength > builtinDownloadLimit {
		return errors.New("下载超过 100 GiB 上限")
	}
	name := path.Base(res.Request.URL.Path)
	if _, params, err := mime.ParseMediaType(res.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		name = params["filename"]
	}
	if !safeName(name) || strings.ContainsAny(name, "\x00\r\n") {
		name = "download.bin"
	}
	if _, err := url.PathUnescape(name); err != nil {
		name = "download.bin"
	}
	temp, err := os.CreateTemp(output, ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	n, copyErr := io.Copy(temp, io.LimitReader(&cancelReader{ctx, res.Body}, builtinDownloadLimit+1))
	if copyErr == nil && (n > builtinDownloadLimit || (res.ContentLength >= 0 && n != res.ContentLength)) {
		copyErr = errors.New("下载大小不符或超出上限")
	}
	if copyErr == nil {
		copyErr = temp.Sync()
	}
	closeErr := temp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	target := filepath.Join(output, name)
	if err := os.Link(temp.Name(), target); err != nil {
		return fmt.Errorf("发布下载文件失败：%w", err)
	}
	return nil
}
