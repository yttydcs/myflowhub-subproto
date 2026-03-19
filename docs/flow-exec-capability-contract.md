# Flow / Exec 能力职责与调用模型（SubProto）

## 目标
统一 Flow DAG 的节点语义，避免 `local` / `exec` 双模型长期并存；明确 `flow` 与 `exec` 在能力体系中的边界。

## 职责边界
- `flow`：工作流编排器，负责 DAG 执行顺序、重试、超时、状态汇总。
- `exec`：远程调用通道，负责跨节点调用路由与权限裁决。
- 两者均可作为能力提供者（provider）和能力消费者（consumer）。

## 节点模型（写入规范）
- 新写入仅支持 `kind=call`。
- `call.spec` 结构：
  - `method`（必填）
  - `args`（可选）
  - `target`（可选；为空/0/本节点视为本地调用）

## 执行语义
- 本地调用（`target` 为空、0、或等于本节点）：
  1. 先查 `flow.localMethods`（内置方法优先）。
  2. 未命中时查通用能力注册中心（`exec/capability`）。
- 远程调用（`target` 为其他节点）：
  1. 由 `flow` 发起 `exec.call` 请求。
  2. `exec` 负责路由与权限判定。

## 兼容策略
- 写兼容：不保留旧格式（`local/exec`）写入兼容。
- 读兼容：运行期保留旧数据解释执行能力（历史存量可运行）。

## 权限说明
- 远程调用权限由 `exec` 负责，继续采用双层校验：
  - 协议权限（如 `exec.call`）
  - 能力权限（capability descriptor permissions）
- 同节点调用保持自动放行能力级权限（协议权限仍生效）。
