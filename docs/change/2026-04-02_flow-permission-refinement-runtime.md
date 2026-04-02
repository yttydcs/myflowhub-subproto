# 2026-04-02_flow-permission-refinement-runtime

## 变更背景 / 目标

- `flow` 已支持 `run`、`cancel_run`、`status`、`detail`、`list_runs`、`list`、`get`，但这些动作尚未分离为稳定的 run/read 权限边界。
- 本轮目标是在不破坏既有逐级授权和父节点信任模型的前提下，为 `flow` 运行时补齐显式权限判定。

## 具体变更内容

### 修改

- `flow/types.go`
  - 引入 `permFlowRun` / `permFlowRead` alias
- `flow/handler.go`
  - `flow::run` capability descriptor 增加 `flow.run`
  - 新增 `forwardToExecutorWithPerm(...)`
  - `run` / `cancel_run` 接入 `flow.run`
  - `status` / `detail` / `list_runs` / `list` / `get` 接入 `flow.read`
  - 保留来自父节点下行请求的已授权直达语义
- `flow/capability_provider_test.go`
  - 增加 `flow::run` 权限描述断言
- `flow/delete_test.go`
  - 增加 run/read permission denied 覆盖

### 删除

- 无

## Requirements impact

- `none`

## Specs impact

- `none`

## Lessons impact

- `none`

## Related requirements

- `D:\project\MyFlowHub3\worktrees\server-run-control-phase1\docs\requirements\flow_data_dag.md`

## Related specs

- `D:\project\MyFlowHub3\worktrees\server-run-control-phase1\docs\specs\flow.md`
- `D:\project\MyFlowHub3\worktrees\server-run-control-phase1\docs\specs\auth.md`
- `D:\project\MyFlowHub3\worktrees\proto-run-control-phase1\docs\protocol_map.md`

## Related lessons

- 无

## 对应 plan.md 任务映射

- `RC-P0-3`
  - runtime route helper 权限化
  - action / capability 权限接线
  - permission denied 测试

## 经验 / 教训摘要

- Flow 的 permission gate 不能只加在 executor 本地，否则会绕开现有逐级授权链路。
- capability descriptor 的权限声明必须与 action handler 使用同一权限语义，否则控制面看到的授权要求会失真。

## 可复用排查线索

- 症状：
  - `run` / `status` 等动作在无权限时没有返回 `403`
  - `flow::run` 被查询时看不到权限要求
  - 父节点转发下行请求被重复判权导致链路异常
- 触发条件：
  - 只在某个 handler 本地单点加 `hasPermission(...)`
  - capability descriptor 未同步更新 `Permissions`
- 关键词 / 错误文本：
  - `permission denied`
  - `flow::run`
  - `forwardToExecutorWithPerm`
  - `permFlowRun`
  - `permFlowRead`
- 快速检查：
  1. 看 `flow/handler.go` 的 run/read 动作是否都通过同一 permission-aware helper
  2. 看父节点分支是否保留“已授权请求直接执行/转发”逻辑
  3. 看 `flow/capability_provider_test.go` 是否锁定 `flow::run` 权限描述

## 关键设计决策与权衡

- 复用统一 helper 而不是给每个 handler 各自补权限
  - 好处：减少逻辑分叉，后续扩展更容易
  - 代价：helper 需要同时兼顾本地执行、上送 LCA、下送 executor 三种路由
- 保持父节点信任模型
  - 好处：不破坏现有授权链路
  - 代价：必须确保判权发生在正确的本地 executor / LCA 节点

## 测试与验证方式 / 结果

- `D:\project\MyFlowHub3`
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-run-control-phase3\go.work go test github.com/yttydcs/myflowhub-subproto/flow/... -count=1 -p 1`
- 结果：通过

## 潜在影响

- 自定义权限配置若未授予 `flow.run` / `flow.read`，相关动作会按预期返回 `403`。
- 默认 `admin/node` 已在 Core / Server runtime 同步补齐，不影响开箱行为。

## 回滚方案

1. 回退 `flow/types.go`、`flow/handler.go`
2. 回退 `flow/capability_provider_test.go`、`flow/delete_test.go`
3. 恢复原有无显式 run/read 权限的路由逻辑

## 子Agent执行轨迹

- 本轮未使用子Agent
