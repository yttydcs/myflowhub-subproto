# 2026-04-03 Flow Transform Node Runtime

## 变更背景 / 目标

- 当前 `flow` 已具备 `call / compose / set_var / branch / foreach / subflow / cron`，但仍缺少正式的纯计算节点。
- 本轮目标是在 `SubProto flow` 主线补齐 `transform` 节点，让 flow 能在图内直接完成结构化运算，而不是继续依赖外部 capability 或调用方预处理。

## 具体变更内容

### 新增

- `flow/transform_test.go`
  - 覆盖 transform graph validation、数值加一、`foreach` 内 nested object/array、`coalesce(required=false)`、运行时错误路径

### 修改

- `flow/runtime_bindings.go`
  - 新增 `transformSpec` / `transformExpr` decode、shape validation、source validation 和 evaluator
  - 新增白名单运算分发：`add/sub/mul/div/mod/neg/abs/min/max`、`eq/ne/gt/gte/lt/lte`、`and/or/not/coalesce/if`、`concat/lower/upper/trim`、`len`
  - `transform.source` 复用现有 binding source 解析，并支持同级 `required`（默认 `true`）
- `flow/handler.go`
  - `executeNode` 新增 `transform` 执行分支
  - `validateSetNodeKindAndSpec` 接受 `transform`
- `flow/graph_test.go`
  - legacy kind 错误消息更新为包含 `transform`

### 删除

- 无

## Requirements impact

- `updated`

## Specs impact

- `updated`

## Lessons impact

- `none`

## Related requirements

- `D:\project\MyFlowHub3\worktrees\server-transform-node\docs\requirements\flow_data_dag.md`

## Related specs

- `D:\project\MyFlowHub3\worktrees\server-transform-node\docs\specs\flow.md`

## Related lessons

- 无

## 对应 plan.md 任务映射

- `TR-RT-1`
  - `flow/runtime_bindings.go`
  - `flow/handler.go`
  - `flow/graph_test.go`
- `TR-RT-2`
  - `flow/runtime_bindings.go`
  - `flow/handler.go`
- `TR-TEST-1`
  - `flow/transform_test.go`
  - 定向 `flow` 运行时 / graph / orchestrator 回归

## 经验 / 教训摘要

- `transform` 的正确落点是“纯计算节点”，而不是在 `compose` 模板里继续塞动态计算语义。
- `required=false` 必须挂在 `source` 所在表达式节点的同级字段上，而不是 `source` 对象内部。
- 复用现有 binding source 能显著降低新节点接入成本，同时保持 `loop_item/loop_index`、`flow_var` 与祖先校验口径一致。

## 可复用排查线索

- 症状：
  - `op unsupported`
  - `requires exactly`
  - `required source missing`
  - `loop_item only allowed in foreach body`
  - `divide by zero`
- 触发条件：
  - 表达式同时声明多个变体
  - `transform` 使用未知运算名或错误参数个数
  - `source.required=false` 写到了错误层级
  - 在非 `foreach.body` 环境引用 `loop_item/loop_index`
- 关键词 / 错误文本：
  - `must define exactly one of literal, source, op, object or array`
  - `op unsupported`
  - `requires number`
  - `required source missing`
- 快速检查：
  1. 看 `runtime_bindings.go` 是否存在 `decodeNodeTransformSpec`、`evaluateTransformExpr`
  2. 看 `handler.go` 是否在 `executeNode` 和 `validateSetNodeKindAndSpec` 接入 `transform`
  3. 看 specs 是否明确 `required` 是 `source` 节点同级字段

## 关键设计决策与权衡

- 采用结构化表达式树，而不是字符串表达式
  - 好处：可审计、可静态校验、无脚本注入面
  - 代价：首版表达能力受白名单约束，新增运算需继续扩表
- `transform` 只产出节点结果，不直接写局部变量
  - 好处：职责单一，和 `set_var` / `call` 可组合
  - 代价：写回中间值时需要额外一个节点
- `concat` 显式承担“转字符串后拼接”
  - 好处：避免把隐式类型转换扩散到所有 arithmetic / compare 运算
  - 代价：若后续需要更强 coercion，需继续扩展白名单 op

## 测试与验证方式 / 结果

- transform 定向测试：
  - `go test -run 'TestValidateGraphRejectsLegacyKind|TestValidateGraphAllowsTransformNode|TestValidateGraphRejectsTransformUnknownOp|TestValidateGraphRejectsTransformInvalidArity|TestValidateGraphRejectsTransformLoopSourceOutsideForeach|TestExecuteFlow_TransformAddsNodeResultNumber|TestExecuteFlow_TransformForeachBuildsNestedObjectArray|TestExecuteFlow_TransformCoalesceOptionalSource|TestExecuteFlow_TransformFailsOnRuntimeErrors' -count=1`
  - workspace：`D:\project\MyFlowHub3\.tmp\verify-transform-node\go.work`
  - 结果：通过
- 邻近回归测试：
  - `go test -run 'TestValidateGraphOK|TestExecuteFlow_BindsAncestorResultsAndCompose|TestExecuteFlow_SetsAndReadsLocalVars|TestValidateTrigger|TestCronScheduleNextAfter|TestExecuteFlow_BranchSkipsUnselectedPath|TestExecuteFlow_ForeachAggregatesResults|TestExecuteFlow_SubflowReturnsResult' -count=1`
  - workspace：`D:\project\MyFlowHub3\.tmp\verify-transform-node\go.work`
  - 结果：通过
- 全量 `flow` 包：
  - `go test -count=1`
  - workspace：`D:\project\MyFlowHub3\.tmp\verify-transform-node\go.work`
  - 结果：失败
  - 失败项：`TestLoadFlowsFromDiskKeepsLegacyKindsForCompatibility`、`TestFlowDeleteFileFailureKeepsState`
  - 判断：与本轮 `transform` 改动无关，属于当前基线已知失败

## 潜在影响

- `flow set` 新写入契约将正式接受 `kind=transform`
- `foreach.body` 现在可用 `transform` 直接消费 `loop_item/loop_index`
- 若下游编辑器尚未补表单支持，仍需先通过高级 JSON 模式写入 `transform`

## 回滚方案

1. 回退 `flow/handler.go` 中的 `transform` 执行与校验入口
2. 回退 `flow/runtime_bindings.go` 中的 transform expression decode / validate / evaluate
3. 回退 `flow/transform_test.go` 与相关 graph 测试变更
4. 同步回退 `MyFlowHub-Server/docs` 中的 `transform` requirements/specs

## 子Agent执行轨迹

- 本轮未使用子Agent
