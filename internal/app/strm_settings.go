package app

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type strmSettings struct {
	Address string `json:"address"`
	Format  string `json:"format"`
}

func validSTRMSettings(in strmSettings) error {
	u, err := url.Parse(in.Address)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("STRM IP 地址须为完整 HTTP(S) 地址及端口，不含路径或参数")
	}
	if in.Format != "pickcode_name" && in.Format != "pickcode" {
		return errors.New("115 STRM 格式无效")
	}
	return nil
}

func (a *App) currentSTRMSettings() strmSettings {
	s := a.store.snapshotWithLogLimit(0).Settings
	in := strmSettings{Address: s.STRMURL, Format: s.STRM115Format}
	if in.Address == "" {
		in.Address = s.PublicURL
	}
	if in.Format == "" {
		in.Format = "pickcode_name"
	}
	return in
}

func (a *App) generatedStreamURL(claim streamClaim) string {
	original := a.signedStreamURL(claim)
	u, err := url.Parse(original)
	if err != nil {
		return original
	}
	base := strings.TrimRight(a.currentSTRMSettings().Address, "/")
	return base + u.RequestURI()
}

func (a *App) strmSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		jsonResponse(w, 200, a.currentSTRMSettings())
		return
	}
	if r.Method != "PUT" {
		w.WriteHeader(405)
		return
	}
	var in strmSettings
	if !decode(w, r, &in) {
		return
	}
	in.Address = strings.TrimRight(strings.TrimSpace(in.Address), "/")
	if err := validSTRMSettings(in); err != nil {
		fail(w, 400, err)
		return
	}
	if err := a.store.update(func(st *State) error {
		st.Settings.STRMURL, st.Settings.STRM115Format = in.Address, in.Format
		return nil
	}); err != nil {
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 200, in)
}
