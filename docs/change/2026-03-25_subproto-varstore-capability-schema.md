# SubProto VarStore Capability Schema

## 变更背景 / 目标

- Win Flow 编辑器准备把 `varstore::get/set/revoke` 的 ordinary mode 迁到后端 capability schema 驱动，但 SubProto registry 之前没有为这三个方法提供稳定的 `input_schema/output_schema`。
- 本轮目标是在不改变 `varstore` 真实业务语义的前提下，补齐这组三方可消费的 schema，并给 `varstore::set.value` 提供最小 UI hint。

## 具体变更内容

- 在 `varstore/varstore.go` 中为 `varstore::get/set/revoke` 新增 schema 常量，并在 capability descriptor 注册时挂上：
  - `get` 输入：`owner`、`name`
  - `set` 输入：`owner`、`name`、`value`、`type`、`visibility`
  - `revoke` 输入：`owner`、`name`
  - `get/set` 输出：统一复用 record schema
  - `revoke` 输出：`owner`、`name`、`deleted`
- 在 `varstore::set.value` 上增加 `x-ui-control: "textarea"`，供 Win ordinary mode 识别为多行输入。
- 在 `varstore/capability_provider_test.go` 中补充 schema 断言 helper，锁定：
  - required 字段集合
  - `visibility` 的 `enum/default`
  - `value` 的 `x-ui-control`
  - `get/set` 共享输出 schema
- 保留现有 invoke 入参与返回语义，不改 handler 业务逻辑。

## Requirements impact: none

## Specs impact: none

## Lessons impact: none

## Related requirements

- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\requirements\flow_data_dag.md`

## Related specs

- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\exec.md`
- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\flow.md`
- `D:\project\MyFlowHub3\repo\MyFlowHub-SubProto\docs\flow-exec-capability-contract.md`

## Related lessons

- none

## 对应 `plan.md` 任务映射

- `SUBVAR-1`
  - `varstore/varstore.go`
- `SUBVAR-2`
  - `varstore/capability_provider_test.go`
  - `varstore/go.sum`
- `SUBVAR-3`
  - `docs/change/README.md`
  - `docs/change/2026-03-25_subproto-varstore-capability-schema.md`

## 经验 / 教训摘要

- 这类字段少且稳定的 capability schema 更适合直接以常量 `json.RawMessage` 维护，审计成本低，也更容易和 invoke 语义逐字段对齐。
- `x-ui-control` 只需要保留最小必要扩展；把复杂 UI 语义都塞进后端 schema，会迅速扩大跨端耦合面。
- SubProto 子模块本地验证时，如果依赖的本地 `exec` 包尚未通过外部版本发布，测试需要显式用临时 `go.work` 指到同 worktree 内的 `exec` module。

## 可复用排查线索

- 症状：
  - Win capability picker 已能看到方法，但 ordinary mode 仍判定 `missing_schema`
  - `varstore::set.value` 不是多行输入
  - `go test ./...` 在 `varstore` 子模块内直接失败
- 触发条件：
  - descriptor 未注册 `InputSchema` / `OutputSchema`
  - `value` 字段没有透传 `x-ui-control`
  - 本地测试仍解析到外部发布的 `github.com/yttydcs/myflowhub-subproto/exec v0.1.1`
- 关键词：
  - `capabilityVarSetInputSchema`
  - `x-ui-control`
  - `exec/capability`
  - `go.work`
- 快速检查：
  - `reg.Lookup("varstore::set", "")` 看 descriptor 上是否带 schema
  - 检查 `visibility` 是否仍有 `enum/default`
  - 本地测试时确认 `GOWORK` 是否指向同时包含 `varstore` 和 `exec` 的临时 workspace

## 关键设计决策与权衡

- 决策：现在就补齐 `output_schema`
  - 原因：虽然 Win 本轮还不做结果强校验，但 `output_schema` 作为 registry 元数据成本低，后续消费无需再返工协议。
- 决策：保留 `value` 为 `string`，不升级为任意 JSON
  - 原因：要与现有 handler 真实语义保持严格一致，不能为了表单方便擅自扩展协议。
- 决策：使用受限 vendor extension `x-ui-control`
  - 原因：只暴露 Win 当前确定支持的控件提示，避免 schema 语义失控。

## 测试与验证方式 / 结果

- `go test ./...`
  - workdir: `D:\project\MyFlowHub3\worktrees\subproto-varstore-capability-schema\varstore`
  - 结果：通过
  - 说明：通过临时 `GOWORK` 指向 `D:\project\MyFlowHub3\worktrees\subproto-varstore-capability-schema\varstore` 与 `D:\project\MyFlowHub3\worktrees\subproto-varstore-capability-schema\exec` 后执行；临时文件已删除，不属于交付内容。

## 潜在影响与回滚方案

- 潜在影响：
  - consumer 会开始直接读取 `varstore::*` 的输入输出 schema；如果 schema 字段与真实返回值漂移，会立刻影响 ordinary mode 表单和后续结果提示。
- 回滚方案：
  - 回退 `varstore/varstore.go` 中新增的 `InputSchema` / `OutputSchema`
  - 回退 `varstore/capability_provider_test.go` 与 `varstore/go.sum`

## 子Agent执行轨迹

- none
