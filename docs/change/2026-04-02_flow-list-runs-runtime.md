# 2026-04-02_flow-list-runs-runtime

## 变更背景 / 目标

- `flow` 已有 retention 索引，但外部只能通过 `status` / `list(last_run_id)` 间接看到最新一次运行。
- 本轮目标是为 `RC-P0-2` 提供显式 `list_runs` 运行时入口，让调用方能查看 retained run history。

## 具体变更内容

### 修改

- `flow/actions.go`
  - 注册 `list_runs`
- `flow/types.go`
  - 新增 `list_runs` action alias 和 `ListRunsReq` / `ListRunsResp` / `RunSummary` alias
- `flow/handler.go`
  - 新增 `handleListRuns` / `sendListRunsResp`
  - 新增 `snapshotRunSummaryLocked()`，复用 `runOrderByFlow` 输出最新到最旧的 retained run 摘要
- `flow/flow_id_test.go`
  - 新增 `list_runs` 非法 `flow_id` 拒绝测试
- `flow/runtime_fix_test.go`
  - 新增 `list_runs` 历史顺序 / limit / cancel msg 用例
  - 新增 `list_runs` 本地 `404` 与远端转发失败响应用例

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

- `RC-P0-2`
  - runtime action registration
  - retained run summary query
  - tests and verification

## 关键设计决策与权衡

- 直接复用 `runOrderByFlow`
  - 好处：不新增新的状态索引，也不破坏 retention 策略
  - 代价：`list_runs` 结果天然受 retention 窗口约束
- `list_runs` 不复用 `list`
  - 好处：`list` 继续只负责 flow 摘要，避免返回结构混杂
  - 代价：调用方需要新增一次显式查询
- 对已删除但仍有 retained run 的 `flow_id` 允许返回历史
  - 好处：更贴合“保留窗口内历史 run”目标
  - 代价：调用方不能把 `list_runs` 成功误解成 flow 当前仍已部署

## 测试与验证方式 / 结果

- Proto 门禁：
  - `D:\project\MyFlowHub3\worktrees\proto-run-control-phase1`
  - `$env:GOWORK='off'; go test ./... -count=1 -p 1`
- 目标测试：
  - `D:\project\MyFlowHub3`
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-run-control-phase2\go.work go test github.com/yttydcs/myflowhub-subproto/flow/... -run 'TestFlowListRunsReturnsRetainedHistory|TestFlowListRunsNotFound|TestFlowRemoteForwardFailureReturnsResp|TestFlowHandlersRejectInvalidFlowID|TestFlowCancelRunSuccess|TestFlowRunRetentionPrunesCompletedRunsAndKeepsLatestLookup' -count=1 -p 1`
- 全量 `flow` 模块：
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-run-control-phase2\go.work go test github.com/yttydcs/myflowhub-subproto/flow/... -count=1 -p 1`
- 结果：通过

## 潜在影响

- `list_runs` 只返回 retained run；窗口外历史仍不可见。
- 若 flow 已删除但 retained run 尚未回收，`list_runs` 仍可能返回历史记录。

## 回滚方案

1. 回退 `flow/actions.go`、`flow/types.go`、`flow/handler.go`
2. 回退 `flow/flow_id_test.go`、`flow/runtime_fix_test.go`
3. 删除 `list_runs` 入口，恢复只靠 `status/detail/list` 的旧观测方式
