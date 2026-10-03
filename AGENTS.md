# Aether 开发约定与变更记录

## 必须遵守的记录规范

- 每次新增、修改或删除文件，都必须在本文件追加记录，再向用户交付。
- 每条记录包含日期、需求、文件路径、关键代码或入口、功能变化、验证结果与未完成项。
- 路径以仓库根目录为基准；同一次变更可分组列出，但不能遗漏文件。
- 历史补记必须标注为补记，不虚构当时已记录或已验证的结果。
- 保留用户已有改动；未经要求不提交、推送或删除配置数据。
- 项目面向 Docker 部署，不在仓库根目录生成或遗留 Windows exe 和运行日志。临时运行产物放在仓库外的临时目录，结束后清理。
- `web/node_modules` 中的构建工具二进制属于依赖，不作为项目发行物提交，也不单独删除。
- `config/`、`data/`、密钥、凭据、日志和测试产物不得提交。清理时不得删除用户数据。
- 新增功能必须明确区分已实现、待联调和预留入口。无 Docker 或真实网盘凭据时，不宣称相关验证已完成。

## 2026-10-03：初始版本与后续变更补记

以下根据当前源码、Git 初始提交 `c18e9ed` 及本次会话补记，之前未及时建立记录。

### 项目结构与部署

- 新增 `go.mod`、`go.sum`：Go 模块与 cron、密码哈希、WebDAV 依赖。
- 新增 `cmd/aether/main.go`：信号退出入口；后续加入 `-config-dir`、`-data-dir` 参数。
- 新增 `.gitignore`、`.dockerignore`：排除依赖、构建产物、配置和运行数据；后续增加 `config/` 排除项。
- 新增 `Dockerfile`：Vue 与 Go 多阶段构建、amd64 Linux 运行镜像、健康检查。后续改用 `AETHER_PORT`，固定容器 `/config`、`/data`；不再预建 `/data/strm`。
- 新增 `.github/workflows/docker-amd64.yml`：Go、前端、浏览器测试，构建并推送 GHCR amd64 镜像。
- 新增并调整 `compose.yaml`：host 网络、15151 可配置端口、配置与数据卷、FUSE 设备及缓存注释。当前文件中的服务名、特权与 host PID 等用户已有设置保持不变。
- 新增并调整 `README.md`：部署、环境变量、目录约定、实现边界、测试命令；后续嵌入与当前 Compose 一致的 YAML，并修正启动命令和图标来源说明。
- 新增并调整 `docs/design-system.md`：颜色、布局、字体、主题及登录星轨动效规范。

### Go 后端

- 新增 `internal/app/model.go`：`NewStore`、`Store.update`、`atomicWrite`；AES-GCM 配置持久化、存储池、任务、设置与日志模型。
- 新增 `internal/app/cache.go`：共享 TTL/LRU 缓存、条目和估算内存限制、快照与恢复。
- 新增并调整 `internal/app/drivers.go`：本地、WebDAV、OpenList、115、夸克目录与下载读取；移动/天翼使用 OpenList 网关。`file115` 兼容多种字段命名。
- 新增并调整 `internal/app/tasks.go`：Cron、任务运行/停止、共享请求间隔、目录扫描、STRM 排除与全量/增量、签名播放链接。`executeTask` 在实际写入匹配文件时才创建输出根目录。
- 新增并调整 `internal/app/server.go`：初始化管理员、登录会话、API、播放代理、只读 WebDAV。`listenAddress` 支持端口校验与地址覆盖；`newWithDirectories` 分离配置与运行数据；空 STRM 目标目录合法。
- 新增 `internal/app/webdav.go`：聚合只读文件系统、逐级目录解析与范围读取。
- 新增并调整 `internal/app/directories.go`：`prepareDirectories`、`migrateLegacyConfig` 保留旧配置并迁移到新目录；`ensureFlatConfigFiles` 创建根目录下缺失的规则 JSON，不覆盖已有文件。
- 新增并调整 `internal/app/app_test.go`：鉴权、路径边界、加密恢复、缓存、STRM、WebDAV、模拟上游、端口、配置迁移与冲突、延迟创建输出目录测试。

### Vue 界面基础

- 新增 `web/package.json`、`web/package-lock.json`：Vue、路由、Lucide、Vite 与 Playwright。
- 新增 `web/index.html`、`web/vite.config.js`、`web/src/main.js`：中文入口、构建、开发 API 代理与路由。
- 新增 `web/src/lib.js`：共享状态、API、通知、驱动元数据及格式化。
- 新增 `web/src/App.vue`：固定侧栏/顶栏、页面分发、三态主题、账户菜单与状态刷新。
- 新增并调整 `web/src/style.css`：低饱和靛蓝设计、响应式、模态框、表单、登录及品牌图片样式；修复手机侧栏遮挡。
- 新增 `web/public/aether.svg`：Aether 自有标记。
- 新增并调整 `web/src/components/Icon.vue`：按需导入 Lucide 图标，避免全量图标包；增加暂停图标。
- 新增并调整 `web/src/components/ProviderIcon.vue`：提供商标识，后续换为本地品牌资源。
- 新增并调整 `web/src/components/Modal.vue`：焦点约束/恢复、Escape、嵌套模态框支持。
- 新增 `web/src/components/DirectoryPicker.vue`：从真实 API 读取并选择目录。

### 页面与流程

- 新增 `web/src/pages/StoragePage.vue`：两步添加存储、编辑/删除、连接测试、搜索筛选。
- 新增并调整 `web/src/pages/TasksPage.vue`：STRM 与缓存任务表单、目录选择、执行/停止、排除规则；本地生成目录改为可留空。
- 新增 `web/src/pages/SettingsPage.vue`：缓存、WebDAV、服务地址、账户与密码设置。
- 新增 `web/src/pages/FilesPage.vue`：只读浏览、刷新、搜索、下载与复制播放链接。
- 新增 `web/src/pages/DashboardPage.vue`、`web/src/pages/LogsPage.vue`：真实运行指标与日志。
- 新增 `web/src/pages/PlannedPage.vue`：未实现功能的明确标识入口。
- 新增并调整 `web/src/pages/LoginPage.vue`：首次创建管理员，自定义账户名和密码，默认均为空；后续将左侧拆为星空组件。
- 新增 `web/src/components/LoginUniverse.vue`：Canvas 星空与星座、椭圆轨道、180 秒环绕、整体浮动、暂停及减少动态效果、隐藏时暂停、ResizeObserver 与资源清理。
- 新增 `web/public/providers/115.ico`、`mobile.png`、`tianyi.ico`、`quark.png`、`openlist.svg`：本地品牌资源。
- 新增 `web/public/providers/SOURCES.md`：上述素材来源与归属记录。
- 新增并调整 `web/playwright.config.js`：隔离测试数据、15159 端口，后续使用配置/数据目录启动参数。
- 新增并调整 `web/tests/workspace.spec.js`：自定义管理员创建/重新登录，存储与 STRM 完整流程、缓存、主题、响应式、默认输出目录、品牌图片加载、星空像素与动画暂停测试。

### 配置文件布局

程序按需在 `/config` 根目录初始化以下文件为 `{}`：

- `organize-rules.json`：整理规则。
- `categories.json`：二级分类。
- `upgrade-policies.json`：洗版策略。
- `ai.json`：AI 辅助识别。
- `recognition-rules.json`：识别规则。

这些规则模块尚未实现，文件暂不参与规则执行。既有规则子目录和数据不自动删除。账户、存储池、任务与系统设置仍保存在 `state.enc`，密钥为 `master.key`。

### 已执行验证与限制

- 最近后端修改后 `go test ./... -count=1`、`go vet ./...` 通过。
- 星空修改后 `npm run build` 通过；最近 `npm run test:e2e` 两个测试通过。
- 本地检查过桌面与手机截图、图标加载和 Canvas 非空像素。
- 本机无 Docker，未本地验证实际容器构建。竞态检测缺少 CGO，未在本机通过。
- 真实网盘鉴权未完成联调；115 自动刷新、原生移动/天翼、备份、FUSE 引擎、反代、整理刮削和规则模块仍未完成。

## 2026-10-03：补齐开发记录并清理本地预览产物

- 新增 `AGENTS.md`：本次补记全部已知源文件变化、关键入口、功能边界与验证；建立以后每次修改必记规范。
- 尝试删除本地生成的 `aether.exe`、`aether.stdout.log`、`aether.stderr.log`：它们由此前启动 Windows 预览产生，均被 Git 忽略，未上传；清理命令被执行环境策略拦截，尚未删除。
- 同一命令拟清理 `web/test-results/` 中的测试截图、运行状态等生成产物，但因策略拦截尚未执行；这些不是源码，也未上传。
- 同一命令拟停止对应仓库的 Windows 预览进程，但因策略拦截未确认停止，不能宣称本地预览已关闭。
- 保留 `config/`、`data/`、`web/dist/`、`web/node_modules/`；尤其不删除依赖自带的 `esbuild.exe`。
- 验证：本次仅增加记录和清理非源码产物，不重跑构建，避免重新生成文件；检查 Git 差异格式及清理后的目录状态。

## 2026-10-03：按用户要求提交并推送本轮改动

- 修改 `AGENTS.md`：追加本次提交范围和验证依据；本次不新增业务代码。
- 提交范围：上述登录星空动画、品牌图标及来源、按需创建 STRM 输出目录、平铺规则配置、对应测试、Dockerfile、README 与设计记录。
- 已执行 `git fetch origin`，确认提交前 `main` 与远程一致；差异格式检查通过。
- 使用此前针对当前功能执行通过的 Go 测试、静态检查、前端构建和两个浏览器测试结果，本次不重复生成运行产物。
- 推送前核对暂存文件，排除配置、密钥、exe、日志、依赖、构建与测试产物；推送完成状态以 Git 命令结果及最终回复为准。

## 2026-10-03：登录交互、密码校验和星河视觉调整

- 修改 `internal/app/server.go`：`setup`、`account` 取消 12 字节密码下限，非空即可；继续限制 bcrypt 支持的 72 字节上限，保留鉴权及会话撤销。
- 修改 `internal/app/app_test.go`：新增 `TestShortPasswordsAndValidation`，覆盖单字符创建、短中文密码修改/登录、空值及超长密码拒绝、旧密码和旧会话失效。
- 修改 `web/src/pages/LoginPage.vue`：移除创建密码最短长度；独立密码 label，闭眼表示隐藏、睁眼表示可见，按钮标签描述下一步操作。
- 修改 `web/src/pages/SettingsPage.vue`：账户与安全中的新密码同步取消 12 字符限制。
- 修改 `web/src/style.css`：登录输入控件仅在已有圆角边框内高亮，密码通过 `focus-within` 使用一层边界；保留键盘可见焦点。
- 修改 `web/src/components/LoginUniverse.vue`：移除手动播放/暂停按钮及状态，默认持续运行；减少动态效果及隐藏页面仍暂停。`buildGalaxy` 缓存星河粒子和尘埃暗带，`paint` 加强不同频率星点闪烁及星河缓慢变化，`paintMeteor` 绘制偶发流星。网盘和中心标志移除附加背景、边框和阴影，不修改原始图标自带底色。
- 修改 `web/src/components/ProviderIcon.vue`：天翼资源改为 `/providers/tianyi.png`。
- 新增 `web/public/providers/tianyi.png`：用户指定地址的高清 PNG，保留原图；旧 ICO 不再引用但保留文件。
- 重新获取 `web/public/providers/mobile.png`：用户指定的 URL 与此前来源一致，内容未改变，无 Git 差异。
- 修改 `web/public/providers/SOURCES.md`：记录用户指定的天翼来源、在用资源与历史 ICO 状态。
- 修改 `web/tests/workspace.spec.js`：以短密码验证创建与重新登录；验证单层焦点、显隐眼睛、无暂停按钮、无图标附加框、高清图加载、Canvas 帧变化及响应式显示。
- 修改 `README.md`、`docs/design-system.md`：同步密码规则、图标来源、自动动效及焦点设计约定。
- 修改 `AGENTS.md`：记录本次全部文件、关键实现及验证。
- 验证：`go test ./... -count=1`、`go vet ./...`、`npm run build`、两个 Playwright 测试通过。首次浏览器测试发现断言与 Lucide 实际 class 名不一致，修正断言后全量重跑通过。已查看桌面及 768px 截图，测试同时覆盖 1920/1024/390/375px、画布非空与动态变化。
- 本次未生成仓库根目录 exe，未修改用户运行配置或重启旧预览后端；密码后端变更需运行新版本生效。按用户要求提交推送，结果以 Git 确认为准。

## 2026-10-03：流星群、星空缎带与连接器图标

- 新增 `web/src/meteor.js`：`createMeteorBatch` 每批随机生成 2–5 条流星，起点限定在右上区域，位移朝左下，错开起始时刻并随机长度和持续时间。
- 修改 `web/src/components/LoginUniverse.vue`：每 12 秒更新一批流星；尾迹朝向与位移相反。`paintRibbons` 绘制三组缓慢起伏的细线缎带，保留粒子星河、闪烁星点及星座。隐藏全部连接器可见名称，保留无障碍名称；仅 OpenList、WebDAV、本机存储增加单层白底圆角框。减少动态效果偏好仍有效。
- 修改 `web/tests/workspace.spec.js`：增加批次数量边界、起点和方向测试；检查名称移除及三个白底图标；保留响应式、画布动态像素和完整工作流测试。
- 修改 `AGENTS.md`：记录本次文件、实现及验证。未改动账户存储逻辑：确认 `server.go` 使用 bcrypt 随机加盐哈希；`model.go` 将用户名与密码哈希等状态通过 AES-GCM 加密写入 `state.enc`，新建密钥为 32 字节，保存在同目录 `master.key`。
- 验证：`npm run build` 通过；三个 Playwright 测试通过，覆盖 1920/1024/768/390/375px、非空画布和动态变化。查看桌面截图确认缎带、流星及图标呈现。未生成根目录 exe、未修改用户配置，本轮无后端改动。

## 2026-10-03：整理识别配置统一归类并推送

- 修改 `internal/app/directories.go`：`ensureOrganizeConfigFiles` 替代根目录初始化，将五类 JSON 平铺到 `/config/organize/`。新位置缺失时复制根目录旧文件并保留原件；已有新文件优先，不覆盖。拒绝将目录或符号链接当作规则文件处理。账户状态和密钥的位置不变。
- 修改 `internal/app/app_test.go`：调整启动布局断言，确认新安装不生成根目录规则文件；新增迁移内容保留、重复启动不覆盖、旧/新路径目录冲突测试。
- 修改 `README.md`：更新五类规则路径、归类布局及兼容迁移说明，保留规则模块尚未实现的说明。
- 修改 `AGENTS.md`：记录本轮变化；前面的根目录平铺记录为历史行为，以本条及当前 README 为准。
- 验证：`go test ./... -count=1`、`go vet ./...`、三个 Playwright 测试通过。前端沿用上一轮已通过的构建。本次未改动本机配置数据，迁移在新版服务下次启动时执行。
- 按用户要求，将本轮配置归类与上一轮尚未提交的星空、流星群、图标样式和对应测试一起提交推送；排除配置、密钥、exe、日志、依赖、构建与测试产物，推送结果以 Git 确认为准。
