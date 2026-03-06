# 2026-03-06 VarStore：逐跳可见回程与并发写匹配对齐（SubProto）

## 变更规模
- 级别：较大（涉及协议回程语义、并发匹配与多 hop 转发行为）

## 背景 / 目标
VarStore 在“规范文档”与“先行实现”之间存在若干关键语义差异，影响多 hop 场景的稳定性与一致性：

- `*_resp/assist_*_resp` 逐跳是否可见（是否进入中间节点 handler）
- `SourceID` 是否端到端保留（actor 身份/权限/审计）
- `set/revoke` 并发写入在回程时是否会错配（需要 1:1 对应）
- `notify_set/notify_revoke` 下行链路是否需要“转发 + 本地处理（缓存/订阅推送）”
- `list` 空集合语义、`set.value` 非空、`subscribe.subscriber` 校验等规则收敛

本次改造目标：在 **不变更 wire schema（action/data 结构不变）** 的前提下，使 SubProto 的 VarStore 行为与已确认规范一致。

## 变更内容
### 1) `*_resp/assist_*_resp` 全部改为 `MajorCmd`（逐跳可见）
- 响应不再依赖 `MajorOKResp/MajorErrResp` 的“快速转发”路径。
- 中间节点可进入 `handle*Resp` 执行：逐跳出队、扇出、沿途缓存更新。

### 2) SourceID 端到端保留
- `assist_* / up_* / notify_* / *_resp` 的转发与回程均保留原始 actor 的 `SourceID`。
- pending waiter 结构补充 `requester`（原始 actor SourceID），回程构建响应头时以此恢复 `SourceID`。

### 3) `set/revoke` 并发写入 1:1 匹配（策略 C：上行 msg_id + 映射表）
- 新增 `writing map[upMsgID]pendingWrite`：
  - 每次上送写操作分配新的上行 `msg_id`；
  - 映射表记录下游等待者的 `(conn_id, requester_source_id, downstream_msg_id, trace_id)`；
  - 回程按响应帧 `hdr.msg_id` 精确命中映射，恢复下游 `msg_id/trace_id`，避免按 `(owner,name,kind)` 错配。
- 增加 TTL sweep（`pendingWriteTTL` + `pendingWriteSweepInterval`）防止异常路径泄漏。
- 断连清理：在 `conn.closed` 事件中清理 pending / mapping / pending subscribe，避免 waiter 堆积。

### 4) notify 下行链路：转发 + 本地处理
- `notify_set/notify_revoke` 在 `TargetID != local` 时仍执行本地处理（更新缓存、触发订阅推送），再继续转发。
- `notify_*` 发送时使用 `SourceID=actor`（让 owner 可识别变更发起者），`TargetID=owner`。

### 5) 规则收敛（按已确认结论）
- `set.value` 禁止空字符串/纯空白（`code=2`）。
- `set_resp code=1` 必带 `value`；仅成功更新缓存，失败不更新。
- `list`：owner 自查包含 private；空集合也返回成功（`code=1`），并保证 `names` 字段在 JSON 中显式为 `[]`。
- `subscribe.data.subscriber`：只允许 `0` 或等于请求帧 `SourceID`；否则 `code=2`（默认不支持代订阅）。
- `private`：允许权限例外（由权限配置决定）。

## 影响范围
- 协议层面：VarStore 的响应帧 `Major` 统一为 `MajorCmd`（对 await/客户端匹配策略有影响）。
- 中间节点：会更频繁进入响应 handler（逐跳可见），需要依赖本次新增的映射/清理逻辑避免状态泄漏。
- 兼容性：wire schema 不变；新增/强化的校验可能会更早返回 `code=2`（例如非法 subscriber / 空 value）。

## 关联任务映射（plan.md）
- VS-1：统一响应逐跳可见（MajorCmd）
- VS-2：SourceID 端到端保留
- VS-3：写操作并发一一对应（上行 msg_id 映射）
- VS-4：查询去重扇出 + 沿途缓存
- VS-5：notify 下行“转发 + 本地处理”
- VS-6：规则收敛（set_resp/list/subscriber/private）
- VS-7：补齐关键单测（已覆盖核心路径；归档见本文件）

## 测试与验证
- 单测（VarStore module）：
  - `cd varstore; $env:GOWORK='off'; go test ./... -count=1 -p 1`
- 新增覆盖点：
  - list 空集合 `names:[]` 显式输出
  - subscribe subscriber 不一致返回 `code=2`
  - notify_set 转发同时本地缓存更新
  - set_resp 并发写映射按 msg_id 恢复下游 msg_id/trace_id

## Code Review
- 结论：通过
- 重点关注：
  - `MajorCmd` 回程将增加 handler 处理频次：已通过 pending/mapping 清理 + TTL sweep 控制状态堆积风险。
  - `SourceID` 端到端保留依赖 `ConnManager` 路由索引覆盖后代节点：若索引缺失可能触发 Dispatcher `sourceMismatch` 丢帧（需在联调/部署配置中确认）。

## 回滚方案
- 回滚 SubProto `varstore` 模块相关提交即可恢复旧行为。
- 若需分步回滚：
  - 先回退 `MajorCmd` 响应策略（但会丢失逐跳可见能力）
  - 或仅回退写入映射逻辑（会退回到并发写错配风险）
