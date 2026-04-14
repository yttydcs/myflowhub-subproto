# 2026-04-05_flow-drop-legacy-compat

## 变更背景 / 目标

- 当前项目尚未上线，用户明确要求移除 `flow` 对旧数据格式的兼容，不再为历史 `local/exec` 节点保留运行或落盘加载路径。
- 本轮目标：
  - 删除 `flow` 运行期对旧 `local/exec` 节点的解释执行
  - 删除 `loadFlowsFromDisk()` 对旧格式 flow 定义的加载兼容
  - 同步把测试口径改成“旧格式必须拒绝或跳过”

## 具体变更内容

### 修改

- `flow/handler.go`
  - 删除 `legacyLocalSpec` / `legacyExecSpec`
  - `decodeNodeCallSpec(...)` 只接受 `kind=call`
  - `loadFlowsFromDisk()` 新增 `validateGraph(...)`，旧格式节点不会再被恢复进内存
- `flow/flow_id_test.go`
  - 将旧测试改为 `TestLoadFlowsFromDiskSkipsLegacyKinds`
  - 锁定旧格式落盘定义会在加载时被跳过
- `flow/local_capability_test.go`
  - 将旧测试改为 `TestFlowLegacyLocalNodeRejectedAtRuntime`
  - 锁定旧 `local` 节点执行期返回失败，而不是继续兼容运行

### 删除

- 无单独文件删除；删除的是 `handler.go` 内的旧节点兼容分支

## Requirements impact

- `none`

## Specs impact

- `none`

## Lessons impact

- `none`

## Related requirements

- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\requirements\flow_data_dag.md`

## Related specs

- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\flow.md`

## Related lessons

- `none`

## 对应 plan.md 任务映射

- `FLOW-FIX-1`
  - 删除 `flow/handler.go` 中的旧节点 decode 兼容
- `FLOW-FIX-2`
  - 收紧 `flow_id_test.go`、`local_capability_test.go` 的测试口径
- `FLOW-FIX-3`
  - 写入本归档并更新 `docs/change/README.md`

## 经验 / 教训摘要

- 如果项目尚未上线，不要为了“可能存在的旧数据”把运行时和加载路径长期保持双语义。
- 仅把写入校验收紧成 `call-only` 还不够；磁盘恢复路径也必须重跑 graph validation，否则旧格式仍会偷偷回流进内存。
- 对旧格式的拒绝需要同时在 set、load、execute 三条路径保持一致，否则很容易出现“保存拒绝但重启还能跑”的半兼容状态。

## 可复用排查线索

- 症状：
  - `flow.set` 已拒绝 `local/exec`，但旧 JSON 定义重启后仍被加载
  - runtime 看起来是 call-only，执行旧 `local` 节点却仍成功
- 触发条件：
  - `loadFlowsFromDisk()` 没有重跑 graph validation
  - `decodeNodeCallSpec(...)` 仍保留 `local/exec` 分支
- 关键词 / 错误文本：
  - `legacyLocalSpec`
  - `legacyExecSpec`
  - `decodeNodeCallSpec`
  - `TestLoadFlowsFromDiskSkipsLegacyKinds`
  - `TestFlowLegacyLocalNodeRejectedAtRuntime`
- 快速检查：
  1. 看 `decodeNodeCallSpec(...)` 是否只接受 `call`
  2. 看 `loadFlowsFromDisk()` 是否在 `validateTrigger` / `validateFlowRunConfig` 之后继续校验 `validateGraph(...)`
  3. 看测试是否仍有“legacy local 应成功”的断言

## 关键设计决策与权衡

- 直接删除旧格式兼容，而不是保留 feature flag
  - 好处：主线语义单一，后续不再被旧格式拖累
  - 代价：任何旧 `local/exec` 数据都不能再被恢复或执行
- 让加载路径和执行路径同时收紧
  - 好处：避免行为不一致
  - 代价：旧数据无法通过“重启后继续运行”的方式绕过当前校验

## 测试与验证方式 / 结果

- `D:\project\MyFlowHub3`
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-flow-drop-legacy\go.work go test github.com/yttydcs/myflowhub-subproto/flow/... -count=1 -p 1`
  - 结果：通过
- `D:\project\MyFlowHub3`
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-flow-drop-legacy\go.work go test github.com/yttydcs/myflowhub-subproto/flow/... -run 'TestLoadFlowsFromDiskSkipsLegacyKinds|TestFlowLegacyLocalNodeRejectedAtRuntime|TestValidateGraphRejectsLegacyKind' -count=1 -p 1 -v`
  - 结果：通过

## 潜在影响

- 旧 `local/exec` flow 定义不再被视为可恢复数据。
- 旧 `local` 节点即使绕过保存路径进入运行时，也会被明确拒绝。
- 主线 `flow` 节点模型现在真正收口为 `call/compose/set_var` 以及已支持的新节点类型，不再保留历史回读兼容。

## 回滚方案

1. 回退 `flow/handler.go`
2. 回退 `flow/flow_id_test.go`、`flow/local_capability_test.go`
3. 删除本归档并恢复 `docs/change/README.md` 索引

## 子Agent执行轨迹

- 本轮未使用子Agent
