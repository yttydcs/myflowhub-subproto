# Capability Provider Observable Side Effects

## Summary

- 当子协议把本地能力暴露为 capability provider 时，不能只复用“本地数据读写”本身，还必须补齐订阅推送、事件发射、上行缓存同步等可观察副作用。

## Lookup Hints

- 症状：
  - `flow` 执行成功但 `Win varpool` 不更新
  - 已订阅子节点收不到 `var_changed` / `var_deleted`
- 关键词：
  - `invokeCapabilitySet`
  - `invokeCapabilityRevoke`
  - `propagateChange`
  - `handleDeletion`
  - `up_set`
  - `up_revoke`
- 触发条件：
  - `flow` 本地 `call` 节点通过 capability registry 调用子协议方法
- 快速检查：
  - 确认调用是否走 capability fallback，而不是常规协议 handler
  - 检查 provider 是否只改本地状态，没复用传播辅助函数

## Symptoms

- 变量值在本地节点已经更新或删除。
- 依赖订阅推送的下游节点没有收到变化。
- 手动 `get` 或本地缓存查看能看到新值，但观察端 UI 不刷新。

## Impact

- 观察端会误以为 flow 没有执行或变量写入失败。
- 祖先链缓存可能保持旧值，导致后续跨节点读取不一致。

## Trigger Conditions

- 本地 `flow` 运行时通过 capability registry 调用子协议 provider。
- provider 只做本地读写，没有走协议写路径的传播逻辑。

## Root Cause

- capability provider 与协议 handler 的责任被拆开后，provider 漏掉了 observable side effects，只保留了“核心读写”逻辑。
- 在本次问题中，`varstore::set/revoke` provider 漏掉了：
  - `propagateChange()` / `handleDeletion()`
  - `up_set` / `up_revoke`
  - `actor != owner` 时的 `notify_set` / `notify_revoke`

## Investigation Trail

- 用户现象先出现在 `Win varpool` 观察层，但订阅链路本身没有证据显示失效。
- 追到 `flow` 执行路径后，确认本地 `call` 节点优先走 capability registry。
- 再对比 `varstore` 的 `invokeCapability*()` 与 `handleSet/handleRevoke()`，发现前者缺少传播语义。
- 最终通过新增回归测试固定住：
  - 订阅子节点是否收到 `var_changed` / `var_deleted`
  - 父链是否收到 `up_set` / `up_revoke`

## Resolution

- 在 provider 路径中复用既有传播逻辑，而不是单独重写一套新协议：
  - `set` 走 `propagateChange()` / `handleVisibilityDowngrade()` + `up_set`
  - `revoke` 走 `handleDeletion()` + `up_revoke`
  - 本地 actor 与 owner 不同的场景补 `notify_set` / `notify_revoke`

## Prevention / Guardrails

- 新增 capability provider 时，评审清单必须显式检查：
  - 是否需要事件发射
  - 是否需要订阅推送
  - 是否需要上行或下行缓存同步
  - 是否需要 owner / requester 定向通知
- 如果已有协议 handler 已经实现这些语义，优先复用已有辅助函数，不要手写“简化版 provider”。
- 当用户报告“flow 成功但观察端不刷新”时，先检查 provider 侧副作用，而不是先怀疑 UI 订阅。

## Related Docs

- Requirements:
  - `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\requirements\flow_data_dag.md`
- Specs:
  - `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\flow.md`
  - `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\varstore.md`
- Changes:
  - `D:\project\MyFlowHub3\repo\MyFlowHub-SubProto\docs\change\2026-03-25_flow-set-child-notify.md`
