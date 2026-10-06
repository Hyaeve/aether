package app

import (
	"context"
	"errors"
	"net/http"
	"time"
)

func (a *App) testMediaLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	for _, link := range a.store.snapshot().Links {
		if link.ID != r.PathValue("id") {
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, "GET", link.Address, nil)
		if err != nil {
			fail(w, 400, err)
			return
		}
		client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := client.Do(req)
		if err != nil {
			fail(w, 502, errors.New("无法连接媒体服务"))
			return
		}
		res.Body.Close()
		if res.StatusCode >= 400 {
			fail(w, 502, errors.New("媒体服务返回错误状态"))
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	fail(w, 404, errors.New("以链不存在"))
}
