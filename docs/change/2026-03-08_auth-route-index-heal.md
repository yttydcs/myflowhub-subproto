# 2026-03-08 Auth 路由索引自愈（auth/v0.1.2）

## 背景 / 目标

近期在多 hop 场景出现回归：Root Hub 因 `sourceMismatch` 丢弃后代节点帧，导致上层子协议（典型是 VarStore `list/get`）返回 `not found (code=4)`。

典型日志（Root 侧）：

```
drop frame due to source mismatch subproto=3 hdr_source=11 meta_node=9
```

目标：
- 不放宽 Core `sourceMismatch` 门禁；
- 修复 Auth 链路导致的“路由索引缺失”，确保可建立 `nodeIndex[descendant] -> childConn`；
- 发布 `github.com/yttydcs/myflowhub-subproto/auth v0.1.2` 供 Server 侧升级依赖。

## 根因分析

1) `register`：当请求未携带 `pubkey` 时，旧实现会把 **本节点公钥** 填入 `req.PubKey`。  
在 Root 节点上，这会把 Root 公钥写入子节点的 `trusted/binding` 记录，形成“公钥毒化”。

2) `up_login`：Root 节点验证 `sender_sig` 时优先使用 `trustedNode[sender_id]`。  
当 `trustedNode[sender_id]` 被毒化后，验签失败 → `AddNodeIndex(descendant)` 无法执行 → 后续来自后代的帧因无法映射到该 child 连接而被 `sourceMismatch` 丢弃。

## 变更内容

### 1) register：缺省 pubkey 不再自动填本机公钥
- 不再在 `register` 中对缺省 `pubkey` 做“填本机公钥”的兜底，避免把 Root 公钥写入子节点记录。

### 2) up_login：trusted sender 验签失败时允许 `sender_pub` 自愈（受限）
- sender 验签流程调整：
  - 先用 `trusted sender pub` 验签；
  - 若失败且携带 `sender_pub`，则在满足约束 `sender_id == hdr.SourceID == conn.meta(nodeID)` 下允许用 `sender_pub` 二次验签；
  - 二次验签成功则更新：
    - `trustedNode[sender_id]`
    - whitelist(binding) 中对应 node 的 `PubKey`
    - `conn.meta("node_pubkey")`
  - 并输出 WARN 审计日志（仅在发生“自愈更新”且原 trusted 非空时）。

### 3) disable_persist：对齐“不读写 trusted_nodes.json”
- `auth.disable_persist=true` 时，`persistState()` 直接返回，不写入 `config/trusted_nodes.json`，避免测试与运行期产生落盘副作用。

### 4) 测试
- 新增回归测试覆盖：
  - `register` 缺省 `pubkey` 不写 trusted；
  - `up_login` 在 trusted 被毒化时可通过 `sender_pub` 自愈并写入路由索引；
  - `sender_id/hdr.SourceID/conn.meta(nodeID)` 不一致时拒绝自愈与建索引。

## 任务映射（plan.md）
- AUTH-1：完成
- AUTH-2：完成
- AUTH-3：完成
- AUTH-4：完成
- AUTH-5：完成（已发布 tag）

## 验证方式 / 结果

在 `auth/` module 下执行：

```powershell
cd auth
$env:GOWORK='off'
go test ./... -count=1 -p 1
```

结果：通过。

发布验证：

```powershell
GOWORK=off go list -m github.com/yttydcs/myflowhub-subproto/auth@v0.1.2
```

结果：可解析到 `v0.1.2`。

## 潜在影响
- 正向：修复“公钥毒化 → up_login 验签失败 → 路由索引缺失 → sourceMismatch 丢弃后代帧”的回归。
- 行为变化：`register` 缺省 `pubkey` 不再被自动填充；若依赖该旧行为，需要由调用方显式提供 pubkey 或依赖后续 `up_login sender_pub` 建立信任。
- 安全：`sender_pub` 的学习/自愈严格受 `sender_id == hdr.SourceID == conn.meta(nodeID)` 约束，避免伪造污染 trusted。

## 回滚方案
- Server 侧回退依赖到 `github.com/yttydcs/myflowhub-subproto/auth v0.1.1`；
- 或发布后续 patch（`auth/v0.1.3`）修正。

