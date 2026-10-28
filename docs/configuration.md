# 模块配置

配置使用固定名称 JSON，不再为每次保存生成带摘要的文件名。

原生移动存储的客户端类型保存在`storage/storage.json`对应池的`config.clientType`，仅`web`或`pc`，新建及旧记录缺失/空值默认PC；明确保存的Web不自动修改，缺省生效不批量改写旧配置。PC上传的500 GB目标上限、分片及暂存边界见[移动PC上传](mobile-pc-upload.md)；Authorization继续字段加密，修改模式不替换凭据。

```text
/config/
  master.key
  state.json
  storage/storage.json
  storage/settings.json
  task/strm.json
  task/cas.json
  task/cas-pool.json
  task/ed2k.json
  task/cache.json
  task/other.json
  task/automation.json
  link/links.json
  tool/tmdb.json
  tool/proxy.json
  tool/ai.json
  tool/emby.json
  tool/quark-takeover.json
  tool/115-simulcast.json
  tool/config.json
  file/webdav.json
  file/mount.json
  transfer/backup.json
  transfer/settings.json
  transfer/resources.json
```

`state.json` 保存账户、签名信息、任务顺序及分享转存目标记忆（`shareTransferPreferences`，含按云盘类型的最近存储池和按池目录记录，不含分享链接或提取码）。`task/other.json` 保留其他任务类型；`tool/config.json` 保留其他工具配置。重命名、整理等既有独立规则 JSON 继续保留。

CAS目录任务的 `casInfoMode` 随 `task/cas.json` 保存，支持 `all`（全计算）、`mobile`（仅SHA256）、`tianyi`（MD5及分片MD5）；备份、导入和重启保留手动选择，未知值在新保存或导入时拒绝。旧记录缺失时按来源默认，移动为mobile、其他为all，不批量重写旧配置。生成固定增量且不提供全量重置；新 `/cas/` 播放地址和旧产物处理见[CAS说明](cas-pool.md)。任务面板只读已有运行态，不另存任务配置或运行历史。

插件市场仓库保存在 `state.json` 的 `pluginRepositories`，包括来源ID、名称、公开GitHub地址、ref、启停和顺序；索引仅作有界内存缓存，可重建。没有外部插件安装清单或运行能力，详见 [插件市场](plugin-market.md)。TG Token 仍加密保存，普通设置读取不回显，管理员可以通过密码框小眼睛按需查看。

夸克链库设置 `quarkLibrary` 与链接卡片 `quarkLibraryCards` 同样保存在 `state.json`，卡片的 `secret` 为字段加密的规范分享链接，`cascadeCleanup` 缺省/`on` 开启、`off` 关闭；旧客户端更新时省略该字段不会覆盖已保存的关闭值。`/data/runtime/state.json` 的 `quarkLibraryRuntime` 保存执行结果、已生成文件授权和摘要、生成目录归属、临时转存与删除意图；目录内 `.aether-library-owner` 与运行态共同校验清理范围，不能随意移除。配置导入保留现有运行回执，重启不重放云端写入。请与主密钥一起备份配置和运行态，详见 [虚拟链库](virtual-library.md)。

[115文件夹清理](115-folder-cleanup.md)规则保存在`state.json`的`folderCleanup`，写前意图及成功回执另存`/data/runtime/state.json`的`folderCleanupReceipts`；导出配置排除回执，导入保留现有回执。全部Cron计划须满足[至少15分钟](cron-policy.md)，旧短周期可载入但不执行，115监控重试策略仍严格大于120分钟。

[115签到](115-signin.md)规则保存在`state.json`的`pan115Signin`，每日提交意图及结果存于`/data/runtime/state.json`的`pan115SigninReceipts`。导出排除回执、导入保留原回执；同一账号同日已提交但未确认的操作不重发，迁移须同时保留运行态和主密钥。

以太链接的 `relayGuard` 在 `link/links.json` 按卡片保存，旧记录缺省按启用处理。更多选项中的“中继守护”默认开启，只在非“始终中继”时可选。开启时，302 所需的 STRM 解析、直链探测或公网访问私网目标失败可退回中继/上游透传；关闭后，仅当客户端网络命中卡片的跳转策略时禁止这种回退，不能得到可用直链则明确返回502。策略本来要求中继的另一侧网络仍正常中继，不改变普通非 STRM 媒体的既有处理方式。

任务进度、执行结果、最近运行时间、存储健康状态及巡检计划、云盘空间快照、挂载状态、通知和 CAS 临时文件记录存于 `/data/runtime/state.json`，会话存于 `/data/runtime/sessions.enc`；目录缓存、刮削索引、播放流水和日志沿用 `/data/cache`、`/data/log`。运行态文件同样校验完整性，涉及凭据的字段仍加密。首次启动迁移配置中的旧运行记录，旧文件不删除；任务更新只改变运行态，不重写任务定义。运行态丢失时不能找回过去的结果，但不会回退旧配置；运行态损坏则明确报错。若需要保留历史、会话及 CAS 临时文件追踪，备份 `/data`，并与配置主密钥一起恢复。

移动CAS播放的未确认提交另记该运行文件的`casPlaybackAttempts`，按账号和特征摘要阻止结果不明后的重复云端写入；配置导出排除、导入保留，迁移时须与运行态一起保留。会员预检与凭据隔离缓存、WebDAV分段下载合并和票据冷却只在当前进程内有效，不随配置导出，不把实时下载作为可自动重放的任务。

[档案中心](taiyi-archives.md)另存于 `/data/runtime/taiyi-archives.json`，包含每份 STRM 最近识别刮削结果及普通任务逐次执行记录，不属于可重建刮削索引，也不加入配置导出或全局状态轮询。显示名称变更不迁移数据文件或接口。重启把未结束档案标中断，不自动重放；完整性验证及主密钥恢复要求与运行态一致。

115 分享 STRM 的资源授权位于 `/data/virtual-library`，文件名到真实资源的加密快照位于 `/data/virtual-library-names`，两者不是可重建目录缓存或配置导出内容。恢复 `/115-share/分享码[-提取码]/文件名.扩展名` 播放须一并保留这两处及配置主密钥；名称索引只有本次生成完整成功后才替换，认证失败不降级到任意分享转存。详见[虚拟链库](virtual-library.md)。

## 传输来源与目标

传输中心分开显示“来源 / 目录”和“目标 / 目录”：上行为存储图标及名称，下行为最深目录名；已有完整祖先信息时可悬浮查看路径，并按存储名、目录名或路径搜索。窄屏保留两端信息，不再隐藏存储列，虚拟列表随桌面及窄屏的固定条目高度切换。

旧界面的“浏览器”是未指定来源时的默认发起方式标签，并不是目录名，也不代表已识别出请求设备。发起方式现在放在文件提示中，备份传输明确标记为备份；实际位置由新的 `from`、`to` 字段表达，包含存储标识、名称、类型、最深目录和可核对的路径，不包含存储凭据。

本地、OpenList、WebDAV 等路径型存储从操作路径取得目录；不透明云盘 ID 只使用既有成功目录读取中的名称建立有界内存索引，不为展示额外扫描云盘或发起目录请求。索引按存储凭据修订区分，目录变更后失效，迟到结果不回填。只知道最深名称时不拼造祖先；未知目录显示“目录未提供”，浏览器或 WebDAV 客户端的本机目录无法获取时明确显示“目录不可获取”，不假定为“下载”目录。

两端位置随新传输创建时保存到条目快照，并进入原传输阶段日志。失败记录继续使用 `/data/cache/transfers.json`、72h 保留规则，包含新增位置；旧记录仍可读取，但缺少的位置不能事后自动补齐。成功记录保留时间、传输控制和取消上游请求的既有行为不变。

## 安全

双向备份的目录基线和历史版本清单另存 `/data/runtime/backup-sync/*.json`，同样校验完整性；不属于模块配置。恢复同步记录应一并备份 `/data` 与配置主密钥，详见[备份规则](backup-rules.md)。

文件网格缩略图保存在 `/data/thumbnails`，连续 72 小时未读取会自动清理；可重建，无需作为配置备份。详见[缩略图缓存](thumbnail-cache.md)。

- 每个文件包含 `version`、可读的 `data` 和完整性校验 `auth`。名称、目录、端口及一般地址可以阅读。
- CK、Token、Authorization、API Key、上游密码及签名密钥采用 AES-256-GCM 字段加密。每次加密使用随机 nonce，认证数据绑定模块、记录 ID 和字段路径；地址含内嵌认证或查询参数时也加密，避免泄露令牌。
- 管理账号及 WebDAV 用户的密码仍为加盐 bcrypt 哈希，不保存明文，也不改为可恢复密码。
- 文件使用 0600、模块目录使用 0700；Windows 权限以系统 ACL 为准。整个文件有认证校验，不能直接手工修改 `data` 后继续使用，修改配置请通过后台。
- 默认主密钥为 `/config/master.key`。支持 `AETHER_MASTER_KEY_FILE=/run/secrets/aether-master-key` 指定单独挂载的现有 32 字节二进制密钥；迁移时必须使用原密钥，不是任意新字符串。外置文件不可读或已有配置缺少密钥时拒绝启动，不生成替代密钥。
- 若密钥与配置同时被窃取，字段加密不能阻止解密。需要较高保护时，应把密钥单独挂载并限制访问权限。

## 保存与迁移

启动一次性读取、校验并解密到内存，页面请求不逐次读取或解密配置文件。保存只改动变化的模块，未变化的文件及密文字段不重写。

多模块修改先持久化固定名称的临时恢复日志 `.config-transaction.enc`，然后逐个原子替换，完成后移除日志。启动发现未完成日志会先恢复旧配置。Linux 下同步文件及目录；Windows 原子替换沿用既有实现，不承诺所有文件系统的断电行为相同。请勿在服务运行时手工删改配置文件，也不支持多个进程同时写同一配置目录。

旧 `state.enc`、模块摘要 `.enc`、旧工具快照仍可读取并迁移。迁移成功后使用 JSON，旧加密文件不删除、不再同步，相当于迁移前的备份。不要混用旧文件与新文件；JSON 缺失或损坏会报错，不会悄悄退回过期配置。

使用内置密码保护配置备份可跨部署恢复，敏感字段在目标部署使用其密钥重新加密。停机备份时保存整个 `/config` 及外置主密钥；导出不包含恢复日志或重复的加密模块 JSON。

## 剪贴板分享链接

已登录、页面可见且获得浏览器权限后，每 1.5 秒检测分享链接；返回页面时也立即检查。支持 115、夸克及移动分享链接，只打开对应转存窗口并填入链接，不自动执行转存。

首次点击可以请求读取权限；被拒绝后可通过账号菜单的「监听分享链接」重新授权。普通内网 HTTP 页面通常不能读取系统剪贴板，需要 HTTPS 或 localhost；这种情况下在非输入区域按 Ctrl+V 仍可识别分享链接。浏览器不提供跨平台的系统剪贴板变化事件，无法保证后台页面或拒绝授权时自动监听。

编辑输入框或其他弹窗打开时不抢占当前操作，退出登录后停止读取；连续轮询同一链接不会重复弹窗，剪贴板换成其他内容后可再次识别原链接。

## 手机与 PWA

iOS Safari 可通过系统菜单添加到主屏幕。部署应使用受信任 HTTPS；普通局域网 HTTP 不会注册 Service Worker。界面适配动态视口及安全区域，Worker 仅缓存公开离线提示，不缓存 API、登录页面、云盘数据或媒体。安装、更新及实体设备验收边界见 [PWA 与 iOS Safari](pwa.md)。
