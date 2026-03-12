# Plan - SubProto：适配 Core Pipe/连接抽象变更（保持子协议行为不变）

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 分支：`refactor/transport-pipe`
- Worktree：`d:\project\MyFlowHub3\worktrees\refactor-transport-pipe\MyFlowHub-SubProto`
- Base：`origin/main`
- 关联仓库（同一 workflow）：
  - `MyFlowHub-Core`：`core.IConnection`/Reader/SendDispatcher 变更
  - `MyFlowHub-Server`：联调与依赖升级（后续）
- 参考：
  - `d:\project\MyFlowHub3\target.md`
  - `d:\project\MyFlowHub3\repos.md`
  - `d:\project\MyFlowHub3\guide.md`（commit 信息中文）

## 背景 / 问题陈述（事实，可审计）
- SubProto 各 module 的业务实现通常只把 `core.IConnection` 当作能力接口使用（不依赖底层 `net.Conn`）。
- 但本仓大量测试 stub/mock 会“实现 `core.IConnection` 接口”，因此当 Core 对 `IConnection` 做破坏性调整时，本仓需要同步适配以保持可测试、可审计。

## 目标
1) 适配 Core `core.IConnection` 变更（主要更新测试 stub/mock），确保本仓相关 module 的单测通过。
2) 不改变任何子协议 wire/语义；仅做适配性改动与必要的测试更新。

## 非目标
- 不在本仓实现 RFCOMM listener/dialer。
- 不重构子协议业务逻辑（除非为适配 Core 变更所必需且影响面可控）。

## 验收标准
- 在 workflow-local `go.work` 联调下，通过以下测试（至少覆盖 Server 当前依赖的模块）：
  - `cd auth; go test ./... -count=1 -p 1`
  - `cd management; go test ./... -count=1 -p 1`
  - `cd varstore; go test ./... -count=1 -p 1`
  - 其他模块按受影响程度补充（file/flow/exec/topicbus/forward/broker）。

## 3.1) 计划拆分（Checklist）

### SUB0 - 归档旧 plan（已执行）
- 已执行：`git mv plan.md docs/plan_archive/plan_archive_2026-03-11_transport-pipe-prev.md`
- 回滚点：撤销该 `git mv`。

### SUB1 - 适配测试 stub/mock：实现新的 `core.IConnection` 形态
**目标**
- 更新本仓测试代码中所有 mock/stub，使其满足新的 `core.IConnection`（例如新增 `Pipe()` 或替代 `RawConn()` 的变更）。

**涉及模块 / 文件（预期）**
- `*/**/*_test.go` 中的 mockConnection / stubConn

**验收条件**
- 受影响 module 能通过编译并运行单测。

**回滚点**
- revert 该提交。

### SUB2 - 回归：关键模块单测通过
**目标**
- 确保 Server 依赖的子协议模块在新 Core 下可用。

**测试点（建议顺序）**
1) `cd auth; go test ./... -count=1 -p 1`
2) `cd management; go test ./... -count=1 -p 1`
3) `cd varstore; go test ./... -count=1 -p 1`
4) `cd file/flow/exec/topicbus/forward/broker` 视受影响情况补充

**回滚点**
- 若出现非适配性失败（业务回归），必须停止并回到需求/架构阶段重新确认。

### SUB3 - 发布与依赖升级（依赖 Core 新 tag）
> 说明：若 Core 本次变更需要发布新版本（例如 `v0.3.0`），本仓各 module 的 `go.mod` 需要升级 `myflowhub-core` 版本后才能在 `GOWORK=off` 下通过 CI 验收。

**目标**
- 在 Core tag 可用后：
  - 更新受影响 module 的 `go.mod/go.sum`；
  - 如有需要发布对应 module 新 tag（按仓库既有 tag 规则：`auth/vX.Y.Z` 等）。

**回滚点**
- revert 依赖升级提交；或改发新的 patch tag（避免重写历史 tag）。

### SUB4 - Code Review（阶段 3.3）+ 归档变更（阶段 4）
- 输出 `docs/change/2026-03-11_transport-pipe-subproto.md`，映射 SUB1~SUB3，并记录测试结果与回滚方案。

---

## 验证命令（建议）
```powershell
$env:GOTMPDIR='d:\\project\\MyFlowHub3\\.tmp\\gotmp'
New-Item -ItemType Directory -Force -Path $env:GOTMPDIR | Out-Null
go test ./... -count=1 -p 1
```

