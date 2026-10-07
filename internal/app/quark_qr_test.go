package app

import (
	"encoding/base64"
	"strings"
	"testing"

	qrcode "github.com/skip2/go-qrcode"
)

func TestQuarkQRPreservesProviderImage(t *testing.T) {
	png, err := qrcode.Encode("https://quark.example/authorization", qrcode.Medium, 460)
	if err != nil {
		t.Fatal(err)
	}
	raw := base64.StdEncoding.EncodeToString(png)
	for _, input := range []string{raw, "data:image/png;base64," + raw} {
		got, err := quarkQRImage(input)
		if err != nil || got != "data:image/png;base64,"+raw {
			t.Fatal("provider image was re-encoded as QR text", err)
		}
	}
	if image, err := quarkQRImage("https://quark.example/authorization"); err != nil || !strings.HasPrefix(image, "data:image/png;base64,") {
		t.Fatal(image, err)
	}
	for _, bad := range []string{"not-an-image", "data:image/svg+xml;base64,PHN2Zz4=", "javascript:alert(1)"} {
		if _, err := quarkQRImage(bad); err == nil {
			t.Fatal("invalid image accepted")
		}
	}
}
