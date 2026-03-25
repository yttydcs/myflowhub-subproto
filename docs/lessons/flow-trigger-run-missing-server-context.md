# Flow Trigger Run Missing Server Context

## Summary
- 当 `flow` 由 trigger 异步启动时，如果启动路径直接使用裸 `context.Background()`，本地 capability provider 虽然能完成核心逻辑，但依赖 `core.ServerFromContext(ctx)` 的通知、事件和同步副作用会一起丢失。

## Lookup Hints
- 症状：
  - `flow` 修改变量成功，但观察端实时不刷新
  - 手动刷新后能读到最新值
  - 直接 `varstore set` 正常，trigger-started flow set 不正常
- 关键词：
  - `tryStartRunWithTrigger`
  - `context.Background()`
  - `core.ServerFromContext(ctx)`
  - `flow::run`
- 触发条件：
  - `event` / `interval` / `var_changed` 触发的 flow
  - flow 节点调用本地 capability provider
- 快速检查：
  - 比较 direct `varstore` 路径和 trigger flow 路径是否只有后者缺实时通知
  - 检查 `flow` 启动路径有没有把 `ctx` 替换成不带 server 的 background context

## Symptoms
- 变量值已经落到本地存储。
- UI 或订阅端没有收到实时更新。
- 手动刷新或重新读取时能看到最新值。

## Impact
- 用户误判为 flow 没有成功执行。
- 依赖订阅事件的 sibling / child 观察端会停留在旧状态。
- 同一个症状可能被误导到 UI 或 provider 侧，增加排查成本。

## Trigger Conditions
- flow 不是通过显式请求上下文同步启动，而是通过 trigger 或后台 capability 启动。
- 启动代码在异步 goroutine 前丢掉了 `server context`。
- 本地被调用能力需要 `server` 才能发送通知、发事件或做上行同步。

## Root Cause
- `flow` 的异步启动路径使用了裸 `context.Background()`，导致 `executeNode(...)` 进入本地 capability provider 时拿不到 `core.ServerFromContext(ctx)`。
- provider 的核心读写仍然成功，所以表现成“值变了但订阅端没收到通知”。

## Investigation Trail
- 先验证 Win `varpool` 订阅链路已存在。
- 再验证直接 `varstore` 写入路径通知正常，排除 `varstore` 基础通知缺失。
- 对比手动 `flow run` 与 trigger-started flow run，发现只有后者使用了不带 server 的后台 context。
- 用回归测试固定：
  - trigger flow 的本地 capability 能否读到 `core.ServerFromContext(ctx)`
  - `flow::run` capability 是否保留调用方上下文

## Resolution
- 统一由 `flow` 侧构造后台运行 context：
  - 若 `Handler` 已绑定 `server`，则在后台 context 中注入该 `server`
  - 若尚未绑定，保留原有 background fallback
- trigger 启动、`ctx == nil` 的 enqueue fallback，以及 `flow::run` capability 都遵守同一条上下文保留规则。

## Prevention / Guardrails
- 异步入口新增或重构时，评审清单要显式检查：
  - 是否保留了 `server context`
  - 是否还需要事件、通知、同步这类可观察副作用
- 如果 helper 需要读共享状态，避免在持锁路径里再次调用会取同一把锁的 helper。
- 当看到“直接写正常、flow 写不触发实时更新”时，先检查 `flow` 启动上下文，再决定是否深入 provider。

## Related Docs
- Change:
  - `D:\project\MyFlowHub3\worktrees\fix-trigger-run-server-context\MyFlowHub-SubProto\docs\change\2026-03-25_flow-trigger-server-context.md`
- Related lesson:
  - `D:\project\MyFlowHub3\worktrees\fix-trigger-run-server-context\MyFlowHub-SubProto\docs\lessons\capability-provider-observable-side-effects.md`
