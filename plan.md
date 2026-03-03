# Plan - SubProto：Management Nodes 仅返回 Children（children-only）

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 分支：`fix/management-children-only`
- Worktree：`d:\project\MyFlowHub3\worktrees\fix-subproto-management-children-only`
- Base：`origin/main`
- 参考：`d:\project\MyFlowHub3\guide.md`（commit 信息中文）

## 背景 / 问题陈述（事实，可审计）
- Management `list_nodes` 当前按“直连连接”枚举节点：遍历 `ConnManager.Range()`，读连接 meta 的 `nodeID` 直接回包。
- 对于非 root hub（存在上游 parent link），该枚举会把 **parent 连接**也包含进来，导致设备树展开出现回指/环，例如：`node5 -> node1`。
- Win/Android 的 Devices/Nodes UI 目前将 `list_nodes` 返回结果视为 children，因此会出现 `Duplicate` 与不可展开节点，影响可用性与一致性。

## 目标
1) 将 Management `list_nodes` / `list_subtree` 调整为 **children-only**：只返回下游 children，不返回上游 parent。
2) 保持 wire 不变（SubProto/Action/JSON schema 不变），仅调整语义与实现筛选逻辑。
3) 增加最小单测覆盖，防止回归。

## 非目标
- 不新增 “查看上游(parent)” 的管理动作；如未来需要，另起 workflow 设计 `list_upstream`/`list_links` 等动作。
- 不改动节点 ID 分配、Auth 登录/注册语义与路由策略。

## 约束（边界）
- wire 不改：SubProto=1、Action 字符串、payload JSON 字段与 tag 不改。
- 仅调整 Management 子协议实现（`myflowhub-subproto/management`）。
- 测试必须以 `GOWORK=off` 运行（避免本地 `go.work` 影响审计与可复现性）。

## 验收标准
- 在典型拓扑 `1 -> 5 -> 6` 下：
  - 对 `node1` 执行 `list_nodes`：仍可看到 `node5`（不受影响）。
  - 对 `node5` 执行 `list_nodes`：只返回 `node6`，不再返回 `node1`。
- 单测通过：
  - `cd management; $env:GOWORK='off'; go test ./... -count=1 -p 1`
- 手工冒烟（建议）：
  - Win/Android 端展开 `node5` 不再出现 `node1(Duplicate)`。

---

## 3.1) 计划拆分（Checklist）

### MGCO0 - 归档旧 plan.md
- 目标：避免旧 workflow plan 覆盖本次任务。
- 已执行：`plan.md` → `docs/plan_archive/plan_archive_2026-03-03_subproto-management-children-only-prev.md`
- 验收条件：归档文件存在且可阅读。
- 回滚点：撤销该移动提交。

### MGCO1 - 调整 `list_nodes` / `list_subtree` 为 children-only
- 目标：Management nodes 枚举只返回 children，不返回 parent。
- 涉及模块/文件：
  - `management/action_nodes.go`
- 方案（实现级）：
  - 在 `enumerateDirectNodes` 中读取连接 meta `role`（`core.MetaRoleKey`）：
    - `role == core.RoleParent` → 跳过（上游 parent link）
    - 其它（含缺省/child）→ 允许进入枚举（仍需 `nodeID != 0`）
  - `list_subtree` 继续复用同一枚举函数（因此同样 children-only），并保持“附带 self 节点”的现有行为（不递归）。
- 验收条件：
  - 对存在 parent link 的 hub 节点执行 `list_nodes`，返回不包含 parent 的 `node_id`。
- 回滚点：revert 对 `action_nodes.go` 的提交。

### MGCO2 - 增加单元测试覆盖（防回归）
- 目标：覆盖 “parent 连接不应出现在 nodes 列表” 的关键行为。
- 涉及模块/文件：
  - `management/action_nodes_test.go`（新增）
- 测试用例（最小）：
  - ConnManager 中包含：
    - parent conn：`role=parent`、`nodeID=1`
    - child conn：`role=child`、`nodeID=6`
  - 断言：`enumerateDirectNodes` 只返回 `nodeID=6`。
- 验收条件：`go test` 通过且覆盖用例稳定。
- 回滚点：revert 测试提交。

### MGCO3 - 回归与冒烟验证
- 目标：确保不影响 root hub 的 nodes 枚举，并消除 UI duplicate。
- 验收条件：
  - `MGCO1/MGCO2` 的自动化验收通过；
  - 手动在 Win/Android UI 验证 `node5` 展开不再出现 `node1`。
- 回滚点：按提交逐个 revert。

### MGCO4 - Code Review（阶段 3.3）
- 目标：对照需求逐项审查：语义、兼容性、性能、测试。
- 产出：Review 结论（通过/不通过）与必要修正。

### MGCO5 - 归档变更（阶段 4）
- 目标：补齐可审计变更说明与回滚路径。
- 涉及文件：
  - `docs/change/2026-03-03_management-nodes-children-only.md`（新增）
- 必含内容：
  - 变更背景/目标、具体变更、任务映射、关键决策与权衡、测试方式/结果、潜在影响与回滚方案。

### MGCO6 -（可选）发布版本 tag
- 说明：若需要让上游以 semver 拉取（非本地 go.work/replace），建议发布：
  - tag：`management/v0.1.1`
- 前置：需要你明确“是否要发布 tag”后再执行（避免误发布）。

