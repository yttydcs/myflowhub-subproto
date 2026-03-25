# 2026-03-26 flow-trigger-server-context

## Project Goal

- 修复 trigger-started `flow` run 修改变量时，观察端只能手动刷新才能看到最新值的问题。
- 保持现有 `varstore` / Win 订阅链路不变，只修复 `flow` 异步启动路径的上下文保留规则。

## Current State

- 用户现象：
  - 直接 `varstore set` 路径实时同步正常。
  - 节点拓扑为 `节点1(root)` 下挂 `节点2` 和 `节点3` 时，`flow` 修改 `节点1` 变量后，`节点2` 订阅不会实时刷新，但手动刷新能看到最新值。
- 已确认根因：
  - `flow` trigger 启动路径使用了裸 `context.Background()`。
  - 本地 capability provider 执行核心读写成功，但依赖 `core.ServerFromContext(ctx)` 的通知、事件和同步副作用丢失。
- 当前判断：
  - 问题在 `MyFlowHub-SubProto/flow` 的异步启动上下文，不在 `run-dev.ps1`、Win `varpool` 订阅桥或 `varstore` 基础通知语义。

## Workflow Metadata

- Repo: `D:\project\MyFlowHub3\repo\MyFlowHub-SubProto`
- Branch: `fix/trigger-run-server-context`
- Base: `main`
- Worktree: `D:\project\MyFlowHub3\worktrees\fix-trigger-run-server-context\MyFlowHub-SubProto`
- Current stage: `completed`
- Participating modules:
  - `flow`
  - `docs`
- Parallelism assessment:
  - 本轮写集合集中在 `flow` 和相关归档文档，未拆分子 Agent。

## Related Requirements

- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\requirements\flow_data_dag.md`

## Related Specs

- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\flow.md`

## Related Lessons

- `D:\project\MyFlowHub3\repo\MyFlowHub-SubProto\docs\lessons\capability-provider-observable-side-effects.md`
- `D:\project\MyFlowHub3\repo\MyFlowHub-SubProto\docs\lessons\flow-trigger-run-missing-server-context.md`

## Requirements Impact

- `none`

## Specs Impact

- `none`

## Executable Checklist

- [x] `FLOWCTX-1` 梳理 requirements/specs/lessons，并确认问题属于 `flow` 异步上下文而不是 `varstore` 或 Win。
- [x] `FLOWCTX-2` 修复 `flow` trigger/background/capability run 启动路径的 `server context` 保留。
- [x] `FLOWCTX-3` 新增 trigger 路径和 `flow::run` 路径的回归测试。
- [x] `FLOWCTX-4` 运行 `flow` 模块测试并完成 Stage 3.3 review。
- [x] `FLOWCTX-5` 完成 Stage 4 归档到 `docs/change`、`docs/lessons`，并更新索引。

## Task Details

### FLOWCTX-1

- Goal:
  - 在动代码前确认长期真相、相关 lesson 和受影响模块，避免继续误判为 Win 订阅问题。
- Files:
  - `docs/README.md`
  - `docs/lessons/README.md`
  - `docs/lessons/capability-provider-observable-side-effects.md`
  - `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\requirements\flow_data_dag.md`
  - `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\flow.md`
- Acceptance:
  - requirements/specs impact 明确记录为 `none`
  - 已知 lessons 被纳入 plan 和 change
- Tests:
  - 文档与代码链路人工校验
- Rollback:
  - 不适用

### FLOWCTX-2

- Goal:
  - 保证异步 flow run 在 `Handler` 已绑定 server 时，不会丢失 `server context`。
- Files:
  - `flow/handler.go`
- Acceptance:
  - `tryStartRunWithTrigger(...)` 不再裸用 `context.Background()`
  - `enqueueRunWithTrigger(...)` 在 `ctx == nil` 时会补带 `server` 的后台 context
  - `flow::run` capability 不再丢弃调用方 context
- Tests:
  - `go test ./... -count=1 -timeout 120s`（`flow` module，临时 `go.test.work`）
- Rollback:
  - 回退 `flow/handler.go` 中上下文 helper 与 `invokeCapabilityRun(...)` 改动

### FLOWCTX-3

- Goal:
  - 用回归测试固定“异步 flow 丢失 server context”这个缺陷模式。
- Files:
  - `flow/trigger_test.go`
  - `flow/capability_provider_test.go`
- Acceptance:
  - trigger 启动 flow 时，本地 capability provider 能看到 `core.ServerFromContext(ctx)`
  - `flow::run` capability 启动 flow 时，同样保留调用方 `server context`
- Tests:
  - `go test ./... -count=1 -timeout 120s`（`flow` module，临时 `go.test.work`）
- Rollback:
  - 回退新增测试

### FLOWCTX-4

- Goal:
  - 完成 Stage 3.3 review 并确认没有引入额外运行时风险。
- Files:
  - worktree 根 `plan.md`
- Acceptance:
  - 需求覆盖、架构、性能、稳定性、测试均通过 review
  - 已识别并修正持锁路径调用 helper 的自锁风险
- Tests:
  - 复核已执行测试结果，无新增命令
- Rollback:
  - 不适用

### FLOWCTX-5

- Goal:
  - 将本轮结果归档到主仓 docs 树，并补充新的可检索 lesson。
- Files:
  - `docs/change/2026-03-25_flow-trigger-server-context.md`
  - `docs/change/README.md`
  - `docs/lessons/flow-trigger-run-missing-server-context.md`
  - `docs/lessons/README.md`
  - `docs/plan_archive/plan_archive_2026-03-26_flow-trigger-server-context.md`
- Acceptance:
  - change / lesson / plan archive 均可检索
  - requirements/specs/lessons impact 记录完整
- Tests:
  - 文档人工复核
- Rollback:
  - 回退对应文档改动

## Dependencies

- `flow` 的 `Handler.BindServer(...)` 已能缓存 `h.srv`
- 本地 capability provider 依赖 `core.ServerFromContext(ctx)` 才能执行通知、事件和同步副作用

## Risks

- 如果某些路径在 `BindServer(...)` 之前就触发，仍会退化为无 `server` 的后台 context；本轮没有改变初始化顺序约束。
- 异步路径上的上下文 helper 若在持锁代码中不慎重入，容易引入自锁；本轮已修正这类风险。

## Notes

- worktree 根控制文档 `plan.md` 仅用于执行期，不并入主仓根目录；workflow 结束时归档到 `docs/plan_archive`。
