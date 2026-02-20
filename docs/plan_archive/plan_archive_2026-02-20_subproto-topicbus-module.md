# Plan - PR3：创建 `myflowhub-subproto/topicbus` 独立 Go module 并发布 `topicbus/v0.1.0`

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 远端：`https://github.com/yttydcs/myflowhub-subproto.git`
- 分支：`refactor/subproto-topicbus-module`
- Worktree：`d:\project\MyFlowHub3\worktrees\pr3-topicbus-subproto\MyFlowHub-SubProto`
- Base：`refactor/subproto-management-module`（堆叠 PR：需先合 PR2 再合本 PR3）
- 参考：
  - `d:\project\MyFlowHub3\target.md`
  - `d:\project\MyFlowHub3\repos.md`
  - `d:\project\MyFlowHub3\guide.md`（commit 信息中文）

## 约束（边界）
- wire 不改：SubProto 值 / Action 字符串 / JSON payload struct / HeaderTcp 语义均保持不变。
- 本仓库采用 A2（单仓多 module）：
  - 每个子协议一个目录 + 独立 `go.mod`；
  - 版本 tag 形如：`topicbus/v0.1.0`（对应 module：`github.com/yttydcs/myflowhub-subproto/topicbus`）。
- 依赖方向必须清晰：
  - `topicbus` 仅依赖 `myflowhub-core` + `myflowhub-proto`（以及标准库）；
  - 禁止依赖 `myflowhub-server` / `myflowhub-win`。
- 验收测试必须使用 `GOWORK=off`（避免本地 `go.work` 干扰审计）。

## 当前状态（事实，可审计）
- Server 中 `subproto/topicbus` 已完成去 internal 化，且无 Server 私有依赖（适合直接迁入 module）。
- 本仓库已包含 `management` module（PR2），本 PR3 增量新增 `topicbus` module。

---

## 目标
1) 在本仓库新增 module：`github.com/yttydcs/myflowhub-subproto/topicbus`。
2) 迁入 Server 的 topicbus 子协议实现（保持行为不变）。
3) 发布 tag：`topicbus/v0.1.0`，供 Server 以 semver 依赖拉取（用于 `GOWORK=off` 验收）。

## 非目标
- 不引入其它子协议 module；
- 不做 topicbus 行为变更/性能优化；
- 不新增“兼容开关”（按当前决策：不考虑兼容旧客户端的开关）。

---

## 3.1) 计划拆分（Checklist）

### SUBTB0 - 归档旧 plan
- 目标：归档上一轮 PR2 的 `plan.md`，避免覆盖。
- 涉及文件：
  - `docs/plan_archive/plan_archive_2026-02-19_subproto-management-module.md`
- 验收条件：旧 plan 已归档且可阅读。
- 回滚点：撤销本次 `git mv`。

### SUBTB1 - 创建 `topicbus` module 并迁入实现
- 目标：落地 `topicbus` 独立 module（仅 core+proto 依赖）。
- 涉及文件（预期）：
  - `topicbus/go.mod`
  - `topicbus/*.go`（从 `MyFlowHub-Server/subproto/topicbus/*` 迁入）
- 验收条件：
  - `cd topicbus; GOWORK=off go test ./... -count=1 -p 1` 通过
- 测试点：
  - 至少覆盖编译通过与基础行为（本 PR 不新增行为测试，保持与 Server 现状一致）。
- 回滚点：revert 提交。

### SUBTB2 - Code Review（阶段 3.3）
- 按 3.3 清单输出结论（通过/不通过）；不通过则回到 SUBTB1 修正。

### SUBTB3 - 归档变更（阶段 4）
- 新增文档：
  - `docs/change/2026-02-19_subproto-topicbus-v0.1.0.md`
- 需包含：
  - 变更背景/目标、目录结构、对外模块路径与版本 tag、验证方式/结果、回滚方案。

### SUBTB4 - 发布 tag 并 push（供 Server `GOWORK=off` 拉取）
- 目标：让 Server 侧可通过 `go get` 拉到 module。
- 操作：
  - `git tag -a topicbus/v0.1.0 -m "发布 topicbus v0.1.0"`
  - `git push -u origin refactor/subproto-topicbus-module`
  - `git push origin topicbus/v0.1.0`
- 验收条件：tag 已 push；Server 侧 `go mod tidy` 可拉取该版本。
- 回滚点：revert + 删除 tag（如需）。

---

## 风险 / 注意事项
- multi-module 仓库的 tag 前缀必须正确，否则 Server 无法拉取到版本。
- commit 信息使用中文（允许 `refactor:`/`docs:` 等英文前缀）。

