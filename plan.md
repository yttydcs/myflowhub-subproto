# Plan - SubProto：补齐剩余模块对 Core v0.3.0 的依赖（对齐 Pipe 抽象重大变更）

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 分支：`chore/bump-core-v0.3.0-subproto-more`
- Worktree：`d:\project\MyFlowHub3\worktrees\chore-bump-core-v0.3.0-subproto-more\MyFlowHub-SubProto`
- Base：`main`
- 关联仓库：
  - `MyFlowHub-Core`：已发布 `v0.3.0`（重大变更：`IConnection.RawConn()` → `IConnection.Pipe()`）

## 背景 / 问题陈述（事实，可审计）
- 本仓已开始在部分模块（如 `auth` / `management` / `varstore`）依赖 Core `v0.3.0`。
- 但仍有模块的 `go.mod` 固定 `myflowhub-core v0.2.1`（`exec` / `forward` / `file` / `flow` / `topicbus`）。
- 在 `GOWORK=off`（CI/用户默认）下，这些模块会拉取旧 Core 版本，带来：
  - 编译口径不一致（同一仓库内不同模块锁不同 Core 版本）；
  - 当代码/测试已按 Pipe 抽象演进时，可能出现 `go test` 编译失败。

## 目标
1) 将剩余模块的 `myflowhub-core` 依赖升级到 `v0.3.0`：
   - `exec` / `forward` / `file` / `flow` / `topicbus`
2) 执行 `go mod tidy`，并确保 `GOWORK=off` 下相关模块 `go test` 通过。

## 非目标
- 不改子协议 wire/语义/路由规则（仅为对齐 Core 版本做必要编译适配）。
- 不发布新 tag（如需发布由后续 workflow 决策）。

## 验收标准
- `GOWORK=off` 下通过：
  - `cd exec; go test ./... -count=1 -p 1`
  - `cd forward; go test ./... -count=1 -p 1`
  - `cd file; go test ./... -count=1 -p 1`
  - `cd flow; go test ./... -count=1 -p 1`
  - `cd topicbus; go test ./... -count=1 -p 1`
- 仓库内不再存在 `myflowhub-core v0.2.1` 的 `go.mod` 引用（用 `rg` 可验证）。
- 合并到 `main` 并 push。

## 3.1) 计划拆分（Checklist）

### SUBMORE0 - 归档旧 plan（已执行）
- 已执行：`git mv plan.md docs/plan_archive/plan_archive_2026-03-12_bump-core-v0.3.0-subproto-more-prev.md`

### SUBMORE1 - 升级依赖到 Core v0.3.0（exec/forward/file/flow/topicbus）
- 目标：各模块 `go.mod` 中 `github.com/yttydcs/myflowhub-core` 从 `v0.2.1` 升级到 `v0.3.0`，并 tidy。
- 说明：若升级后出现编译失败（例如接口从 `RawConn()` 迁移到 `Pipe()` 的实现差异），允许在不改变子协议语义的前提下做最小必要适配。
- 涉及文件：
  - `exec/go.mod`、`exec/go.sum`
  - `forward/go.mod`、`forward/go.sum`
  - `file/go.mod`、`file/go.sum`
  - `flow/go.mod`、`flow/go.sum`
  - `topicbus/go.mod`、`topicbus/go.sum`
- 验收条件：`GOWORK=off go test` 至少编译通过（见 SUBMORE2）。
- 回滚点：revert 本任务提交。

### SUBMORE2 - 回归测试（GOWORK=off）
- 测试点：
  - `cd exec; GOWORK=off go test ./... -count=1 -p 1`
  - `cd forward; GOWORK=off go test ./... -count=1 -p 1`
  - `cd file; GOWORK=off go test ./... -count=1 -p 1`
  - `cd flow; GOWORK=off go test ./... -count=1 -p 1`
  - `cd topicbus; GOWORK=off go test ./... -count=1 -p 1`

### SUBMORE3 - Code Review（强制）
- 逐项审查：需求覆盖/架构/性能/可读性/扩展性/稳定性与安全/测试覆盖。

### SUBMORE4 - 归档变更（强制）
- 输出：`docs/change/2026-03-12_bump-core-v0.3.0-subproto-more.md`

### SUBMORE5 - 合并 / push（需 workflow 结束后执行）
- 在 `repo/MyFlowHub-SubProto` 合并到 `main` 并 push。
