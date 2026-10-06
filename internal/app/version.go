package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// Version may be set by the image build with -ldflags.
var Version = "0.1.8"
var Revision = ""

func (a *App) version(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, 200, map[string]string{"version": Version, "revision": Revision})
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
	a.versionMu.Lock()
	defer a.versionMu.Unlock()
	if time.Now().Before(a.versionExpiry) {
		jsonResponse(w, 200, a.versionResult)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := checkImage(ctx, &http.Client{Timeout: 12 * time.Second})
	if err != nil {
		fail(w, 502, err)
		return
	}
	a.versionResult, a.versionExpiry = result, time.Now().Add(5*time.Minute)
	jsonResponse(w, 200, result)
}

func checkImage(ctx context.Context, client *http.Client) (map[string]any, error) {
	get := func(address, token string, out any) error {
		req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "Aether/"+Version)
		req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := client.Do(req)
		if err != nil {
			return errors.New("无法连接 GHCR 镜像仓库")
		}
		defer res.Body.Close()
		if res.StatusCode == 401 || res.StatusCode == 403 {
			return errors.New("GHCR 镜像不可匿名读取，请确认镜像已公开")
		}
		if res.StatusCode == 404 {
			return errors.New("GHCR 尚未发布 latest 镜像")
		}
		if res.StatusCode == 429 {
			return errors.New("GHCR 镜像仓库请求受限，请稍后重试")
		}
		if res.StatusCode != 200 {
			return fmt.Errorf("GHCR 镜像检查 HTTP %d", res.StatusCode)
		}
		if json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(out) != nil {
			return errors.New("GHCR 镜像元数据无效")
		}
		return nil
	}
	var auth struct {
		Token string `json:"token"`
	}
	if err := get("https://ghcr.io/token?service=ghcr.io&scope=repository:hyaeve/aether:pull", "", &auth); err != nil {
		return nil, err
	}
	if auth.Token == "" {
		return nil, errors.New("GHCR 未返回读取令牌")
	}
	type descriptor struct {
		Digest   string
		Platform struct{ OS, Architecture string }
	}
	type manifest struct {
		Manifests []descriptor
		Config    descriptor
	}
	const base = "https://ghcr.io/v2/hyaeve/aether/"
	var latest manifest
	if err := get(base+"manifests/latest", auth.Token, &latest); err != nil {
		return nil, err
	}
	validDigest := regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	if len(latest.Manifests) > 0 {
		digest := ""
		for _, m := range latest.Manifests {
			if m.Platform.OS == "linux" && m.Platform.Architecture == "amd64" {
				digest = m.Digest
				break
			}
		}
		if !validDigest.MatchString(digest) {
			return nil, errors.New("镜像缺少 linux/amd64 构建")
		}
		latest = manifest{}
		if err := get(base+"manifests/"+digest, auth.Token, &latest); err != nil {
			return nil, err
		}
	}
	if !validDigest.MatchString(latest.Config.Digest) {
		return nil, errors.New("镜像配置摘要无效")
	}
	var config struct {
		Config struct{ Labels map[string]string }
	}
	if err := get(base+"blobs/"+latest.Config.Digest, auth.Token, &config); err != nil {
		return nil, err
	}
	labels := config.Config.Labels
	remoteRevision, remoteVersion := labels["org.opencontainers.image.revision"], labels["org.opencontainers.image.version"]
	result := map[string]any{"current": Version, "latest": remoteVersion, "revision": remoteRevision, "available": false, "source": "ghcr.io/hyaeve/aether:latest"}
	switch {
	case Revision != "" && remoteRevision != "":
		result["available"] = Revision != remoteRevision
		if Revision == remoteRevision {
			result["message"] = "当前镜像与 GHCR latest 一致"
		} else {
			result["message"] = "GHCR latest 镜像已变化，可拉取更新"
		}
	case semver.IsValid("v"+strings.TrimPrefix(remoteVersion, "v")) && semver.IsValid("v"+strings.TrimPrefix(Version, "v")) && semver.Compare("v"+strings.TrimPrefix(remoteVersion, "v"), "v"+strings.TrimPrefix(Version, "v")) > 0:
		result["available"], result["message"] = true, "发现新版本，请更新容器镜像"
	default:
		result["message"] = "已读取 GHCR 镜像；当前构建缺少可比对修订号，无法确认是否需要更新"
	}
	return result, nil
}
