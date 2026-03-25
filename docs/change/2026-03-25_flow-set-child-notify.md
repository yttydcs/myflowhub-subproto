# 2026-03-25_flow-set-child-notify

## 变更背景 / 目标

- 背景：
  - 用户反馈节点 1 上的 `flow` 调用 `varstore::set` 修改变量后，已订阅的子节点 2 没有在 `Win varpool` 中看到更新。
  - 排查后确认 `flow` 本地 `call` 节点会走 capability registry，而 `varstore::set/revoke` 的 capability provider 只做本地读写和 flow trigger event，没有补齐订阅通知与上行缓存同步。
- 目标：
  - 修复 `varstore` capability provider 路径，使其与常规协议写路径保持一致的可观察行为。

## 具体变更内容

### 新增

- `varstore/capability_provider_test.go`
  - 新增 capability `set` 回归测试，校验：
    - 已订阅子节点会收到 `var_changed`
    - 父链会收到 `up_set`
  - 新增 capability `revoke` 回归测试，校验：
    - 已订阅子节点会收到 `var_deleted`
    - 父链会收到 `up_revoke`

### 修改

- `varstore/varstore.go`
  - `invokeCapabilitySet()` 改为在本地保存记录后复用现有传播逻辑：
    - `propagateChange()` / `handleVisibilityDowngrade()`
    - 向父链发送 `up_set`
    - 在 `actor != owner` 时补发 `notify_set`
  - `invokeCapabilityRevoke()` 改为在删除记录后复用现有传播逻辑：
    - `handleDeletion()`
    - 向父链发送 `up_revoke`
    - 在 `actor != owner` 时补发 `notify_revoke`
  - 新增 `capabilityActorID()`，统一在 capability 路径下推导本地 actor node。

### 删除

- 无。

## Related Plan

- `D:\project\MyFlowHub3\repo\MyFlowHub-SubProto\docs\plan_archive\plan_archive_2026-03-25_flow-set-child-notify.md`

## Requirements Impact

- `none`

## Specs Impact

- `none`

## Lessons Impact

- `updated`

## Related Requirements

- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\requirements\flow_data_dag.md`

## Related Specs

- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\flow.md`
- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\varstore.md`

## Related Lessons

- `D:\project\MyFlowHub3\repo\MyFlowHub-SubProto\docs\lessons\capability-provider-observable-side-effects.md`

## 对应 `plan.md` 任务映射

- `FLOWVARNOTIFY-1`
  - `varstore/capability_provider_test.go`
- `FLOWVARNOTIFY-2`
  - `varstore/varstore.go`
- `FLOWVARNOTIFY-3`
  - `varstore`、`flow` 模块回归测试
- `FLOWVARNOTIFY-4`
  - Stage 3.3 Code Review
- `FLOWVARNOTIFY-5`
  - 当前变更归档与 lessons 更新

## 经验 / 教训摘要

- capability provider 不能只复用“本地数据写入”而漏掉订阅推送、缓存同步这类 observable side effects。
- 当 `flow` 通过 capability registry 调本地方法时，问题可能不在 `flow` 本身，而在 provider 是否完整复用了协议 handler 语义。

## 可复用排查线索

- 症状：
  - `flow` 执行成功，变量值本地已变，但 `Win varpool` 已订阅节点看不到更新。
- 触发条件：
  - 通过 `flow` 本地 `call` 节点调用 `varstore::set` / `varstore::revoke`
  - 观察方依赖 `var_changed` / `var_deleted` 推送，而不是手动刷新
- 关键词：
  - `flow varstore set no notify`
  - `varpool subscribed but not updated`
  - `invokeCapabilitySet`
  - `propagateChange`
  - `up_set`
- 快速检查：
  - 看 `flow/handler.go` 是否走 capability registry fallback
  - 看 provider 的 `invokeCapability*()` 是否只改本地记录，没调用 `propagateChange` / `handleDeletion`
  - 看父链是否收到 `up_set` / `up_revoke`

## 关键设计决策与权衡

- 决策：修复点放在 `varstore` provider，而不是改 `flow` 调度器。
  - 原因：`flow` 的本地 capability fallback 本身是正确入口，缺的是 provider 侧副作用复用。
  - 权衡：改动最小，同时所有通过 capability 调用 `varstore` 的路径都能受益。

- 决策：补齐上行缓存同步，而不是只修子节点通知。
  - 原因：仅修 `var_changed` / `var_deleted` 仍会让祖先链缓存保持陈旧。
  - 权衡：多一次既有 `up_*` 发包，但这本就是协议写路径的应有语义。

## 测试与验证方式 / 结果

- `go test ./... -count=1 -p 1`
  - workdir: `D:\project\MyFlowHub3\worktrees\MyFlowHub-SubProto-fix-flow-set-child-notify\varstore`
  - 结果：通过
  - 说明：使用临时 `go.work` 指向本地 `varstore/exec` 与 `repo/MyFlowHub-Core/Proto`，测试后已删除，不属于交付内容。

- `go test ./... -count=1 -p 1`
  - workdir: `D:\project\MyFlowHub3\worktrees\MyFlowHub-SubProto-fix-flow-set-child-notify\flow`
  - 结果：通过
  - 说明：使用临时 `go.work` 指向本地 `flow/broker/exec` 与 `repo/MyFlowHub-Core/Proto`，确认 capability fallback 未回归。

## 潜在影响与回滚方案

- 潜在影响：
  - 若此前有场景依赖 capability `varstore::set/revoke` 的“静默本地写入”行为，本次会让这些路径开始产生订阅通知与上行同步。
- 回滚方案：
  - 回退 `varstore/varstore.go`
  - 回退 `varstore/capability_provider_test.go`
  - 重新执行上述 `varstore` / `flow` 测试确认恢复

## 子Agent执行轨迹

- 本轮未使用子Agent。
- Task ID → Agent → Worktree → 文件 / 范围 → 验收结果
  - `FLOWVARNOTIFY-1` → 主Agent → `D:\project\MyFlowHub3\worktrees\MyFlowHub-SubProto-fix-flow-set-child-notify` → `varstore/capability_provider_test.go` → 通过
  - `FLOWVARNOTIFY-2` → 主Agent → `D:\project\MyFlowHub3\worktrees\MyFlowHub-SubProto-fix-flow-set-child-notify` → `varstore/varstore.go` → 通过
  - `FLOWVARNOTIFY-3` → 主Agent → `D:\project\MyFlowHub3\worktrees\MyFlowHub-SubProto-fix-flow-set-child-notify` → `varstore` / `flow` 测试 → 通过
  - `FLOWVARNOTIFY-4` → 主Agent → `D:\project\MyFlowHub3\worktrees\MyFlowHub-SubProto-fix-flow-set-child-notify` → Stage 3.3 Review → 通过
  - `FLOWVARNOTIFY-5` → 主Agent → `D:\project\MyFlowHub3\worktrees\MyFlowHub-SubProto-fix-flow-set-child-notify` → 当前文档与 lessons → 通过
