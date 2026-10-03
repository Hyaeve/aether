# Aether 以太

Go + Vue 的自托管存储工作空间。主服务、STRM 播放代理、只读 WebDAV 共用 **15151** 端口。

## 当前实现

- 首次启动创建管理员，无默认密码；bcrypt 密码哈希、HttpOnly 会话、同源写请求校验、登录限流。
- 存储池新增、编辑、删除、启停、连接测试。同类型可重复添加，名称唯一。凭据 AES-GCM 加密落盘。
- 本机目录、WebDAV、OpenList 浏览与下载；115 Open、夸克直接接口；移动支持原生新版个人云和 OpenList 网关，天翼支持原生个人云账户登录，不再使用 OpenList 网关。
- STRM 全量/增量、五字段 Cron、手动执行/停止、排除目录/文件/后缀、API 请求间隔、输出目录约束。
- 缓存扫描任务：深度、执行间隔、缓存有效期。共享 TTL/LRU 缓存、条目与估算内存限制、磁盘快照及重启恢复。
- 聚合只读 WebDAV：`/dav/<storage-id>/`，支持 PROPFIND、GET、HEAD，复用管理员 Basic Auth。
- 中文界面、深色侧栏、固定顶栏、三态主题、移动端布局、登录轨道视觉、日志与账户设置。
- GitHub Actions 测试及 `linux/amd64` 镜像构建，推送 GHCR。
- LitePan 风格单层导航、页面内任务栏目、插件卡片；单按钮三态主题、带未读数量的最近任务通知、系统设置内的关于以太及 GitHub 正式版本检查。
- 存储授权：夸克二维码获取、轮询及 Cookie 回填；115 Open 通过用户提供的可信 HTTPS OAuth 代理启动授权并回填令牌。代理需兼容 LitePan OAuth 协议且允许 Aether 客户端，不默认借用 LitePan 的授权服务器。

通知展示每个任务最近一次结束状态（最多 30 项），已读标记按账户保存在当前浏览器，不是完整执行历史。更新检查只查询 `Hyaeve/aether` 的最新正式 Release，不自动下载或重启；无正式 Release、网络失败和限流分别提示。镜像的版本标签通过构建参数写入后端。

授权接口与回填已通过模拟测试，真实网盘扫码和第三方 OAuth 代理仍待账号联调。存储删除模式默认“移到回收站”，用于原生移动、天翼 CAS 临时文件清理；其他文件管理仍为只读。目录整理、STRM 刮削和辅助插件尚未提供执行引擎，界面明确标注待实现。

### CAS 任务

参考 139Strm 的特征文件播放流程，任务管理中的 CAS 栏目已接入执行引擎：

1. 添加移动云盘，选择“原生新版个人云”，填写 Authorization（支持 Basic 前缀）；或添加天翼云盘，填写账号、密码，个人云根目录 ID 默认 `-11`。
2. 在 CAS 任务选择存储池、源目录、本地生成目录，配置全量/增量、Cron、API 间隔和排除规则。
3. 任务读取 `.cas` 的 Base64 JSON，校验视频名称、大小及哈希（移动需要 SHA256，天翼需要 MD5），生成 `电影.mkv.strm`；不会在扫描时还原整部媒体。未匹配文件不会创建输出目录。
4. 播放时调用对应网盘的原生秒传接口，在网盘根目录的 `/Aether` 内创建临时文件，通过 Aether 同端口代理播放，支持 Range/HEAD。并发请求与重启后的重播复用已记录文件。
5. 创建或编辑 CAS 任务可设置“还原文件保留时间”，默认 12 小时，支持 1–8760 小时；从最后一次播放结束后计时，活跃播放期间不清理。同一还原文件被多任务复用时取已应用的最长保留时间。新签名链接关联任务，后续播放读取任务当前设置；缩短设置不提前缩短已存在文件的保留时间，重新还原时应用。旧链接、旧任务及缺少保留字段的记录按 12 小时处理。
6. 临时文件记录随 `/config/state.enc` 加密保存，也可在 CAS 临时文件窗口触发过期清理。默认移到回收站，选择永久删除时才彻底删除。只删除以太记录的文件 ID，不扫描删除 `/Aether` 内的其他文件。旧目录中的已记录文件仍按 ID 复用、清理，不迁移或删除整个旧目录；新还原文件才写入 `/Aether`。

支持移动新版个人云的 SHA256 CAS、天翼个人云的 MD5 CAS；不支持家庭/群组云、旧版移动个人云、仅有 SHA1 的 CAS 或 torrent CAS 扩展。两种网盘的哈希不能互换。移动 Authorization 暂不自动刷新，失效后可更新同账号令牌。秒传依赖云端源数据及账号权益，不保证所有文件可还原，也不承诺不占配额。没有媒体上传兜底。签名 STRM 内含名称、大小和哈希，不含账户令牌；全量任务重写匹配文件但不清除本地孤儿文件。临时链接失效时会标记记录，下一次播放重试还原；崩溃恰发生在云端创建与本地记录落盘之间仍可能留下未记录残留。

天翼账号密码沿用 AES-GCM 加密配置存储，向官方 HTTPS 登录接口提交时使用官方公钥 RSA PKCS#1 v1.5 加密。会话仅缓存在内存，重启或一小时后重新登录；检测到失效时清除会话，下次请求重新登录。暂不支持图形验证码及设备短信二次验证，遇到风控会明确报错，需要在官方客户端处理后重试，不能保证由此解除风控。旧天翼网关池不会继续走网关：请编辑存储池填写天翼账户、重新选择数字目录 ID，并更新关联任务的源目录。

原生协议、CAS 扫描/播放/清理通过模拟云盘测试；尚未使用真实移动、天翼账号验证。协议参考及许可见 `THIRD_PARTY_NOTICES.md`。

## 明确的边界

这是一版可运行的基础实现，**不是完整需求的全部实现**。

- 115 / 夸克未以真实账户完成端到端验证。115 当前使用手工配置访问令牌；刷新令牌可保存但尚未实现自动刷新或扫码授权。
- 移动的网关模式填写 OpenList 地址、API Token、根路径；原生移动使用 Authorization，天翼直接填写账号密码。真实账号仍待联调。
- 文件管理目前只读浏览、下载和复制播放链接，不含上传、移动、删除或重命名。上传统计因此为 0。
- 备份、FUSE 挂载管理、媒体反代（Audiobookshelf / Emby / 飞牛影视）、整理、刮削、辅助工具均明确标记待开发。
- Compose 已按需求配置特权及 `/dev/fuse`，**不代表 FUSE 挂载引擎已实现**。
- 元数据内存上限按 JSON 载荷及固定条目开销估算，不是 Go 进程 RSS 的硬限制。
- WebDAV 根目录使用存储池 ID。云端目录逐级解析；可能产生额外请求。当前不支持 WebDAV 写入和锁操作。
- 本版本配置与任务以加密 JSON 原子文件保存，适合单实例；不支持多实例共用数据目录。

## 本地运行

需要 Go 1.26+、Node.js 22+。

```powershell
cd web
npm ci
npm run build
cd ..
go run ./cmd/aether
```

访问 `http://localhost:15151`，创建管理员（密码非空即可，不设 12 字符下限；受 bcrypt 限制，最多 72 字节）。先在系统设置填写媒体服务器能访问的地址，例如 `http://192.168.1.10:15151`，再生成 STRM。

前端开发：在另一个终端执行 `cd web` 后运行 `npm run dev`，Vite 代理 API 至 15151。

环境变量：

| 名称                 | 默认值         | 用途                        |
| ------------------ | ----------- | ------------------------- |
| `AETHER_PORT`      | `15151`     | 管理后台、STRM、WebDAV 共用监听端口 |
| `AETHER_ADDR`      | 未设置       | 高级监听地址，设置后优先于 `AETHER_PORT` |
| `AETHER_WEB_DIR`   | `web/dist`  | 前端构建目录                    |
| `TZ`               | 由环境决定       | 定时任务时区，容器默认 Asia/Shanghai |

目录不再通过 `AETHER_DATA_DIR` / `AETHER_STRM_ROOT` 环境变量配置。容器固定使用 `/config` 保存配置、`/data` 保存运行数据；本地开发对应工作目录下的 `config/`、`data/`。独立测试可使用命令行参数 `-config-dir`、`-data-dir` 指定隔离目录。

任务目标目录留空时默认写入 `/data/strm`（本地开发为 `data/strm`）。也可填写相对路径（例如 `movies`），或 STRM 根目录内的绝对路径。源文件名保留原后缀，例如 `movie.mkv.strm`，避免同名不同格式文件覆盖。全量重写匹配文件但**不会删除已不存在源文件对应的旧 STRM**；增量只创建不存在的 STRM。

缓存有效期优先级：任务 > 存储池 > 全局；0 表示继承。缓存任务默认 60 分钟执行一次，有效期仍按此优先级决定。扫描层级 0 为全部（最多 128 层），1 仅当前目录。API 间隔默认 200ms，安全下限 200ms；同存储池一次只运行一个任务。Cron 使用五字段，本地运行跟随进程时区。

## Docker

在支持 `/dev/fuse` 的 Linux Docker 主机：

`compose.yaml`：

```yaml
services:
  aetherlink:
    image: ghcr.io/hyaeve/aether:latest
    container_name: Aether
    network_mode: host
    volumes:
      - ./config:/config
      - ./data:/data
      # 可选：将 FUSE 读缓存单独映射，建议放到更快的磁盘
      # - ./fuse_read_cache:/data/fuse_read_cache
    environment:
      - TZ=Asia/Shanghai
      - AETHER_PORT=15151
    devices:
      - /dev/fuse:/dev/fuse
    pid: "host"
    privileged: true
    restart: unless-stopped
```

启动服务：

```sh
mkdir -p config data
docker compose pull
docker compose up -d
```

默认 `host` 网络，不使用 `ports` 映射；特权与 FUSE 设备按需求启用。Docker Desktop 或非 Linux 环境不适用此默认 FUSE 配置。没有挂载需求时，建议移除 `privileged`、`devices` 并将本机目录绑定设为只读。

自定义端口：修改 `compose.yaml` 的 `environment` 中 `- AETHER_PORT=15151`，例如改为 `- AETHER_PORT=15200`，然后运行 `docker compose up -d` 重建容器。管理后台、STRM 播放代理和 WebDAV 都使用新端口，容器健康检查同步跟随。有效端口为 1–65535；不要同时设置 `AETHER_ADDR`，除非需要覆盖监听地址。

端口变更不会覆盖已保存的外部访问地址。请在「系统设置 → 常规设置」更新媒体服务器可访问的地址（例如 `http://192.168.1.10:15200`），并重新全量生成已有 STRM。反向代理使用独立公网端口时，外部访问地址应保留其实际地址。

如需本机存储，可自行在 `volumes` 下添加 `- ./storage:/mnt`，将宿主机 `./storage` 映射为容器内 `/mnt`。默认配置不包含这一挂载；它不是 STRM 输出目录。

FUSE 读缓存预留路径为 `/data/fuse_read_cache`，默认随 `/data` 映射。需要单独放置到其他磁盘时，可取消对应挂载行的注释并修改左侧宿主机路径。当前 FUSE 读缓存功能尚未实现。

容器持久化目录：

| 容器目录 | 宿主机默认目录 | 内容 |
| --- | --- | --- |
| `/config` | `./config` | `state.enc` 保存管理员、存储池、任务和系统设置；`master.key` 保存加密密钥 |
| `/config/organize/organize-rules.json` | `./config/organize/organize-rules.json` | 整理规则预留配置 |
| `/config/organize/categories.json` | `./config/organize/categories.json` | 二级分类预留配置 |
| `/config/organize/upgrade-policies.json` | `./config/organize/upgrade-policies.json` | 洗版策略预留配置 |
| `/config/organize/ai.json` | `./config/organize/ai.json` | AI 辅助识别预留配置 |
| `/config/organize/recognition-rules.json` | `./config/organize/recognition-rules.json` | 识别规则预留配置 |
| `/data` | `./data` | 目录缓存快照等运行数据 |
| `/data/strm` | `./data/strm` | 默认 STRM 输出位置，仅首次实际写入时创建 |

整理识别配置统一归入 `/config/organize/`，五个 JSON 文件在该目录中平铺，不为每项规则再建子目录。账户、密钥和系统设置仍保存在 `/config` 根目录。启动时，新位置缺失的文件优先从旧 `/config` 根目录复制，旧文件保留作为备份；新位置已有文件不会被覆盖，无旧文件时初始化为 `{}`。规则模块尚未实现，这些文件暂不参与规则执行。启动、创建任务或没有匹配视频的扫描都不会创建 `/data/strm`。更早版本的独立规则目录不自动删除或迁移其内容，以免误删已有数据。

`config/master.key` 与 `config/state.enc` 必须一起备份；丢失密钥不可恢复配置。升级时，如果 `/config` 尚无配置，自动复制旧 `/data/master.key` 与 `/data/state.enc` 至 `/config`，保留旧文件；已有 `/config` 配置不会被覆盖。迁移后以 `/config` 为准，旧文件只作备份，不会继续同步。

GitHub 构建默认发布 `ghcr.io/<owner>/<repo>:latest`、版本标签和 SHA 标签。修改 Compose 的 `image` 可指定已发布镜像版本；首次发布可能需在 GHCR 中配置包可见性。PR 只构建不推送。

## 安全与运维

- 首次初始化接口只在尚无管理员时生效；初始化前应仅在可信网络开放服务。
- 公网部署应通过 HTTPS 反向代理访问。WebDAV Basic Auth 不应在不可信网络使用明文 HTTP。
- STRM 链接携带长期 HMAC 签名，可被持有者访问；将 STRM 视为访问凭据。
- 管理员有权指定网络上游和可读取的本机根目录，因此管理员权限等同服务可访问范围。不要向不可信用户授予管理员。
- 数据密钥与密文位于同一持久化目录，加密不防御完整主机或完整数据卷失窃。
- 元数据缓存快照不含认证凭据，但包含文件名；需要保护数据卷。
- 当前配置会在保存缓存策略时清空旧缓存；源文件变动需刷新目录或等待缓存失效。

## 验证

```sh
go test ./...
go vet ./...
cd web
npm ci
npx playwright install chromium
npm run build
npm run test:e2e
```

浏览器测试使用独立临时数据目录、15159 测试端口，截图位于 `web/test-results/`，不修改实际服务配置。

## 参考说明

参考本机 LitePan 的存储添加流程、配置字段、目录缓存与 STRM 的交互约定；未引用 AetherLink。LitePan 本机版本的许可证为 PolyForm Noncommercial 1.0.0，本项目未直接复制其源文件或品牌图片。网盘标识来自各服务官网（天翼使用用户指定的 PNG），OpenList 标识来自其官方 Logo 仓库，来源记录见 `web/public/providers/SOURCES.md`；标识仅用于辨识服务，不代表官方授权或合作。Vue、Lucide、Go 扩展库、robfig/cron 等依赖遵循各自许可证。
