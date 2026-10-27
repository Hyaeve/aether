package app

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type strmSettings struct {
	Address       string `json:"address"`
	Format        string `json:"format"`
	OverwriteMode string `json:"overwriteMode"`
}

func validSTRMOverwriteMode(mode string) bool {
	return mode == "" || mode == "skip" || mode == "overwrite"
}

func validSTRMSettings(in strmSettings) error {
	u, err := url.Parse(in.Address)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("STRM IP 地址须为完整 HTTP(S) 地址及端口，不含路径或参数")
	}
	if in.Format != "pickcode_name" && in.Format != "pickcode" {
		return errors.New("115 STRM 格式无效")
	}
	if !validSTRMOverwriteMode(in.OverwriteMode) {
		return errors.New("STRM覆盖模式无效")
	}
	return nil
}

func (a *App) currentSTRMSettings() strmSettings {
	s := a.store.snapshotWithLogLimit(0).Settings
	in := strmSettings{Address: s.STRMURL, Format: s.STRM115Format, OverwriteMode: s.STRMOverwriteMode}
	if in.Address == "" {
		in.Address = s.PublicURL
	}
	if in.Format == "" {
		in.Format = "pickcode_name"
	}
	if in.OverwriteMode == "" {
		in.OverwriteMode = "skip"
	}
	return in
}

func (a *App) generatedStreamURL(claim streamClaim) string {
	var payload any = claim
	if claim.CAS != nil && claim.Storage == "" && claim.ED2K == nil {
		// Portable CAS links carry features only; the pool owns playback policy.
		type features struct {
			Name      string `json:"name"`
			Size      int64  `json:"size"`
			SHA256    string `json:"sha256"`
			MD5       string `json:"md5"`
			SliceMD5  string `json:"slice_md5"`
			SliceSize int64  `json:"slice_size"`
		}
		info := claim.CAS
		payload = struct {
			Redirect bool     `json:"redirect,omitempty"`
			CAS      features `json:"cas"`
		}{claim.Redirect, features{info.Name, info.Size, info.SHA256, info.MD5, info.SliceMD5, info.SliceSize}}
	}
	original := a.signedStreamPayload(payload)
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
		if in.OverwriteMode == "" {
			in.OverwriteMode = st.Settings.STRMOverwriteMode
			if in.OverwriteMode == "" {
				in.OverwriteMode = "skip"
			}
		}
		st.Settings.STRMURL, st.Settings.STRM115Format = in.Address, in.Format
		st.Settings.STRMOverwriteMode = in.OverwriteMode
		return nil
	}); err != nil {
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 200, in)
}
