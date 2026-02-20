# Plan - PR4：拆剩余子协议为独立 Go module（含 broker）

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 分支：`refactor/subproto-modules-all`
- Worktree：`d:\project\MyFlowHub3\worktrees\pr4-subproto-modules\MyFlowHub-SubProto`
- Base：`origin/main`
- 关联仓库（同一 workflow）：`MyFlowHub-Server`（同名分支/独占 worktree）
- 参考：
  - `d:\project\MyFlowHub3\target.md`
  - `d:\project\MyFlowHub3\repos.md`
  - `d:\project\MyFlowHub3\guide.md`（commit 信息中文）

## 约束（边界）
- wire 不改：SubProto 值 / Action 字符串 / JSON payload struct / HeaderTcp 语义均保持不变。
- A2（单仓多 module）：
  - 每个子协议一个目录 + 独立 `go.mod`；
  - 版本 tag 形如：`<moduleDir>/vX.Y.Z`（例如 `auth/v0.1.0`）。
- 依赖方向必须清晰：
  - 本仓库的子协议 module 只依赖 `myflowhub-core` + `myflowhub-proto`（以及标准库）；
  - 禁止依赖 `myflowhub-server` / `myflowhub-win`。
- 版本策略（已确认）：
  - 本轮新 module 首发统一 `v0.1.0`。
- 验收测试必须使用 `GOWORK=off`（避免本地 `go.work` 干扰审计）。

## 当前状态（事实，可审计）
- 本仓库已有 module：
  - `management`（tag：`management/v0.1.0`）
  - `topicbus`（tag：`topicbus/v0.1.0`）
- `MyFlowHub-Server` 仍包含以下子协议实现目录：
  - `subproto/auth`、`subproto/varstore`、`subproto/file`、`subproto/forward`、`subproto/exec`、`subproto/flow`
- `exec/flow` 当前通过 `myflowhub-server/internal/broker` 做“同进程 reqID -> resp 投递”，导致它们无法直接拆成独立 module（会反向依赖 Server）。

## 目标
1) 新增共享 module：`github.com/yttydcs/myflowhub-subproto/broker`（tag：`broker/v0.1.0`）
   - 用于承载 `Broker[T]` 与 `SharedExecCallBroker()`，解耦 `exec/flow` 对 Server 的依赖。
2) 将剩余子协议一次性拆为独立 module（首发均 `v0.1.0`）：
   - `auth`、`varstore`、`file`、`forward`、`exec`、`flow`
3) 保持 Server 侧行为不变：Server 仅做装配，子协议实现由本仓库 module 提供（Server 侧计划见其 worktree 的 `plan.md`）。
4) 文档策略（已确认）：
   - SubProto 仓 `docs/change/*` 输出“完整设计 + 变更清单 + 版本/tag 列表”；
   - Server 仓 `docs/change/*` 只做“依赖切换 + 删除目录”的短引用，并链接 SubProto 文档；
   - 控制面文档（`d:\project\MyFlowHub3\repos.md`）同步口径。

## 非目标
- 不做协议语义调整/兼容开关；
- 不做 “minimal/full 变体产品化”（仍保持 Deferred）；
- 不做 “协议生成（single source-of-truth 生成 types/constants）”。

---

## 3.1) 计划拆分（Checklist）

### SUBALL0 - 归档旧 plan
- 目标：保留上一轮 topicbus 拆分 plan，避免覆盖。
- 已执行（可审计）：`git mv plan.md docs/plan_archive/plan_archive_2026-02-20_subproto-topicbus-module.md`
- 验收条件：归档文件存在且可阅读。
- 回滚点：撤销该 `git mv`。

### SUBALL1 - 新增 `broker` module（共享投递器）
- 目标：
  - 新增 module：`github.com/yttydcs/myflowhub-subproto/broker`
  - 迁移 `myflowhub-server/internal/broker/*` 逻辑到此 module（wire 无关）。
- 涉及文件（预期）：
  - `broker/go.mod`
  - `broker/broker.go`（`Broker[T]`、`Register/Deliver`）
  - `broker/exec_call.go`（`SharedExecCallBroker()`：类型使用 `myflowhub-proto/protocol/exec.CallResp`）
  - `broker/broker_test.go`
- 验收条件：
  - `cd broker; $env:GOWORK='off'; go test ./... -count=1 -p 1` 通过
- 测试点：
  - 重复注册同 reqID 关闭旧通道（防泄漏）
  - Deliver 后通道关闭（等待者可退出）
- 回滚点：revert 提交。

### SUBALL2 - 新增 `auth` module（从 Server 迁入）
- 目标：创建 `auth` 独立 module，并迁入 Server 的 `subproto/auth` 实现（行为不变）。
- 依赖要求：只依赖 `myflowhub-core` + `myflowhub-proto`（以及标准库）。
- 涉及文件（预期）：
  - `auth/go.mod` / `auth/go.sum`
  - `auth/*.go`（从 `MyFlowHub-Server/subproto/auth/*.go` 迁入）
- 验收条件：
  - `cd auth; GOWORK=off go test ./... -count=1 -p 1` 通过
- 回滚点：revert 提交。

### SUBALL3 - 新增 `varstore` module（从 Server 迁入）
- 目标：创建 `varstore` 独立 module，并迁入 Server 的 `subproto/varstore` 实现（行为不变）。
- 涉及文件（预期）：
  - `varstore/go.mod` / `varstore/go.sum`
  - `varstore/*.go`（从 `MyFlowHub-Server/subproto/varstore/*.go` 迁入）
- 验收条件：`cd varstore; GOWORK=off go test ./... -count=1 -p 1` 通过
- 回滚点：revert 提交。

### SUBALL4 - 新增 `file` module（从 Server 迁入）
- 目标：创建 `file` 独立 module，并迁入 Server 的 `subproto/file` 实现（行为不变）。
- 涉及文件（预期）：
  - `file/go.mod` / `file/go.sum`
  - `file/*.go`（从 `MyFlowHub-Server/subproto/file/*.go` 迁入）
- 验收条件：`cd file; GOWORK=off go test ./... -count=1 -p 1` 通过
- 回滚点：revert 提交。

### SUBALL5 - 新增 `forward` module（默认转发 handler）
- 目标：创建 `forward` 独立 module，并迁入 Server 的 `subproto/forward` 实现（行为不变）。
- 涉及文件（预期）：
  - `forward/go.mod` / `forward/go.sum`
  - `forward/*.go`（从 `MyFlowHub-Server/subproto/forward/*.go` 迁入）
- 验收条件：`cd forward; GOWORK=off go test ./... -count=1 -p 1` 通过
- 回滚点：revert 提交。

### SUBALL6 - 新增 `exec` module（改用 `subproto/broker`）
- 目标：
  - 创建 `exec` 独立 module，并迁入 Server 的 `subproto/exec` 实现（行为不变）。
  - 将 `myflowhub-server/internal/broker` 引用替换为 `github.com/yttydcs/myflowhub-subproto/broker`。
- 涉及文件（预期）：
  - `exec/go.mod` / `exec/go.sum`
  - `exec/*.go`（从 `MyFlowHub-Server/subproto/exec/*.go` 迁入，并改 import）
- 验收条件：`cd exec; GOWORK=off go test ./... -count=1 -p 1` 通过
- 回滚点：revert 提交。

### SUBALL7 - 新增 `flow` module（改用 `subproto/broker` + 直依赖 Proto exec）
- 目标：
  - 创建 `flow` 独立 module，并迁入 Server 的 `subproto/flow` 实现（行为不变）。
  - 将 `myflowhub-server/internal/broker` 引用替换为 `github.com/yttydcs/myflowhub-subproto/broker`。
  - 将 `myflowhub-server/protocol/exec` 引用替换为 `myflowhub-proto/protocol/exec`（避免依赖 Server）。
- 涉及文件（预期）：
  - `flow/go.mod` / `flow/go.sum`
  - `flow/*.go`（从 `MyFlowHub-Server/subproto/flow/*.go` 迁入，并改 import）
- 验收条件：`cd flow; GOWORK=off go test ./... -count=1 -p 1` 通过
- 回滚点：revert 提交。

### SUBALL8 - 统一验收（逐 module）
- 目标：确保每个 module 在 `GOWORK=off` 下独立可测。
- 验收命令（示例，逐个目录执行）：
```powershell
$env:GOTMPDIR='d:\\project\\MyFlowHub3\\.tmp\\gotmp'
New-Item -ItemType Directory -Force -Path $env:GOTMPDIR | Out-Null
$env:GOWORK='off'
go test ./... -count=1 -p 1
```
- 验收条件：`broker/auth/varstore/file/forward/exec/flow` 均通过。

### SUBALL9 - 发布 tags 并 push（供 Server 拉取）
- 目标：让 Server 侧 `GOWORK=off` 可拉取到对应 module 版本。
- 操作（同一 commit 上打多个 tag，均为 annotated）：
  - `broker/v0.1.0`
  - `auth/v0.1.0`
  - `varstore/v0.1.0`
  - `file/v0.1.0`
  - `forward/v0.1.0`
  - `exec/v0.1.0`
  - `flow/v0.1.0`
- 验收条件：
  - tags 已 push；
  - Server 侧 `go mod tidy` 能拉取上述版本（见 Server plan）。
- 回滚点：revert + 删除 tag（如需）。

### SUBALL10 - Code Review（阶段 3.3）
- 按 3.3 清单逐项审查并输出结论（通过/不通过）；不通过则回到对应任务修正。

### SUBALL11 - 归档变更（阶段 4：SubProto 完整文档）
- 新增文档（完整版）：`docs/change/2026-02-20_subproto-split-remaining-modules.md`
- 必须包含：
  - 变更背景/目标
  - 新增 module 清单（path + tag + 依赖）
  - broker 设计与 `exec/flow` 解耦说明
  - 验收命令与结果
  - 对 Server 的要求（需切换依赖并删除旧目录）
  - 回滚方案

### SUBALL12 - 控制面文档同步（非代码仓，控制面文件）
- 目标：更新 `d:\project\MyFlowHub3\repos.md` 的“Server 当前结构（事实）”与 “MyFlowHub-SubProto 定位/现状”，避免误导。
- 验收条件：文档无矛盾（Server 不再宣称包含已迁走的 `subproto/*` 实现）。

