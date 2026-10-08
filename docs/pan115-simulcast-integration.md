# 115 同播复制接入清单

## 配置和接口

- 类型定义：`internal/app/pan115_simulcast.go` 中的 `SimulcastConfig`，字段 `Enabled bool`、`Directory string`、`DirectoryLabel string`。
- `State` 和 `toolSettings` 增加 `Simulcast map[string]SimulcastConfig`，JSON 名 `simulcast`。纳入工具快照的写入、恢复、迁移、备份及恢复；启用工具快照后主配置清空该字段。工具快照结构体使用命名字段初始化。
- 注册 `GET/PUT /api/115-simulcast` 到 `a.pan115SimulcastSettings`，保留正常管理员中间件。handler 本身也检查登录并返回 `Cache-Control: no-store`。
- GET 返回以存储 ID 为键的对象；PUT 全量替换该对象，返回规范化后的对象。空对象删除全部设置。每个存储最多一项。禁用/删除存储的旧设置可以通过提交不含该键的对象清理。
- 保存只验证存储类型、启用状态、目录 ID 格式；是否仍可访问该目录在复制时通过上游 uncached list 校验。`/` 保存为当前存储根 ID。
- ToolsPage 导入 `Pan115Simulcast.vue`，组件自带主模态窗口，无必需 props，支持 `@close`，读取现有 `state.storages`。选择目录使用 `TaskSourcePicker`，编辑同一设置不切换存储；可删除后重新添加。组件自带局部样式。

请求及响应示例：

```json
{"storage-id":{"enabled":true,"directory":"123456","directoryLabel":"同播副本"}}
```

## 播放 Hook

App 添加 `simulcast *pan115Simulcast`，App 创建后初始化一次：

```go
a.simulcast = newPan115Simulcast(a, trustedProxyCIDRs)
```

仅在 `stream` 已完成签名及存储权限校验、`claim.CAS == nil` 且 `download != 1` 的真正播放分支接入：

```go
file := File{ID: claim.File, PickCode: claim.Pick}
if s.Type == "115" {
    file, err = a.simulcast.PlayFile(r, s, file)
    if err != nil {
        fail(w, 502, err)
        return
    }
}
d, err = a.downloadWithUA(r.Context(), s, file.ID, file.PickCode, r.UserAgent())
```

- `PlayFile(r *http.Request, s Storage, source File) (File, error)` 返回原文件或副本，不缓存下载 URL，不调用自身或改写底层下载函数。
- `/d/...` 的 `playSTRMReference` 已转入 `stream`，此处只挂一次。普通签名文件下载也使用 `stream` 时必须保留 `download=1` 判断；不得在 `drivers.go`、DAV、FUSE 或通用 `downloadWithUA` 加 hook。
- 若以后从以链独立播放入口接入，也应在其播放授权后显式调用同一个实例，携带真实客户端请求；不要从内部回环 HTTP 请求取身份。
- 使用现有 `proxy.ClientIP(r, trustedCIDRs...)`，再结合完整 UA 哈希；可信代理范围必须来自管理员配置，不能无条件信任 XFF。不配置可信代理时只信任 socket peer IP。相同出口 IP 且相同 UA 的设备无法区分，这是 IP+UA 方案的明确限制。

## AetherLink 范围

- AetherLink 原生反代监听器是独立的 `linkcore/proxy` HTTP 服务，不经过 `internal/app/stream`，因此其自身生成或解析的上游直链不会触发 115 同播复制；这是有意的边界，不将同播 hook 植入 AetherLink resolver/proxy。
- Aether 生成的 115 STRM 链接（`/d/<pickcode>.<ext>?/<name>`）由 `playSTRMReference` 转入 Aether `/stream`，因此会触发同播 hook。浏览器文件下载使用带 `download=1` 的签名 `/stream` URL，不是给 `/d/...` 追加下载参数（该转入过程重新构造 URL，不保留这个参数）。
- AetherLink 若读取包含 Aether URL 的 STRM，首次解析可通过 HEAD/Range GET 跟随跳转而间接访问 Aether；但探测只传 UA，不传原始客户端 IP。解析器缓存最终直链后，后续命中不再访问 Aether `/stream`。因此即使 STRM 最初由 Aether 生成，也不保证经 AetherLink 二次解析/缓存时按真实客户端触发同播。
- 本插件承诺范围仅为 Aether 生成 STRM 链接及直接请求 Aether `/stream` 的播放入口，不覆盖 AetherLink 自己的直链、媒体服务器直接访问、WebDAV、FUSE、文件下载接口或外部生成的 STRM。
- `server.go` 使用 `linkTrustedProxies()` 初始化，共用已存在的可信代理逻辑：信任回环及本机接口地址，额外代理须在 `AETHER_TRUSTED_PROXIES` 明确配置CIDR，不信任整段局域网。未受信任代理之后同UA客户端可能合并为同一身份，不无条件信任XFF。

## 行为边界

- 最近 10 分钟滑动窗口：首个客户端使用原文件，其他客户端各复制一份；同客户端重复请求更新活动时间并复用文件 ID。HEAD 也计作获取直链。重启丢失会话，可能生成新副本。
- 最多 512 个文件会话、每文件 32 客户端、总计 4096 客户端。过期项在请求时清理；满额返回错误，不淘汰仍活跃的会话。每实例用可取消的锁串行复制，避免同目标目录的并发同名结果串用；单请求含等待锁最多 60 秒。
- 复制目录 uncached 分页最多一万项；从 `GetFile` 读取真实源元数据，复制前后校验目录及新增 ID，仅接受唯一新增且名称、大小、SHA1 匹配、pickcode 不同的文件。再次 `GetFile` 确认后重命名，最终核验 ID、父目录、名称及内容指纹。
- 数字后缀位于扩展名前，如 `movie (1).mkv`。已存在的后缀跳过。目录已有原文件同名项目时拒绝复制，不覆盖或擅自复用它。
- 上游 Copy/Rename 与目录列表不是原子事务。外部同时操作仍存在竞态；有歧义或未确认就失败，不声称支持与外部写入的原子隔离。建议专用复制目录。
- 写入或确认失败会在当前客户端会话内记住错误，避免重放不确定请求；错误请求也刷新十分钟活动窗口。请求停止十分钟后或进程重启后才会自然重新尝试。
- 停用、删除配置、过期、重启和异常都不自动删除云端副本。用户自行在网盘清理；本插件不包含回收站/删除调用。不保证绕过 115 风控、播放权益或并发限制。

## 验证

- `go test ./internal/app -run TestPan115Simulcast -count=1`：mock HTTP，无真实账户。
- `node web/tests/pan115-simulcast-component.mjs`：独立 Vite 临时端口和 Playwright，mock 配置及目录接口，不使用现有服务；测试结束关闭服务器，截图保存于系统临时目录。
- `TestPan115SimulcastHTTPPlaybackAndDownloadBypass` 使用真实本地 HTTP listener、完整 App Handler 和 mock 上游，验证 `/d` 与签名 `/stream` 的 GET/HEAD 播放走 hook、无效签名先拒绝、`download=1` 绕过 hook 到达原 115 下载 API。成功响应需要上游私钥，因此下载分支以明确的 mock 115 拒绝响应结束，不宣称获得成功直链。
- `TestPan115SimulcastRegisteredSettingsAndToolsRestore` 验证实际注册路由的 GET/PUT 鉴权、配置返回、主配置剥离字段和工具加密快照重启恢复；配置备份导入及真实账号播放仍需集成方验证。
