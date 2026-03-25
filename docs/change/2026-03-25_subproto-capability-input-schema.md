# SubProto Capability Input Schema

## 变更背景 / 目标

- Win Flow 编辑器 ordinary mode 已支持消费 capability `input_schema`，但 `topicbus::publish`、`file::mkdir`、`file::list`、`file::read_text` 仍未在后端注册 schema。
- 本轮目标是把这批常用 capability 的输入结构收敛到 SubProto capability registry，作为跨端复用的统一元数据来源。

## 具体变更内容

- 在 `topicbus::publish` 注册点补充 `InputSchema`，暴露 `topic`、`name`、`ts`、`payload` 四个字段，其中 `name` 为必填。
- 在 `file::mkdir`、`file::list`、`file::read_text` 注册点补充 `InputSchema`，字段与各自 invoke 函数真实接收的 JSON 参数保持一致。
- schema 全部限制在 Win 当前支持的 JSON Schema 子集内，不使用 `oneOf/anyOf/allOf/$ref/array`。
- 扩充 provider 注册测试，验证 descriptor 已带上正确 schema。
- 扩充 `exec.cap_query(include_schema=true)` 测试，验证 query 路径会按请求返回 schema，且 `include_schema=false` 时不透传 schema。
- 为 `file` 和 `topicbus` 模块补充 `go.sum` 中 `github.com/yttydcs/myflowhub-subproto/exec v0.1.1` 的校验项，保证独立 module 在本地验证时依赖完整。

## Requirements impact: none

## Specs impact: none

## Lessons impact: none

## Related requirements

- `D:\project\MyFlowHub3\repo\MyFlowHub-Win\docs\requirements\flow-editor-visual-form.md`

## Related specs

- `D:\project\MyFlowHub3\repo\MyFlowHub-Win\docs\specs\flow-editor-visual-form.md`
- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\exec.md`
- `D:\project\MyFlowHub3\worktrees\subproto-capability-input-schema\docs\flow-exec-capability-contract.md`

## Related lessons

- none

## 对应 `plan.md` 任务映射

- `SUBCAP-1`
  - `topicbus/topicbus.go`
  - `file/handler.go`
- `SUBCAP-2`
  - `topicbus/capability_provider_test.go`
  - `file/capability_provider_test.go`
  - `exec/cap_registry_test.go`
- `SUBCAP-3`
  - `docs/change/README.md`
  - `docs/change/2026-03-25_subproto-capability-input-schema.md`

## 经验 / 教训摘要

- capability schema 一旦要给 Win ordinary mode 使用，就必须在后端侧保持“字段名、类型、required 约束”与实际 invoke 参数严格一致。
- `cap_query` 的 schema 透传测试不需要跨模块依赖真实 provider，直接往本地 capability registry 注册带 schema 的 descriptor 就能锁定 query 行为。

## 可复用排查线索

- 症状：
  - Flow 编辑器选择方法后只剩 `Advanced JSON`
  - capability picker 中 route 已显示 schema 标记，但 inspector 仍判定 `missing_schema`
- 触发条件：
  - provider 没有注册 `InputSchema`
  - schema 使用了 Win resolver 不支持的特性
  - `cap_query` 没带 `include_schema=true`
- 关键词：
  - `missing_schema`
  - `include_schema`
  - `InputSchema`
  - `exec/capability`
- 快速检查：
  - `reg.Lookup(method, "")` 看 `Descriptor.InputSchema`
  - `exec` `cap_query` 响应是否携带 `routes[].input_schema`
  - Win `flow_schema_resolver.test.ts` 是否能解析同结构 schema

## 关键设计决策与权衡

- 采用“后端 registry 提供 schema，前端只保留必要 override”的方向，不再把这批普通方法继续写死在 Win 本地。
- `varstore::*` 本轮不迁移到后端 schema，避免把“schema 真相来源迁移”和“现有特殊 UI 语义对齐”两件事绑在一起。
- `payload` 使用 `type=object` 且 `properties={}` 的形式暴露为 JSON 控件，兼容 Win 当前 resolver 的 `json` 字段映射。

## 测试与验证方式 / 结果

- `go test ./...`
  - workdir: `D:\project\MyFlowHub3\worktrees\subproto-capability-input-schema\file`
  - 结果：通过
- `go test ./...`
  - workdir: `D:\project\MyFlowHub3\worktrees\subproto-capability-input-schema\topicbus`
  - 结果：通过
- `go test ./...`
  - workdir: `D:\project\MyFlowHub3\worktrees\subproto-capability-input-schema\exec`
  - 结果：通过
- 说明：
  - 验证时临时在 worktree 根生成 `go.work` 以指向本地模块和 `repo/MyFlowHub-Core`、`repo/MyFlowHub-Proto`，测试后已删除，不属于交付内容。

## 潜在影响与回滚方案

- 潜在影响：
  - Win ordinary mode 会开始为这批方法显示表单字段；若 schema 填错，用户会在 UI 中看到错误字段或 required 约束。
- 回滚方案：
  - 回退 `topicbus/topicbus.go` 和 `file/handler.go` 中新增的 `InputSchema`
  - 回退配套测试与 `go.sum` 变更

## 子Agent执行轨迹

- none
