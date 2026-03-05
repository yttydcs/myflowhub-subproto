# 2026-03-05 - VarStore：跨层目标转发与 owner 路由自愈

## 变更背景 / 目标
- 背景：在拓扑 `1 -> (2,9) -> 10` 中，Win(2) 对 owner=10 的 `varstore:set` 可能返回 `not found (code=4)`；同层（1直连10）可用。
- 目标：
  1. 补齐 VarStore 在多跳拓扑下的 `target!=local` 转发能力，避免中间节点错误本地消费命令。
  2. 增加 owner 路由“自愈”能力：收到下游 `up_set` 时，将 `owner -> child conn` 补写到路由索引，降低跨层 `code=4` 概率。

## 具体变更内容

### 新增
- `varstore/target_forward_test.go`
  - `TestForwardCmdByHeaderTarget`：验证 `MajorCmd + target!=local` 场景会转发到下一跳，且保持原 `source/target`。
  - `TestForwardCmdByHeaderTargetNotFoundReturnsResp`：验证无路由时返回 `set_resp code=4`。
  - `TestUpSetIndexesOwnerRoute`：验证收到 `up_set(owner=10)` 后，建立 `owner(10) -> child conn` 路由索引。

### 修改
- `varstore/varstore.go`
  - `OnReceive` 增加 `target` 转发前置判定：
    - 条件：`MajorCmd`、`TargetID!=0`、`TargetID!=local`、且 action 非 `*_resp`。
    - 行为：优先按 `ConnManager.GetByNode(target)` 转发；未命中时尝试父连接；失败则返回动作对应错误响应。
  - 新增 `forwardCmdByHeaderTarget` / `sendForwardError` / `buildForwardError` / `shouldForwardByHeaderTarget`。
  - 新增 `indexOwnerRoute`：在 `handleUpSet` 里用下游上报 owner 反向补路由（仅在映射缺失时写入）。
  - `handleUpSet` / `handleUpRevoke` 签名更新，接收 `conn` 以支持路由自愈与后续扩展。
- `varstore/actions.go`
  - `up_set` / `up_revoke` action 注册传递 `conn` 到 handler。

## plan 任务映射
- VSROUTE-1：`varstore/varstore.go` 转发入口与错误回包。
- VSROUTE-2：`varstore/target_forward_test.go` 三个回归测试。
- VSROUTE-3：本文档归档 + review 结论。

## 关键设计决策与权衡
- 方案选择：在 VarStore 子协议层补齐 `target` 转发，而非恢复 Core 对 `MajorCmd` 的自动转发。
  - 原因：与既有架构一致（management/exec/flow 已在子协议层处理跨层转发），避免改变 Core 全局行为。
- 安全与稳定性：
  - 对 `*_resp` 不做转发，避免 pending 响应路径被短路。
  - 转发使用 `CloneToTCPForForward`，遵守 hop-limit 递减，避免回环。
  - owner 路由自愈仅在缺失映射时写入，不覆盖已有映射，降低错误路由风险。

## 测试与验证方式 / 结果
- 在 `varstore` 模块执行：
  - `GOWORK=off go test ./... -count=1 -p 1`
- 结果：通过。

## 潜在影响
- 正向：多跳拓扑下，VarStore 命令/通知可按 target 跨层到达；owner 路由在 `up_set` 后可自动补齐。
- 风险：若上游错误上报 owner，可能引入错误路由提示；通过“仅缺失时写入”与 hop-limit 约束降低影响范围。

## 回滚方案
1. 回滚 `varstore/varstore.go` 的 target-forward 与 indexOwnerRoute 变更。
2. 删除 `varstore/target_forward_test.go`。
3. 发布回滚 patch tag，并同步下游依赖回退。
