# 模块配置

配置使用固定名称 JSON，不再为每次保存生成带摘要的文件名。

```text
/config/
  master.key
  state.json
  storage/storage.json
  storage/settings.json
  task/strm.json
  task/cas.json
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
```

`state.json` 保存账户、签名信息、任务顺序和必要运行记录。`task/other.json` 保留其他任务类型；`tool/config.json` 保留其他工具配置。重命名、整理等既有独立规则 JSON 继续保留。

## 安全

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
