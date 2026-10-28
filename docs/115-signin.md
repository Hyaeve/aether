# 115 签到

“我的插件”第一项。插件卡片用115标志与右下签到日历修饰，点击卡片打开固定大小窗口，底部“添加签到”。编辑器选择已启用的115存储和五字段Cron，默认`0 8 * * *`；全部Cron相邻实际触发至少15分钟。每个真实115账号只允许一个签到任务，不只按存储ID去重。任务条可手动执行、编辑和确认删除；插件图标切换全局启停。保存任务自动启用，删除任务不撤销已经完成的签到。

管理员接口：`GET/PUT /api/115-signin`，配置为`enabled`和`rules:[{storageId,cron}]`；手动`POST /api/115-signin/run`只接受已保存的`storageId`。Cron调度在后台进行，重启不补跑错过计划；原时区规则保持。删除存储同步移除其签到规则，旧存储失效或改账号不使启动失败，执行时严格核对。

协议独立实现对照p115client的`user_points_sign_post`，固定修订`856e4f8f09ce034534bd458f06d3dee4474a1698`，源文件为`p115client/client.py`：

```text
https://github.com/ChenyangGao/p115client/blob/856e4f8f09ce034534bd458f06d3dee4474a1698/p115client/client.py
POST https://proapi.115.com/android/2.0/user/points_sign
token = SHA1(accountID + "-Points_Sign@#115-" + unixSeconds)
token_time = unixSeconds
```

这是115客户端接口，不是官方开放平台授权接口。复用现有存储CK，不新增凭据输入或静默登录；参考实现注明Web Cookie可能失败，授权/设备限制以115实际响应为准，不承诺所有设备CK均可签到。只向固定官方HTTPS地址发送已识别的CK字段，45s超时，不跟随重定向、不重发POST、不回显正文或凭据；只把HTTP200且明确`state:true`记为成功。

按真实账号和北京时间自然日去重。提交前持久化意图，同日成功回执返回“今日签到已完成”；网络、响应异常、上游拒绝或服务重启后未确认的回执不再次提交，需核对网盘/运行日志。下一自然日可新签到。最多保留5000回执，32天以上淘汰；这不是自动补签或强行绕过签到限制。

设置存于加密配置`/config/state.json`的`pan115Signin`，每日意图存于`/data/runtime/state.json`的`pan115SigninReceipts`，不随配置导出，配置导入保留原运行回执。日志包含存储、手动/Cron来源和安全结果。相关测试使用模拟上游，无真实账号签到、积分权益或设备兼容验收。
