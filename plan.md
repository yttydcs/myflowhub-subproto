# Plan - SubProto：升级依赖到 Core v0.3.0（对齐 Pipe 抽象重大变更）

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 分支：`chore/bump-core-v0.3.0`
- Worktree：`d:\project\MyFlowHub3\worktrees\chore-bump-core-v0.3.0\MyFlowHub-SubProto`
- Base：`main`
- 关联仓库：
  - `MyFlowHub-Core`：已发布 `v0.3.0`（重大变更：`IConnection.RawConn()` → `IConnection.Pipe()`）

## 背景 / 问题陈述（事实，可审计）
- 本仓 `main` 的部分单测已适配 `core.IConnection.Pipe()`（不再实现 `RawConn()`），但各 module `go.mod` 仍固定依赖 `myflowhub-core v0.2.1`。
- 在 `GOWORK=off`（CI/用户默认）下运行 `go test` 会拉取 `v0.2.1`，从而导致测试编译失败。

## 目标
1) 将受影响的子模块（至少 `auth` / `management` / `varstore`）的 `myflowhub-core` 升级到 `v0.3.0`。
2) 执行 `go mod tidy` 并确保 `GOWORK=off` 下单测通过。

## 非目标
- 不改子协议 wire/语义/路由规则（仅做依赖升级与 go.mod/go.sum 更新）。
- 不强制发布新 tag（如需发布由后续 workflow 决策）。

## 验收标准
- `GOWORK=off` 下至少通过：
  - `cd auth; go test ./... -count=1 -p 1`
  - `cd management; go test ./... -count=1 -p 1`
  - `cd varstore; go test ./... -count=1 -p 1`
- 合并到 `main` 并 push。

## 3.1) 计划拆分（Checklist）

### SUBDEP0 - 归档旧 plan（已执行）
- 已执行：`git mv plan.md docs/plan_archive/plan_archive_2026-03-12_bump-core-v0.3.0-prev.md`

### SUBDEP1 - 升级依赖（auth/management/varstore）
- 目标：各模块 `go.mod` 中 `github.com/yttydcs/myflowhub-core` 从 `v0.2.1` 升级到 `v0.3.0`，并 tidy。
- 涉及文件：
  - `auth/go.mod`、`auth/go.sum`
  - `management/go.mod`、`management/go.sum`
  - `varstore/go.mod`、`varstore/go.sum`
- 回滚点：revert 本任务提交。

### SUBDEP2 - 回归测试（GOWORK=off）
- 测试点：
  - `cd auth; GOWORK=off go test ./... -count=1 -p 1`
  - `cd management; GOWORK=off go test ./... -count=1 -p 1`
  - `cd varstore; GOWORK=off go test ./... -count=1 -p 1`

### SUBDEP3 - Code Review + 归档变更
- 输出：`docs/change/2026-03-12_bump-core-v0.3.0.md`

### SUBDEP4 - 合并 / push（需 workflow 结束后执行）
- 在 `repo/MyFlowHub-SubProto` 合并到 `main` 并 push。

