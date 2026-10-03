package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// Version may be set by the image build with -ldflags.
var Version = "0.1.0"

func (a *App) version(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, 200, map[string]string{"version": Version})
}

func checkRelease(client *http.Client, request *http.Request) (map[string]any, error) {
	result := map[string]any{"current": Version, "latest": "", "available": false}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Aether/"+Version)
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("无法连接 GitHub，请稍后重试")
	}
	defer response.Body.Close()
	if response.StatusCode == 404 {
		result["message"] = "仓库尚未发布正式版本"
		return result, nil
	}
	if response.StatusCode == 403 || response.StatusCode == 429 {
		return nil, errors.New("GitHub 请求频率受限，请稍后重试")
	}
	if response.StatusCode != 200 {
		return nil, errors.New("GitHub 更新检查失败")
	}
	var release struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&release); err != nil || release.Tag == "" {
		return nil, errors.New("GitHub 版本响应无效")
	}
	tag := release.Tag
	if tag[0] != 'v' {
		tag = "v" + tag
	}
	current := "v" + strings.TrimPrefix(Version, "v")
	result["latest"] = release.Tag
	if !semver.IsValid(tag) || !semver.IsValid(current) {
		result["message"] = "已获取发布版本，请在 GitHub 核对版本信息"
	} else if semver.Compare(tag, current) > 0 {
		result["available"] = true
		result["message"] = "发现新版本，请更新容器镜像"
	} else {
		result["message"] = "当前版本无需更新"
	}
	return result, nil
}

func (a *App) checkVersion(w http.ResponseWriter, r *http.Request) {
	request, err := http.NewRequestWithContext(r.Context(), "GET", "https://api.github.com/repos/Hyaeve/aether/releases/latest", nil)
	if err != nil {
		fail(w, 500, err)
		return
	}
	result, err := checkRelease(&http.Client{Timeout: 12 * time.Second}, request)
	if err != nil {
		fail(w, 502, err)
		return
	}
	jsonResponse(w, 200, result)
}
