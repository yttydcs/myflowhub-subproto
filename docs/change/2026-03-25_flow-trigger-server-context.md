# 2026-03-25_flow-trigger-server-context

## 变更背景 / 目标
- 用户反馈：`flow` 修改变量时，Win 侧 `varpool` 已订阅但不会实时刷新，手动刷新后能看到最新值。
- 结合拓扑复现后确认：直接 `varstore set` 路径正常，只有 trigger 启动的 `flow` 写变量时丢失了实时通知。
- 本次目标是在不修改 `varstore` 语义和 Win 订阅链路的前提下，恢复 trigger-started flow run 的可观察副作用。

## 具体变更内容
- 在 `flow/handler.go` 中新增后台运行上下文 helper：
  - 当 `Handler` 已绑定 `server` 时，把它注入到后台 `context`。
  - 当没有已绑定 `server` 时，维持原有 `context.Background()` 退化行为。
- 修复 trigger 启动路径：
  - `tryStartRunWithTrigger(...)` 不再直接使用裸 `context.Background()`。
- 修复后台 enqueue 兜底路径：
  - `enqueueRunWithTrigger(...)` 在 `ctx == nil` 时会补带 `server` 的后台上下文。
- 修复 `flow::run` capability 路径：
  - `invokeCapabilityRun(...)` 不再丢弃调用方传入的 `ctx`。
- 新增回归测试：
  - trigger 启动的 flow 调用本地 capability provider 时，provider 能看到 `core.ServerFromContext(ctx)`。
  - `flow::run` capability 启动 flow 时，同样保留调用方 `server context`。

## Requirements impact
- none

## Specs impact
- none

## Lessons impact
- updated

## Related requirements
- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\requirements\flow_data_dag.md`

## Related specs
- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\flow.md`

## Related lessons
- `D:\project\MyFlowHub3\worktrees\fix-trigger-run-server-context\MyFlowHub-SubProto\docs\lessons\capability-provider-observable-side-effects.md`
- `D:\project\MyFlowHub3\worktrees\fix-trigger-run-server-context\MyFlowHub-SubProto\docs\lessons\flow-trigger-run-missing-server-context.md`

## 对应 plan.md 任务映射
- `T1`:
  - `flow/handler.go`
  - 保留 trigger/background/capability run 启动路径中的 `server context`
- `T2`:
  - `flow/trigger_test.go`
  - `flow/capability_provider_test.go`
  - 锁定 trigger 和 `flow::run` 两条回归路径
- `T3`:
  - `go test ./... -count=1 -timeout 120s`（在 `flow` module 目录执行，并通过临时 `go.test.work` 指向 worktree module）
- `T4`:
  - 本归档
  - 新增 lesson 文档并更新 lessons 索引

## 经验 / 教训摘要
- “变量值更新但观察端不刷新”不只可能是 provider 漏副作用，也可能是异步 flow 启动路径把 `server context` 丢了。
- 对异步运行路径统一收口上下文构造，比在每个触发入口手工补 `WithServerContext(...)` 更稳妥。
- 在持锁路径复用 helper 时，要先确认 helper 是否会再次取同一把锁，否则很容易引入自锁。

## 可复用排查线索
- 症状：
  - `flow` 执行成功
  - 手动刷新能读到新值
  - 订阅端实时 UI 不刷新
- 触发条件：
  - `event` / `interval` / `var_changed` 触发的 flow run
  - flow 节点走本地 capability provider
- 关键词：
  - `tryStartRunWithTrigger`
  - `context.Background()`
  - `core.ServerFromContext(ctx)`
  - `flow::run`
- 快速检查：
  - 先比较“直接 `varstore set`”和“trigger-started flow set”是否只有后者失效
  - 检查 trigger 启动路径是否把后台 context 构造成不带 `server`
  - 如果 provider 代码看起来已补齐副作用，继续检查 `flow` 是否在进入 provider 前就丢了上下文

## 关键设计决策与权衡
- 选择在 `flow` 侧修复，而不是继续改 `varstore`：
  - 直接 `varstore` 写路径已经正常，问题定位更符合 `flow` 上下文丢失。
- 选择新增 helper 统一构造后台 context：
  - 这样能同时覆盖 trigger 路径和 `ctx == nil` 的 enqueue 兜底路径。
- 顺手修复 `flow::run` capability 路径：
  - 变更很小，但能避免同类问题在嵌套 flow 或 capability 链路里再次出现。

## 测试与验证方式 / 结果
- 命令：
  - `go test ./... -count=1 -timeout 120s`
- 位置：
  - `D:\project\MyFlowHub3\worktrees\fix-trigger-run-server-context\MyFlowHub-SubProto\flow`
- 结果：
  - 通过
- 额外说明：
  - 由于顶层 `go.work` 不包含新建 worktree，本次验证使用了 worktree 外部的临时 `go.test.work` 文件指向 worktree modules；验证结束后已删除临时文件。

## 潜在影响
- trigger-started flow run 现在会继承已绑定 `server`，本地 capability provider 可以重新发出通知、事件和上行同步。
- 若某条路径在 `BindServer` 之前触发，仍会退化为原有无 `server` 的后台 context；本次没有改变这类初始化时序约束。

## 回滚方案
- 回滚 `flow/handler.go` 中的后台 context helper 和 `invokeCapabilityRun(...)` 上下文传递。
- 删除两条回归测试并重新验证既有 `flow` 测试集。

## 子Agent执行轨迹
- 未使用子Agent。
