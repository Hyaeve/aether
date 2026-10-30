# 存储会员信息

存储卡片支持115年费VIP、永久VIP、普通VIP，移动白银/黄金/钻石、天翼黄金/铂金，以及夸克VIP、SVIP、SVIP+、88VIP和迷你VIP的统一线条背景标识。仅展示上游明确返回的当前会员，不按容量、PC客户端、用户名或套餐价格推测；背景标识本身不修改授权或账号权益。CAS新建秒传另按实际会员和客户端预检单文件额度，移动及天翼规则见[CAS绑定池](cas-pool.md)，仍以上游实际命中为准。

## 只读来源

- 115：官方网页使用的`https://passportapi.115.com/app/1.0/web/26.0/user/base_info`，匹配当前账号ID后读取`vip_info.level_name`中的明确年费/永久名称。未能明确识别时，复用115driver的只读`GetUser()`，仅在`forever > 0`时显示永久VIP，或`vip > 0`且`expire`未过期时显示普通VIP；不把普通VIP猜成年费，不把“长期VIP”擅自等同永久VIP。
- 移动：原生个人云只读`/orchestration/group-rebuild/member/v1.0/queryUserBenefits`，请求`isNeedBenefit:0`。按`userSubMemberList`的`memberLvName`或`chargingPolicyList.prodName`中的明确名称选最高已识别等级，不猜未确认数字编码。非原生移动接入不查询。
- 天翼：官方网页只读`/order/queryUserLogo.action`，读取`data.vipInfoList`，仅将未过期的`memberLevel:1000/2000`分别显示为黄金/铂金。未知等级、缺失或无法解析到期时间不显示。
- 夸克：PC只读`https://drive-pc.quark.cn/1/clouddrive/member`，复用同一响应中的容量和`member_type`，不重复查询。对照本地LitePan源码快照`drivers/Quark/profile.go`与`transport.go`：`VIP/SUPER_VIP/Z_VIP/EXP_SVIP/MINI_VIP`分别为VIP/SVIP/SVIP+/88VIP/迷你VIP；未知或非成功响应不显示，不能根据大容量推断88VIP或SVIP。

接口与字段核对自2026年10月28日官方公开网页代码：移动`/w/static/js/app.56b4a805.js`、天翼`https://cloud.dlife.cn/web/main/js/chunk-common.8e0669c6.js`、115`https://115.com/_next/static/chunks/52645-2a86b028e3c4f89c.js`。同时检查OpenList固定提交`6aeaf1dafd66fd1ac2b7ca7104b41a69e0070749`的139/189驱动；没有据此猜测不存在的通用会员接口。

## 缓存与边界

- 会员与容量共用受凭据修订隔离、单飞、有时限的可选只读查询；不在`/api/state`轮询时额外发云盘请求。页面打开或5分钟到期时读取，配置和账号变化丢弃旧结果。
- 移动、天翼和115的容量与会员查询在同一12秒预算内独立进行，避免较慢的容量接口先耗尽会员查询的时间；仍共用账号API节流。夸克通过一次PC会员请求同时读取容量及等级。无法查询不等同普通用户，未核实真实账号之前不承诺必定显示。
- 等级最多信任5分钟；明确到期则立即不显示。查询失败不影响存储健康和卡片显示，也不因保留旧容量而沿用会员等级。未识别或不可查询时不显示徽标，不标为非会员。
- 管理接口只返回规范等级、名称、检查时间及可取得的到期时间，不返回会员接口原始正文、账号凭据或任意上游图标URL。
- 背景使用本项目Lucide皇冠、盾牌、钻石和徽章线条，以银、金、青和淡紫统一层级，不冒充原厂会员商标；不会遮挡名称、容量或操作菜单。
- 协议模拟和页面截图不等于真实账号会员查询已验收。官方接口、授权设备及网络出口仍可能影响可用性，不绕过会员权益。
