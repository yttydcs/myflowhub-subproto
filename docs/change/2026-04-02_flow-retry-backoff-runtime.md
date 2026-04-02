# 2026-04-02_flow-retry-backoff-runtime

## 变更背景 / 目标

- `flow` 节点配置已有 `retry`，但失败后会立即重试，容易对远程 `exec.call` 或短暂故障节点形成连续冲击。
- 本轮目标是在运行时补齐固定间隔 retry backoff，同时保持旧 graph 兼容。

## 具体变更内容

### 修改

- `flow/handler.go`
  - 在节点重试循环中新增 `retry_backoff_ms` 处理
  - 新增 `waitRetryBackoff(...)`，等待期间监听 `ctx.Done()`
  - 在 `validateSetNodeKindAndSpec(...)` 中拒绝负值 `retry_backoff_ms`
- `flow/graph_test.go`
  - 新增负值 backoff 拒绝测试
- `flow/runtime_fix_test.go`
  - 新增固定间隔 backoff 生效测试
  - 新增 backoff 等待期间取消生效测试

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

- 无

## 对应 plan.md 任务映射

- `RC-P1-1`
  - retry loop fixed backoff
  - graph validation
  - runtime tests

## 经验 / 教训摘要

- backoff 等待不能直接 `time.Sleep`，否则 `cancel_run` / `delete` 会被拖慢到等待结束后才生效。
- 固定间隔策略可以先把“立即重试”这个主要问题解决掉，而不需要一次性引入完整退避矩阵。

## 可复用排查线索

- 症状：
  - 节点失败后立刻再次尝试
  - 等待重试期间发出取消，但 run 仍继续下一次 attempt
  - graph 中给了负值 backoff 却未被拒绝
- 触发条件：
  - 重试循环只看 `retry` 次数，没有单独等待 helper
  - backoff 等待没有监听 `ctx.Done()`
- 关键词 / 错误文本：
  - `retry_backoff_ms`
  - `waitRetryBackoff`
  - `retry_backoff_ms must be >= 0`
- 快速检查：
  1. 看 `flow/handler.go` 的重试循环是否只在“失败且仍有剩余尝试”时等待
  2. 看 `waitRetryBackoff(...)` 是否用 `select` 监听 `ctx.Done()`
  3. 看 `graph_test.go` 是否锁定负值 backoff 校验

## 关键设计决策与权衡

- 保持固定间隔字段，不直接做指数退避
  - 好处：协议和测试面最小
  - 代价：失败恢复节奏仍较简单
- 旧 graph 默认 `retry_backoff_ms=0`
  - 好处：向后兼容，不改变旧部署行为
  - 代价：调用方若需要新策略，必须显式配置

## 测试与验证方式 / 结果

- `D:\project\MyFlowHub3`
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-run-control-phase3\go.work go test github.com/yttydcs/myflowhub-subproto/flow/... -count=1 -p 1`
- 结果：通过

## 潜在影响

- 新 graph 可以显式声明固定重试间隔。
- 旧 graph 未设置该字段时仍保持立即重试行为。

## 回滚方案

1. 回退 `flow/handler.go`
2. 回退 `flow/graph_test.go` 与 `flow/runtime_fix_test.go`
3. 恢复“只有 retry 次数、无 backoff 等待”的旧运行时逻辑

## 子Agent执行轨迹

- 本轮未使用子Agent
