# 2026-04-02_flow-cancel-run-runtime

## 变更背景 / 目标

- `flow` 运行时此前只有 `delete` 会内部中断 run，缺少显式 `cancel_run` 控制面。
- 本轮目标是为 `RC-P0-1` 落地最小可用的单 run 取消能力，并保证取消结果能被 `status/detail` 读到。

## 具体变更内容

### 修改

- `flow/actions.go`
  - 注册 `cancel_run`
- `flow/types.go`
  - 新增 `cancel_run` action alias 和 `CancelRunReq` / `CancelRunResp` alias
- `flow/flow_id.go`
  - 新增 `validateRunID(...)`
- `flow/handler.go`
  - 新增 `handleCancelRun` / `cancelRunLocal` / `sendCancelRunResp`
  - 抽取统一的 run 取消 helper，供 `cancel_run` 与 `delete` 复用
  - `status` 增加 `flow_id + run_id` 归属校验
  - `detail` 在 run 已取消时回显取消原因，并把活动节点状态收敛为 `cancelled`
- `flow/delete_test.go`
  - 新增 `cancel_run` 成功 / not found / flow mismatch / terminal-state 用例
- `flow/runtime_fix_test.go`
  - 新增 `cancel_run` 远端转发失败响应用例
- `flow/flow_id_test.go`
  - 新增 `cancel_run` 非法 `flow_id` / `run_id` 用例

### 删除

- 无

## Requirements impact

- none

## Specs impact

- none

## Lessons impact

- none

## Related requirements

- `D:\project\MyFlowHub3\worktrees\server-run-control-phase1\docs\requirements\flow_data_dag.md`

## Related specs

- `D:\project\MyFlowHub3\worktrees\server-run-control-phase1\docs\specs\flow.md`
- `D:\project\MyFlowHub3\worktrees\proto-run-control-phase1\docs\protocol_map.md`

## 对应 plan / todo 任务映射

- `RC-P0-1`
  - runtime action registration
  - local cancel execution
  - cancel status/detail reflection
  - tests and verification

## 关键设计决策与权衡

- 保持 `cancel_run` 走现有 `forwardToExecutorNoPerm(...)` 路由模型，不再复制一套新的无权限控制面。
- 不扩展 `DetailResp` 顶层字段，而是在取消时把 `msg` 与活动节点状态同步到 `cancelled`，以最小改动满足调试可见性。
- 保持 `delete` 的定义删除语义不变，只把底层取消逻辑提炼为共享 helper，避免两条链路状态收敛口径分叉。

## 测试与验证方式 / 结果

- Proto 联动门禁：
  - `D:\project\MyFlowHub3\worktrees\proto-run-control-phase1`
  - `$env:GOWORK='off'; go test ./... -count=1 -p 1`
- 目标测试：
  - `D:\project\MyFlowHub3`
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-run-control-phase1\go.work go test github.com/yttydcs/myflowhub-subproto/flow/... -run 'TestFlowDeleteSuccess|TestFlowDeleteNotFound|TestFlowDeleteInterruptsActiveRun|TestFlowDeleteFileFailureKeepsState|TestFlowDeletePermissionDenied|TestFlowCancelRunSuccess|TestFlowCancelRunNotFoundOrTerminal|TestFlowCancelRunRejectsInvalidRunID|TestFlowRemoteForwardFailureReturnsResp|TestFlowHandlersRejectInvalidFlowID' -count=1 -p 1`
- 全量 `flow` 模块：
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-run-control-phase1\go.work go test github.com/yttydcs/myflowhub-subproto/flow/... -count=1 -p 1`
- 结果：通过

## 潜在影响

- `cancel_run` 会把当前活动节点状态同步改写为 `cancelled`，调用方不应再把这类节点视为普通失败。
- `GOWORK=off` 仍可能命中已发布的旧 `core/proto` 版本，联调验收应优先使用临时 `go.work` 绑定本地 worktree。

## 回滚方案

1. 回退 `flow/actions.go`、`flow/types.go`、`flow/flow_id.go`、`flow/handler.go`
2. 回退 `flow/delete_test.go`、`flow/runtime_fix_test.go`、`flow/flow_id_test.go`
3. 删除对 `cancel_run` 的调用入口，恢复“仅 delete 可中断 run”的旧行为
