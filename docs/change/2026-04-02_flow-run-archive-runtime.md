# 2026-04-02_flow-run-archive-runtime

## 变更背景 / 目标

- `flow` 运行时此前只在内存里保留 recent terminal runs；执行器重启后，这部分 retained run 会全部丢失。
- 本轮目标是在不改 wire 的前提下，为 retained window 增加可选 local archive，让 `status/detail/list_runs` 在重启后仍能查询最近保留的 run。

## 具体变更内容

### 修改

- `flow/config.go`
  - 新增 `flow.run_archive_enabled` 配置解析，默认关闭
- `flow/handler.go`
  - `Init()` 在定义加载后预热 retained archive
  - `executeFlow(...)` 在 run 终态时先 archive，再做 retained prune
  - `pruneRunsLocked(...)` 返回被裁掉的 terminal run，用于同步删除旧 archive 文件
- `flow/run_archive.go`
  - 新增 retained archive sidecar：
    - `flow.base_dir/_runs/<flow_id>/<run_id>.json`
  - 新增 archive record <-> `runState` 转换
  - 新增 archive save/load/prune helper
- `flow/runtime_fix_test.go`
  - 增加 retained archive reload 测试
  - 增加旧 archive pruning 测试
  - 增加 delete 后 archive 仍可查询测试

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

- `RC-P1-4`
  - archive config
  - archive save/load/prune
  - retained archive regression tests

## 经验 / 教训摘要

- retained archive 最稳的切口不是新增 wire，而是把现有 retained window 变成可持久化 sidecar，并继续复用原有查询面。
- `archive -> prune` 顺序要固定；否则会出现当前 run 已被内存裁掉但 archive 还没写入的窗口。
- archive 默认关闭可以避免给手工构造 handler 的测试或未显式配置的部署引入意外本地 I/O。

## 可复用排查线索

- 症状：
  - recent run 重启后消失
  - delete 后 retained run 查不到
  - `flow.max_retained_runs=2` 时 archive 目录仍不断增长
- 触发条件：
  - run 终态时没有先写 archive
  - `Init()` 没有加载 `_runs`
  - prune 只删了内存 `runs`，没删旧 archive 文件
- 关键词 / 错误文本：
  - `flow.run_archive_enabled`
  - `_runs`
  - `loadArchivedRunsFromDisk`
  - `finalizeRun`
- 快速检查：
  1. 看 `flow/run_archive.go` 是否把 retained run 落到 `flow.base_dir/_runs/...`
  2. 看 `Init()` 是否调用 `loadArchivedRunsFromDisk()`
  3. 看 `pruneRunsLocked(...)` 的返回值是否被用于删除旧 archive
  4. 看 `runtime_fix_test.go` 是否覆盖 reload / prune / delete 三类场景

## 关键设计决策与权衡

- archive 只覆盖 retained window，不扩展为永久历史
  - 好处：不改变现有 `flow.max_retained_runs` 的窗口心智模型
  - 代价：窗口外 run 仍不承诺长期保留
- 采用 local JSON sidecar，而不是并入现有 definition persistence 接口
  - 好处：职责边界清晰，改动面小
  - 代价：未来如需 PG archive，还要再抽独立 backend

## 测试与验证方式 / 结果

- `D:\project\MyFlowHub3`
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-run-control-phase3\go.work go test github.com/yttydcs/myflowhub-subproto/flow/... -count=1 -p 1`
- 结果：通过

## 潜在影响

- 启用 archive 后，retained window 内的 terminal run 会额外落本地 JSON。
- 未启用 archive 的环境行为不变。

## 回滚方案

1. 回退 `flow/config.go`
2. 回退 `flow/handler.go`
3. 回退 `flow/run_archive.go`
4. 回退 `flow/runtime_fix_test.go`
5. 恢复“retained run 仅内存保留”的运行时行为

## 子Agent执行轨迹

- 本轮未使用子Agent
