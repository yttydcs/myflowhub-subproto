# VarStore Authoritative Cache Boundary

## Summary

- VarStore 的 `records` 既承载逐跳缓存，又承载本地可读状态，但“缓存存在”不等于“缓存有资格直接回答查询”。
- `get/list/subscribe` 只能把子树内 owner 的缓存视为权威来源；非子树快照只能用于沿途同步和 UI 连续性，不能短路 `assist_*`。

## Lookup Hints

- 症状
  - `var_changed` 显示最新值，但 refresh/get 又回到旧值
  - `list` / `subscribe` 看起来命中本地，但真实 owner 状态不同
- 关键词
  - `ownerInSubtree`
  - `lookupOwned`
  - `listNames`
  - `assist_get`
  - `assist_list`
  - `assist_subscribe`
  - `var_changed`
  - `notify_set`
- 快速检查
  - 检查 `get/list/subscribe` 直接返回前是否先判 `ownerInSubtree`
  - 检查 forwarded `var_changed` / `var_deleted` 是否还会本地更新缓存

## Symptoms

- 观察端先收到实时变化，界面上短暂显示了新值。
- 用户点击 refresh、重新读取变量或重建订阅后，又回到了更早的旧值。
- local hub 日志显示缓存里“有这个变量”，但真实 owner 的数据已经更新。

## Impact

- refresh/read 路径与订阅路径出现分叉，用户会误判为“刷新有问题”或“hub 缓存没更新”。
- 祖先链上的 stale snapshot 可能阻断真实 owner 查询，导致 UI、脚本和 capability 调用出现不一致。

## Trigger Conditions

- local hub 保存了非子树 owner 的历史快照。
- 查询处理代码只检查 `lookupOwned` / `listNames`，没有先检查 `ownerInSubtree`。
- `var_changed` / `notify_set` 仍在正常刷新沿途缓存，因此问题会表现为“实时对，刷新错”。

## Root Cause

- 实现把 `records` 的两层语义混在了一起：
  - 作为逐跳缓存保存远端快照
  - 作为 direct-hit 查询的本地权威来源
- 当这两层语义没有分开时，非子树旧缓存就会在 `get/list/subscribe` 里被误当成 authoritative cache 使用。

## Investigation Trail

- 先根据用户现象确认订阅链路并未完全失效，因为 `var_changed` 仍能把新值带到 Win。
- 再对照稳定 spec，定位到 `get/list/subscribe` 的 direct-hit 前提应是“子树含 owner 且本地有缓存”。
- 最终在 `varstore/varstore.go` 里确认 `handleGet`、`handleList`、`handleSubscribe` 只看了本地缓存命中，没有先卡 `ownerInSubtree`。

## Resolution

- 保留 `records` 和逐跳缓存更新逻辑。
- 把 `get/list/subscribe` 的 direct-hit 条件改为：
  - `ownerInSubtree(ctx, owner)` 为真
  - 且 handler 所需的本地数据存在
- 同时保证 forwarded `var_changed` / `var_deleted` 继续做本地缓存更新，避免沿途缓存停滞。

## Prevention / Guardrails

- 评审 VarStore 相关查询逻辑时，明确区分：
  - “这个节点是否保存过该变量快照”
  - “这个节点是否有资格直接回答该变量查询”
- 当用户反馈“实时变化正常，但 refresh 又旧了”时，优先排查 authority gate，而不是先改 UI 或缓存 TTL。
- 若后续增加更多 remote snapshot 优化，必须保持 direct-answer gate 独立于 snapshot storage。

## Related Docs

- Specs
  - `D:/project/MyFlowHub3/repo/MyFlowHub-Server/docs/specs/varstore.md`
- Changes
  - `D:/project/MyFlowHub3/repo/MyFlowHub-SubProto/docs/change/2026-04-14_varstore-authoritative-cache.md`
  - `D:/project/MyFlowHub3/repo/MyFlowHub-SubProto/docs/change/2026-03-06_varstore-hop-align-subproto.md`

