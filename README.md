# Aether 以太

Go + Vue 的自托管存储工作空间。主服务、STRM 播放代理、只读 WebDAV 共用 **15151** 端口。

## 当前实现

- 首次启动创建管理员，无默认密码；bcrypt 密码哈希、HttpOnly 会话、同源写请求校验、登录限流。
- 存储池新增、编辑、删除、启停、连接测试。同类型可重复添加，名称唯一。凭据 AES-GCM 加密落盘。
- 本机目录、WebDAV、OpenList 浏览与下载；115 Open、夸克直接接口；移动、天翼通过 OpenList 网关。
- STRM 全量/增量、五字段 Cron、手动执行/停止、排除目录/文件/后缀、API 请求间隔、输出目录约束。
- 缓存扫描任务：深度、执行间隔、缓存有效期。共享 TTL/LRU 缓存、条目与估算内存限制、磁盘快照及重启恢复。
- 聚合只读 WebDAV：`/dav/<storage-id>/`，支持 PROPFIND、GET、HEAD，复用管理员 Basic Auth。
- 中文界面、深色侧栏、固定顶栏、三态主题、移动端布局、登录轨道视觉、日志与账户设置。
- GitHub Actions 测试及 `linux/amd64` 镜像构建，推送 GHCR。

## 明确的边界

这是一版可运行的基础实现，**不是完整需求的全部实现**。

- 115 / 夸克未以真实账户完成端到端验证。115 当前使用手工配置访问令牌；刷新令牌可保存但尚未实现自动刷新或扫码授权。
- 移动、天翼当前填写已挂载该网盘的 OpenList 地址、API Token、根路径，不是原生驱动。
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

访问 `http://localhost:15151`，创建管理员（密码至少 12 字符）。先在系统设置填写媒体服务器能访问的地址，例如 `http://192.168.1.10:15151`，再生成 STRM。

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

```sh
mkdir -p config data storage
docker compose up -d --build
```

默认 `host` 网络，不使用 `ports` 映射；特权与 FUSE 设备按需求启用。Docker Desktop 或非 Linux 环境不适用此默认 FUSE 配置。没有挂载需求时，建议移除 `privileged`、`devices` 并将本机目录绑定设为只读。

自定义端口：修改 `compose.yaml` 的 `environment` 中 `- AETHER_PORT=15151`，例如改为 `- AETHER_PORT=15200`，然后运行 `docker compose up -d` 重建容器。管理后台、STRM 播放代理和 WebDAV 都使用新端口，容器健康检查同步跟随。有效端口为 1–65535；不要同时设置 `AETHER_ADDR`，除非需要覆盖监听地址。

端口变更不会覆盖已保存的外部访问地址。请在「系统设置 → 常规设置」更新媒体服务器可访问的地址（例如 `http://192.168.1.10:15200`），并重新全量生成已有 STRM。反向代理使用独立公网端口时，外部访问地址应保留其实际地址。

`./storage:/mnt` 是可选的本机存储挂载：将宿主机 `./storage` 映射为容器内 `/mnt`，无本机存储需求时可删除这一行。它不是 STRM 输出目录。原来的 `${AETHER_LOCAL_ROOT:-./storage}:/mnt:rshared` 使用环境变量选择宿主机路径，并启用双向挂载传播；普通文件浏览不需要这一传播选项，因此改为直接绑定。

容器持久化目录：

| 容器目录 | 宿主机默认目录 | 内容 |
| --- | --- | --- |
| `/config` | `./config` | `state.enc` 保存管理员、存储池、任务和系统设置；`master.key` 保存加密密钥 |
| `/config/organize-rules` | `./config/organize-rules` | 整理规则预留目录 |
| `/config/categories` | `./config/categories` | 二级分类预留目录 |
| `/config/upgrade-policies` | `./config/upgrade-policies` | 洗版策略预留目录 |
| `/config/ai` | `./config/ai` | AI 辅助识别预留目录 |
| `/config/recognition-rules` | `./config/recognition-rules` | 识别规则预留目录 |
| `/data` | `./data` | 目录缓存快照等运行数据 |
| `/data/strm` | `./data/strm` | 未填写输出目录时的 STRM 生成位置 |

规则模块尚未实现，当前只创建其配置目录，不生成虚假的规则文件。`config/master.key` 与 `config/state.enc` 必须一起备份；丢失密钥不可恢复配置。升级时，如果 `/config` 尚无配置，自动复制旧 `/data/master.key` 与 `/data/state.enc` 至 `/config`，保留旧文件；已有 `/config` 配置不会被覆盖。迁移后以 `/config` 为准，旧文件只作备份，不会继续同步。

GitHub 构建默认发布 `ghcr.io/<owner>/<repo>:latest`、版本标签和 SHA 标签。设置 `AETHER_IMAGE` 使用已发布镜像；首次发布可能需在 GHCR 中配置包可见性。PR 只构建不推送。

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

参考本机 LitePan 的存储添加流程、配置字段、目录缓存与 STRM 的交互约定；未引用 AetherLink。LitePan 本机版本的许可证为 PolyForm Noncommercial 1.0.0，本项目未直接复制其源文件或品牌图片。当前品牌与提供商标记为独立绘制的文字/图标标识，不代表官方授权。Vue、Lucide、Go 扩展库、robfig/cron 等依赖遵循各自许可证。
