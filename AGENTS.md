# Aether 开发约定与变更记录

## 2026-10-06：修复以链直接返回 STRM 中间地址

- 修改 `internal/app/links.go`：`buildLink` 显式开启 `FollowUpstreamRedirects`，复用本地 AetherLink 的 HEAD 探测、Range GET 回退、有效 UA 传递和有界重定向跟随；此前沿用默认 false 导致中间地址直接返回。客户端网段分类及跳转模式逻辑不变。持久化缓存换用 `*-direct-v2.json`，避免恢复旧的未探测地址，不删除旧缓存或用户数据。
- 修改 `internal/app/link_playback_test.go`：Emby/飞牛使用真实本地 HTTP 中间跳转服务和下载服务，断言最终 Location、第二次请求缓存命中、HEAD 拒绝后单字节 GET、播放器 UA 保留和不泄漏 Emby 凭据。ABS 用本地下载服务替代外部测试域名，避免探测依赖外网。
- 修改 `README.md`：说明解析顺序、缓存隔离以及后端本地代理/探测失败的边界。修改 `AGENTS.md` 记录本次全部文件。
- 验证：针对性播放测试、`go test ./... -count=1`、`go vet ./...`、`git diff --check` 通过；未改前端，不重跑浏览器测试。
- 限制：未连接用户的 10.0.0.31 服务，未做真实 Emby/115/Docker 播放联调。上游直接返回媒体而非重定向时不能推导不存在于响应中的网盘直链；探测失败保留参考项目原地址回退行为。未操作15151服务、未生成exe；本轮未要求推送，未提交、未发布或递增版本。

## 2026-10-06：目录名称导航与本地 CAS 生成

- 修改 `internal/app/model.go`：Task 新增 sourceLabel、sourceTrail、casOperation，分离目录显示信息和接口 ID，保留旧任务默认还原语义。修改 `internal/app/server.go`：校验生成/还原操作，本地仅允许生成，云盘仍保留原生限定。
- 修改 `web/src/components/TaskSourcePicker.vue`、`DirectoryPicker.vue`：显示目录名称而非 opaque ID；父级名称历史、根目录按钮及无历史时返回根目录。TaskSourcePicker 将名称和父级轨迹一并回传，旧任务缺失名称显示“已选目录”，重选后补齐。
- 修改 `web/src/pages/TasksPage.vue`：CAS 选择器加入本地存储，新增生成 CAS/读取 CAS 生成 STRM 两种操作；本地仅生成，保留时间只用于还原；任务列表和编辑按钮使用显示名称，重开恢复目录导航。
- 修改 `internal/app/drivers.go`、`mobile.go`、`tianyi.go`：File 增加 SHA256/MD5 元数据，移动读取显式 SHA256 contentHash、天翼读取 MD5。只接受兼容有效哈希，缺失报错，不伪造或隐式下载视频补算。
- 新增 `internal/app/cas_generate.go`：generateCASInfo 在 os.Root 内流式计算本地 SHA256 与 MD5、检查取消/普通文件/读前后大小时间，拒绝路径越界；writeCASOutput 临时文件写入同步后发布，增量通过硬链接避免覆盖已有输出，全量原子替换；文件系统不支持硬链接时明确返回错误。
- 修改 `internal/app/tasks.go`：生成操作遍历普通视频，复用目录排除/类型排除/调度/全量增量，输出 Base64 JSON 的原文件名.cas；原还原生成 STRM 流程保持。输出仍受既有 STRM 根目录限制。
- 新增 `internal/app/cas_generate_test.go`，修改 `internal/app/cas_test.go`：本地 SHA256/MD5 精确值、解码兼容、全量增量、取消、越界和符号链接保护、云端缺失哈希拒绝；原本地全部拒绝断言改为本地还原拒绝。
- 修改 `web/tests/workspace.spec.js`，新增 `web/tests/zz-cas.spec.js`：云端目录名回显、刷新恢复、回上级/根目录、本地 CAS 真实执行与输出哈希验证、表单截图。修改 `README.md`、`AGENTS.md`：记录操作差异与边界。
- 验证过程：前端构建和 go vet 通过；首次测试暴露旧测试假设及新增夹具缺少天翼凭据、自定义选择器角色不符，已修正并全量重跑。最终结果追加于下方。
- 限制：无真实移动/天翼凭据或 Docker 联调；云端生成依赖目录接口返回兼容哈希，未实现缺失哈希的额外详情接口查询。本地计算只支持现有视频类型；旧任务未保存名称无法凭空恢复路径。未改用户配置、15151 服务或生成根目录 exe；本轮未提交推送，前一轮本地 v0.1.1 推送失败状态不变。

## 必须遵守的记录规范

- 发布推送默认递增补丁版本：以 `VERSION` 为准，未指定版本时每次发布加 `0.0.1`（如 `0.1.9` → `0.1.10`），同步后端、Docker默认值、前端包版本并推送对应 `v` 标签。本轮从 `v0.1.0` 升至 `v0.1.1`。

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

## 2026-10-03：移除缎带并调整流星流畅度和图标比例

- 修改 `web/src/components/LoginUniverse.vue`：移除 `paintRibbons` 及调用；取消 30 FPS 节流，每次 requestAnimationFrame 绘制。流星使用等量像素横纵位移，在不同画布比例下保持一致的 45 度左下方向；保留隐藏页面及减少动态效果暂停。OpenList 白底框尺寸不变，内边距缩为 1px，补偿原始 SVG 留白。
- 修改 `web/src/meteor.js`：保留每批 2–5 条流星，缩短随机行程；新增 `meteorOpacity`，快速柔和淡入并在途中渐隐，不要求到达左下角。
- 修改 `web/tests/workspace.spec.js`：更新短行程范围断言，覆盖淡入淡出曲线端点和亮度变化，检查 OpenList 内边距；保留动态像素、图标、响应式和业务流程验证。
- 修改 `AGENTS.md`：记录以上实现和验证。
- 验证：`npm run build`、三个 Playwright 测试通过；查看桌面截图确认无缎带、平行流星与放大的 OpenList 主体。实际帧率依赖运行设备，未宣称固定帧率。本轮未改动后端、用户配置或生成根目录 exe。

## 2026-10-03：LitePan 导航、公共控件、存储授权和关于页

- 修改 `web/src/App.vue`：十一项单层主导航，移除空间切换框和底部连接/版本信息；主题单按钮循环三态；最近任务通知与浏览器内按账户保存的已读标记；账户菜单加入关于入口并统一图标。
- 新增 `web/src/components/TaskTabs.vue`、`NumberInput.vue`：任务栏目移入页面；数字输入单位与上下按钮共用尾部空间，悬浮/聚焦显示步进按钮，支持原生上下键和边界。
- 修改 `web/src/components/Icon.vue`、`Modal.vue`：加入通知及步进图标；紧凑存储弹窗变体。
- 修改 `web/src/pages/SettingsPage.vue`、`TasksPage.vue`：设置内部导航、公共数值控件、移除重复任务栏目。
- 新增 `web/src/pages/ToolsPage.vue`：十类插件卡片，详情显示当前待实现边界；识别规则列出最小视频、黑名单、自定义识别词和匹配。
- 新增 `web/src/pages/AboutPage.vue`，修改 `web/src/components/LoginUniverse.vue`：复用登录星空作关于页背景，显示后端版本并显式检查 GitHub 正式 Release。
- 修改 `web/src/pages/StoragePage.vue`：移除连接器底部区和步骤说明行，图标/名称选择、紧凑表单、上一步、默认回收站策略、115 获取 TOKEN 及夸克扫码授权弹窗。关闭/卸载停止轮询并忽略过期请求。
- 修改 `web/src/style.css`：252px 侧栏、16px 导航、17px 账户菜单、单边框内焦点、数字尾部复用、通知、插件、关于、紧凑弹窗及响应式。小屏弹窗可滚动但不显示滚动条。
- 新增 `internal/app/authorization.go`：夸克扫码及 Cookie 回填、用户指定可信 HTTPS 115 OAuth 代理协议。授权续询令牌 AES-GCM 加密且绑定登录会话、提供商与五分钟期限；接口鉴权、响应禁止缓存、网络超时及错误处理。不默认使用 LitePan 授权服务，不冒充其客户端。
- 新增 `internal/app/version.go`，修改 `internal/app/server.go`：受保护版本及 GitHub Release 检查接口；处理未发布、限流、网络异常和语义版本比较；新增授权路由和删除模式校验。
- 新增 `internal/app/workspace_test.go`：版本检查模拟响应、授权加密/篡改/跨会话/跨提供商/过期验证、接口鉴权及删除模式测试。
- 修改 `go.mod`、`go.sum`：二维码生成库及语义版本比较依赖；已运行 go mod tidy。
- 修改 `Dockerfile`、`.github/workflows/docker-amd64.yml`：构建参数注入后端版本，版本标签构建传入标签。
- 修改 `web/tests/workspace.spec.js`：导航、主题、通知已读、数字步进及焦点、紧凑表单、授权模拟回填、关于更新检查、插件卡片及桌面/移动截图。旧测试标签模糊匹配和登录后跳页竞态修正后全套重跑。
- 修改 `README.md`、`docs/design-system.md`、`AGENTS.md`：记录当前行为、LitePan 主参考及未实现边界。
- 验证：最终 `go test ./... -count=1`、`go vet ./...`、`npm run build`、四项 Playwright 测试均通过；查看存储桌面、115 配置弹窗及手机关于页截图。无真实网盘授权账号联调，无本地 Docker 构建验证。
- 限制：CAS 独立入口尚未实现，缓存任务仍单独保留；辅助插件、目录整理、刮削仍为明确标注的预留模块。删除模式仅保存策略，文件服务仍只读。通知是每任务最新结果而非完整历史。115 授权需要用户信任且兼容协议的代理。
- 隔离预览：启动 `127.0.0.1:15152`，临时 exe、日志及独立配置位于系统临时目录 `aether-preview-20fc6f670f984067921c3f084b997548`，进程 9980；不改动既有 15151 后端和用户配置，不在仓库根目录生成 exe。本轮未提交推送。

## 2026-10-03：原生移动云盘与 CAS 任务

- 参考本地 `139strm-main/yun139/{client,crypto,cas,strm}.py` 的协议及流程；新增 `THIRD_PARTY_NOTICES.md` 保留其 MIT 许可与协议来源，修改 `Dockerfile` 将声明放入镜像。
- 新增 `internal/app/mobile.go`：新版个人云 Authorization 校验、请求签名、受限路由发现、分页目录、PC 秒传请求头和下载链接。原生模式与旧 OpenList 网关模式并存；没有自动令牌刷新。
- 新增 `internal/app/cas.go`：有界 Base64 JSON 解析、名称/大小/SHA256 校验、秒传分片描述、存储池专用临时目录、90 秒播放准备上限、签名信息播放还原、可取消互斥门、活跃播放租约、加密临时记录及重启复用、闲置两小时清理。只处理记录的文件 ID，默认回收站，永久删除需存储池显式选择；失败还原记录不可复用，失效链接记录下次请求重新还原。
- 修改 `internal/app/model.go`：加密状态中保存临时文件清单。修改 `internal/app/drivers.go`：原生移动目录及下载分流。
- 修改 `internal/app/server.go`：CAS 校验、状态/清理鉴权接口、Authorization 脱敏、Range/HEAD 播放分流、临时文件存在时禁止切换账号/模式或删除停用存储；允许更新同账号授权。
- 修改 `internal/app/tasks.go`：CAS 全量/增量、Cron、取消、排除与目录扫描复用任务框架；签名携带已验证 CAS 元数据；输出重名检测；周期清理过期临时文件。
- 新增 `internal/app/cas_test.go`：格式与路径拒绝、原生存储约束、任务扫描与增量、取消、秒传复用、重启恢复、活跃保护、定向清理、签名篡改、Range/HEAD 代理及授权不泄漏、失败记录和互斥取消测试。上游均为模拟，无真实账号。
- 修改 `web/src/pages/StoragePage.vue`、`web/src/lib.js`：移动原生/网关切换、Authorization 输入与删除模式说明。
- 修改 `web/src/pages/TasksPage.vue`、`web/src/App.vue`：CAS 替换预留页，支持创建/编辑/调度/停止/删除任务、筛选原生移动存储、源目录选择、排除和临时文件管理；通知跳转 CAS。
- 修改 `web/src/style.css`：任务表格保留最小列宽，小屏横向滚动避免逐字换行。
- 修改 `web/tests/workspace.spec.js`：原生移动配置、CAS 任务表单/目录/持久化/编辑、临时清理和桌面手机截图；目录响应模拟。
- 修改 `README.md`、`AGENTS.md`：使用步骤、与网关模式兼容、清理和权限边界及限制。历史 CAS 预留记录以本条为准。
- 验证：Go 全量测试及 vet 通过，前端构建通过；五项浏览器测试通过后调整手机表格并再次全量验证，最终结果以工具输出为准。已查看 CAS 桌面表单与手机列表截图。
- 真实移动账号、权益和大文件秒传未联调；仅支持 SHA256 视频 CAS。全量不删除本地孤儿，崩溃在云端创建和记录提交之间可能残留未记录文件；不能宣称云端无配额占用。
- 更新隔离预览 `127.0.0.1:15152`，临时目录保持不变，进程改为 13792；未重启原 15151 实例，未在仓库根目录构建 exe，本轮未提交推送。

## 2026-10-03：天翼原生账户登录、双网盘 CAS 与推送

- 新增 `internal/app/tianyi.go`：参考 OpenList 官方仓库 `drivers/189pc` 的协议（修订 `4c39bbe9c228680e2a6f78555175f7f2063d452c`），用标准库独立接入天翼个人云。账户 RSA PKCS#1 v1.5 提交、HTTPS 域名与跳转限制、登录参数新旧页面兼容、内存会话复用及失效清除、HMAC-SHA1 请求签名、分页目录、下载链接、MD5 秒传、临时目录和定向批量清理；不使用 OpenList 网关。验证码和设备二次验证明确报错，尚无交互验证流程。
- 修改 `internal/app/drivers.go`：天翼目录/下载原生分流，默认根 ID `-11`，移除天翼网关分支。
- 修改 `internal/app/server.go`：天翼账号密码与数字根 ID 校验，移除旧网关配置字段；初始化会话缓存；CAS 接受移动、天翼原生池，临时文件存在时保护两种网盘的账户身份。
- 修改 `internal/app/cas.go`：新增 MD5 字段及按存储校验，拒绝空 CAS 文件名，分派还原/下载/清理；沿用移动既有签名及临时记录键。增加已移入回收站状态，永久清理中断后继续清空指定文件，失败不删除记录。
- 新增 `internal/app/tianyi_test.go`：模拟 RSA 登录、签名、数字/字符串 ID、目录/CAS 扫描、MD5 还原、Range/HEAD、凭据不泄漏、会话和重启复用、清理失败保留记录、永久清理续步、秒传未命中不提交、会话过期重登、验证码/二次验证/错误密码/无效公钥/网关拒绝和不可信域名。
- 修改 `web/src/lib.js`、`web/src/pages/StoragePage.vue`：天翼原生账户元数据和精简账号密码表单、根 ID、删除策略；旧网关池编辑时重新配置原生账户，不保留网关密码作天翼凭据。修改 `web/src/pages/TasksPage.vue`：CAS 存储池选择包含原生天翼。
- 修改 `web/tests/workspace.spec.js`：新增天翼配置、密码脱敏、目录选择、CAS 保存/刷新/编辑以及桌面手机截图测试。
- 修改 `README.md`、`THIRD_PARTY_NOTICES.md`、`AGENTS.md`：记录协议来源、双网盘 CAS、账号加密、会话周期、验证码限制、旧网关池及任务目录迁移和本轮文件功能。旧记录中“天翼网关”“CAS 仅移动”以本条为准。
- 验证：Go 测试、vet、前端构建及六项 Playwright 测试通过；检查天翼手机存储表单和桌面 CAS 截图。新增永久清理续步测试首次因测试存储未启用失败，修正夹具后重跑全套。Windows 本机 race 因未启用 CGO 无法执行，GitHub Linux 工作流保留 race 检测。没有真实移动/天翼账号或 Docker 联调，不宣称完成。
- 隔离预览更新到 `127.0.0.1:15152`，进程 `31332`，临时程序 `aether-next.exe`、日志及配置仍在系统临时目录；健康检查正常。未操作原 15151 后端，未修改仓库用户配置，未生成仓库根目录 exe。
- 按本轮用户要求，将之前尚未提交的界面、原生移动 CAS 和本轮天翼功能一起提交到 `main` 并推送；排除配置、密钥、exe、日志、依赖、构建与测试产物，实际提交及推送结果以 Git 输出为准。

## 2026-10-03：CAS 任务保留时间与默认还原目录

- 修改 `internal/app/model.go`、`internal/app/server.go`：任务新增 `retentionHours`；CAS 缺省 12 小时，校验 1–8760 小时。
- 修改 `internal/app/tasks.go`：CAS 签名链接带任务 ID 和保留时长；普通 STRM 链接不新增字段。播放读取现存关联任务配置，旧链接缺省 12 小时。
- 修改 `internal/app/cas.go`：移动还原目录改为网盘根 `/Aether`，临时记录持久化保留时长，按最后播放结束时间计算到期；活跃租约保护不变。复用记录采用最长已应用时长，缩短配置不提前缩短已有记录；状态接口返回每文件时长及到期时间。旧记录缺省 12 小时，旧目录记录仍按 ID 复用与清理，不迁移或整目录删除。
- 修改 `internal/app/tianyi.go`：天翼同样查找或创建个人云根目录下的 `Aether`。
- 修改 `web/src/pages/TasksPage.vue`：CAS 创建/编辑新增“还原文件保留时间”公共数值控件，默认 12h；临时文件窗口按条目显示保留时间和到期时间，移除固定两小时说明。
- 修改 `internal/app/cas_test.go`、`internal/app/tianyi_test.go`：两云根目录创建参数断言、默认/非法/自定义时长、签名字段和持久化、24 小时记录在闲置 13 小时不清理；调整旧生命周期和重启清理的过期夹具。
- 修改 `web/tests/workspace.spec.js`：移动默认 12h、自定义 24h 保存刷新后恢复，天翼默认 12h及临时窗口默认文案。修改 `README.md`：目录、计时语义、多任务复用及旧记录兼容说明。修改 `AGENTS.md`：本次记录。
- 验证：`go test ./... -count=1`、`go vet ./...`、前端构建和六项 Playwright 测试通过，查看天翼 CAS 表单截图。无真实云盘账号联调。
- 隔离预览更新至 `127.0.0.1:15152`，临时程序 `aether-retention.exe`、进程 `31388`，未改动原 15151 服务、用户配置或在仓库生成 exe。本轮未要求推送，未提交推送。

## 2026-10-03：推送 CAS 保留时间改动

- 按用户后续要求，提交并推送上一条记录中的 CAS 保留时间、`/Aether` 默认目录、测试及文档改动；修改 `AGENTS.md` 补充推送记录。沿用上一轮已通过的测试结果，本次不修改功能代码，不包含配置、密钥或构建产物；推送结果以 Git 确认为准。

## 2026-10-04：紧凑工作区、组合目录选择、账号会话与镜像检查

- 修改 `web/src/App.vue`：侧栏改为无 href 的可键盘操作按钮导航，避免悬浮触发浏览器链接地址预览；WebDAV 简称、系统设置底部固定、默认进入账号页、账号术语统一；任务页自带同排栏目和操作区。
- 修改 `web/src/pages/StoragePage.vue`：删除全部/云端/本地筛选，刷新、搜索、添加按钮按顺序同排；保留存储数量统计。
- 新增 `web/src/components/TaskSourcePicker.vue`：左选存储账号、右选目录，目录名筛选、修改时间、上级与刷新、确认和取消；请求序号防止切换后旧响应覆盖；无效日期显示短横线；可访问目录按钮名不包含日期。
- 修改 `web/src/pages/TasksPage.vue`：栏目和添加按钮同排；名称/下拉生成方式、组合源目录/生成目录各占两列，更多选项两列；移除多余说明。STRM、CAS、缓存共用选择器，CAS 仅传入原生移动/天翼池。
- 修改 `web/src/pages/SettingsPage.vue`：横向账号与安全/关于栏目，移除常规入口；账号密码并列、移除当前密码、会话天数控件默认 7；外部访问地址迁至 WebDAV 页，避免原功能不可配置。
- 修改 `web/src/pages/LoginPage.vue`、`web/src/lib.js`：界面统一“账号”用词。修改 `web/src/meteor.js`、`web/src/components/LoginUniverse.vue`：流星并行斜率 0.42，淡出起点在路程三分之一之后随机，淡出终点也随机，保留数量、方向及减少动画偏好。
- 修改 `web/src/style.css`：236px 蓝紫侧栏、56px 顶栏、主界面上下衔接圆角、紧凑存储卡片和 14px 圆角、账号菜单右移且 16px 字体；去重页面标题、横向设置栏目、桌面四列工具卡片、组合选择器及手机适配。
- 修改 `internal/app/model.go`、`internal/app/server.go`：设置保存 `sessionDays`，新会话默认七天，账号接口校验 1–365 天。已登录管理员无需当前密码可修改账号密码，其他会话撤销并签发新会话；保留鉴权、同源校验和 Strict Cookie。普通设置接口不能覆盖会话策略。会话仍仅驻内存，重启须登录。
- 修改 `internal/app/version.go`：更新检查查询 GHCR token、latest 索引、linux/amd64 manifest 与配置 OCI 标签，对比构建修订号，成功缓存五分钟；错误/缺少修订号明确提示，不误报最新。不自动拉取或重启。旧 Release 工具函数仅保留既有单元测试，不再用于更新检查路由。
- 修改 `Dockerfile`、`.github/workflows/docker-amd64.yml`：注入 `AETHER_REVISION` 与版本、OCI 标签，使后续镜像能准确与 GHCR latest 比对。
- 新增 `internal/app/preferences_test.go`：账号无需旧密码、鉴权、默认/自定义会话时长、非法范围、旧会话撤销及持久化；GHCR 相同/不同/未知修订、拒绝/限流、摘要与平台错误模拟。
- 修改 `web/tests/workspace.spec.js`：更新标签与组合目录交互；增加侧栏无链接、设置置底、顶栏高度、工具四列、账号策略保存、任务操作对齐、CAS 存储限制、目录过滤及手机截图、流星淡出边界断言。初轮目录精确定位因日期参与名称失败，修正可访问名称后全部重跑通过。
- 修改 `README.md`：首部 Aether 图标，GHCR 检查、会话语义及安全提醒、外部地址新入口、组合选择器。修改 `docs/design-system.md`、`AGENTS.md`：记录布局与本轮文件功能。
- 验证：Go 全量测试、vet、前端构建通过；最终六项 Playwright 全部通过，检查存储桌面、账号设置、目录选择器桌面/手机截图。真实 GHCR 匿名读取成功，latest 标签读到修订 `804edbe44648f6bb1184af03c4d5629c44c0889e`。未在本机构建 Docker、未声称真实网盘联调完成。
- 隔离预览 `127.0.0.1:15152` 更新为临时 `aether-workspace.exe`，进程 `14264`；未改原 15151 实例、仓库用户配置或生成仓库 exe。本轮未请求推送，未提交推送。

## 2026-10-04：WebDAV 独立用户与目录权限、工具说明及推送

- 新增 `internal/app/dav_users.go`：管理员鉴权的 WebDAV 用户列表、新增、编辑、删除接口；账号唯一性、与管理员重名拒绝、密码 bcrypt、空密码编辑保留、目录可读性及别名校验，最多 100 项授权；列表不返回哈希。
- 修改 `internal/app/model.go`：加密状态保存独立 DAV 用户及目录授权。修改 `internal/app/server.go`：注册用户接口，Basic Auth 支持独立用户，停用拒绝；管理员保持全盘访问，普通用户请求上下文携带授权；错误认证受限流，账号修改不能与 DAV 用户重名。
- 修改 `internal/app/webdav.go`：授权别名构成独立虚拟根，只解析授权目录及子项；直接其他存储 ID、上级路径均不可越权；本地目录使用授权子目录作为 os.Root，隔离管理员根目录缓存，保持只读与 Range/HEAD。已开始的响应不强制中断。
- 新增 `internal/app/dav_users_test.go`：用户 API 鉴权、密码不泄漏、管理员预热缓存后权限隔离、目录枚举及读取、越权路径、只读、错误密码、撤销、停用和删除。
- 新增 `web/src/components/WebDAVUsers.vue`，修改 `web/src/pages/SettingsPage.vue`：WebDAV 页面新增用户管理，复用组合目录选择器配置多个目录，别名输入、启用、编辑留空密码及删除确认。
- 修改 `web/src/App.vue`、`web/src/components/Icon.vue`：仪表盘使用 Gauge 仪表语义图标，注册用户/目录添加和辅助工具专用 Lucide 图标。修改 `web/src/pages/ToolsPage.vue`：每工具有独立语义图标、标题与功能描述，仍明确标注待实现。
- 修改 `web/src/pages/FilesPage.vue`：刷新仅图标按钮，移到搜索栏左侧，保留禁用、旋转状态及可访问标签。修改 `web/src/style.css`：工具图标色块、标题/小字单行省略与悬浮完整说明、用户授权布局。
- 修改 `web/tests/workspace.spec.js`：新增 WebDAV 用户创建/选目录/编辑与桌面手机截图、仪表盘图标、工具描述省略、刷新按钮顺序断言。修改 `README.md`、`AGENTS.md`：独立用户配置、权限和在途响应限制。
- 验证：Go 全量测试、vet 和前端构建通过；浏览器最终结果以本轮输出为准。新增 Go 测试首轮因多余 import 编译失败，移除后全量通过。无真实云盘/Docker 联调。
- 用户要求完成推送，包含上一轮未提交的紧凑工作区、账号会话、GHCR 检查和本轮 WebDAV 功能；不提交配置、密钥、构建或截图产物。推送结果以 Git 确认为准。
- 最终七项 Playwright 测试通过，已查看工具说明桌面截图及 WebDAV 用户手机编辑截图。隔离预览 15152 更新为临时 `aether-dav-users.exe`，进程 `31548`；原 15151 服务不变。

## 2026-10-04：宇宙侧栏、紧凑控件、存储启停与系统日志

- 修改 `web/src/App.vue`、`web/src/components/Icon.vue`：WebDAV 使用 Router、本地挂载使用 FolderSync、系统设置使用齿轮 Settings；系统日志与设置固定侧栏底部。修改 `web/src/components/TaskTabs.vue`：默认 `/tasks` 明确选中首个 STRM 栏目。
- 修改 `web/src/style.css`：48px 顶栏、侧栏自身右侧上下圆角、白色主体直角、蓝紫侧栏与透明星点选中态；14px 紧凑账号菜单、17px 页面栏目与明显选中线、淡蓝紫细滚动条；账号设置边框窗口、关于页填满剩余高度、登录无外部滚动条；日志固定窗口及44px虚拟行、WebDAV用户卡片和响应式。
- 修改 `web/src/components/LoginUniverse.vue`、`web/src/meteor.js`：流星斜率0.22、拖尾缩短至行程的5.5%–10%，保留随机淡出、平行方向与减少动画偏好；扩大粒子星河横向宽度。
- 修改 `web/src/pages/StoragePage.vue`：移除刷新/搜索/添加工具行，虚线卡片为添加入口；提供商图标通过现有 PUT 接口启停存储，保留运行任务与CAS保护、错误反馈及忙状态。
- 修改 `web/src/components/WebDAVUsers.vue`、`web/src/pages/SettingsPage.vue`：只读用户卡片展示授权路径，编辑、启停、删除；服务开关自动保存，失败恢复原状态；保留外部地址编辑及保存。修改 `internal/app/dav_users.go`：独立 PATCH 启停用户，仅更改 Enabled，不依赖上游在线，不修改密码或授权。
- 新增 `internal/app/logs.go`：模块化事件、级别规范化、最近2000条持久化上限、JSON行标准输出；持久化失败写标准错误。新增响应状态包装与模块分类；`defaultPublicURL` 根据本机网卡及监听端口初始化，不信任请求Host。
- 修改 `internal/app/model.go`：日志增加 module；旧日志兼容；新安装默认外部地址自动探测，旧固定 localhost:15151 启动迁移，其他自定义地址保留。修改 `internal/app/server.go`：受保护 GET `/api/logs`，变更操作与文件浏览按模块记录路由模板和状态，不记录正文或查询参数；启动事件输出日志；不记录轮询、日志读取、播放请求。修改 `internal/app/tasks.go`：任务结果归入任务模块。
- 修改 `web/src/pages/LogsPage.vue`：独立获取日志，原始JSON/结构化切换、搜索与级别/模块组合筛选、按账号在浏览器持久化、刷新仅获取日志、ResizeObserver驱动的固定行高虚拟列表。
- 新增 `internal/app/logs_test.go`：鉴权、审计不含请求凭据、级别归一化、重载、2000条上限、地址端口、离线用户启停权限保持。修改 `internal/app/app_test.go`：STRM断言采用配置的实际外部地址。修改 `web/tests/workspace.spec.js`：紧凑布局、存储/用户启停、默认栏目、1500条虚拟列表、筛选持久化、日志刷新、手机布局与登录无滚动条。
- 修改 `README.md`、`docs/design-system.md`、`AGENTS.md`：记录日志、默认地址、多网卡确认要求、视觉约定、实现与验证边界。
- 验证：最终 Go 全量测试、go vet、前端构建通过；八项 Playwright 全部通过，覆盖桌面/手机、星空像素和运动、图标加载。首次后端测试固定localhost断言失效，更新为实际配置；新增日志测试误将搜索结果预计为5条（实际包含149本身共6条），修正后全量重跑。查看日志桌面/手机、WebDAV卡片、关于页截图。
- 限制：保留最近2000条而非无限日志归档，原始视图是应用JSON事件，不是任意进程输出；未实现的备份、挂载、以太链接不生成模拟日志。WebDAV仍只读。多网卡或桥接容器地址需管理员确认；没有本地Docker和真实网盘联调。
- 隔离预览15152原进程已不存在，确认端口空闲后启动临时目录中的 `aether-log-workspace.exe`，进程25992；健康检查通过，标准输出确认JSON启动事件。未操作原15151服务、未在仓库根生成exe、未修改仓库用户配置。本轮未提交或推送。

## 2026-10-04：提交推送系统日志与界面调整

- 按用户要求提交并推送上一条记录中的全部源代码、测试和文档；本轮仅修改 `AGENTS.md` 补充交付记录，无新增业务改动。
- 验证沿用上一轮通过的 Go 测试、go vet、前端构建与八项 Playwright；推送前执行远程同步、差异格式及暂存文件检查，不提交配置、密钥、exe、日志、依赖或测试产物。
- 实际提交与推送结果以 Git 输出及最终回复为准；Docker与真实网盘联调限制不变。

## 2026-10-04：流星三档速度、原始日志与提示气泡

- 修改 `web/src/meteor.js`、`web/src/components/LoginUniverse.vue`：每条流星随机选择原时长、两倍或三倍时长，原速度为最快档；统一倾角斜率改为0.30，保留短尾及随机淡出。关于页装饰模式显示七个存储图标，围绕正文周边缓慢浮动，不遮挡中央操作；登录页继续椭圆轨道环绕。
- 修改 `web/src/components/Icon.vue`、`web/src/components/ProviderIcon.vue`、`web/src/App.vue`：WebDAV统一为CloudDownload，本机存储使用与本地挂载一致的FolderSync；注册Logs和TableProperties用于日志当前模式；全局提示通过TransitionGroup实现右上角滑入、自动淡出和关闭，增加状态语义。
- 修改 `web/src/pages/AboutPage.vue`：检查更新结果和错误使用全局提示气泡，不再常驻中央结果段落。修改 `web/src/pages/SettingsPage.vue`：WebDAV移除保存按钮，开关即时保存，外部地址变更失焦保存，失败保留错误并提示。
- 修改 `web/src/pages/LogsPage.vue`：单图标按钮切换原始/结构化列表，图标和无障碍标签反映当前模式；原始日志展示级别和完整换行JSON。ResizeObserver测量行高、累计高度和二分定位维持动态高度虚拟滚动；筛选或视口宽度变化重置测量，卸载清理观察器。
- 修改 `web/src/style.css`：清透蓝紫侧栏、圆角晕染选中态；原始日志级别色条、浅色背景和等宽换行；提示左侧色条、实心状态图标、滑入淡出与减少动画兼容；关于页周边预留图标空间。手机日志工具栏改两行网格，避免下拉框文字被挤掉。
- 修改 `web/tests/workspace.spec.js`：三档时长和短尾断言；更新提示自动消失、关于页图标浮动、侧栏/卫星图标一致；WebDAV无保存按钮、开关与地址自动保存刷新恢复；日志当前模式、长JSON手机换行、1500条虚拟滚动至末尾、行不重叠及下拉框最小宽度；沿用Canvas动态像素和多视口覆盖。
- 修改 `README.md`、`docs/design-system.md`、`AGENTS.md`：同步当前交互、动效及验证记录。
- 验证：Go全量测试、go vet、前端构建通过。第一轮八项浏览器测试通过后，截图发现手机筛选框过窄，修正并增加宽度断言，最终再次八项全部通过；查看原始日志桌面/手机、更新提示与关于页桌面/手机截图。未改后端协议，无Docker/真实云盘联调声明。
- 已运行的隔离预览15152继续使用新构建的前端，无需重启Go后端；未改原15151服务、用户配置或在仓库生成exe。按用户要求提交推送，本次仅源码、测试及文档，实际推送结果以Git输出为准。

## 2026-10-04：登录与账号统一表单、环绕关于页及115授权窗口

- 新增 `web/src/components/AccountFields.vue`：账号/密码共用62px等尺寸圆角图标输入框，提示文本代替可见标题，保留无障碍名称、自动填充及显隐按钮。修改 `web/src/pages/LoginPage.vue`、`web/src/pages/SettingsPage.vue`：复用控件、移除登录版本页脚，账号设置限制380px窗口。
- 修改 `web/src/components/Icon.vue`、`web/src/components/ProviderIcon.vue`、`web/src/App.vue`：Lucide云轮廓内组合DAV标记，本地挂载及本机存储使用CloudDownload，侧栏与卫星一致；品牌副标题“云端本地 · 以太空间”。
- 修改 `web/src/components/LoginUniverse.vue`、`web/src/pages/AboutPage.vue`：关于页图标慢速椭圆环绕中心，底部独立更新/GitHub按钮；确认同修订号时提示“当前版本已是最新”，有更新简短提示，不把无法比对误报最新。修改 `web/src/meteor.js`：尾迹缩短为行程1.8%–3.8%。
- 修改 `web/src/pages/StoragePage.vue`、`web/src/lib.js`：移动仅原生个人云CAS表单，旧网关编辑需重新填写授权，后端旧模式运行兼容保留；添加卡片去说明、降低高度。115先同步打开新窗口，申请HTTPS授权地址后跳转，保留轮询回填、取消关闭及弹窗拦截提示；可信代理首次手动指定并由当前浏览器记忆，不擅自使用LitePan公共代理。
- 修改 `internal/app/authorization.go`：参考本地LitePan的OAuth请求流程，增加server_use标识；不复制其源码或客户端身份，不改变会话绑定、加密、HTTPS和鉴权保护。
- 修改 `web/src/style.css`：登录字号、紧凑账号窗口、圆角晕染高亮、选中未选中同字号字重、深蓝紫侧栏及浮动闪烁星点、完整换行原始日志14px/24px可读性、关于页环绕布局，减少动态效果兼容。
- 修改 `web/tests/workspace.spec.js`：115真实新窗口模拟跳转/回填、移动无模式切换、图标一致、输入框等尺寸无标题与版本页脚、短尾、栏目不跳宽、紧凑账号、底部按钮及最新提示断言。修改 `README.md`、`docs/design-system.md`、`AGENTS.md`同步说明。
- 验证：Go全量测试、go vet、前端构建通过，首轮八项Playwright通过并检查登录桌面、账号窗口和关于页手机截图；补充断言后的最终结果以本轮工具输出为准。无真实115账号/代理或Docker联调，授权仍需可信代理，不宣称真实授权验证完成。
- 按用户要求提交推送，仅源码测试文档，不含配置密钥与产物；未操作15151服务。15152前端已由新构建更新，已有预览后端尚未重启，server_use变动需运行新后端生效。
- 最终补充断言后八项Playwright全部通过（36.6秒），栏目切换宽度一致、最新提示和底部按钮验证通过。

## 2026-10-04：圆角选择器、栏目悬浮态与中心引力阱

- 新增 `web/src/components/GravityWell.vue`：Canvas投影下沉膜面，同心网格和径向线条形成凹陷，暗部增强纵深；响应式尺寸、DPR上限2，ResizeObserver按尺寸重绘并在卸载清理，无额外持续动画循环。
- 修改 `web/src/components/LoginUniverse.vue`、`web/src/pages/AboutPage.vue`：中心图标复用引力阱；关于页图标增加独立容器。OpenList、WebDAV、本机存储白底框统一25%圆角比例，接近夸克原始图标，不增加其他网盘外框。
- 修改 `web/src/App.vue`、`web/src/pages/FilesPage.vue`、`web/src/pages/LogsPage.vue`：移除顶栏主题/通知/账号、文件刷新、日志刷新和模式切换的title，保留aria-label和键盘行为。
- 修改 `web/src/style.css`：下拉选择器触发框12px圆角，保留原生弹出菜单及平台行为；侧栏和页面栏目非选中悬浮使用弱于选中态的圆角晕染，文字不改变尺寸；关于图标容器样式。
- 修改 `web/tests/workspace.spec.js`：多Canvas场景精准定位星空；校验引力阱可见且像素非空、圆角比例、无悬浮title和选择器圆角。修改 `docs/design-system.md`、`AGENTS.md`同步规范记录。
- 验证：前端构建、git diff --check通过，八项Playwright全部通过（37.3秒），包含多视口布局、Canvas动态、减少动画偏好及既有业务流程；查看登录桌面和关于页手机截图确认凹陷网格不遮挡内容。本轮纯前端未重复Go测试，无Docker或真实网盘联调声明。
- 按用户要求提交推送，仅源码测试文档；15152沿用新前端构建，未操作15151、用户配置或生成仓库exe。实际提交与推送结果以Git输出为准。

## 2026-10-04：下方光晕、流星范围与文件服务归类

- 修改 `web/src/components/GravityWell.vue`：移除膜面网格和径向线，改为图标下方柔和椭圆光晕及暗心，登录和关于页共用，保留响应式Canvas重绘与清理。
- 修改 `web/src/meteor.js`：起点横向覆盖星空顶部从三分之一至右边，纵向限定顶部1%–9%，拖尾改为行程4%–6.5%，保留三档随机时长、统一倾角及渐隐。
- 新增 `web/src/components/FileTabs.vue`：文件管理、WebDAV、本地挂载三个栏目，带图标、选中态和无障碍标记。修改 `web/src/App.vue`：侧栏合并为文件服务，九个主入口，文件服务默认浏览文件，内部分发WebDAV与挂载预留页；面包屑与侧栏保持文件服务选中。修改 `web/src/main.js`：旧/webdav和/mounts重定向到/files下对应栏目。
- 修改 `web/src/style.css`：登录表单最大350px；账号密码和会话输入框同宽40px高；放大DAV云轮廓；栏目光晕独立伪元素圆角，底边保持水平直线，保留悬浮态。
- 修改 `web/tests/workspace.spec.js`：流星起点与尾长范围、九项侧栏、文件服务默认栏目/三个栏目/旧地址跳转、账号输入等宽等高、底线直角断言；既有Canvas像素、图标加载、动画、多视口和业务流程继续覆盖。
- 修改 `README.md`、`docs/design-system.md`、`AGENTS.md`：导航归类和视觉规范、功能边界说明。
- 验证：前端构建、差异格式检查通过，八项Playwright全部通过（37.6秒）；查看登录桌面、关于页手机、WebDAV文件服务桌面截图。纯前端改动未重跑Go测试；无Docker或真实网盘联调声明，本地挂载仍未实现。
- 按用户要求提交推送，不包含用户配置、密钥、截图或构建产物；15152前端更新，未操作原15151后端。提交推送结果以Git输出为准。

## 2026-10-04：115新窗口授权与存储卡片操作菜单

- 修改 `web/src/pages/StoragePage.vue`：115 Open 默认使用 LitePan OAuth 服务地址，点击获取 TOKEN 立即打开独立窗口，申请成功后自动跳转 115 登录授权页并轮询回填令牌；授权窗口关闭会提示失败，授权设置折叠为可选替换入口。添加说明明确第三方授权服务会接收本次令牌。
- 修改 `web/src/pages/StoragePage.vue`、`web/src/style.css`：存储卡和虚线添加卡统一固定尺寸；移除卡片底部测试、浏览、删除操作及“待验证”状态；右上角三点菜单按编辑存储、测试连接、删除存储排列，使用图标与文本，支持外部点击和Escape关闭。
- 修改 `web/tests/workspace.spec.js`：增加卡片尺寸相等、无待验证、无底部操作、三点菜单顺序/截图/删除确认、Escape关闭，以及默认 LitePan OAuth 地址和新窗口授权跳转回填断言。
- 修改 `README.md`、`AGENTS.md`：同步115新窗口授权边界、默认授权服务和卡片操作方式。
- 验证：前端构建通过；八项Playwright全部通过（38.5秒），覆盖115授权模拟、存储卡菜单和既有多视口业务流程。未进行真实115账号、第三方OAuth或Docker联调；未改15151服务、用户配置或生成仓库exe。
- 按用户要求提交推送，本次仅源码、测试和文档，推送结果以Git输出为准。

## 2026-10-04：淡化光晕与缩小登录控件

- 修改 `web/src/components/GravityWell.vue`：桌面光晕宽100px、平板80px、手机76px，降低透明度峰值至56/255，移除明显暗心环，紧贴图标下方；登录和关于页共同生效。
- 修改 `web/src/style.css`：登录表单最大宽310px，账号密码框高46px、间隔18px，登录按钮高44px；输入文字16px、按钮15px。只作用于登录，不改变账号设置窗口。
- 修改 `web/tests/workspace.spec.js`：增加光晕宽度/透明度上限与非空像素断言，确认登录表单宽度及输入框、按钮高度。
- 修改 `AGENTS.md`：记录本轮文件、功能与验证。前端构建及八项Playwright测试通过（38.2秒），查看登录桌面截图确认淡光与缩小控件。纯前端未重跑Go测试，未修改用户配置或后端。
- 15152预览沿用新前端构建。本轮用户未要求推送，未提交或推送。

## 2026-10-04：存储卡与添加卡共同降低高度并推送

- 修改 `web/src/style.css`：已添加存储卡与虚线添加卡共同从172px降至136px，保持等宽等高；压缩内边距、状态和目录间距，错误行移除底部留白，长内容省略以避免撑高。
- 修改 `web/tests/workspace.spec.js`：卡片等宽等高断言增加固定136px高度检查；保留上一轮登录尺寸和淡光像素测试。
- 修改 `AGENTS.md`：记录本轮范围和验证。前端构建、八项Playwright全部通过（37.8秒），查看存储桌面截图确认两种卡片共同变矮、内容无溢出。纯前端未重跑Go测试。
- 按用户要求，将本轮卡片调整与上一轮尚未提交的 `web/src/components/GravityWell.vue` 淡光、登录控件样式及测试一并提交推送；不包含配置、密钥、构建或截图产物，不操作15151后端。推送结果以Git输出为准。

## 2026-10-04：关于页弧形背景、移除版本展示与恢复栏目细线

- 修改 `web/src/pages/AboutPage.vue`：移除版本号展示及初始 `/version` 读取，删除不再需要的版本和错误状态；保留手动检查更新及成功、失败提示气泡。
- 修改 `web/src/style.css`：关于页背景独立裁切为浅弧形顶部，保留环绕图标和底部按钮；清除关于页布局继承的gap及栏目下边距，使背景从栏目下方开始。栏目底线由3px恢复为历史2px直线，保留独立圆角晕染与一致字体。
- 修改 `web/tests/workspace.spec.js`：增加关于页无版本文字、桌面/手机栏目与背景无额外间距、弧形背景和2px选中底线断言，继续覆盖更新气泡、图标动画和Canvas动态像素。
- 修改 `AGENTS.md`：记录本轮文件、入口、功能和验证。前端构建、八项Playwright测试全部通过（37.8秒）；查看桌面及手机关于页截图，背景、文字和操作按钮正常。纯前端未重跑Go测试，未进行Docker或真实网盘联调。
- 未修改用户配置、运行数据或15151后端；15152预览使用新前端构建。本轮未要求提交推送，未提交或推送。

## 2026-10-04：关于页图标环绕与描述分区并推送

- 修改 `web/src/components/LoginUniverse.vue`：关于页的装饰模式使用独立320px高、最大680px宽的轨道区域；以太图标和淡光恢复在轨道中心，七种存储图标绕同一中心缓慢运行，随宽度缩放椭圆半径。登录模式原有轨道参数不变。
- 修改 `web/src/pages/AboutPage.vue`：移除文本区中重复的以太图标与光晕，名称和项目描述单独置于轨道区域下方。
- 修改 `web/src/style.css`：关于文案从轨道下方开始，移除旧的about-emblem样式，保持底部更新与仓库按钮；沿用上一轮弧形背景、隐藏版本及栏目细线。
- 修改 `web/tests/workspace.spec.js`：桌面和手机检查以太中心位置、存储图标边界、轨道与描述不重叠，保留星空动态像素及更新操作验证。
- 修改 `AGENTS.md`：记录本轮文件与验证。前端构建、八项Playwright测试全部通过（38.0秒），检查关于页桌面及手机截图。纯前端未重跑Go测试；未进行Docker或真实网盘联调。
- 按用户要求将本轮及上一轮未提交的关于页调整一并提交推送；不包含配置、密钥、构建和测试产物，不操作15151后端。实际提交和推送结果以Git输出为准。

## 2026-10-04：日志圆角筛选与存储池拖拽排序

- 新增 `web/src/components/RoundedSelect.vue`：自定义圆角选择器，支持键盘上下/Home/End/Enter/Escape、当前项标记、点击外部关闭和展开/收起过渡动画。
- 修改 `web/src/pages/LogsPage.vue`：日志级别和模块筛选改用圆角选择器；搜索栏固定为紧凑宽度，避免占满日志工具栏。
- 修改 `web/src/pages/StoragePage.vue`：已添加存储池改为裸存储类型图标、右侧自定义名称和下方类型；移除卡片中缓存、目录和状态信息；三点操作保留；支持鼠标拖拽及 Alt+方向键调整顺序。
- 修改 `internal/app/server.go`：新增受保护的 `POST /api/storages/reorder`，在加密状态中移动存储池顺序并记录操作日志。
- 修改 `web/src/style.css`：补充圆角选择器、弹出面板动画、拖拽态和裸图标卡片样式，并适配日志移动端工具栏。
- 修改 `web/tests/workspace.spec.js`：适配自定义选择器交互，增加筛选持久化、弹出层和圆角验证；保留存储卡片布局与业务流程检查。
- 新增 `internal/app/storage_order_test.go`：验证排序鉴权、方法限制、向前/向后/原位移动、无效ID拒绝及重新加载加密状态后的顺序。
- 验证：Go格式化、全量Go测试、go vet、前端构建通过；九项Playwright测试通过（40.3秒），覆盖真实拖拽、键盘排序、刷新持久化、圆角筛选键盘操作及桌面/手机布局。检查卡片和日志截图，补充等待展开动画完成再截图的断言。未进行Docker或真实网盘联调。
- 按用户要求提交推送；不含配置、密钥或构建测试产物，不操作15151实例。15152仅前端构建已更新，其旧后端需重启新版程序才能使用排序接口；本轮排序端到端在隔离15159测试服务验证。

## 2026-10-05：收紧存储卡片高度与图文对齐

- 修改 `web/src/style.css`：存储卡片与添加卡片由136px共同降至84px；图标48px，右侧名称17px与类型12px上下排列，两行文本与图标整体垂直居中。添加卡片改为图标和文字横排，保持等宽等高，保留三点菜单和拖拽排序。
- 修改 `web/tests/workspace.spec.js`：更新84px卡片等高断言，增加图标高度、图文高度匹配、左右顺序及垂直居中验证。
- 修改 `AGENTS.md`：记录本轮变化。前端构建及九项Playwright测试通过（49.3秒），查看桌面存储截图确认底部空白已收紧。纯样式修改未重跑Go测试，未进行Docker或真实云盘联调。
- 未修改配置、运行数据或15151实例；本轮未请求推送，未提交推送。

## 2026-10-05：v0.1.1 以太链接、日志管理与文件工作区

- 发布规则：用户要求未指定版本时每次发布加一个补丁号，本轮从0.1.0更新为0.1.1；新增 `VERSION`，修改 `internal/app/version.go`、`Dockerfile`、`.github/workflows/docker-amd64.yml`、`web/package.json`、`web/package-lock.json`。镜像从VERSION取版本并增加对应镜像标签；Docker和CI安装FFmpeg。
- 新增 `internal/app/links.go`：加密MediaLink模型、受保护增删改查、热启停独立端口、冲突回滚、四种跳转策略、UA屏蔽、真实播放流水接口、共享音频缓存；缓存位于data/cache/link，不使用AetherLink原管理后台。
- 新增 `internal/app/link_auth.go`：媒体解析前使用播放器自身凭据请求上游授权，不注入服务API Key，防止通过配置密钥绕过媒体登录；校验缓存命中之前也执行。
- 新增 `internal/app/config_backup.go`：管理员POST导出配置ZIP，包含密文状态、密钥与JSON规则，拒绝跟随符号链接，排除日志和缓存，总大小限制32MB；UI明确提示备份包含敏感凭据。
- 修改 `internal/app/model.go`、`internal/app/server.go`：Links加密存储、日志设置默认15天/20000条、API与启动退出挂接；日志从加密配置迁出。修改 `internal/app/logs.go`：迁移和读取data/log/system.json，按天数及数量联合裁剪、原子保存并继续stdout输出；以链归入links模块。修改 `internal/app/tasks.go`：每小时执行日志过期维护。
- 新增 `internal/app/links_test.go`、`internal/app/link_playback_test.go`、`internal/app/config_backup_test.go`，修改 `internal/app/logs_test.go`：覆盖监听启停、端口冲突回滚、加密恢复、三种媒体模拟302、缓存与播放器鉴权、日志独立恢复及保留限制、备份鉴权与内容。
- 按用户指定从本地AetherLink机械迁入 `internal/linkcore/config/{config.go,config_test.go,fnos_test.go,intranetcidrs_test.go,trustedproxy_test.go}`、`logx/{logx.go,redact.go,redact_test.go}`、`pathmap/{pathmap.go,pathmap_test.go}`、`proxy/{proxy.go,proxy_test.go,playback_cache_test.go,fnos_test.go,clientip.go,clientip_test.go}`、`resolver/{resolver.go,resolver_test.go,cache.go,cache_scope_test.go,localnet.go}`、`stats/{stats.go,stats_test.go}`、`strm/{strm.go,strm_test.go}`、`upstream/{upstream.go,upstream_test.go,abs.go,emby.go,fnos.go,fnos_test.go,cache_scope.go,cache_scope_test.go}`、`urlx/{urlx.go,urlx_test.go}`。保留来源注释与测试，将内部导入改为aether/internal/linkcore；修改Go模块 `go.mod`、`go.sum`加入yaml依赖。
- 核心适配：`internal/linkcore/logx/logx.go`增加系统日志sink；`proxy/proxy.go`增加共享、可取消的音频缓存与M4B转M4A、FFmpeg输入协议限制、生成及活动播放保护；`resolver/cache.go`修复持久化错误变量作用域。新增 `proxy/aether_audio_test.go`，实际生成M4B、FFmpeg转码和复用缓存。沿用两小时闲置、十分钟清理、24小时空目录清扫与源项目TTL/LRU规则。
- 新增 `web/src/pages/LinksPage.vue`：以链管理/直链缓存栏目，两步创建三类型、独立端口/地址/凭据/跳转/UA表单、媒体图标启停、编辑删除、真实播放流水。新增 `web/public/media/{abs.png,emby.png,fnmovie.png,SOURCES.md}`，素材来自指定本地项目并记录归属。
- 修改 `web/src/App.vue`：接入真实以太链接页与日志管理设置；修改 `web/src/pages/SettingsPage.vue`：日志管理居中窗口、保留天数及条数；修改 `web/src/pages/ToolsPage.vue`：新增可执行配置备份卡片，其他插件保持待实现。
- 修改 `web/src/pages/FilesPage.vue`：可点击路径、按账号/存储池收藏及移除、侧栏展开、列表/网格切换和浏览器持久化、图标反映当前视图，文件窗口圆角。参考LitePan FileBrowser的收藏栏交互，未复制其代码。
- 修改 `web/src/components/RoundedSelect.vue`：支持禁用及空选项保护；`web/src/pages/StoragePage.vue`、`TasksPage.vue`和文件页全部改用圆角弹出选择器。修改 `web/src/style.css`：以链、日志设置、文件网格和收藏布局及手机工具栏；包含上一轮未提交的84px紧凑存储卡。
- 修改 `web/src/components/Icon.vue`：WebDAV三瓣云轮廓扩大、共享DAV字样，增加文件视图与收藏图标；修改 `web/src/meteor.js`：流星起点覆盖顶部1/6至右边；修改 `web/src/components/LoginUniverse.vue`：缓存Canvas绘制带明暗与坑纹的月球，复用登录和关于页。
- 修改 `web/src/pages/AboutPage.vue`：更新气泡带当前版本号，确认一致时仍显示已是最新，不误报无法比对结果；不恢复关于页常驻版本。
- 修改 `web/tests/workspace.spec.js`，新增 `web/tests/z-links.spec.js`：适配圆角选项及11张工具卡、流星范围，覆盖以链保存/启停/编辑脱敏/跳转持久化、日志设置、文件收藏与网格、备份下载、桌面手机截图。
- 修改 `.gitignore`、`.dockerignore`：仅忽略运行配置，确保linkcore/config源码参与提交和镜像构建。修改 `README.md`、`THIRD_PARTY_NOTICES.md`、`AGENTS.md`：更新功能、部署、敏感缓存、来源许可边界和逐文件记录；本地AetherLink未带LICENSE，不虚构许可声明。
- 当前验证：全量Go测试、go vet通过；本机FFmpeg实际M4B转换及复用通过；10项Playwright通过（46.9秒），查看以链、月球及文件桌面/手机截图后进一步改善手机工具栏。最终重跑结果在下条补充。
- 限制：无真实媒体库、真实iOS或Docker内联调；M4A适配覆盖STRM音频目标和映射到容器内的ABS原始AAC/WMA/M4B文件，其他普通非STRM媒体仍交原服务器；通用媒体标签补全、备份恢复未实现。原15151服务与用户数据不改动，15152旧预览后端尚未更新。
- 按用户要求连同此前84px卡片调整提交发布v0.1.1并推送main及版本标签，结果以Git工具输出为准。
- 最终验证补充：Go全量测试、go vet、前端v0.1.1构建通过；10项Playwright最终全部通过（44.0秒）。查看手机文件网格截图确认工具栏已重新排列、搜索框不再挤窄。模拟ABS/Emby/飞牛302及未授权拒绝、真实FFmpeg M4B转换与缓存复用均通过。CI显式写入OCI版本标签避免分支名覆盖镜像版本。

## 2026-10-06：目录导航与 CAS 生成验证补充

- 本轮逐文件实现记录见本文件顶部同日条目；最终 `go test ./... -count=1`、`go vet ./...`、`npm run build`、`git diff --check` 通过，11项 Playwright 全部通过（47.4秒）。
- 已查看本地 CAS 创建表单截图，名称与生成方式、源目录与生成目录两列对齐，没有重叠。本地实际生成 CAS 并校验 SHA256；未声明云盘真实生成联调完成。
- 本轮未提交、推送或升版本；测试运行于隔离15159且已退出，未更新15151或既有15152后端。

## 2026-10-06：纠正 CAS 任务为仅生成 CAS

- 修改 `web/src/pages/TasksPage.vue`：移除 CAS 操作选择及转 STRM 入口，保存时统一 generate；还原保留时长恢复显示，默认12h。修改 `internal/app/server.go`：缺省操作为 generate，拒绝提交 restore；修改 `internal/app/tasks.go`：startTask 拒绝旧转换任务，要求重新选择视频源目录保存，避免自动改变旧任务行为；生成结果写入保留时长。
- 修改 `internal/app/cas.go`：CASInfo 增加可选 retentionHours 元数据。不宣称外部消费端已经支持该字段或支持直接播放；旧签名播放解码与临时文件清理兼容保留，不删除已有输出。
- 修改 `internal/app/cas_generate_test.go`：覆盖旧转换提交/执行拒绝及新默认生成；修改 `internal/app/cas_test.go`、`internal/app/tianyi_test.go`：既有生命周期测试显式标为旧解码兼容，不再依赖新任务默认行为。
- 修改 `web/tests/workspace.spec.js`、`web/tests/zz-cas.spec.js`：断言无 CAS 操作选择器，保留时长默认12h，本地真实生成及目录导航回归。修改 `README.md`、`AGENTS.md`：撤销上一条“双操作”的当前功能说明，区分新生成和旧播放兼容。
- 验证：首次 Go 回归发现两个生命周期测试依赖旧默认值，调整夹具后全量 `go test ./... -count=1` 通过；go vet、前端构建通过；11项 Playwright 通过（48.7秒）。未进行真实云端/媒体库 CAS 直接播放联调，Base64 JSON CAS 不等于任意播放器可识别的 URL 文本。
- 未提交推送、未升版本、未修改用户数据或15151服务。

## 2026-10-06：发布 v0.1.2 并补推上一版本

- 按用户“推送”要求，修改 `VERSION`、`internal/app/version.go`、`Dockerfile`、`web/package.json`、`web/package-lock.json`，补丁版本从0.1.1同步提升到0.1.2；修改 `AGENTS.md` 追加发布记录。
- 提交范围为前两轮已逐文件记录的目录名称/回根导航、本地 CAS 生成及“仅生成 CAS”纠正、测试与文档；一并推送此前因网络失败仅在本地的 v0.1.1 提交与标签。不包含 config、data、凭据、exe、日志、依赖或构建测试产物。
- 已成功 fetch 远端，main 无分叉；本轮全量 Go 测试、go vet、前端0.1.2构建、差异格式检查通过。浏览器沿用上一轮同一业务代码11项通过（48.7秒）的结果，本轮仅更新版本元数据，不重复浏览器测试。
- 真实云盘、媒体库直接识别 CAS 和 Docker 联调限制保持不变。计划推送 main、v0.1.1、v0.1.2，最终远端确认以 Git 输出为准；未操作15151服务或用户配置。

## 2026-10-06：v0.1.3 工作区交互、文件操作与加密配置备份

- 修改 `web/src/style.css`、`web/src/components/LoginUniverse.vue`：登录星轨按桌面高度收缩，最低520px后允许页面滚动；关于页轨道随可用高度调整，星空铺满侧栏右侧内容区，深浅主题保持星空与可读栏目；表单输入、数值、下拉统一40px，菜单收窄、收藏夹240ms展开收起、选中态与双三角排序。
- 新增 `web/src/components/SecretInput.vue`：单行密码输入及尾部眼睛显隐，不在外层再套焦点框。修改 `web/src/components/Icon.vue`：DAV三瓣云轮廓、GripVertical/Power/Copy/FolderInput图标。
- 修改 `web/src/pages/StoragePage.vue`：Cookie/Authorization/Token 使用单行显隐；卡片非按钮点击编辑，450ms长按后才允许原生拖拽，清晰六点手柄、Alt方向键保留；右键和三点共用菜单，删除前加入启停项。保留既有拖拽排序接口，未改变配置数据。
- 修改 `web/src/pages/LinksPage.vue`：反代端口默认空，编辑窗口复用存储的compact wide尺寸/隐藏滚动条；右键编辑、测试连接、启停、删除菜单。新增 `internal/app/link_test_connection.go`、`link_test_connection_test.go`：管理员POST连接探测，十秒超时、不跟随重定向、非2xx/3xx报错，测试鉴权/不存在/离线；仅探测服务可达性，不声称凭据验证。
- 修改 `web/src/pages/TasksPage.vue`：删除任务搜索栏和搜索过滤。修改 `web/src/pages/SettingsPage.vue`：日志管理排到关于前。
- 修改 `web/src/pages/FilesPage.vue`：工具栏与文件框一体；收窄存储下拉并移除返回上层按钮；收藏夹标题右侧切换当前目录收藏与高亮，移除逐项删除按钮；单击选中/双击进入、Shift范围/Ctrl增选、F2或右键内联重命名与回车提交、右键移动/复制/确认删除；目标选择限制同类型池，列表四字段双向排序，无操作列。保留右键复制播放链接。
- 新增 `internal/app/file_actions.go`、`file_actions_test.go`：受保护文件操作，单次最多200项；本地os.Root约束、根目录/符号链接/父子混选/自身与子目录/目标重名检查、同池原生移动、跨本地池流式复制后删除；文件复制校验大小时间，失败清理新建副本并报告已完成数；默认移入源根`.aether-trash/<随机ID>/`，永久删除需存储显式配置。任务执行时拒绝文件写入，成功失败均清目录缓存。
- 新增 `internal/app/cloud_file_actions.go`、`cloud_file_actions_test.go`：独立实现LitePan本地协议参考的115/夸克/移动/天翼同池重命名、移动、复制及回收站删除；源目录实时核对ID、目标重名拒绝、天翼/夸克异步轮询有超时。移动/天翼支持显式永久删除；115/夸克永久删除直接拒绝，不照搬可能误清其他回收记录的逻辑。模拟覆盖四种提供商重命名/删除/不存在及跨池拒绝，移动复制等真实云端流程待联调。
- 修改 `internal/app/config_backup.go`、`config_backup_test.go`：原明文ZIP替换为版本化`.aether`加密包，PBKDF2-HMAC-SHA256 600000轮/随机16字节盐、AES-256-GCM/随机nonce；导出State及JSON规则，日志不包含，master.key不外发；导入校验密码/认证标签/大小/文件数/路径，将待导入内容用当前密钥加密写到import.pending。启动时应用，普通写失败回滚；测试密码错误、篡改、空密码、路径穿越、存储与以链恢复及磁盘持久化。
- 修改 `internal/app/server.go`：新增文件操作/以链探测/配置导入受保护路由，在监听及调度前应用待导入配置。修改 `internal/app/logs.go`：文件写操作归入files模块。
- 修改 `web/src/pages/ToolsPage.vue`：配置备份排最后，导入/导出独立流程；导出设置密码，导入先选文件再密码；错误不关闭流程，导入成功明确重启后生效、使用备份账号。
- 修改 `web/tests/workspace.spec.js`、`web/tests/z-links.spec.js`：适配菜单项、真实长按拖拽、双击目录、收藏位置、无圆弧关于背景及加密导出。新增 `web/tests/zzz-workspace-actions.spec.js`：1920×1080/1366×768/1280×600/1024×600/800×480登录截图、卡片右键/点击、输入下拉等高、空端口、F2/Shift/复制/排序、关于完整可见、备份错误密码及导入成功。套件新增登录触发现有限流时等待一分钟重试，不削弱服务限流。
- 修改 `README.md`、`THIRD_PARTY_NOTICES.md`、`AGENTS.md`：同步功能/协议参考及边界。修改 `VERSION`、`internal/app/version.go`、`Dockerfile`、`web/package.json`、`web/package-lock.json`：按要求发布补丁版本0.1.3。
- 验证过程：后端全量测试、go vet、前端构建通过；新增浏览器用例初次因登录限流失败，明确处理后12项通过（2.3分钟）。已查看1280×600登录与1366×768关于截图，修正关于选中栏目低对比；最终版本全量重跑结果后补。
- 限制：OpenList/WebDAV写操作和跨云端池传输尚未实现，同类型不等于跨账号已支持；四云写协议无真实凭据联调，不宣称全部操作云端验证。云端异步操作及多文件操作失败可能部分完成，需刷新核对。新备份不导入旧明文ZIP；导入重启生效且覆盖同名配置，备份外JSON保留；普通错误回滚不等于断电事务保证，须保留原始备份。收藏与视图属于浏览器偏好，不在服务端备份内。未本地构建Docker，未修改用户config/data或原15151服务。
- 最终验证：v0.1.3全量Go测试、go vet、前端构建、git diff --check通过；12项Playwright最终全部通过（2.0分钟），低高度截图及关于页/文件操作截图检查完成。预览使用独立`127.0.0.1:15153`，PID32676，程序、日志与隔离配置位于系统临时目录`aether-preview-v013`，健康检查ok；仓库未生成exe，原服务与用户数据不改动。按用户要求提交main及v0.1.3标签，推送结果以Git确认。

## 2026-10-06：v0.1.4 栏目、星空、卡片与以链排序

- 修改 `web/src/components/TaskTabs.vue`、`web/src/App.vue`：任务栏目增加语义图标，预留任务页复用 task-heading，固定栏目间距和位置。
- 修改 `web/src/components/Icon.vue`：重绘等比例三瓣 DAV 云，新增竖向三点、播放流水及配置归档图标。修改 `web/src/components/LoginUniverse.vue`：月球上移至右上区域。
- 修改 `web/src/pages/StoragePage.vue`：移除六点手柄及名称悬浮提示，竖三点居右居中，Cookie 独占整行；保留长按拖拽和启停行为。
- 修改 `web/src/pages/LinksPage.vue`：四列紧凑卡片、点击编辑、横三点菜单、450ms 长按排序和 Alt 方向键排序；API Key/密码复用显隐输入框；播放流水增加处理类型过滤，筛选与刷新搜索工具栏融入列表，参考本地 AetherLink LogsView 的 outcome 分类。
- 修改 `internal/app/server.go`、`internal/app/links.go`：新增受保护 POST /api/links/reorder，使用以链互斥锁和加密状态持久化，仅调整顺序不重启反代监听。新增 `internal/app/link_order_test.go`：鉴权、方法、前后排序、同项、无效 ID 不修改及重读磁盘验证。
- 修改 `web/src/pages/ToolsPage.vue`：配置备份使用 FileArchive 图标，导入/导出左右分布。
- 修改 `web/src/style.css`：栏目统一15px及等宽选中态；浅灰主区域、纯白卡片；收藏仅浅黄色实心星，目录浅选中态及弱边线；日志底部贴近视口；放大登录轨道并保留桌面响应式；关于星空延伸到透明顶栏和原位栏目后方。
- 修改 `web/tests/workspace.spec.js`、`web/tests/z-links.spec.js`、`web/tests/zzz-workspace-actions.spec.js`：日志底部位置、栏目切换坐标、四列布局、凭据显隐、流水过滤、长按以链排序及刷新持久化、透明关于顶栏；继续验证桌面/移动截图与画布动态像素。
- 修改 `VERSION`、`internal/app/version.go`、`Dockerfile`、`web/package.json`、`web/package-lock.json`：补丁版本递增至0.1.4。修改 `AGENTS.md` 记录本轮全部文件；提交和标签说明使用中文。
- 验证：Go全量测试、go vet、前端构建通过。首轮浏览器11项通过，发现放大轨道导致900px高桌面溢出，调整宽高比后全量12项通过（1.9分钟）；检查登录、关于和以链桌面截图。差异格式检查通过。
- 限制：未做真实媒体库、网盘或Docker联调，本轮不变更转码协议；拖拽沿用桌面原生拖拽，提供键盘排序替代，不宣称移动端触控拖拽已联调。未操作原15151和用户配置；隔离15153预览更新为临时目录中的 aether-v014.exe，PID33528，不生成根目录exe。按用户要求推送main及v0.1.4，实际结果以Git确认为准。

## 2026-10-06：v0.1.5 真实凭据显隐、以链路由和播放流水

- 新增 `internal/app/secrets.go`，修改 `internal/app/server.go`：管理员 POST 单字段凭据读取接口 `/api/storages/{id}/secret`、`/api/links/{id}/secret`；白名单校验、no-store、沿用鉴权和同源写入检查。普通列表继续返回脱敏值，不记录凭据正文。新增 `internal/app/secrets_test.go`：Cookie/Token/Authorization/密码/API Key、鉴权、方法、非法字段、资源不存在、列表脱敏及日志不泄漏测试。
- 修改 `web/src/components/SecretInput.vue`：点击显示时按需读取真实字段，加载禁用眼睛、错误提示、卸载或用户修改后忽略旧响应；新填入值直接显隐，不覆盖编辑。修改 `web/src/pages/StoragePage.vue`：所有秘密字段绑定读取路径，Cookie固定整行，天翼/WebDAV密码也使用统一控件。
- 修改 `web/src/main.js`、`web/src/App.vue`、`web/src/pages/LinksPage.vue`：以链管理 `/links/manage`、直链缓存 `/links/cache`，旧 `/links` 重定向；以链凭据真实显隐，跳转模式排在凭据之后；长按450ms采用全局指针移动和落点排序，取消处理、保留Alt方向键替代，避免动态启用浏览器原生拖拽失效。
- 修改 `web/src/pages/LinksPage.vue`：参考本地 AetherLink LogsView，播放流水展示时间、上游、UA、结果、目标、客户端IP、缓存状态、有效期、耗时九列；UA/目标可复制、完整内容悬浮查看、恢复命中区分、纳秒换算毫秒；固定52px行高虚拟列表、搜索筛选重置滚动、刷新收缩数据时回到有效位置。后端仍对目标地址敏感内容脱敏，复制不绕过脱敏。
- 修改 `web/src/pages/FilesPage.vue`：根目录禁止收藏，过滤旧根目录收藏显示，移除底部项目数与缓存状态。修改 `web/src/style.css`：取消根据password/text类型改变表单列宽；文件视图撑高、关于页与其他设置使用相同栏目坐标、星空继续延伸顶栏；低高度桌面放大轨道，播放流水九列与滚动窗口样式。
- 修改 `web/src/components/Icon.vue`：DAV改为偏心圆滑云形。修改 `web/src/components/LoginUniverse.vue`：加宽粒子星河分布，登录与关于同步。
- 修改 `web/tests/z-links.spec.js`、`web/tests/zzz-workspace-actions.spec.js`：真实API Key/飞牛密码/夸克Cookie/115双Token/OpenList Token显隐与坐标不变、Cookie整行；真实鼠标长按移动排序及刷新持久化；以链路由刷新、九列字段、1500条虚拟滚动和搜索、根目录收藏禁用、关于栏目位置不变、桌面/移动截图。
- 修改 `README.md`：记录显隐安全边界、地址与流水操作。修改 `VERSION`、`internal/app/version.go`、`Dockerfile`、`web/package.json`、`web/package-lock.json`：补丁版本0.1.5；修改 `AGENTS.md` 逐文件记录。
- 验证：Go全量测试、go vet、前端构建通过；首轮11项通过，新Cookie整行断言漏算眼睛按钮38px，改为校验完整输入控件后全量12项通过（1.3分钟）。已查看800×480登录、1366×768关于和播放流水截图；检查后进一步收紧流水列宽，最终回归结果后补。
- 限制：无真实云盘/媒体库和Docker联调；桌面指针拖拽已验证，不宣称真实手机触摸拖拽验证完成。凭据只向已登录管理员按需返回，需HTTPS及保护会话；后端仅为可逆加密的存储/媒体库凭据提供显示，不返回管理员密码哈希或主密钥。未修改用户config/data或原15151服务。提交与标签说明使用中文，计划推送main与v0.1.5。

## 2026-10-06：v0.1.6 CAS账号绑定、媒体映射与工作区完善

- 修改 `internal/app/model.go`、`internal/app/server.go`、`internal/app/cas_generate.go`、`internal/app/tasks.go`：Task增加casBindingId，保存和执行时校验绑定为启用的原生移动/天翼；源仅本地或同类型云盘。移动输出provider=mobile/SHA256，天翼输出provider=tianyi/MD5，不写入凭据。旧云端未绑定任务按源账号兼容，旧本地须重新绑定。修改 `internal/app/cas_generate_test.go`：双类型真实本地输出、缺失账号和跨类型拒绝。
- 修改 `internal/app/links.go`：以链增加上游/Aether媒体目录与路径映射，默认读取白名单补齐/audiobooks、/media、/strm、/NetDisk，保留/data运行目录和/mnt；配置校验跨平台绝对路径并拒绝根目录。fallback UA为Aether，日志桥接及流水错误中的AetherLink替换为Aether。新增 `internal/app/link_paths_test.go`：映射、白名单、根目录拒绝。修改 `internal/app/link_playback_test.go`：模拟ABS返回/audiobooks路径，真实STRM置于不同临时目录，经映射取得302。
- 修改 `internal/linkcore/resolver/resolver.go`、`internal/linkcore/proxy/proxy.go`：读不到指针的提示改为检查Aether挂载/映射/权限，不保证挂载后必然302；消除透传日志重复UA前缀。
- 修改 `web/src/pages/TasksPage.vue`：CAS绑定账号选择器、按类型限制源目录、切换绑定重置不兼容源、无绑定禁止保存；任务弹窗复用存储的compact规格。修改 `web/src/components/LoginUniverse.vue`：本机存储采用最外圈轨道。
- 修改 `web/src/components/SecretInput.vue`：已保存且未修改的秘密字段统一八位遮罩，显示后再次隐藏长度不变；选中遮罩后输入替换，实际表单值保留，避免把遮罩保存成真实凭据。
- 新增 `web/src/clipboard.js`：Clipboard API不可用或拒绝时使用临时textarea与execCommand兼容HTTP复制，失败不假报成功并恢复焦点。修改 `web/src/pages/LinksPage.vue`：UA/目标/客户端IP复制、月日时分秒、路径映射表单、名称/模式及地址/端口同行、长按指针捕获、创建时清空映射字段。修改 `web/src/pages/FilesPage.vue`：搜索输入与已提交关键词分离，Enter响应；进入目录清空搜索，复制复用兼容函数，文件名使用自定义全文提示。
- 新增 `web/src/components/OverflowTooltip.vue`，修改 `web/src/App.vue`：全局圆角悬浮全文提示，监听列表截断单元格和显式data-tooltip，延迟显示、Escape/滚动关闭、卸载清理。修改 `web/src/pages/LogsPage.vue`：去掉原生title提示，复用统一提示。
- 修改 `web/src/components/Icon.vue`、`web/src/pages/SettingsPage.vue`：日志管理使用独立FileClock图标。修改 `web/src/style.css`：统一40px任务控件和6px标签间距、隐藏任务弹窗滚动条、放大以链端口、拖拽落点高亮、搜索14px、柔和栏目光晕、夜间星光选中、关于页栏目分隔线隐藏；播放流水视口撑到底部、隐藏滚动条但保留滚动、缩小刷新按钮和圆角全文提示。
- 修改 `web/tests/workspace.spec.js`、`web/tests/z-links.spec.js`、`web/tests/zz-cas.spec.js`、`web/tests/zzz-workspace-actions.spec.js`：CAS绑定筛选与真实输出、任务控件等高/隐藏滚动条、遮罩长度、HTTP复制回退三字段、全文提示圆角、文件搜索Enter、流水时间/底部、以链两列对齐、真实鼠标拖拽回归。
- 修改 `README.md`：绑定兼容边界、挂载白名单、显式映射及HTTP复制说明。修改 `VERSION`、`internal/app/version.go`、`Dockerfile`、`web/package.json`、`web/package-lock.json`：版本递增0.1.6。修改 `AGENTS.md` 记录全部文件。
- 验证：最终Go全量测试、go vet、前端0.1.6构建、差异检查通过；12项Playwright全部通过（2.3分钟）。修正过程中发现Windows的POSIX路径校验、控件标签3px间距和绑定账号测试误认自定义名称，均修复后重跑。查看CAS表单与流水截图，套件同时覆盖登录画布非空/运动及桌面移动响应式。隔离15153预览更新为临时目录aether-v016.exe，PID29404，未生成仓库根exe。
- 限制：用户实际容器挂载、读权限、真实移动/天翼秒传和媒体库未联调，不声称该用户播放已现场恢复；新增映射已模拟302验证。旧本地CAS需选择绑定账号；复制回退仍受浏览器策略限制，测试使用禁用Clipboard API的模拟验证，不宣称所有浏览器均可复制。真实手机触摸拖拽未联调。未改用户config/data及原15151。中文提交并推送main和v0.1.6，实际结果以Git输出为准。

## 2026-10-06：文件服务界面细化、CAS手动选择与FUSE挂载

- 修改 `web/src/App.vue`：品牌改为非导航元素，仪表盘只由侧栏导航项触发；接入 `MountsPage`。新增 `web/src/pages/MountsPage.vue`：挂载配置卡片、创建编辑删除、手动挂载/卸载与状态轮询；源选择支持所有启用池、单池或子目录；容器目录浏览；只读默认关闭、自动挂载默认开启；UID/GID/八进制权限为0/0/0755并排展示。删除需确认，不删除源目录。
- 修改 `web/src/pages/TasksPage.vue`：CAS新增任务不自动绑定存储或选择源，绑定改名为“绑定存储”并放在名称右侧；仅原生移动/天翼可绑定，未绑定不开放源选择，改变类型清除不兼容源。修改 `web/src/components/TaskSourcePicker.vue`：不默认选择首池、可选聚合根；修改 `web/src/components/RoundedSelect.vue`：支持空选项占位提示。
- 修改 `web/src/pages/LinksPage.vue`：隐藏两项媒体路径映射输入，兼容保留旧数据而不破坏既有映射。修改 `web/src/components/OverflowTooltip.vue`：包括data-tooltip在内均只在文本实际截断时显示全文。
- 修改 `web/src/style.css`：淡化文件表格分隔线；栏目换行不横向滚动；规划中的刮削等页减少空白高度；关于星空固定延伸到侧栏右侧圆角后方且顶栏按钮透明；播放流水关闭滚动锚定与滚动链传递、分离表格边框和固定52px行高，防止虚拟列表触底抖动；挂载表单与高级控件样式。
- 修改 `internal/app/model.go`：新增MountConfig及加密State.Mounts，配置备份沿用State序列化自动包含挂载；修改 `internal/app/server.go`：挂载管理鉴权API、容器目录浏览、初始化与自动启动/退出清理。
- 新增 `internal/app/mounts.go`：rclone进程管理、Linux/FUSE检查、mountinfo确认真正挂载、超时错误、正常卸载及退出延迟卸载兜底、启动自动挂载、日志归入storage。私有DAV仅回环监听，随机Bearer令牌通过进程环境传递，不写配置或参数；运行缓存目录不改变。挂载点必须存在且为空，拒绝与系统/config/data/本机存储/其他挂载点重叠，拒绝符号链接；修改/删除前必须卸载。保存不会自动挂载，自动挂载语义仅为服务启动。
- 新增 `internal/app/mount_fs.go`：私有DAV桥复用现有云盘读取与元数据缓存；所有池聚合、选定池或子目录解析，停用池不再返回。云端只读，不支持的写操作拒绝；本机使用os.Root限制读写范围、支持新建/重命名/删除，删除遵循回收站策略。无磁盘VFS读写缓存，原读缓存路径仍预留，不宣称云盘上传或完整POSIX兼容已实现。
- 修改 `Dockerfile`：增加rclone运行依赖，仍仅amd64且版本未变。修改 `THIRD_PARTY_NOTICES.md`：rclone独立进程/MIT来源。修改 `README.md`：挂载行为、目录与传播条件、写入边界、隐藏映射字段兼容和未联调说明。
- 新增 `internal/app/mounts_test.go`：鉴权/持久化/非法权限/非空挂载点/路径重叠、只读/子目录隔离/本地真实读写重命名回收站、聚合池停用、模拟云端读取及写入拒绝。增加显式 `AETHER_TEST_FUSE=1` 的Linux真实挂载读写卸载测试，本机未运行该集成测试。
- 修改 `web/tests/workspace.spec.js`、`web/tests/zz-cas.spec.js`：手动绑定及空源、控件同行与现有任务回归；修改 `web/tests/z-links.spec.js`：触底连续滚动稳定、短文本不提示/截断长文本提示；修改 `web/tests/zzz-workspace-actions.spec.js`：品牌不跳转、挂载默认值/聚合确认/目录选择/保存恢复删除与截图。
- 修改 `AGENTS.md`：本条记录以上所有文件，保留上一轮未提交的302修复。验证：最终 `go test ./... -count=1`、`go vet ./...`、前端构建、Linux/amd64交叉编译通过；12项Playwright全量通过（1.5分钟），首轮旧测试用未截断短地址要求提示而失败，改为长地址后重跑成功。检查关于页与挂载表单截图；其后仅补齐高级数字输入宽度并重新构建，后端卸载兜底与404校验重新全量测试。
- 限制：本机没有可用Docker或WSL发行版，真实FUSE、真实网盘账号和容器传播未联调，不宣称生产挂载成功；云盘写入、磁盘读缓存尚未实现。未改变用户config/data、15151服务，无根目录exe。隔离预览运行于127.0.0.1:15153，程序/配置/日志在系统临时目录aether-mount-preview，进程34344；Linux检查产物已清理。本轮未要求推送，不提交、不递增发布版本。

## 2026-10-06：云盘挂载读写、挂载卡片与 v0.1.7 发布

- 修改 `internal/app/cloud_write.go`、`internal/app/cloud_file_actions.go`、`internal/app/mount_fs.go`、`internal/app/mounts.go`：挂载写入先落到 `/data/cache/mount-upload`，校验完整长度与云端确认结果后发布；接入云端建目录、移动/删除回收站、同存储池改名与跨目录移动，防止覆盖旧文件；PUT 读入错误立即失败，保留失败暂存。
- 新增 `internal/app/cloud_upload_mobile.go`、`internal/app/cloud_upload_tianyi.go`、`internal/app/cloud_upload_115.go`、`internal/app/cloud_upload_quark.go`：按现有驱动协议实现移动 SHA256 分片、天翼 MD5 分片、115 OSS 单片/分片、夸克 OSS 分片上传及目录创建；新增 `internal/app/cloud_write_test.go`：WebDAV 生命周期、回收站、只读、失败暂存和四种上传协议模拟验证。
- 修改 `internal/app/secrets.go`、`internal/app/secrets_test.go`、`web/src/components/SecretInput.vue`：保存密钥按真实字符数显示遮罩，显隐仍只通过管理员按需接口返回明文。
- 修改 `web/src/pages/StoragePage.vue`、`web/src/lib.js`、`web/src/pages/MountsPage.vue`、`web/src/style.css`、`web/tests/workspace.spec.js`、`web/tests/z-links.spec.js`、`web/tests/zzz-workspace-actions.spec.js`：存储类型选项移除额外圆角包装，本地字段改为“本地目录”，WebDAV 默认 `/dav`；存储名称/删除模式同行；文件视图字体放大；存储、挂载、以链卡片增加悬浮描边；挂载卡片三列、启停图标、右键菜单和点击编辑。
- 修改 `README.md`、`Dockerfile`、`VERSION`、`internal/app/version.go`、`web/package.json`、`web/package-lock.json`：同步云盘读写边界、上传暂存策略与 `v0.1.7` amd64 镜像版本。
- 验证：`go test ./... -count=1`、`go vet ./...`、`npm run build`、12 项 Playwright 全量测试通过；上传协议使用模拟服务验证，未使用真实网盘账号、Docker、Linux FUSE 或宿主机传播环境。
- 未完成项：挂载仍要求完整文件顺序写入，不等同于 POSIX 随机写入；真实云盘风控、配额、秒传和生产 FUSE 联调待后续验证。没有生成仓库根目录 exe、没有修改用户 `config/data` 或原 15151 服务。

## 2026-10-06：挂载目录交互与 ABS 新会话播放校验

- 新增 `web/src/components/LocalDirectoryPicker.vue`：本地存储与挂载点复用的640px目录弹窗、固定受视口约束高度、隐藏滚动条但保留滚动、根目录及各级路径点击跳转、异步过期响应保护。
- 修改 `web/src/pages/MountsPage.vue`、`web/src/App.vue`：添加挂载与文件栏目同排右对齐；名称/源/挂载点分三行，目录图标内置输入框，使用共用目录选择器。修改 `web/src/pages/StoragePage.vue`、`web/src/lib.js`：新本地目录默认留空，可浏览容器目录，已有配置不改动。
- 修改 `web/src/components/TaskSourcePicker.vue`：固定窗口及隐藏滚动条，挂载选择根目录时明确显示“选择整个存储池”。修改 `web/src/style.css`：上述布局、折叠标题悬浮高亮、卡片淡描边、夜间滚动条及播放结果差异颜色。修改 `web/src/pages/LinksPage.vue`：超过60分钟的有效期按小时/分钟显示。
- 修改 `internal/app/link_auth.go`：ABS公开会话音轨使用实际播放端点及原始客户端凭据进行有界Range校验，不使用服务密钥越权；保留上游401/403/404和跳转。参考本地 `C:/Develop/AetherLink/internal/upstream/abs.go` 的新会话尚未落库行为，修复Aether额外鉴权层提前404而阻止活跃会话解析的问题。
- 新增 `internal/app/link_auth_test.go`，修改 `internal/app/link_playback_test.go`：根路径/子路径、授权成功与拒绝、不注入服务密钥、新会话详情404后活跃会话回退及真实HTTP302测试。
- 修改 `web/tests/workspace.spec.js`、`web/tests/zzz-workspace-actions.spec.js`、`web/tests/z-links.spec.js`：本地空目录及浏览、挂载三行和栏目按钮对齐、单池根选择、等宽隐藏滚动条目录弹窗、缓存小时显示。修改 `README.md`、`AGENTS.md`：记录交互、鉴权和验证边界。
- 验证：前端构建、Go全量测试与vet通过；新增ABS端到端测试另行通过。浏览器全量回归结果见后续补充。无本次真实播放错误日志或真实ABS环境，修复的是已复现阻断点，不声称用户现场播放已恢复。未修改用户配置、原15151实例；本轮未要求推送，版本保持0.1.7，不提交推送。
- 最终验证补充：12项Playwright全部通过（1.7分钟），已查看挂载三行表单与栏目右侧按钮截图；差异格式检查通过。隔离预览 `127.0.0.1:15153` 返回200，PID20692，程序及日志位于系统临时目录 `aether-mount-preview`，继续使用该隔离预览配置，不操作仓库用户数据。

## 2026-10-06：存储图标、夜间账号按钮与 v0.1.8 发布

- 修改 `web/src/style.css`：添加存储池的类型图标容器从44px放大到54px，协议/本机矢量主体40px；不影响已添加卡片或星轨图标。不新增外层包装。账号按钮使用夜间主题色及悬浮/键盘焦点配色，保留关于页透明优先级；以链卡片左侧图标和名称统一右移6px，不移动右侧模式及端口。
- 修改 `web/tests/workspace.spec.js`、`web/tests/z-links.spec.js`：断言选择图标尺寸、夜间账号背景及线条颜色、以链左侧内边距，保留桌面/手机截图与完整工作流回归。
- 修改 `VERSION`、`internal/app/version.go`、`Dockerfile`、`web/package.json`、`web/package-lock.json`：发布版本同步递增至0.1.8，仍仅构建linux/amd64。修改 `AGENTS.md` 记录此次全部文件。
- 发布范围包含上一条记录中尚未推送的挂载/目录交互、直链缓存展示和ABS会话鉴权修复；不包含用户配置、凭据、构建及测试产物。使用中文提交及标签，推送main和v0.1.8，实际结果以Git输出为准。
- 验证：前端0.1.8构建、Go全量测试和vet通过；浏览器全量回归正在执行，最终结果另补。真实ABS客户端和Linux/FUSE环境仍未联调，不声称现场播放验证完成。
- 最终验证：12项Playwright全部通过（1.5分钟），查看存储类型选择和夜间界面截图，差异格式检查通过；此次不重启隔离预览，15153继续提供最新web/dist界面，后台原15151及用户配置保持不变。

## 2026-10-06：存储选择图标、直链缓存布局、AetherDrive 挂载与 v0.1.9 发布

- 修改 `web/src/pages/StoragePage.vue`、`web/src/style.css`：给移动云、天翼、夸克、OpenList 的选择图标增加真实图片主体尺寸，保持 115、本机和 WebDAV 图标比例不变。
- 修改 `web/src/style.css` 中容器目录选择器样式：放大目录项和路径文字，保留固定弹窗、根目录导航及隐藏滚动条；组件逻辑未变。
- 修改 `web/src/style.css`：高级设置和更多选项的悬浮/焦点态只强调图标与文字，不再显示整行圆角背景。
- 修改 `web/src/pages/LinksPage.vue`、`web/src/style.css`：直链缓存的类型筛选、刷新、搜索移动到栏目右侧；播放流水扩大可用高度，文字放大，UA、目标、客户端 IP、缓存状态改为胶囊展示，时间列与后续列更紧凑，保留虚拟列表和触底稳定行为。
- 修改 `internal/app/mounts.go`、`internal/app/mounts_test.go`：挂载配置保存所选父目录，实际启动时在其下创建并挂载到 `AetherDrive`；父目录可保留其他文件，专用子目录必须为空、真实目录且不能为符号链接，避免覆盖用户数据。
- 修改 `web/src/pages/MountsPage.vue`：移除 `/mnt/media` 挂载点占位提示。
- 修改 `web/tests/workspace.spec.js`、`web/tests/z-links.spec.js`、`web/tests/zzz-workspace-actions.spec.js`：增加图标主体、目录文字、折叠标题、缓存工具栏/胶囊流水和挂载目录边界回归。
- 修改 `README.md`：记录父目录与 `/AetherDrive` 实际挂载路径、创建时机和数据保护约束。
- 修改 `VERSION`、`internal/app/version.go`、`Dockerfile`、`web/package.json`、`web/package-lock.json`：版本由 `0.1.8` 递增到 `0.1.9`，工作流继续只构建 Linux amd64 镜像。
- 验证：`go test ./... -count=1`、`go vet ./...`、`npm run build`、12 项 Playwright 全量测试及 `git diff --check` 通过；已检查直链缓存和目录选择器截图。
- 未完成项：本机无 Docker/Linux FUSE 与真实云盘环境，未进行生产容器挂载和真实账号联调；本次未修改用户 `config/data`、15151 服务或生成仓库根目录 exe。

## 2026-10-07：挂载路径补全、WebDAV 目录展示与 v0.1.10 发布

- 修改 `web/src/pages/MountsPage.vue`、`web/src/style.css`：挂载点框内以独立颜色显示自动补全的 `/AetherDrive`；仍保存父目录，避免重复追加。长路径缩减输入区域而保留补全部分可见，目录选择按钮和单层焦点边框保持。
- 修改 `web/src/pages/LinksPage.vue`、`web/src/style.css`：UA、目标和客户端 IP 使用 8px 圆角框；缓存状态按首次获取、缓存命中、恢复命中区分日夜颜色，参考本地 AetherLink `LogsView.vue` 的分类。保留 52px 行高、最多 40 条渲染的虚拟列表及触底稳定行为。
- 修改 `web/src/pages/LinksPage.vue`：以链列表与播放流水独立加载，管理页不再请求播放流水，缓存页刷新和定时轮询仅请求流水，并阻止轮询重叠。修改 `internal/app/links.go`：GET 列表仅在读锁内复制以链配置，避免 JSON 深拷贝整个状态，副本脱敏不修改原始密钥。
- 修改 `web/src/components/WebDAVUsers.vue`、`internal/app/dav_users.go`：新授权自动以实际目录名命名，根目录使用存储名称，同名自动加序号；移除可编辑别名及 opaque 目录 ID 展示，新增 `directoryLabel` 持久化与长度限制。旧授权仍保留原 DAV 访问名称，缺少新名称元数据时显示旧名称，不擅自更改客户端路径。
- 修改 `web/src/pages/StoragePage.vue`、`web/src/pages/TasksPage.vue`、`web/src/style.css`：移除存储/任务名称提示文本，115 选择图标主体从 70% 调至 82%；全部公共下拉选择器隐藏滚动条但仍支持滚轮和键盘。
- 修改 `internal/app/dav_users_test.go`、`internal/app/links_test.go`：验证目录名称元数据回传及列表脱敏不改变保存密钥。修改 `web/tests/workspace.spec.js`、`web/tests/z-links.spec.js`、`web/tests/zzz-workspace-actions.spec.js`：新增图标尺寸、自动目录名称、无额外流水请求、圆角/缓存类型、下拉隐藏滚动条及挂载补全可见范围/颜色断言。
- 修改 `VERSION`、`internal/app/version.go`、`Dockerfile`、`web/package.json`、`web/package-lock.json`：同步版本 `0.1.10`；修改 `AGENTS.md` 记录所有变更。使用中文提交与标签推送，仍只构建 amd64 镜像。
- 验证：首轮 Go 全量测试、vet、前端构建与 12 项 Playwright 通过；已检查 WebDAV 授权窗口及直链缓存截图。长挂载路径布局补强后的最终回归结果另补。
- 限制：未在用户生产环境测量以链加载耗时，优化为已确认的多余请求和全状态复制；未做真实 Docker/FUSE 或云盘账号联调。未修改用户配置、原 15151 服务或创建根目录 exe。
- 最终验证：补强后的 Go 全量测试、前端构建、12 项 Playwright（2.3 分钟）再次全部通过；差异格式检查通过，已检查挂载补全截图。推送结果以 Git 确认为准。
