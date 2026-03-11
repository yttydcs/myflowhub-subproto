# 2026-03-11 修复 Flow/Exec 响应继承 MsgID/TraceID，解除 Win Await 超时

## 背景 / 目标

Win 端 Flow 页面通过 `MyFlowHub-SDK/await` 使用 `SendAndAwait` 等待响应，匹配键为：

- `MsgID + SubProto + Action`

但当前 `flow.*_resp`（以及 `exec.call_resp`）在构造响应 HeaderTcp 时未继承请求头的 `MsgID`（日志中表现为响应 `msg_id=0`），导致：

- Win 能收到 `*_resp` payload，但 SDK 无法 deliver 到 await，最终报 `request timed out`。

本次目标：

1) `flow`：`set_resp/run_resp/status_resp/list_resp/get_resp` 响应头继承请求的 `MsgID/TraceID`；
2) `exec`：`call_resp` 响应头继承请求的 `MsgID/TraceID`；
3) wire 不变（action 名称/JSON schema/SubProto 编号不变），不改变权限与逐级裁决逻辑。

## 具体变更内容

### 1) Flow：响应回包继承 MsgID/TraceID

- 修改：`flow/handler.go`
  - 新增 `sendCtrlToNodeWithReqHdr(ctx, reqHdr, target, msg)`：在构造 `MajorOKResp` 响应时，若 `reqHdr` 非空则写回 `MsgID/TraceID`。
  - `sendListResp/sendGetResp/sendRunResp/sendStatusResp/sendSetRespToNode` 统一改为带 `reqHdr` 发送，保证所有 `flow.*_resp` 都继承请求的 ID。
  - `applySetLocal` 增加 `reqHdr` 入参，确保落盘链路的 `set_resp` 也继承请求 ID。

### 2) Exec：call_resp 回包继承 MsgID/TraceID

- 修改：`exec/handler.go`
  - `execLocal` 增加 `reqHdr` 入参并透传到回包发送点。
  - `sendCallResp/sendCallRespToNode` 在构造 `MajorOKResp` 响应时写回请求 `MsgID/TraceID`。

### 3) 单测覆盖

- 新增：`flow/resp_ids_test.go`
  - 覆盖 `list_resp` / `set_resp` 的 `MsgID/TraceID` 继承断言。
- 新增：`exec/resp_ids_test.go`
  - 覆盖本地执行路径返回 `call_resp` 的 `MsgID/TraceID` 继承断言。

### 4) 联调配置（workspace 控制面）

- 修改：`d:\project\MyFlowHub3\go.work`
  - 追加 `use ./worktrees/fix-subproto-resp-msgid/flow` 与 `use ./worktrees/fix-subproto-resp-msgid/exec`，
    使 `repo/MyFlowHub-Server` 在 `go run ./cmd/hub_server` 时使用本 worktree 的修复版本进行联调。

## 任务映射（plan.md）

- RESP-1：Flow `*_resp` 继承 `MsgID/TraceID`
- RESP-2：Exec `call_resp` 继承 `MsgID/TraceID`
- RESP-3：go.work 指向本 worktree module（用于本地联调）

## 关键设计决策与权衡

1) **只补齐 `MsgID/TraceID`，不重写其它头字段语义**：
   - `SourceID/TargetID/Major/SubProto` 仍沿用既有实现（保持 wire/路由语义不变）；
   - 变更面最小，风险最低。

2) **继承 `TraceID` 的收益**：
   - Core/SDK 在发送侧会自动补 `trace_id`；若响应不继承会生成新的 trace，链路无法串联；
   - 继承不会影响忽略 trace 的客户端，但显著提升排障与观测一致性。

## 测试与验证

- 单测：
  - `cd worktrees/fix-subproto-resp-msgid/flow && go test ./... -count=1 -p 1`
  - `cd worktrees/fix-subproto-resp-msgid/exec && go test ./... -count=1 -p 1`
- Server 回归（确保依赖本地 module 可正常编译/测试）：
  - `cd repo/MyFlowHub-Server && go test ./... -count=1 -p 1`
- Win 手工冒烟（需要你执行）：
  1) `scripts/run-dev.ps1 -WaitServer`
  2) Win → Flow：Executor 填 `1`，点 `Refresh`，期望不再超时；
  3) 再试 `Save/Run/Status/Get`。

## 潜在影响与回滚方案

### 潜在影响

- 响应头现在携带请求的 `MsgID/TraceID`：
  - 对不依赖这两字段的客户端无影响；
  - 对 SDK Awaiter 是必要增强（修复超时）。

### 回滚方案

- 功能回滚：revert 本次对 `flow/handler.go`、`exec/handler.go`、测试文件的提交。
- 联调回滚：从 `d:\project\MyFlowHub3\go.work` 移除本 worktree 的 `use` 条目。

## Code Review 结论（3.3）

- 需求覆盖：通过（Flow/Exec 响应继承 `MsgID/TraceID`；wire/权限/路由语义不变）。
- 架构合理性：通过（在“统一回包入口”补齐头字段；不引入跨模块耦合）。
- 性能风险：通过（仅常量级字段赋值；无新增 I/O、无额外循环/锁）。
- 可读性与一致性：通过（新增辅助函数命名明确；调用点集中；gofmt）。
- 可扩展性与配置化：通过（为 SDK Awaiter/链路追踪打底；后续其它子协议可复用同模式）。
- 稳定性与安全：通过（不放开权限；不改变转发与裁决流程；仅增强响应头）。
- 测试覆盖情况：通过（新增单测断言 `MsgID/TraceID`；并回归 Server `go test ./...`）。
