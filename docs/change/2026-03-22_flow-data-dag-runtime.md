# 2026-03-22 Flow 数据流 DAG 运行时

## 变更背景 / 目标

- 现有 `flow` 运行时只保留节点 `status/code/msg` 摘要，后续节点无法显式引用上游结果。
- 你确认的首版目标是：
  - 支持祖先节点结果向后续节点传递；
  - 增加 `compose` 节点；
  - 保持默认 `status_resp` 轻量，不回传完整节点结果。

## Requirements / Specs Impact

- Requirements impact：`updated`
- Specs impact：`updated`
- Related requirements：
  - `D:\project\MyFlowHub3\worktrees\server-data-dag-specs\docs\requirements\flow_data_dag.md`
- Related specs：
  - `D:\project\MyFlowHub3\worktrees\server-data-dag-specs\docs\specs\flow.md`

## 具体变更内容（新增 / 修改 / 删除）

### 新增

- `flow/runtime_bindings.go`
  - 新增运行时 `runContext` / 节点结果存储结构。
  - 新增结构化输入绑定、JSON Pointer、祖先关系校验与触发上下文 helper。
- `flow/data_dag_test.go`
  - 覆盖祖先结果传递、`compose` 结果拼装、缺失必填绑定失败。

### 修改

- `flow/handler.go`
  - `runState` 增加内部 `runtime` 上下文，保存 `trigger` 与节点 `result`。
  - `executeFlow` 增加节点运行中/成功/失败状态写回，并在成功时保留结果。
  - `executeNode` 支持：
    - `call.args_template + inputs`
    - `compose.template + inputs`
    - 本地 / capability / 远程 `exec.call` 的结果写回
  - 事件触发与定时触发统一写入规范化 trigger 上下文。
  - `validateGraph` / `validateSetNodeKindAndSpec` 增强为 `call + compose`，并校验输入绑定与祖先依赖。
- `flow/graph_test.go`
  - 覆盖 `compose` 合法图、未知依赖节点、非祖先绑定失败。
- `flow/local_capability_test.go`
  - 对齐新的 `executeNode` 返回签名。
- `flow/trigger_test.go`
  - 增加 event / var_changed / interval 的 trigger 上下文断言。
- `flow/runtime_fix_test.go`
  - 对齐新的 `runState` 结构。

### 删除

- 无。

## 对应 `plan.md` 任务映射

- `DAG-RT-1` → `flow/handler.go`, `flow/runtime_bindings.go`
- `DAG-RT-2` → `flow/handler.go`, `flow/runtime_bindings.go`, `flow/data_dag_test.go`
- `DAG-RT-3` → `flow/handler.go`, `flow/runtime_bindings.go`, `flow/graph_test.go`
- `DAG-RT-4` → `flow/data_dag_test.go`, `flow/trigger_test.go`, `flow/local_capability_test.go`, `flow/runtime_fix_test.go`

## 关键设计决策与权衡

- 结果保存在运行时内存，不扩展 `status_resp`：
  - 好处：避免把大结果带入常规状态查询链路；
  - 代价：完整结果只在单次运行上下文内可见。
- 输入绑定使用结构化对象而不是字符串模板：
  - 好处：前后端都能做静态校验，错误定位更明确；
  - 代价：运行时需要增加 JSON Pointer 读写逻辑。
- 祖先关系按真实 DAG 可达性判断：
  - 好处：不会被节点声明顺序误导；
  - 代价：`set` 阶段要多做一次拓扑和祖先集计算。
- 首版 trigger 上下文只提供最小必要字段：
  - `interval`：`triggered_at`
  - `event`：`type/mode/topic/name/payload/ts`
  - `var_changed`：`type/owner/name/op`

## 测试与验证方式 / 结果

- 目录：`D:\project\MyFlowHub3\worktrees\subproto-data-dag-bindings\flow`
- 因本地 worktree 需要引用未发布的本地 `core/proto/exec/broker`，验证时使用临时 `go.test.mod` 做 replace，验证后已删除。
- 命令：
  - `$env:GOWORK='off'; go test -mod=mod -modfile go.test.mod ./... -count=1 -p 1`
- 结果：
  - 通过（`ok github.com/yttydcs/myflowhub-subproto/flow`）

## 3.3 Code Review 结论

- 需求覆盖：通过。`call + compose + 祖先结果引用 + trigger 上下文` 均已落地。
- 架构合理性：通过。运行时新增能力集中在 `flow` 内部 helper，没有扩散到 proto wire。
- 性能风险：通过。`status_resp` 仍只返回摘要；祖先计算仅发生在 `set` / build 阶段；无额外 I/O。
- 可读性与一致性：通过。绑定、Pointer、trigger 规范化逻辑集中到 `flow/runtime_bindings.go`。
- 可扩展性与配置化：通过。`source.kind`、`InputBinding`、`compose` 都留有后续扩展位。
- 稳定性与安全：通过。非法 Pointer、未知节点、非祖先引用、缺失必填来源均会显式失败。
- 测试覆盖情况：通过。新增端到端数据流测试，并补齐 trigger / graph / 兼容路径。
- 子Agent治理与审计：通过。本轮未使用子Agent；原因是共享写集集中且当前会话未获得显式委派授权。

## 潜在影响与回滚方案

- 潜在影响：
  - 新写入图现在允许 `compose` 且会严格校验输入绑定；旧的非法引用会在 `set` 阶段直接被拒绝。
  - event / var_changed 触发的运行上下文结构变得稳定，后续节点可以依赖这些字段。
- 回滚方案：
  1. 回退 `flow/runtime_bindings.go` 与 `flow/handler.go` 的数据流 DAG 逻辑。
  2. 回退 `flow/data_dag_test.go`、`flow/graph_test.go`、`flow/trigger_test.go` 等新增/修改测试。

## 子Agent执行轨迹

- 本轮未使用子Agent。
- 原因：
  - 当前运行环境未得到用户显式授权进行委派；
  - `flow/handler.go` 与运行时 helper 写集强耦合，拆分后集成风险高。
- Task ID → Agent → Worktree → 文件 → 验收结果
  - `DAG-RT-1` → 主Agent → `D:\project\MyFlowHub3\worktrees\subproto-data-dag-bindings` → `flow/handler.go`, `flow/runtime_bindings.go` → 通过
  - `DAG-RT-2` → 主Agent → `D:\project\MyFlowHub3\worktrees\subproto-data-dag-bindings` → `flow/handler.go`, `flow/runtime_bindings.go`, `flow/data_dag_test.go` → 通过
  - `DAG-RT-3` → 主Agent → `D:\project\MyFlowHub3\worktrees\subproto-data-dag-bindings` → `flow/handler.go`, `flow/runtime_bindings.go`, `flow/graph_test.go` → 通过
  - `DAG-RT-4` → 主Agent → `D:\project\MyFlowHub3\worktrees\subproto-data-dag-bindings` → `flow/data_dag_test.go`, `flow/trigger_test.go`, `flow/local_capability_test.go`, `flow/runtime_fix_test.go` → 通过
