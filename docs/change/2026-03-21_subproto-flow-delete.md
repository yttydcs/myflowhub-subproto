# 2026-03-21_subproto-flow-delete

## 变更背景 / 目标
在 Flow 子协议执行层新增删除部署能力，并满足用户要求：删除部署时立即中断该 flow 的运行中/排队 run。

## 具体变更内容（新增 / 修改 / 删除）
- 修改 `flow/types.go`：
  - 新增 `actionDelete`、`actionDeleteResp`。
  - 新增 `permFlowDelete`。
  - 新增 `deleteReq/deleteResp` 类型别名。
- 修改 `flow/actions.go`：
  - 注册 `delete` action。
- 修改 `flow/handler.go`：
  - 新增 `handleDelete` 路由处理，转发策略与 `set` 对齐（逐级裁决 + LCA 判权）。
  - 权限从 `flow.set` 扩展到 `flow.delete`。
  - 新增本地删除执行：删除内存 flow、删除落盘文件、停止/移除 scheduler。
  - 新增删除响应发送链路（`delete_resp`，继承 MsgID/TraceID）。
  - 新增运行取消能力：run 上下文可取消；删除时立即取消该 flow 的 running/queued run，并标记 `cancelled`。
- 新增 `flow/delete_test.go`：
  - 覆盖 delete 成功、not found、permission denied、删除中断运行。

## 对应 plan.md 任务映射
- `SUB-DEL-1`：完成。
- `SUB-DEL-2`：完成。
- `SUB-DEL-3`：完成。
- `SUB-DEL-4`：完成。
- `SUB-DEL-5`：完成。

## 关键设计决策与权衡（性能 / 扩展性）
- 复用 set 的路由模型，避免引入第二套权限/转发机制，降低维护成本。
- run 取消采用 `context.WithCancel`，删除时只针对目标 flow 的 run 精准取消，避免全局扫描外的额外开销。
- 通过统一 `sendCtrlToNodeWithReqHdr` 返回响应，保持链路可观测性一致。

## 测试与验证方式 / 结果
- 直接命令 `GOWORK=off go test ./...` 受依赖解析限制（子模块依赖为独立版本）无法直接通过。
- 使用临时 modfile 指向本地 worktree（测试后已清理）执行：
  - `GOWORK=off go test -mod=mod -modfile go.test.mod ./... -count=1`
  - 结果：通过（`ok github.com/yttydcs/myflowhub-subproto/flow`）。

## 潜在影响与回滚方案
- 潜在影响：删除动作会立即打断正在执行的流程，调用方需处理 `cancelled` 状态。
- 回滚方案：回退 `flow/types.go`、`flow/actions.go`、`flow/handler.go` 与 `flow/delete_test.go`。 
