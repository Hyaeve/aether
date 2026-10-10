<p align="center"><img src="web/public/aether.svg" alt="Aether" width="88" /></p>

# Aether 以太

自托管的云盘与本地存储工作空间，使用 Go + Vue 构建。

当前版本：**v0.4.6** · [版本说明](docs/releases/v0.4.6.md)

## 功能概览

| 模块 | 主要功能 |
| --- | --- |
| 仪表盘 | 实时传输速率、缓存命中统计、存储快捷访问和最近活动 |
| 存储管理 | 接入 115、夸克、移动云盘、天翼云盘、OpenList、WebDAV 和本地目录，管理凭据与连接状态 |
| 文件管理 | 浏览、搜索、收藏、上传下载、复制移动、批量重命名、分享转存、离线下载与媒体预览 |
| 文件服务 | WebDAV 用户及目录授权、原生 go-fuse 本地挂载与磁盘读缓存 |
| 任务管理 | STRM、CAS、ED2K、目录缓存及联动任务；支持手动和定时执行 |
| STRM 刮削 | 按作品目录识别电影和剧集，匹配 TMDB、生成元数据和海报 |
| 以太链接 | AudioBookShelf、Emby、飞牛影视反代、302 直链、中继、播放流水和直链缓存 |
| 传输中心 | 查看传输进度，管理目录备份规则、文件筛选和备份计划 |
| 辅助工具 | TMDB、AI 接口、代理、Emby 通知、夸克 STRM 接管、115 同播复制、STRM 内容替换与加密配置备份 |
| 系统设置 | 账号安全、主题、日志保留及系统日志查询 |

不同存储支持的操作有所区别；云盘权限和风控、浏览器媒体格式支持以实际服务为准。AI 插件提供配置及识别接口，尚未接入自动刮削流程。

## Docker 部署

Linux Docker Compose 配置如下，也可使用仓库中的 [compose.yaml](compose.yaml)：

```yaml
services:
  aetherlink:
    image: ghcr.io/hyaeve/aether:latest
    container_name: Aether
    network_mode: host
    volumes:
      - ./config:/config
      - ./data:/data
      - ./fuse_read_cache:/fuse_read_cache
    environment:
      - TZ=Asia/Shanghai
      - AETHER_PORT=15151
      - AETHER_FUSE_CACHE_DIR=/fuse_read_cache
    devices:
      - /dev/fuse:/dev/fuse
    pid: "host"
    privileged: true
    restart: unless-stopped
```

```bash
docker compose up -d
```

打开 `http://服务器IP:15151`，首次访问创建管理员账号。管理页面、播放代理与 WebDAV 共用此端口；以太链接使用各自配置的反代端口。

- 镜像：`ghcr.io/hyaeve/aether:latest`，目前仅构建 x86-64。
- 持久化：`/config` 保存配置和密钥，`/data` 保存日志、索引与播放缓存，FUSE 读缓存单独映射到 `/fuse_read_cache`，可放在较快的磁盘。
- 本地文件及媒体库需要映射到容器；本地挂载需要 `/dev/fuse` 和相应容器权限。不需要挂载时可移除 Compose 中的特权及 FUSE 配置。
- WebDAV 地址为 `/dav/`。独立 WebDAV 用户只读，管理员可按存储能力读写。

`AETHER_FUSE_CACHE_DIR` 指定 FUSE 缓存目录，镜像默认为 `/fuse_read_cache`；原生运行未设置时仍使用数据目录下的 `fuse_read_cache`。升级时将旧映射的容器路径改为 `/fuse_read_cache`，无需迁移可重建的缓存块。

## 配置目录

存储与设置在 `/config/storage`，任务在 `/config/task`，以太链接在 `/config/link`，辅助工具在 `/config/tool`，WebDAV 与挂载在 `/config/file`，备份规则在 `/config/transfer`。

模块配置采用固定名称 JSON，CK、Token、上游密码和 API Key 字段加密；登录密码为加盐哈希。请使用内置配置备份，或停机备份整个 `/config` 及主密钥。日志在 `/data/log`，播放缓存在 `/data/cache/link`。迁移、外置密钥和剪贴板监听见[配置说明](docs/configuration.md)。

传输中心的备份规则支持多源多目标、同名跳过或覆盖、名称/扩展名/正则/大小筛选及 Cron/间隔扫描，留空计划可手动执行。不自动删除源文件或目标多余文件。覆盖、全量重置及刮削元数据清理前请先备份。

## 开发

```bash
go test ./...
cd web
npm ci
npm run build
npm run test:e2e
```

第三方组件及参考项目见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
