package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

func quarkQRImage(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if u, err := url.Parse(raw); err == nil && u.Scheme == "https" && u.Host != "" {
		png, err := qrcode.Encode(raw, qrcode.Medium, 256)
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), err
	}
	if strings.HasPrefix(raw, "data:") {
		_, encoded, ok := strings.Cut(raw, ",")
		if !ok {
			return "", errors.New("二维码图片无效")
		}
		raw = encoded
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(data) > 2<<20 {
		return "", errors.New("二维码图片无效")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width > 2048 || cfg.Height > 2048 || (format != "png" && format != "jpeg") {
		return "", errors.New("二维码图片无效")
	}
	return "data:image/" + format + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
