package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestGeneratedCASPayloadFeaturesOnly(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.Settings.STRMURL = "http://strm.example:15151"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, info := range []CASInfo{
		{Provider: "mobile", Name: "film.mkv", Size: 12, SHA256: strings.Repeat("a", 64)},
		{Provider: "tianyi", Name: "film.mkv", Size: 12, MD5: strings.Repeat("b", 32), SliceMD5: strings.Repeat("c", 32), SliceSize: 10 << 20},
	} {
		t.Run(info.Provider, func(t *testing.T) {
			info.PlaybackURL, info.RetentionHours = "http://old.example/file", 99
			original := info
			link := a.generatedStreamURL(streamClaim{CAS: &info, Redirect: true})
			got := generatedCASClaim(t, []byte(link))
			if got.Name != info.Name || got.Size != info.Size || got.SHA256 != info.SHA256 || got.MD5 != info.MD5 || got.SliceMD5 != info.SliceMD5 || got.SliceSize != info.SliceSize {
				t.Fatal("features changed", got)
			}
			if info != original {
				t.Fatal("serialization mutated source", info)
			}
			u, err := url.Parse(link)
			if err != nil || u.Host != "strm.example:15151" {
				t.Fatal("custom STRM address lost", link, err)
			}
			token := strings.TrimPrefix(u.Path, "/stream/")
			mac := hmac.New(sha256.New, []byte(a.store.snapshot().SignKey))
			mac.Write([]byte(token))
			signature, err := base64.RawURLEncoding.DecodeString(u.Query().Get("sign"))
			if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
				t.Fatal("invalid compact payload signature", err)
			}
			raw, _ := base64.RawURLEncoding.DecodeString(token)
			if !(strings.Index(string(raw), `"sha256"`) < strings.Index(string(raw), `"md5"`) && strings.Index(string(raw), `"md5"`) < strings.Index(string(raw), `"slice_md5"`)) {
				t.Fatal("unexpected hash order", string(raw))
			}
			standalone, err := json.Marshal(info)
			if err != nil || !strings.Contains(string(standalone), `"provider":"`+info.Provider+`"`) {
				t.Fatal("standalone CAS schema changed", string(standalone), err)
			}
		})
	}
}

func TestGeneratedNonportableStreamPayloadUnchanged(t *testing.T) {
	a := testApp(t)
	info := CASInfo{Provider: "mobile", Name: "film.mkv", Size: 12, SHA256: strings.Repeat("a", 64)}
	for _, claim := range []streamClaim{
		{Storage: "account", File: "file", Pick: "pick"},
		{Storage: "account", File: "file", CAS: &info, TaskID: "old-task", RetentionHours: 24, Redirect: true},
		{ED2K: &ED2KInfo{}, Redirect: true},
	} {
		legacy, err := url.Parse(a.signedStreamURL(claim))
		if err != nil {
			t.Fatal(err)
		}
		generated, err := url.Parse(a.generatedStreamURL(claim))
		if err != nil || generated.RequestURI() != legacy.RequestURI() {
			t.Fatal("nonportable payload changed", claim, generated, err)
		}
	}
}
