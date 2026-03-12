# SubProto：适配 Core Pipe 抽象（仅更新测试 stub/mock，不改子协议语义）

## 变更背景 / 目标
现状（变更前）：
- SubProto 业务代码通常只依赖 `core.IConnection` 能力，不关心承载。
- 但本仓多个单测通过 mock/stub “实现 `core.IConnection` 接口”，当 Core 将 `RawConn()` 改为 `Pipe()` 后会导致编译失败。

目标（本次变更后）：
- 仅做适配性改动，使测试可编译/可运行；
- 不改变任何子协议 wire/语义/路由规则。

## 具体变更内容
- 更新以下模块的测试 mock/stub：
  - `auth/test_mocks_test.go`
  - `management/action_nodes_test.go`
  - `varstore/target_forward_test.go`
- 统一改为实现 `Pipe() core.IPipe`，移除 `RawConn() net.Conn`；
- 引入 `nopPipe`（仅满足接口，不参与业务逻辑）。

## plan.md 任务映射
- SUB1：适配测试 stub/mock 以满足新的 `core.IConnection`
- SUB2：关键模块单测回归

## 测试与验证
在 workflow-local `go.work`（`worktrees/refactor-transport-pipe/go.work`）下验证：
- `cd auth; go test ./... -count=1 -p 1` ✅
- `cd management; go test ./... -count=1 -p 1` ✅
- `cd varstore; go test ./... -count=1 -p 1` ✅

## 潜在影响
- 仅影响测试代码；业务逻辑与子协议行为不变。

## 回滚方案
- 回滚本次提交（或整体 revert），并同步回滚 Core 的 `IConnection` 变更（恢复 `RawConn()`）。

