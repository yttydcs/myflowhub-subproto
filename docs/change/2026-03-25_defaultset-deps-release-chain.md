# 2026-03-25 Defaultset 依赖链发布收口

## 变更背景 / 目标
- 背景：
  - `MyFlowHub-Server/modules/defaultset` 已切到显式 `WithDeps` / `runtimedeps` 装配。
  - 已发布的 `exec v0.1.1` 缺失 `capability` / `runtimedeps` 子包。
  - 已发布的 `file`、`flow`、`topicbus`、`varstore`、`management` 版本链也未覆盖当前 `WithDeps` 构造函数。
- 目标：
  - 为 `defaultset` 依赖链补齐正式可发布的 patch 版本；
  - 让下游 `MyFlowHub-Server` 在 `GOWORK=off` 模式下可通过真实 semver 解析到这些 API。

## 具体变更内容
- module metadata
  - `file/go.mod`：`exec v0.1.1` -> `v0.1.2`
  - `flow/go.mod`：`exec v0.1.1` -> `v0.1.2`
  - `management/go.mod`：`exec v0.1.1` -> `v0.1.2`
  - `topicbus/go.mod`：`exec v0.1.1` -> `v0.1.2`
  - `varstore/go.mod`：`exec v0.1.1` -> `v0.1.2`
- release scope
  - `exec`：对外提供 `capability` / `runtimedeps`
  - `file` / `flow` / `topicbus` / `varstore` / `management`：对外提供 `NewHandlerWithDeps`
- 验证方式
  - 使用 repo-local `go.work` 绑定本地 `Core/Proto/broker/exec/file/flow/topicbus/varstore/management`
  - 分模块执行 `go test ./... -count=1 -p 1`

## Requirements impact
- `none`

## Specs impact
- `none`

## Lessons impact
- `updated`

## Related requirements
- `none`

## Related specs
- `none`

## Related lessons
- `D:\project\MyFlowHub3\docs\lessons\cross-repo-semver-release.md`

## 对应 plan.md 任务映射
- `SUBREL1`：确认 `defaultset` 依赖链的发布边界
- `SUBREL2`：完成 repo-local 联调验证
- `SUBREL3`：发布 `exec` / `file` / `flow` / `topicbus` / `varstore` / `management` patch tag

## 经验 / 教训摘要
- 只看最先报错的 `file/management` 容易误判范围，真正阻塞的是整条 `defaultset` 依赖链。
- `go.work` 可以证明本地联调通过，但不能替代 module-level release chain 的收口。
- 只要上游 module 新增了同仓 shared package，所有引用它的 sibling modules 都要同步检查 `go.mod` 的版本约束。

## 可复用排查线索
- 症状
  - `undefined: filehandler.NewHandlerWithDeps`
  - `undefined: management.NewHandlerWithDeps`
  - `no required module provides package github.com/yttydcs/myflowhub-subproto/exec/runtimedeps`
  - `no required module provides package github.com/yttydcs/myflowhub-subproto/exec/capability`
- 触发条件
  - 下游仓库在 `GOWORK=off` 或单仓构建模式下消费 `defaultset`
  - 本地 worktree 已合入 `WithDeps` / `runtimedeps`，但 semver tag 尚未同步
- 关键词
  - `defaultset`
  - `WithDeps`
  - `runtimedeps`
  - `capability`
  - `GOWORK=off`
- 快速检查
  - 检查 `exec` 最新 tag 是否包含 `capability` / `runtimedeps`
  - 检查 `file/flow/topicbus/varstore/management` 的 `go.mod` 是否仍锁在旧 `exec`
  - 检查下游 `Server` 是否仍引用旧的 patch 版本

## 关键设计决策与权衡
- 采用“一次补齐整条依赖链”的发布方式，而不是只发布最先报错的两个 module。
- 继续保留 `WithDeps` 设计，不通过回退 `Server` 来绕过发布链问题。
- 上游验证使用 repo-local `go.work`，正式有效性则交给下游 `GOWORK=off` 验证。

## 测试与验证方式 / 结果
- `exec`
  - `go test ./... -count=1 -p 1` -> 通过
- `file`
  - `go test ./... -count=1 -p 1` -> 通过
- `flow`
  - `go test ./... -count=1 -p 1` -> 通过
- `topicbus`
  - `go test ./... -count=1 -p 1` -> 通过
- `varstore`
  - `go test ./... -count=1 -p 1` -> 通过
- `management`
  - `go test ./... -count=1 -p 1` -> 通过

## 潜在影响
- 下游若继续锁定旧 patch 版本，仍会复现相同问题。
- 本次 patch tag 会把当前 `defaultset` 依赖链上的兼容性改动一起暴露给下游。

## 回滚方案
- 若 tag 尚未推送：
  - 删除本地 tag
  - 回退本次 `go.mod` 与归档变更
- 若 tag 已推送：
  - 不重写已发布 tag，改发更高 patch 版本覆盖

## 子Agent执行轨迹
- 本轮未使用子 Agent。
