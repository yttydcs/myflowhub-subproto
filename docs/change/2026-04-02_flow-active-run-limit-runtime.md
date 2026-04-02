# 2026-04-02_flow-active-run-limit-runtime

## 变更背景 / 目标

- `flow` 运行时此前对活动 run 上限只有隐式行为：
  - 手动 `run` 默认允许并发重入
  - trigger 默认在已有活动 run 时跳过
- 本轮目标是在不破坏旧 flow 兼容性的前提下，为运行时补齐显式 `max_active_runs` gate，并让检查与 run 登记保持竞态安全。

## 具体变更内容

### 修改

- `flow/handler.go`
  - 新增 `validateFlowRunConfig(...)`，拒绝负值 `max_active_runs`
  - `applySetLocal(...)` 在保存前校验 active-run 配置
  - `loadFlowsFromDisk()` 跳过 persisted 的负值 `max_active_runs`
  - `handleGetLocal(...)` 回显 `max_active_runs`
  - 新增 `activeRunCountLocked(...)`、`effectiveMaxActiveRuns(...)`
  - 新增统一的 start gate：`prepareQueuedRunLocked(...)` / `newQueuedRunStateLocked(...)`
  - 手动 `run` 在超限时返回 `409 active run limit reached`
  - trigger 启动在超限时跳过，不创建新 run
- `flow/flow_id_test.go`
  - 增加负值 `max_active_runs` 保存拒绝与落盘加载跳过测试
- `flow/runtime_fix_test.go`
  - 增加手动 `run` 超限返回 `409`
  - 增加 `get` 回显 `max_active_runs`
- `flow/trigger_test.go`
  - 锁定未设置字段时的 legacy trigger 单飞行为
  - 锁定 `max_active_runs=0` 时 trigger 允许重叠

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

## Related lessons

- `D:\project\MyFlowHub3\worktrees\subproto-run-control-phase1\docs\lessons\flow-trigger-run-missing-server-context.md`

## 对应 plan.md 任务映射

- `RC-P1-2`
  - set validation / load guard
  - get echo
  - manual run conflict gate
  - trigger active-run gate
  - runtime / trigger tests

## 经验 / 教训摘要

- active-run 判断和 run 注册必须放在同一把 `h.mu` 下，否则双击或双 trigger 会同时穿透上限。
- `max_active_runs` 不能直接覆盖 legacy 默认值；未设置字段时必须继续保留“手动可并发、trigger 单飞”的历史行为。
- 统一 start gate 比分别在 manual/trigger 两条路径各写一份判断更稳，后续要加 queue 或 `cancel_previous` 时也有固定落点。

## 可复用排查线索

- 症状：
  - `max_active_runs=1` 时连续两次手动 `run` 仍同时启动
  - trigger 在已有活动 run 时没有按预期跳过
  - `get` 返回缺少 `max_active_runs`
  - 磁盘中出现负值 `max_active_runs` 的脏定义仍被加载
- 触发条件：
  - active-run 检查和 `recordRunLocked(...)` 不在同一临界区
  - 只改了 trigger 路径，没改 manual `run`
  - 保存校验和磁盘加载校验口径不一致
- 关键词 / 错误文本：
  - `max_active_runs`
  - `active run limit reached`
  - `effectiveMaxActiveRuns`
  - `prepareQueuedRunLocked`
- 快速检查：
  1. 看 `prepareQueuedRunLocked(...)` 是否先检查上限再登记 run，且两步都在锁内
  2. 看 `effectiveMaxActiveRuns(...)` 是否区分 `nil` / `0` / `>0`
  3. 看 `handleGetLocal(...)` 是否回写 `MaxActiveRuns`
  4. 看 `flow/trigger_test.go` 是否锁定 legacy trigger 单飞和显式 unlimited 两种场景

## 关键设计决策与权衡

- 通过 `max_active_runs` 统一表达 active-run cap，而不是新增 `allow_reentry` 布尔值
  - 好处：既能表达 legacy 兼容，又能表达显式无限制和具体上限
  - 代价：字段语义需要额外解释 `nil` / `0` / `>0`
- trigger 超限时保持跳过，而不是返回失败 run
  - 好处：维持当前 trigger 行为模型，不凭空制造伪运行记录
  - 代价：外部调用方只能通过缺失新 run 来观察跳过结果

## 测试与验证方式 / 结果

- `D:\project\MyFlowHub3`
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-run-control-phase3\go.work go test github.com/yttydcs/myflowhub-subproto/flow/... -count=1 -p 1`
- 结果：通过

## 潜在影响

- `max_active_runs>0` 的 flow 现在会对手动和 trigger 启动统一执行 active-run 限流。
- `max_active_runs=0` 可显式放开 trigger 重叠运行。
- 未设置该字段的旧 flow 继续保持历史兼容行为。

## 回滚方案

1. 回退 `flow/handler.go`
2. 回退 `flow/flow_id_test.go`、`flow/runtime_fix_test.go`、`flow/trigger_test.go`
3. 恢复原有“手动重入 / trigger 单飞”的隐式运行时行为

## 子Agent执行轨迹

- 本轮未使用子Agent
