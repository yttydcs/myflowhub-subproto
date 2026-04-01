# 2026-04-02 Flow Local Vars Detail Mainline

## 变更背景 / 目标

- 主线 `SubProto flow` 仍缺少 `set_var`、`flow_var` 和 `detail`，而 Win 编辑器与旧 dirty worktree 已经先行。
- 本轮目标是在 clean branch 上把 local vars 与 detail 运行时收口到当前 `main` 基线，并保留可审计测试结果。

## 具体变更内容

### 新增

- `flow/detail_test.go`
  - 覆盖 detail 根结果、子路径、not found、非法 path

### 修改

- `flow/actions.go`
  - 注册 `detail` action
- `flow/types.go`
  - 新增 `detail` action alias 和 `DetailReq` / `DetailResp` alias
- `flow/handler.go`
  - 新增 `handleDetail` / `sendDetailResp`
  - `executeNode` 支持 `set_var`
  - `validateGraph` / `validateSetNodeKindAndSpec` 接受 `set_var`
- `flow/runtime_bindings.go`
  - 新增 `RunContext.vars` / `varRuntimeData`
  - 新增 `setVarSpec`
  - 新增 `flow_var` 解析、`set_var` 模板归一化与祖先写入者校验
- `flow/data_dag_test.go`
  - 新增 local vars 成功写入 / 运行时缺失用例
- `flow/flow_id_test.go`
  - 新增 detail 非法 `flow_id` 拒绝测试
- `flow/graph_test.go`
  - 新增 `flow_var` 唯一祖先写入者 / 缺失 / 歧义测试
- `flow/runtime_fix_test.go`
  - 新增 detail 远端转发失败响应测试

### 删除

- 无

## Requirements impact

- `updated`

## Specs impact

- `updated`

## Lessons impact

- `none`

## Related requirements

- `D:\project\MyFlowHub3\worktrees\server-local-vars-clean\docs\requirements\flow_data_dag.md`

## Related specs

- `D:\project\MyFlowHub3\worktrees\server-local-vars-clean\docs\specs\flow.md`

## Related lessons

- 无

## 对应 plan.md 任务映射

- `SUB-LV-1`
  - `flow/handler.go`
  - `flow/runtime_bindings.go`
  - `flow/data_dag_test.go`
  - `flow/graph_test.go`
- `SUB-RD-1`
  - `flow/actions.go`
  - `flow/types.go`
  - `flow/handler.go`
  - `flow/detail_test.go`
  - `flow/flow_id_test.go`
  - `flow/runtime_fix_test.go`
- `SUB-VAL-1`
  - 目标测试与全量 `flow` 模块回归评估

## 经验 / 教训摘要

- `flow_var` 的正确语义不是按声明顺序取最后一次写入，而是按当前节点祖先子图解析唯一写入者。
- `set_var` 的默认模板值应是 `null`，不能复用 `compose` 的 `{}` 默认值。
- `detail` 应保持为独立重查询入口，不污染 `status` 轮询语义。

## 可复用排查线索

- 症状：
  - `flow_var "<name>" has no ancestor writer`
  - `flow_var "<name>" has ambiguous ancestor writers`
  - `flow_var name required`
  - `node_id required`
  - `invalid detail path`
- 触发条件：
  - `flow_var` 引用了当前节点祖先链外的变量
  - 多个并行祖先 `set_var` 写入同名变量并流向同一消费者
  - detail 请求缺少 `node_id` 或 path 不是合法 JSON Pointer
- 关键词 / 错误文本：
  - `unsupported binding source kind: flow_var`
  - `kind must be call, compose or set_var`
  - `invalid detail path`
- 快速检查：
  1. 看 `runtime_bindings.go` 是否存在 `case "flow_var"`
  2. 看 `handler.go` 是否注册 `handleDetail`
  3. 看 Proto workspace 是否已提供 `ActionDetail`

## 关键设计决策与权衡

- 读取局部变量采用 `source.kind=flow_var`
  - 好处：复用现有模板物化链路
  - 代价：静态校验必须并入 binding 校验
- detail 维持“单节点 + 可选 path”
  - 好处：适配结果面板且不拉大响应
  - 代价：若后续要查整次 run，需要扩展契约

## 测试与验证方式 / 结果

- 目标测试：
  - `go test -run 'TestFlowDetail|TestExecuteFlow|TestValidateGraph|TestFlowHandlersRejectInvalidFlowID|TestFlowRemoteForwardFailureReturnsResp' github.com/yttydcs/myflowhub-subproto/flow/... -count=1 -p 1`
  - workspace：`D:\project\MyFlowHub3\.tmp\verify-local-vars-clean\go.work`
  - 结果：通过
- 全量 `flow` 模块：
  - `go test github.com/yttydcs/myflowhub-subproto/flow/... -count=1 -p 1`
  - 结果：失败
  - 失败项：`TestFlowDeleteSuccess`、`TestFlowDeleteNotFound`、`TestFlowDeleteInterruptsActiveRun`、`TestFlowDeleteFileFailureKeepsState`
  - 判断：与本轮 local vars/detail 迁移无关，属于当前基线 delete 权限语义问题
- `git diff --check`
  - 结果：通过

## 潜在影响

- 主线 `flow set` 将开始接受 `set_var`
- 主线运行时开始支持 `flow_var` 和 `detail`
- 若下游未同步 Proto detail 契约，会在编译期失败

## 回滚方案

1. 回退 `flow/handler.go` 中的 `set_var` / `detail` 路径
2. 回退 `flow/runtime_bindings.go` 中的 vars 模型与静态校验
3. 回退新增测试和 action/type alias

## 子Agent执行轨迹

- 本轮未使用子Agent
