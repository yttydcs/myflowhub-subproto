# Plan - SubProto：Flow 删除部署执行链路

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- Branch：`feat/subproto-deploy-delete`
- Worktree：`D:/project/MyFlowHub3/repo/MyFlowHub-SubProto/repo/MyFlowHub-SubProto/worktrees/feat-subproto-deploy-delete`
- Base：`main`

## 项目目标与当前状态
- 目标：在 Flow 子协议中新增 `delete/delete_resp`，支持远端删除部署，并按需求“删除时立即中断运行中的 run”。
- 当前状态：`flow/actions.go` 仅注册 `set/run/status/list/get`；`handler.go` 仅支持写入/查询/运行，不支持删除与运行中断。

## 可执行任务清单（Checklist）
- [x] SUB-DEL-1 扩展 Flow 类型别名与 action 注册
- [x] SUB-DEL-2 新增 delete 路由与权限判定
- [x] SUB-DEL-3 新增本地删除执行（内存+落盘+scheduler 清理）
- [x] SUB-DEL-4 新增“删除时中断运行”机制
- [x] SUB-DEL-5 补齐单测与回归

## 任务明细

### SUB-DEL-1 扩展类型与 action 注册
- 目标：让 handler 能识别 delete 动作并使用 proto 新类型。
- 涉及模块/文件：
  - `flow/types.go`
  - `flow/actions.go`
- 验收条件：
  - actions 注册包含 `actionDelete`。
  - 类型别名包含 `deleteReq/deleteResp` 与 `permFlowDelete`。
- 测试点：
  - 编译通过。
- 回滚点：
  - 回退上述文件新增符号。

### SUB-DEL-2 新增 delete 路由与权限判定
- 目标：delete 路由策略与 set 对齐（逐级裁决 + LCA 判权）。
- 涉及模块/文件：
  - `flow/handler.go`
- 验收条件：
  - 支持 `origin_node/executor_node` 路由。
  - 权限使用 `flow.delete`。
  - 响应走 `delete_resp` 且继承 MsgID/TraceID。
- 测试点：
  - 权限拒绝返回 403。
  - 非法请求返回 400。
- 回滚点：
  - 回退 delete 路由入口与响应发送代码。

### SUB-DEL-3 本地删除执行与状态清理
- 目标：在 executor 本地删除部署定义并停止触发器调度。
- 涉及模块/文件：
  - `flow/handler.go`
- 验收条件：
  - 从 `flows` map 删除 flow。
  - 删除 `baseDir/<flow_id>.json`。
  - 停止并移除该 flow 的 scheduler。
  - 返回 `delete_resp(code=1)`。
- 测试点：
  - 删除后 list/get 不再返回该 flow。
  - 重复删除返回 404（或约定错误码）。
- 回滚点：
  - 回退本地删除逻辑。

### SUB-DEL-4 删除时中断运行机制
- 目标：满足需求“删除部署时立即中断运行中的 run”。
- 涉及模块/文件：
  - `flow/handler.go`
- 验收条件：
  - 为 run 增加可取消上下文与 cancel 索引。
  - delete 命中 flow 时，主动 cancel 该 flow 的 running/queued run。
  - 被中断 run 的状态可观测（例如 cancelled/failed + msg）。
- 测试点：
  - 长耗时节点运行中删除，run 可在超时前中断并状态更新。
- 回滚点：
  - 回退 cancel 索引与 delete 中断逻辑。

### SUB-DEL-5 单测与回归
- 目标：覆盖关键路径与边界。
- 涉及模块/文件：
  - `flow/*_test.go`（新增/修改）
- 验收条件：
  - 至少覆盖：delete 成功、delete not found、权限拒绝、删除中断运行。
  - `go test ./flow -count=1` 通过。
- 测试点：
  - 行为断言 + 响应头继承断言。
- 回滚点：
  - 回退新增测试与实现。

## 依赖关系
- 依赖 Proto workflow 提供 delete 协议结构与常量。
- 被 Win workflow 依赖（DeleteSimple 调用链）。

## 风险与注意事项
- 风险：运行中断语义若处理不完整，可能造成 run 状态与 list/status 不一致。
- 风险：并发取消与 map 清理需避免竞态。
- 注意：保持 wire 兼容，禁止计划外改动其他 action 语义。

