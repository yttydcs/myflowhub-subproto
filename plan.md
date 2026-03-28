# Plan - SubProto Stream Release

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- Branch：`feat/subproto-stream-subproto`
- Base：`main`
- Worktree：`D:\project\MyFlowHub3\worktrees\subproto-stream-subproto`
- 当前 Stage：`3.1`

## 当前状态
- `stream` module 首版实现已在本 worktree 落地，并已归档：
  - `D:\project\MyFlowHub3\worktrees\subproto-stream-subproto\docs\change\2026-03-28_stream-module.md`
- 远端当前不存在 `stream/v0.1.0`
- 本地 `stream/go.mod` 仍依赖 `github.com/yttydcs/myflowhub-proto v0.1.3`，因此不能直接通过 `GOWORK=off` 拉取 `protocol/stream`

## Stage 1 - 需求分析

### 目标
- 将 `stream` module 对齐到已发布 Proto patch，并发布 `github.com/yttydcs/myflowhub-subproto/stream` 的首个 semver tag。
- 让 Server 可以在不依赖 `go.work` 的情况下引用 `stream` module。

### 范围
- 必须：
  - 将 `stream/go.mod` 对齐到 `github.com/yttydcs/myflowhub-proto v0.1.4`
  - 更新 `stream/go.sum`
  - 在 `stream/` 目录以 `GOWORK=off` 完成测试
  - 创建并推送 `stream/v0.1.0`
  - 补齐本轮发布归档
- 可选：
  - 无
- 不做：
  - 不扩展 `stream` handler 业务语义
  - 不新增 plan 外的公共 API
  - 不改写已发布 module tag

### 使用场景
- `MyFlowHub-Server` 通过 `require github.com/yttydcs/myflowhub-subproto/stream v0.1.0` 直接装配 `stream`
- 其他下游仓库以 `GOWORK=off` 模式编译 `stream` 相关代码，不再依赖本地 worktree

### 功能需求
- `stream` module 必须只依赖 `myflowhub-core + myflowhub-proto`
- `stream/go.mod` 必须指向包含 `protocol/stream` 的 Proto 版本
- 发布后远端必须存在 `stream/v0.1.0`

### 非功能需求
- 依赖方向保持清晰，不回退到 `myflowhub-server`
- 验证必须使用 `GOWORK=off`
- tag 不可重写，如需修复只发更高 patch

### 输入输出
- 输入：
  - `D:\project\MyFlowHub3\worktrees\subproto-stream-subproto\stream\go.mod`
  - `D:\project\MyFlowHub3\worktrees\subproto-stream-subproto\stream\go.sum`
  - `D:\project\MyFlowHub3\worktrees\subproto-stream-subproto\stream\*.go`
  - `D:\project\MyFlowHub3\worktrees\server-stream-subproto-design\docs\requirements\stream.md`
  - `D:\project\MyFlowHub3\worktrees\server-stream-subproto-design\docs\specs\stream.md`
- 输出：
  - 远端 tag：`stream/v0.1.0`
  - 可被 Server 消费的 `stream` module 版本
  - 本轮发布归档文档

### 边界异常
- 若 `v0.1.4` Proto 远端还不可见，则 `stream` 的 `GOWORK=off` 验证必然失败
- 若远端已存在 `stream/v0.1.0`，不得覆盖重发
- 若 `go.mod` 与 `go.sum` 不一致，必须先 tidy / 校验后再发布

### 验收标准
- `cd stream; GOWORK=off go test ./... -count=1 -p 1` 通过
- `git ls-remote --tags origin refs/tags/stream/v0.1.0` 能看到远端 tag
- Server 后续可在 `GOWORK=off` 下拉取 `stream v0.1.0`

### 风险
- module tag 一旦 push 不可改写
- 发布时序若早于 Proto 远端可见，会让验证结论失真

## Stage 2 - 架构设计

### 总体方案
- 方案 A：继续仅用本地 `go.work`，不发布 `stream` module
  - 不选：Server 仍无法在真实依赖模式下消费
- 方案 B：先发布 Proto `v0.1.4`，再把 `stream` module 发布为 `stream/v0.1.0`
  - 采用：与单仓多 module 的既有 tag 规则一致，且范围最小
- 方案 C：在 `stream` 发布前继续扩大 handler 能力或测试矩阵
  - 不选：当前阻塞点是 semver 可消费性，不是功能缺口

### 模块职责
- `MyFlowHub-SubProto/stream`
  - 暴露 `NewHandler / NewHandlerWithConfig`
  - 依赖公开 Proto contract
  - 以 `stream/v0.1.0` 形式发布
- `MyFlowHub-Server`
  - 消费 `stream/v0.1.0`
  - 补多节点最小集成测试

### 数据 / 调用流
1. Proto 发布 `v0.1.4`
2. `stream/go.mod` 升级到 `v0.1.4`
3. 在 `stream/` 目录执行 `GOWORK=off go test`
4. 创建并推送 `stream/v0.1.0`
5. Server 升级依赖并回归

### 接口草案
- module path：`github.com/yttydcs/myflowhub-subproto/stream`
- 发布 tag：`stream/v0.1.0`
- 验证命令：`cd stream; GOWORK=off go test ./... -count=1 -p 1`

### 错误与安全
- 若 `go test` 失败，不得带着未验证版本打 tag
- 若 tag 已存在，必须改发更高 patch，而不是强推覆盖

### 性能与测试策略
- 不新增大规模集成测试，保持本轮以 module 级 `GOWORK=off` 验证为主
- 控制 / DATA / ACK 语义继续由现有 `handler_test.go` 覆盖

### 可扩展性设计点
- 首发版本先只固定可消费版本锚点，未来增强继续走 `stream/v0.1.x`

## Stage 3.1 - 计划
- Requirements impact：`none`
- Specs impact：`none`
- Related requirements：
  - `D:\project\MyFlowHub3\worktrees\server-stream-subproto-design\docs\requirements\stream.md`
- Related specs：
  - `D:\project\MyFlowHub3\worktrees\server-stream-subproto-design\docs\specs\stream.md`
- Related lessons：
  - `D:\project\MyFlowHub3\docs\lessons\cross-repo-semver-release.md`

### 执行清单
- [ ] `SUBSTRM-REL-1` 升级 `stream` module 依赖到 Proto `v0.1.4`
- [ ] `SUBSTRM-VAL-1` 在 `stream/` 目录完成 `GOWORK=off` 验证
- [ ] `SUBSTRM-REL-2` 创建并推送 `stream/v0.1.0`
- [ ] `SUBSTRM-DOC-1` 归档本轮 module 发布结果

### 任务明细

#### SUBSTRM-REL-1
- Owner：主 Agent
- Worktree：`D:\project\MyFlowHub3\worktrees\subproto-stream-subproto`
- Files：
  - `stream/go.mod`
  - `stream/go.sum`
- Goal：
  - 让 `stream` module 在真实依赖模式下引用 `myflowhub-proto v0.1.4`
- Acceptance：
  - `stream/go.mod` 不再依赖 `v0.1.3`
  - `go.sum` 与更新后的依赖一致
- Tests：
  - `cd stream; GOWORK=off go test ./... -count=1 -p 1`
- Rollback：
  - 回退 `stream/go.mod` 和 `stream/go.sum`

#### SUBSTRM-VAL-1
- Owner：主 Agent
- Worktree：`D:\project\MyFlowHub3\worktrees\subproto-stream-subproto`
- Files：
  - `stream/*`
- Goal：
  - 在不依赖 `go.work` 的情况下确认 `stream` 首版实现仍通过
- Acceptance：
  - `cd stream; GOWORK=off go test ./... -count=1 -p 1` 通过
- Tests：
  - `cd stream; GOWORK=off go test ./... -count=1 -p 1`
- Rollback：
  - 若验证失败，先修正依赖或实现，再继续发布

#### SUBSTRM-REL-2
- Owner：主 Agent
- Worktree：`D:\project\MyFlowHub3\worktrees\subproto-stream-subproto`
- Files：
  - `none (git commit / tag / push)`
- Goal：
  - 创建并推送 `stream/v0.1.0`
- Acceptance：
  - `git ls-remote --tags origin refs/tags/stream/v0.1.0` 有结果
- Tests：
  - `git ls-remote --tags origin refs/tags/stream/v0.1.0`
- Rollback：
  - 不删除 tag；如有问题，追加更高 patch

#### SUBSTRM-DOC-1
- Owner：主 Agent
- Worktree：`D:\project\MyFlowHub3\worktrees\subproto-stream-subproto`
- Files：
  - `docs/change/2026-03-28_stream-module-release.md`
  - `docs/change/README.md`
- Goal：
  - 归档 `stream/v0.1.0` 的发布与验证证据
- Acceptance：
  - 文档显式记录发布顺序、版本号、验证命令和回滚策略
- Tests：
  - 人工核对文档与远端 tag 一致
- Rollback：
  - 回退本轮文档改动

### 依赖 / 风险 / 备注
- 依赖 `MyFlowHub-Proto v0.1.4`
- 本仓采用单仓多 module；tag 格式必须是 `stream/v0.1.0`
- 下游 `Server` 的真实 `GOWORK=off` 验证属于后续依赖消费，不在本仓完成前给出结论

阻塞：否
进入 3.2
