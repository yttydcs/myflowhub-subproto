# 2026-03-22 Management Display Name And Persistence Hook

## 变更背景 / 目标

让 management 子协议同时具备两项能力：

- 对外暴露节点显示名，供 `node_info` / `list_*` 消费
- 在目标配置支持时，将 `config_set` 写入持久化层；不支持时继续保持原有运行期 `Set`

## 具体变更内容

- `management/action_config.go`
  - `config_set` 优先调用 `interface{ SetPersistent(string, string) error }`
  - 未实现持久化能力时回退到 `Set`
  - 持久化失败会返回失败的 `config_set_resp`
- `management/action_node_info.go`
  - `node_info.items["display_name"]` 从 effective config 的 `node.display_name` 读取
- `management/action_nodes.go`
  - `list_subtree` 的 self 节点回传 `display_name`
  - `list_nodes` / `list_subtree` 的 child 节点在连接元数据已携带名称时透传 `display_name`
- `management/types.go`
  - 本地兼容化 `NodeInfo` / `list*Resp`，避免等待 Proto workspace 切换才能落地
- 新增覆盖测试：
  - `action_config_test.go`
  - `action_node_info_test.go`
  - `test_helpers_test.go`
  - 扩充 `action_nodes_test.go` / `capability_provider_test.go`

## plan.md 任务映射

- `SUB1 - Management Display Name And Optional Persistence Hook`
- `SUB2 - Config Get Effective Value / Persistence Fallback Validation`

## 关键设计决策与权衡

- 使用 duck-typed `SetPersistent`，避免改动 Core `IConfig` 接口并影响所有实现
- `list_nodes` 不伪造 child 昵称；只有在连接元数据已有名称时才返回，避免把本节点名称错误复制到 child
- 在 Proto 未切入当前 workspace 时，本地保持兼容 wire struct，减小跨仓联调阻塞

## 需求 / 规范影响检查

- 控制面 requirement 已记录在 `D:\project\MyFlowHub3\docs\requirements\management-node-display-name.md`
- 控制面 spec 已记录在 `D:\project\MyFlowHub3\docs\specs\management-config-layering.md`
- 本仓未维护独立 `requirements/specs` 索引，本次无需新增 repo-local 真相文档
- 本仓 `docs/change/` 无独立索引 README，本次无需更新分类索引
- 无 lessons 沉淀新增

## 测试与验证方式 / 结果

为解决已发布 `exec v0.1.1` 缺少 `capability` 包的问题，测试时使用临时 `go.work` 指向当前 `management` 与本地 `../exec`：

```powershell
@'
go 1.25.0

use (
	.
	../exec
	../../../repo/MyFlowHub-Core
)
'@ | Set-Content go.work
$code = 0
try {
	go test ./... -count=1
	if ($LASTEXITCODE -ne 0) { $code = $LASTEXITCODE }
} finally {
	Remove-Item go.work -ErrorAction SilentlyContinue
	Remove-Item go.work.sum -ErrorAction SilentlyContinue
}
exit $code
```

结果：通过。

## 潜在影响与回滚方案

### 潜在影响

- 严格意义上，`list_nodes` 只有在 child 连接元数据已带名称时才会返回 `display_name`
- 若上层需要“所有 child 都稳定返回其自身 effective config 的昵称”，仍需后续补充名称传播或查询机制

### 回滚

- 回退 `management/action_config.go`、`management/action_node_info.go`、`management/action_nodes.go` 及对应测试

## 子 Agent 执行轨迹

- `SUB1` / `SUB2` -> `Descartes (019d15a7-b62b-7db1-9ba1-91fd125d87a2)` -> `D:\project\MyFlowHub3\worktrees\MyFlowHub-SubProto-feat-management-node-display-name`
  - 文件：`management/action_config.go`、`management/action_node_info.go`、`management/action_nodes.go`、`management/types.go`、`management/management.go` 与相关测试
  - 验收：management 模块测试通过；持久化回退逻辑与 `display_name` 回传逻辑已覆盖
