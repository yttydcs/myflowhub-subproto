# 2026-04-02_flow-trigger-dedup-runtime

## 变更背景 / 目标

- `flow` 运行时此前已经具备 active-run gate，但对短窗口内重复的 `event/var_changed` trigger 仍会再次尝试启动。
- 本轮目标是在不引入持久化成本的前提下，为 trigger 启动补齐最小 dedup window，并把去重判断放进现有 start gate，避免竞态穿透。

## 具体变更内容

### 修改

- `flow/handler.go`
  - `Handler` 新增 `triggerDedup map[string]map[string]time.Time`
  - `validateTrigger(...)` 拒绝负值 `dedup_window_ms`，并拒绝 `interval` + `dedup_window_ms>0`
  - 在 `prepareQueuedRunLocked(...)` 中加入 trigger dedup 检查和窗口记录
  - dedup key 复用规范化 trigger 上下文：
    - `event` 走 `buildTopicTriggerContext(...)`
    - `var_changed` 走 `buildVarChangedTriggerContext(...)`
  - `set/delete` 时清理对应 flow 的 dedup 状态
- `flow/trigger_test.go`
  - 增加窗口内重复 event 被抑制测试
  - 增加不同 payload 不被误杀测试
  - 增加 `interval` 不支持 dedup 校验覆盖
- `flow/flow_id_test.go`
  - 增加负值 `dedup_window_ms` 保存拒绝测试

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

- `RC-P1-3`
  - trigger dedup validation
  - trigger dedup start gate
  - dedup state cleanup
  - runtime / trigger tests

## 经验 / 教训摘要

- dedup 检查和 run 登记必须在同一把 `h.mu` 下完成，否则重复 trigger 仍可能在竞争窗口里同时穿透。
- dedup key 必须复用已规范化的 trigger 上下文，而不是重新手写一套字段拼装逻辑，否则不同路径很容易出现“看起来相同、实际 key 不同”的漂移。
- `set/delete` 后要清理旧 dedup 状态，否则新定义或重建的 flow 会被上一版窗口误伤。

## 可复用排查线索

- 症状：
  - 相同事件在短窗口内仍连续启动多个 run
  - 不同 payload 的事件也被错误去重
  - 重设或删除后重新创建 flow，首次 trigger 仍被窗口挡住
- 触发条件：
  - dedup key 没用规范化 trigger 上下文
  - dedup 检查放在锁外，run 记录放在锁内
  - `set/delete` 后没有清理 dedup map
- 关键词 / 错误文本：
  - `dedup_window_ms`
  - `triggerDedup`
  - `prepareQueuedRunLocked`
  - `buildTopicTriggerContext`
  - `buildVarChangedTriggerContext`
- 快速检查：
  1. 看 `prepareQueuedRunLocked(...)` 是否在锁内完成 dedup 检查和更新时间戳
  2. 看 dedup key 是否直接来源于规范化 trigger JSON
  3. 看 `applySetLocal(...)` / delete 路径是否清理 `triggerDedup[flowID]`
  4. 看 `flow/trigger_test.go` 是否覆盖窗口内重复与不同 payload 两种场景

## 关键设计决策与权衡

- 仅对 `event/var_changed` 做 dedup，不对 `interval` 开放
  - 好处：语义清晰，避免把调度频率控制和重复通知抑制混在一起
  - 代价：若后续需要 interval 侧更复杂抖动控制，需要单独设计
- dedup 命中时直接跳过，不生成失败 run
  - 好处：维持 trigger 启动的现有“无新 run 即跳过”模型
  - 代价：外部只能通过缺失新 run 观察到 dedup 命中

## 测试与验证方式 / 结果

- `D:\project\MyFlowHub3`
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-run-control-phase3\go.work go test github.com/yttydcs/myflowhub-subproto/flow/... -count=1 -p 1`
- 结果：通过

## 潜在影响

- 配置 `dedup_window_ms>0` 的 `event/var_changed` trigger 在窗口内重复出现时不会生成额外 run。
- dedup 状态只保存在内存中；重启后窗口记忆会清空。
- 手动 `run` 和未配置 dedup 的 flow 行为不受影响。

## 回滚方案

1. 回退 `flow/handler.go`
2. 回退 `flow/trigger_test.go`、`flow/flow_id_test.go`
3. 恢复“trigger 不做显式 dedup” 的运行时行为

## 子Agent执行轨迹

- 本轮未使用子Agent
