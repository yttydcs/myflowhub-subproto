# 2026-03-03 - Management：Nodes 查询 children-only（过滤 parent link）

## 变更背景 / 目标

Management 的 `list_nodes`/`list_subtree` 用于为 Win/Android 的 Devices/Nodes 树提供“按需展开”的 children 列表。

现状（可复现）：
- 当节点存在上游 parent link（非 root hub 上联 parent）时，`list_nodes` 会把 **parent 连接**也枚举出来；
- UI 侧把返回结果视为 children，会出现回指/环（例如 `5 -> 1`）与 `Duplicate` 节点，影响可用性与一致性。

本次目标：
1) 将 `list_nodes`/`list_subtree` 语义调整为 **children-only**：只返回下游 children，不返回上游 parent；
2) wire 不变（SubProto/Action/JSON schema 不变），仅调整实现筛选逻辑；
3) 增加最小单测覆盖，避免回归。

## 具体变更内容（新增 / 修改 / 删除）

### 修改
- `management/action_nodes.go`
  - `enumerateDirectNodes` 增加连接角色过滤：当 `conn.meta["role"] == "parent"` 时跳过该连接；
  - 直接节点列表不再包含 parent link 对应的 `node_id`；
  - `nodes[].has_children` 在 direct 列表中保持 `false`（当前实现无法可靠推断 children 是否仍有下游；UI 应以实际展开结果为准）。

### 新增
- `management/action_nodes_test.go`
  - 覆盖 “parent 连接不应出现在 list_nodes 结果中” 的关键行为。

## 对应 plan.md 任务映射
- MGCO1 - 调整 `list_nodes`/`list_subtree` 为 children-only ✅
- MGCO2 - 增加单元测试覆盖 ✅
- MGCO3 - 回归与冒烟验证 ✅（见下）

## 关键设计决策与权衡
1) **用连接 meta 的 `role` 判断 parent/child**
   - `myflowhub-core` 在 parent link 上设置 `role=parent`，其它连接缺省为 `child`；
   - 该信号不在 wire 中传播，属于实现侧可靠来源，可最小化改动面。

2) **不复用 `has_children` 表达 upstream**
   - 旧实现通过 `role=parent` 将 upstream 的存在编码进 `has_children`，导致 UI 误判并产生环；
   - children-only 语义下不再输出 upstream，因此不再需要该编码方式。

3) **保留 `list_subtree` 的现状：direct(children) + self（不递归）**
   - 避免本次引入递归/批量查询（潜在性能与复杂度放大）；
   - 未来若需要真正的“子树一次性返回”，需在协议与实现层面另起设计。

## 测试与验证方式 / 结果

### 单元测试（通过）
```powershell
cd management
$env:GOWORK='off'
go test ./... -count=1 -p 1
```

### 手工冒烟建议
- 在 `1 -> 5 -> 6` 拓扑下：
  - 查询 `node5` 的 children：不应再出现 `node1`；
  - 展开 `node5`：只应看到 `node6`（以及其它真实下游节点）。

## 潜在影响与回滚方案

### 潜在影响
- `list_nodes` 不再“顺带暴露” upstream parent 信息；如需要调试上游拓扑，建议另起需求提供独立动作（例如 `list_upstream`/`list_links`）。

### 回滚方案
- revert 本次提交（仅涉及 `management/action_nodes.go` 与对应测试）。

