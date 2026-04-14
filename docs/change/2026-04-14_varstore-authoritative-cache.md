# 2026-04-14_varstore-authoritative-cache

## 变更背景 / 目标

- 近期在 `root -> local hub -> win` 的多跳拓扑里，Win 端会先通过 `var_changed` 看见新值，但点击 refresh 或重新执行 `get/list/subscribe` 后，又可能被 local hub 的旧缓存覆盖。
- 根因不在缓存“有没有保存”，而在 local hub 把非子树 owner 的远端快照当成了可直接回答查询的权威数据。
- 本次目标是在不改 wire schema、不推翻逐跳缓存模型的前提下，把 direct-hit 条件收紧到“仅子树权威缓存可直接应答”。

## 具体变更内容

- 修改 `varstore/varstore.go`
  - `handleGet`：只有 `ownerInSubtree(owner)` 且本地存在记录时才直接回 `get_resp`，否则继续 `assist_get`。
  - `handleList`：只有 `ownerInSubtree(owner)` 才允许使用本地 `listNames` 直接返回；非子树 owner 改为上送 `assist_list`。
  - `handleSubscribe`：只有 `ownerInSubtree(owner)` 且本地存在记录时才本地建订阅；否则走 `assist_subscribe`。
  - `OnReceive`：把 `var_changed` / `var_deleted` 纳入与 `notify_*` 一致的“forward + local handle”，确保沿途缓存继续被更新。
- 修改 `varstore/target_forward_test.go`
  - 新增 non-subtree stale cache 的 `get/list/subscribe` 回归测试。
  - 新增 `var_changed` forwarded + local cache update 回归测试。
  - 新增 subtree direct-hit 基线测试，确认权威缓存命中行为未回退。

## Requirements impact

- none

## Specs impact

- none

## Lessons impact

- updated

## Related requirements

- none

## Related specs

- `D:/project/MyFlowHub3/repo/MyFlowHub-Server/docs/specs/varstore.md`

## Related lessons

- `D:/project/MyFlowHub3/repo/MyFlowHub-SubProto/docs/lessons/capability-provider-observable-side-effects.md`
- `D:/project/MyFlowHub3/repo/MyFlowHub-SubProto/docs/lessons/varstore-authoritative-cache-boundary.md`

## 对应 plan.md 任务映射

- `VARAUTH-1`：收紧 `get/list/subscribe` 的 direct-hit 权威判定。
- `VARAUTH-2`：把 `var_changed` / `var_deleted` 补进 forward + local handle。
- `VARAUTH-3`：补 stale cache 与 forwarded cache 回归测试。
- `VARAUTH-4`：执行模块验证并完成 review。

## 经验 / 教训摘要

- VarStore 的 `records` 不能简单理解为“有就能答”，还需要区分这份记录是不是当前 hub 子树内的权威缓存。
- `var_changed` / `notify_set` 负责沿途刷新缓存，但缓存更新本身不等于“这个节点有资格回答后续查询”。
- 当用户反馈“实时变化对了，但 refresh 又回旧值”时，要优先检查 direct-hit gate，而不是先怀疑订阅链路。

## 可复用排查线索

- 症状
  - `var_changed` 后 UI 显示新值，但 refresh/get 又回旧值
  - `list` / `subscribe` 行为和实时变化不同步
- 触发条件
  - local hub 保存了非子树 owner 的远端快照
  - 查询路径优先命中 `lookupOwned` / `listNames`
- 关键词
  - `ownerInSubtree`
  - `lookupOwned`
  - `listNames`
  - `assist_get`
  - `assist_list`
  - `assist_subscribe`
  - `var_changed`
- 快速检查
  - 看 `get/list/subscribe` 直接回包前是否先判定 `ownerInSubtree`
  - 看 forwarded `var_changed` 是否仍执行本地缓存更新

## 关键设计决策与权衡

- 保留非子树快照缓存，但取消其 direct-answer 资格。
  - 这样既保留了逐跳缓存、订阅链路和 UI 连续性，又避免 stale snapshot 阻断真实 owner 查询。
- 不引入 TTL / 版本戳。
  - 当前问题是 authority gate 错误，不是 freshness 机制缺失；加 TTL 只会降低复现概率，不会修复语义。
- 不删除 `records`。
  - 稳定 spec 已把沿途缓存定义为协议行为的一部分，这次只修“如何使用缓存”，不改“是否保存缓存”。

## 测试与验证方式 / 结果

- 格式化
  - `gofmt -w varstore/varstore.go`
  - `gofmt -w varstore/target_forward_test.go`
- 模块测试
  - 由于工作区祖先目录已有 `D:/project/MyFlowHub3/go.work`，且 `varstore` 依赖 repo 内本地 `broker/exec/Core/Proto`，直接 `go test` 或单独 `GOWORK=off` 都不足以完成本地联测。
  - 验证方式：临时创建 worktree 内 `go.work`，绑定 `broker`、`exec`、`varstore`、`MyFlowHub-Core`、`MyFlowHub-Proto`，执行：
    - `go test ./... -count=1` in `varstore`
  - 结果：通过

## 潜在影响

- non-subtree stale cache 不再直接答复查询，某些原本“看起来能立即返回”的路径现在会改为继续上送父链，符合稳定 spec，但会改变旧错误行为。
- `list` 的根节点空结果语义保持原实现，不在本次范围内额外调整。

## 回滚方案

- 回退 `varstore/varstore.go` 中 `OnReceive`、`handleGet`、`handleList`、`handleSubscribe` 的变更。
- 回退 `varstore/target_forward_test.go` 中新增回归测试。
- 重新运行同样的 `varstore` 模块测试确认恢复旧行为。

## 子Agent执行轨迹

- 未使用子Agent。

