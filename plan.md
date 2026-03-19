# Plan - SubProto：Flow 节点统一为 call（Exec 作为远程调用能力）

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 分支：`refactor/unified-call-node`
- Worktree：`d:\project\MyFlowHub3\repo\MyFlowHub-SubProto\worktrees\refactor-unified-call-node`
- Base：`main`

## 1) 需求分析

### 目标
- 将 Flow DAG 节点语义收敛为统一 `call` 模型，避免 `local/exec` 二分。
- 明确职责：
  - `flow` 负责工作流编排与节点执行；
  - `exec` 负责远程调用链路；
  - 两者都作为能力提供者 / 消费者。
- 新明确内容写入 SubProto 文档，便于后续协作与审计。

### 范围
- 必须：
  - Flow 节点执行新增统一 `call` 路径（本地/远程由 `target` 决定）。
  - Set 写入路径仅接受 `kind=call`（不保留旧写兼容）。
  - `flow` 远程调用通过 `exec.call` 链路（`sendExecCall`）执行。
  - 补齐单测覆盖：`call` 本地、`call` 远程、旧格式读取执行兼容、写入校验。
  - 在 SubProto 文档中记录新的职责边界与节点模型。
- 可选：
  - 旧 `local/exec` 历史数据读取后兼容执行（不作为新写入格式）。
- 不做：
  - 本轮不改 wire 协议字段结构（`protocol/flow.Node` 仍保留 `kind+spec`）。
  - 本轮不改 Win 前端编辑器交互。

### 使用场景
- 用户在 Flow 中配置单一 `call` 节点：
  - `target` 为空/0/本节点 → 本地能力调用；
  - `target` 为其他节点 → 远程能力调用（经 exec）。

### 功能需求
- Flow `executeNode` 支持 `kind=call`。
- `validateGraph` 对写入做 `kind=call` 强约束。
- 运行期保留 `local/exec` 兼容解释（用于历史数据读取执行）。

### 非功能需求
- 性能：避免新增跨节点探测或重复查表，保持单节点 O(1) 方法路由。
- 可维护：统一调用分发逻辑，减少分支重复。
- 可扩展：后续增加调用选路策略时仅扩展 `call` spec。

### 输入输出
- 输入：`flow.set` 的 DAG 节点 `kind/spec`。
- 输出：`flow.run/status` 行为一致；`flow.set` 对旧写格式返回 400。

### 边界异常
- `call.method` 为空 → 400。
- `call.target` 非法（负值/0 视本地，远程必须正数）→ 本地/远程分支按规则处理。
- 远程调用超时/拒绝沿用现有 `exec.call` 错误码。

### 验收标准
- `kind=call` 节点可成功执行本地与远程调用。
- `kind=local/exec` 的新写入被拒绝。
- 历史 `local/exec` 数据在运行期可执行（读取兼容）。
- 文档包含职责边界与迁移说明。

### 风险
- Win 端若仍写 `local/exec` 将被后端拒绝（预期行为）。
- 若历史数据存在非法 spec，运行时可能失败（保持显式错误）。

阻塞：否

---

## 2) 架构设计（分析）

### 总体方案
- 采用统一 `call` 节点模型：
  - `spec = { target?, method, args }`
  - 本地调用：直接调本地方法表 + capability registry。
  - 远程调用：复用 `exec.call` 请求/响应链路。

### 选型理由
- 相比保留 `local/exec` 双模型：
  - 减少 UI/执行器分支复杂度；
  - 更符合“能力提供者/消费者”统一抽象；
  - 扩展新调用策略时无需新增节点 kind。

### 备选方案对比
- 方案 A（采用）：Flow 内统一 `call`，旧数据仅读兼容。
- 方案 B（未采用）：继续公开 `local/exec`，仅内部共用逻辑。
  - 问题：外部语义仍分裂，无法达成本轮目标。

### 模块职责
- `flow/handler.go`：节点 spec 解析、调用路由、写入校验。
- `exec/handler.go`：远程调用裁决与执行（本轮不改协议）。
- `docs/change/*`：记录职责边界与迁移策略。

### 数据/调用流
1. `flow.set`：`validateGraph` 要求 `kind=call`。
2. `flow.run`：`executeNode` 解析调用 spec。
3. `target` 本地：调用 `localMethods`，未命中再查 capability registry。
4. `target` 远程：发送 `exec.call`，等待 `call_resp`。

### 接口草案
- `kind=call` spec：
  - `target` `uint32` 可选；
  - `method` `string` 必填；
  - `args` `json` 可选。

### 错误与安全
- 写入阶段拒绝旧 kind，避免新数据继续扩散旧模型。
- 远程调用仍走既有双层权限（协议权限+能力权限，由 exec 负责）。

### 性能与测试策略
- 保持现有 broker 等待模型，不新增额外网络跳。
- 单测覆盖：本地调用、远程调用、旧格式运行、写入拒绝。

### 可扩展性设计点
- 后续可在 `callSpec` 增加 `version/route_policy` 字段，不破坏节点 kind 语义。

阻塞：否

---

## 3.1) 计划拆分（Checklist）

### UCN-1 - 统一 call 节点执行路径
- 目标：在 `flow` 执行器中落地统一 `call` 解析与路由。
- 涉及文件：
  - `flow/handler.go`
- 验收条件：
  - `kind=call` 支持本地与远程调用。
- 测试点：
  - 本地 `debug::echo` / 远程 call 响应。
- 回滚点：revert UCN-1 提交。

### UCN-2 - 写入校验改为 call-only
- 目标：`flow.set` 阶段拒绝 `local/exec` 新写入。
- 涉及文件：
  - `flow/handler.go`
- 验收条件：
  - `validateGraph` 返回明确错误信息。
- 回滚点：revert UCN-2 提交。

### UCN-3 - 旧数据读取执行兼容
- 目标：运行期仍可执行历史 `local/exec` 节点。
- 涉及文件：
  - `flow/handler.go`
- 验收条件：
  - 兼容路径单测通过。
- 回滚点：revert UCN-3 提交。

### UCN-4 - 补充测试
- 目标：覆盖 call 本地/远程与写入校验。
- 涉及文件：
  - `flow/local_capability_test.go`
  - `flow/graph_test.go`
  - `flow/handler_test.go`（如需新增）
- 验收条件：
  - `go test ./...` 在 `flow` 模块通过。
- 回滚点：revert UCN-4 提交。

### UCN-5 - 文档归档（SubProto）
- 目标：记录职责重定义、节点模型迁移与影响。
- 涉及文件：
  - `docs/change/2026-03-19_unified-call-node-model.md`
- 验收条件：
  - 包含任务映射、权衡、测试与回滚方案。
- 回滚点：revert UCN-5 提交。
