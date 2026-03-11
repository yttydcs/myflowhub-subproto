# Plan - MyFlowHub-SubProto：修复 Flow/Exec 响应 MsgID，解除 Win Await 超时

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 分支：`fix/resp-msgid`
- Worktree：`d:\project\MyFlowHub3\worktrees\fix-subproto-resp-msgid`
- Base：`main`
- 关联仓库（本轮不改代码）：
  - `MyFlowHub-Server`：`scripts/run-dev.ps1` 启动的 `hub_server` 会依赖 `myflowhub-subproto/flow|exec`
  - `MyFlowHub-Win`：Flow 页面通过 SDK `SendAndAwait` 等待 `*_resp`
  - `MyFlowHub-SDK`：Await 按 `MsgID + SubProto + Action` 匹配响应

## 项目目标与当前状态
- 目标：在 Win 的 Flow 页面中，`Refresh / Save / Run / Status / Get` 不再出现 `request timed out`；响应能够被 SDK await 正确匹配并返回给 UI。
- 当前状态（已复现）：Win Logs 能看到 `[RX] ... payload={"action":"list_resp",...}`，但仍超时；根因是响应 Header 的 `msg_id` 未回写请求的 `msg_id`，SDK await 无法 deliver。
- 明确不做：不改 wire（action 名称/JSON schema/SubProto 编号不变），不改权限模型，仅修复响应头部字段。

## 关键约束与设计原则
- SDK await 匹配键为 `MsgID + SubProto + Action`，响应必须带回请求的 `MsgID`。
- handler 可能在 `AcceptCmd()` 场景“拦截处理非本地 Target”的 Cmd 帧；因此响应头需要：
  - 保留请求的 `MsgID/TraceID`（用于 await/链路追踪）；
  - `SourceID` 必须是当前发送响应的节点（`srv.NodeID()`）；
  - `TargetID` 必须是实际回包目标节点（通常为请求发起方/`origin_node`）。

## 可执行任务清单（Checklist）

### RESP-1 修复 flow：所有 `*_resp` 回包带回请求 MsgID
- 目标：`set_resp/run_resp/status_resp/list_resp/get_resp` 的 header.MsgID == request header.MsgID。
- 涉及模块/文件：`flow/handler.go`
- 验收条件：Win Flow `Refresh` 不超时；日志不再出现 `flow list await failed: context deadline exceeded`。
- 测试点：新增单测覆盖 `list_resp` 的 MsgID 回写；并回归 `go test ./...`（flow module）。
- 回滚点：回退本任务提交。

### RESP-2 修复 exec：`call_resp` 回包带回请求 MsgID
- 目标：`call_resp` 的 header.MsgID == request header.MsgID（便于未来客户端对 `exec.call` 使用 await）。
- 涉及模块/文件：`exec/handler.go`
- 验收条件：单测通过；不引入行为差异（权限/转发逻辑不变）。
- 测试点：新增单测覆盖 `call_resp` 的 MsgID 回写；并回归 `go test ./...`（exec module）。
- 回滚点：回退本任务提交。

### RESP-3 开发联调：让 run-dev 的 hub_server 使用本 worktree 的 flow/exec 模块
- 目标：本地 `go run ./cmd/hub_server` 能引用本 worktree 的 `myflowhub-subproto/flow|exec`，从而验证 Win UI 不再超时。
- 涉及模块/文件：`d:\project\MyFlowHub3\go.work`
- 验收条件：`scripts/run-dev.ps1` 启动后 Win → Flow → Refresh 正常返回。
- 回滚点：从 `go.work` 移除本 worktree 的 `use` 条目。

## 验证步骤（可交接执行）
1) 代码层：
   - `cd d:\project\MyFlowHub3\worktrees\fix-subproto-resp-msgid\flow && go test ./... -count=1 -p 1`
   - `cd d:\project\MyFlowHub3\worktrees\fix-subproto-resp-msgid\exec && go test ./... -count=1 -p 1`
2) 联调：
   - `d:\project\MyFlowHub3\scripts\run-dev.ps1 -WaitServer`
   - Win → Home：Connect + Login（确保有 node_id）
   - Win → Flow：Executor 填 `1`，点 `Refresh`，期望立即返回（不超时）；再试 `Save/Run/Status`。

## 风险与注意事项
- 若 `origin_node` 与 `hdr.SourceID` 可能不一致，响应 `TargetID` 仍必须遵循现有语义（以当前实现的 `target` 入参为准），但 `MsgID` 必须来自当前收到的请求头。
- 改动涉及回包 header，需确保不破坏 Core 的快速转发（`MajorOKResp + TargetID` 路由）。
