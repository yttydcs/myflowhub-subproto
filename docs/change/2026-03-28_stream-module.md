# 2026-03-28 SubProto：stream module 首版落地

## 变更背景 / 目标

- 为新的 `stream` 子协议落地独立 module，实现 source / consumer / delivery 三层状态，以及 CTRL / DATA / ACK 处理。
- 保持 `MyFlowHub-SubProto` 只依赖 `myflowhub-core + myflowhub-proto`，不回退到 Server 私有实现。

## 具体变更内容

- 新增 [`stream/go.mod`](D:/project/MyFlowHub3/worktrees/subproto-stream-subproto/stream/go.mod)
  - module：`github.com/yttydcs/myflowhub-subproto/stream`
- 新增 [`stream/types.go`](D:/project/MyFlowHub3/worktrees/subproto-stream-subproto/stream/types.go)
  - 映射公开 `protocol/stream` 常量 / 类型
  - 定义私有 helper action：
    - `delivery_prepare`
    - `delivery_activate`
    - `delivery_abort`
    - `delivery_close`
- 新增 [`stream/bin.go`](D:/project/MyFlowHub3/worktrees/subproto-stream-subproto/stream/bin.go)
  - 实现 `DATA/ACK` 小头编解码
- 新增 [`stream/uuid.go`](D:/project/MyFlowHub3/worktrees/subproto-stream-subproto/stream/uuid.go)
  - 实现 `delivery_id / txn_id` 所需 UUID helper
- 新增并完成 [`stream/handler.go`](D:/project/MyFlowHub3/worktrees/subproto-stream-subproto/stream/handler.go)
  - source / consumer catalog
  - producer / consumer delivery state
  - coordinator `delivery_routes`
  - `announce/list/get/subscribe/connect/disconnect/signal`
  - DATA / ACK 方向校验与 active gate
  - 私有 `prepare/activate/abort/close` 协调路径
  - owner 撤销后的 best-effort 清理
- 新增 [`stream/handler_test.go`](D:/project/MyFlowHub3/worktrees/subproto-stream-subproto/stream/handler_test.go)
  - 覆盖：
    - 本地 source catalog
    - 本地 consumer catalog
    - 同节点 connect 成功
    - `kind mismatch` 回滚
    - DATA active gate
    - ACK 方向校验与推进
- 新增 [`stream/go.sum`](D:/project/MyFlowHub3/worktrees/subproto-stream-subproto/stream/go.sum)
  - 补齐 module 依赖校验

## Requirements impact

- `none`

## Specs impact

- `updated`

## Lessons impact

- `none`

## Related requirements

- [`docs/requirements/stream.md`](D:/project/MyFlowHub3/worktrees/server-stream-subproto-design/docs/requirements/stream.md)

## Related specs

- [`docs/specs/stream.md`](D:/project/MyFlowHub3/worktrees/server-stream-subproto-design/docs/specs/stream.md)

## Related lessons

- `none`

## 对应 plan.md 任务映射

- `SUBSTRM-1`
- `SUBSTRM-2`
- `SUBSTRM-3`

## 经验 / 教训摘要

- coordinator 身份不能偷懒复用 `requester`；第三方控制节点与实际协调节点是两个不同概念。
- consumer 侧 `unit_mode` 不能硬编码；至少要跟随 producer/source 的声明进入 delivery state。
- 新 module 在 Proto 尚未发布新 tag 前，`GOWORK=off` 无法形成端到端联调；这轮验证必须依赖 worktree-local `go.work`。

## 可复用排查线索

- 症状
  - `unknown delivery` / active 前 DATA 被错误接收
  - `kind mismatch` 后残留半开 delivery
  - 远程 owner 已撤销，但 coordinator route 还残留
- 触发条件
  - 新建 `connect/subscribe` 协调路径
  - owner 撤销 source / consumer endpoint
- 关键词
  - `delivery_prepare`
  - `delivery_activate`
  - `delivery_abort`
  - `delivery_close`
  - `kind mismatch`
  - `unit_mode`
  - `coordinator`
- 快速检查
  - 先看 [`stream/handler.go`](D:/project/MyFlowHub3/worktrees/subproto-stream-subproto/stream/handler.go) 里的 `establishDelivery`
  - 再看 [`stream/handler_test.go`](D:/project/MyFlowHub3/worktrees/subproto-stream-subproto/stream/handler_test.go)

## 关键设计决策与权衡

- 保持 `file` 风格的逐级控制路由，但把 delivery 的装拆协调隐藏在私有 helper action 里。
- `sendToNode` 在目标就是本节点时不做 loopback 递归，避免在 handler 内制造隐式本地重入。
- 同步补单测，而不是先把 module 编过再留待后续补覆盖，减少交付时的状态机回归风险。

## 测试与验证方式 / 结果

- 临时 workspace：
  - `D:\project\MyFlowHub3\worktrees\subproto-stream-subproto\go.work`
  - 指向本地 `Core + Proto + stream`
- 执行：`go test ./... -count=1 -p 1`
- 目录：`D:\project\MyFlowHub3\worktrees\subproto-stream-subproto\stream`
- 结果：通过

## 潜在影响与回滚方案

- 潜在影响
  - 这轮 module 仍依赖本地 workspace 才能和未发布的 `protocol/stream` 联调
- 回滚方案
  - 回退 `stream/` 目录新增文件
  - 删除临时 `go.work` / `go.work.sum`

## 子Agent执行轨迹

- 本轮未使用子Agent
