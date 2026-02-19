# Plan - PR2：创建 `myflowhub-subproto/management` 独立 Go module 并发布 `management/v0.1.0`

## Workflow 信息
- Repo：`MyFlowHub-SubProto`（新仓）
- 远端：`https://github.com/yttydcs/myflowhub-subproto.git`
- 分支：`refactor/subproto-management-module`（空仓 orphan 分支）
- Worktree：`d:\project\MyFlowHub3\worktrees\pr2-management-subproto\MyFlowHub-SubProto`
- Base：`origin/main`（空仓，无提交）
- 参考：
  - `d:\project\MyFlowHub3\target.md`
  - `d:\project\MyFlowHub3\repos.md`
  - `d:\project\MyFlowHub3\guide.md`（commit 信息中文）

## 约束（边界）
- wire 不改：SubProto 值 / Action 字符串 / JSON payload struct / HeaderTcp 语义均保持不变。
- 本仓库采用 A2（单仓多 module）：
  - 每个子协议一个目录 + 独立 `go.mod`；
  - 版本 tag 形如：`management/v0.1.0`（对应 module：`github.com/yttydcs/myflowhub-subproto/management`）。
- 依赖方向必须清晰：
  - `management` 仅依赖 `myflowhub-core` + `myflowhub-proto`（以及标准库）；
  - 禁止依赖 `myflowhub-server` / `myflowhub-win`。
- 验收测试必须使用 `GOWORK=off`（避免本地 `go.work` 干扰审计）。

## 当前状态（事实，可审计）
- Server 已存在 `subproto/management` 实现，且当前已依赖 `myflowhub-core/subproto/kit` 与 `myflowhub-proto/protocol/management`，天然适合抽离为独立 module。
- 本仓库当前为空仓（无代码、无提交）。

---

## 目标
1) 在本仓库新增 module：`github.com/yttydcs/myflowhub-subproto/management`。
2) 迁入 Server 的 management 实现（保持行为不变）。
3) 发布 tag：`management/v0.1.0`，供 Server 以 semver 依赖拉取（用于 `GOWORK=off` 验收）。

## 非目标
- 不引入其它子协议 module；
- 不调整 management 业务逻辑；
- 不新增“兼容开关”（按当前决策：不考虑兼容旧客户端的开关）。

---

## 3.1) 计划拆分（Checklist）

### SUBMGMT0 - 初始化仓库骨架（README + 目录约定）
- 目标：为多 module 仓库建立最小可交接结构。
- 涉及文件（预期）：
  - `README.md`（说明：多 module；如何测试；如何打 tag）
  - `docs/change/`（阶段 4 使用）
- 验收条件：新仓可被 clone，接手者能理解目录与发布方式。
- 回滚点：revert 提交。

### SUBMGMT1 - 创建 `management` module 并迁入实现
- 目标：落地 `management` 独立 module（仅 core+proto 依赖）。
- 涉及文件（预期）：
  - `management/go.mod`
  - `management/*.go`（从 `MyFlowHub-Server/subproto/management/*` 迁入）
- 验收条件：
  - `cd management; GOWORK=off go test ./... -count=1 -p 1` 通过
- 回滚点：revert 提交。

### SUBMGMT2 - Code Review（阶段 3.3）
- 按 3.3 清单输出结论（通过/不通过）；不通过则回到 SUBMGMT1 修正。

### SUBMGMT3 - 归档变更（阶段 4）
- 新增文档：
  - `docs/change/2026-02-19_subproto-management-v0.1.0.md`
- 需包含：
  - 变更背景/目标、目录结构、对外模块路径与版本 tag、验证方式/结果、回滚方案。

### SUBMGMT4 - 发布 tag 并 push（供 Server `GOWORK=off` 拉取）
- 目标：让 Server 侧可通过 `go get` 拉到 module。
- 操作：
  - `git tag -a management/v0.1.0 -m \"发布 management v0.1.0\"`
  - `git push -u origin refactor/subproto-management-module`
  - `git push origin management/v0.1.0`
- 验收条件：tag 已 push；Server 侧 `go mod tidy` 可拉取该版本。
- 回滚点：revert + 删除 tag（如需）。

---

## 风险 / 注意事项
- 空仓首提交即承载模块发布：需确保 module path 与 tag 前缀正确，否则 Server 侧无法通过 `GOWORK=off` 验收。
- commit 信息使用中文（允许 `refactor:`/`docs:` 等英文前缀）。

