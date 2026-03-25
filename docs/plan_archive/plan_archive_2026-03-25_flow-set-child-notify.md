# 2026-03-25 flow-set-child-notify

## Project Goal

- 修复 `flow` 通过本地 capability 调用 `varstore::set/revoke` 时，没有把变量变更通知到已订阅子节点的问题。
- 保持现有 `flow` 本地调用 capability 的入口不变，只补齐 `varstore` provider 路径与协议语义的一致性。

## Current State

- 用户现象：节点 1 上的 `flow` 修改变量后，节点 2 的 `Win varpool` 已订阅但未收到变更。
- 已确认根因：
  - `flow` 本地 `call` 节点优先走 capability registry。
  - `varstore::set/revoke` 的 capability 实现当前只做本地读写和 `varstore.changed/deleted` 触发事件发布。
  - 正常协议路径 `handleSet/handleRevoke` 还会执行订阅推送、删除推送以及必要的上行缓存同步。
- 当前判断：问题在 `MyFlowHub-SubProto/varstore` provider 路径，不在 `Win varpool` 订阅展示层。

## Workflow Metadata

- Repo: `D:\project\MyFlowHub3\repo\MyFlowHub-SubProto`
- Branch: `fix/flow-set-child-notify`
- Base: `main`
- Worktree: `D:\project\MyFlowHub3\worktrees\MyFlowHub-SubProto-fix-flow-set-child-notify`
- Current stage: `completed`
- Participating modules:
  - `varstore`
  - `flow`（只读验证调用链，不计划改动）
- Parallelism assessment:
  - 本轮主写集合集中在 `varstore`，改动耦合强，不拆分子 Agent。

## Related Requirements

- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\requirements\flow_data_dag.md`

## Related Specs

- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\flow.md`
- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\varstore.md`

## Related Lessons

- `D:\project\MyFlowHub3\repo\MyFlowHub-SubProto\docs\lessons\capability-provider-observable-side-effects.md`

## Requirements Impact

- `none`

## Specs Impact

- `none`

## Executable Checklist

- [x] `FLOWVARNOTIFY-1` 为 `varstore` capability provider 增加回归测试，覆盖已订阅子节点的变更/删除通知。
- [x] `FLOWVARNOTIFY-2` 修复 `varstore::set/revoke` provider 路径，补齐与协议路径一致的传播行为。
- [x] `FLOWVARNOTIFY-3` 运行 `varstore` 相关单测并补充必要的定向回归验证。
- [x] `FLOWVARNOTIFY-4` 完成 Stage 3.3 Code Review 记录。
- [x] `FLOWVARNOTIFY-5` 完成 Stage 4 归档到 `docs/change`，必要时补 lessons。

## Task Details

### FLOWVARNOTIFY-1

- Goal:
  - 先用测试固化当前缺陷，避免只凭手工复现修复。
- Files:
  - `varstore/capability_provider_test.go`
- Acceptance:
  - 新增用例能验证 capability `set` 会向已订阅子节点发送 `var_changed`。
  - 新增用例能验证 capability `revoke` 会向已订阅子节点发送 `var_deleted`。
- Tests:
  - `go test ./... -count=1 -p 1`（`varstore`，临时 `go.work`）
- Rollback:
  - 回退新增测试文件改动。

### FLOWVARNOTIFY-2

- Goal:
  - 在不改 `flow` 调用入口的前提下，让 `varstore` capability 路径复用既有传播语义。
- Files:
  - `varstore/varstore.go`
- Acceptance:
  - `varstore::set` 成功后会更新本地记录、发出 flow trigger event、向订阅者推送、并执行必要的上行缓存同步。
  - `varstore::revoke` 成功后会删除本地记录、发出 flow trigger event、向订阅者推送删除、并执行必要的上行缓存同步。
  - 不引入与现有 `handleSet/handleRevoke` 相冲突的重复通知。
- Tests:
  - `go test ./... -count=1 -p 1`（`varstore`，临时 `go.work`）
- Rollback:
  - 回退 `varstore/varstore.go` 中 provider 路径改动。

### FLOWVARNOTIFY-3

- Goal:
  - 验证修复不会破坏现有 varstore / flow 能力调用基线。
- Files:
  - 无新增文件，执行测试验证
- Acceptance:
  - `varstore` 模块测试通过。
  - `flow` 模块测试通过，确认 capability fallback 未回归。
- Tests:
  - `go test ./... -count=1 -p 1`（`varstore`，临时 `go.work`）
  - `go test ./... -count=1 -p 1`（`flow`，临时 `go.work`）
- Rollback:
  - 若回归无法快速收敛，先回退 provider 改动并保留失败线索。

### FLOWVARNOTIFY-4

- Goal:
  - 按 Stage 3.3 清单完成代码复核，确认覆盖、风险、测试与一致性。
- Files:
  - 当前归档计划
- Acceptance:
  - Review 结论完整记录，且无阻塞项。
- Tests:
  - 复核已执行验证结果，无新增命令。
- Rollback:
  - 不适用。

### FLOWVARNOTIFY-5

- Goal:
  - 将本轮排查与修复结果归档到 `docs/change`，并沉淀为可复用 lesson。
- Files:
  - `docs/change/2026-03-25_flow-set-child-notify.md`
  - `docs/change/README.md`
  - `docs/lessons/README.md`
  - `docs/lessons/capability-provider-observable-side-effects.md`
- Acceptance:
  - 归档包含 requirements/specs/lessons impact、任务映射、验证、回滚与排查线索。
- Tests:
  - 文档人工复核。
- Rollback:
  - 回退对应文档改动。

## Dependencies

- `flow` 本地 `call` 节点的 capability fallback 行为保持不变。
- `varstore` 传播辅助函数 `propagateChange` / `propagateDelete` / 上行同步路径可直接复用。

## Risks

- capability provider 没有请求头上下文，传播时需要谨慎处理“排除源节点”逻辑，避免错误跳过合法订阅者。
- 若 provider 当前被其他场景用于“静默本地写入”，补齐通知后会改变观测行为；需要以现有协议语义为基线确认。

## Notes

- 根工作树控制文档已在 workflow 结束时归档到 `docs/plan_archive`，不再保留仓库根 `plan.md`。
