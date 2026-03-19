# 变更背景 / 目标

- 背景：`exec` 已具备逐级 capability 聚合（`cap_snapshot/cap_query`），但本地能力来源仍偏 `exec` 内置方法，且 `exec.call` 未强制执行 capability-level 权限。
- 目标：
  1) 在 `exec` 域下提供可复用的通用能力注册中心（供其他子协议自注册）；
  2) 在 `exec.call` 执行路径补齐能力级权限校验，并支持“同节点自动放行”；
  3) 让 `flow` 成为能力提供者，暴露调用指定 flow 的公开能力；
  4) 继续按“全开”补齐 `varstore/topicbus/file/management` 四个能力提供者；
  5) 让 `flow` 的 `local` 节点可直接调用通用能力中心（A 方案，不经 `exec(target=self)` 桥接）。

# 具体变更内容

## 新增

- 新增通用能力注册子包：`exec/capability`
  - `Descriptor`（provider/method/version/schema/permissions/tags）
  - `Registry`（注册、查找、枚举、共享实例）
  - 冲突策略：同 `method+version` 若 provider/描述不同，`fail-fast` 拒绝注册
  - 文件：
    - `exec/capability/registry.go`
    - `exec/capability/registry_test.go`

- 新增测试：
  - `exec/capability_permission_test.go`
  - `flow/capability_provider_test.go`
  - `varstore/capability_provider_test.go`
  - `topicbus/capability_provider_test.go`
  - `file/capability_provider_test.go`
  - `management/capability_provider_test.go`

## 修改

- `exec` handler 接入能力中心并统一本地能力来源：
  - `RegisterMethod` 改为注册到 capability registry；
  - `cap_query` / 上行聚合前刷新本地能力快照（来自 registry）；
  - `exec.call` 本地执行改为通过 registry 查找并调用；
  - 新增能力级权限校验（descriptor.permissions）；
  - 同节点自动放行能力权限（`caller_node == provider_node`）；
  - 新增开关读取：`exec.cap.permission.self_bypass`（默认 `true`）。
  - 文件：`exec/handler.go`

- `flow` 作为 capability provider：
  - 注册 `flow::run`；
  - 新增 capability 调用入口：参数 `{"flow_id":"..."}`，返回 `{"flow_id":"...","run_id":"..."}`；
  - 复用 run 入队逻辑，支持 capability 触发执行。
  - `local` 节点执行新增 fallback：若 `localMethods` 未命中，则从 capability registry 查找并调用。
  - 文件：`flow/handler.go`、`flow/go.mod`

- `varstore` 作为 capability provider：
  - 注册 `varstore::set/get/revoke`；
  - 能力作用于当前节点本地 var 数据，并复用 flow trigger 事件发布。
  - 文件：`varstore/varstore.go`、`varstore/go.mod`

- `topicbus` 作为 capability provider：
  - 注册 `topicbus::publish`；
  - 复用 topicbus 本地分发 + 上行转发链路。
  - 文件：`topicbus/topicbus.go`、`topicbus/go.mod`

- `file` 作为 capability provider：
  - 注册 `file::list/read_text/mkdir`；
  - 复用既有路径清洗与本地文件安全约束。
  - 文件：`file/handler.go`、`file/go.mod`

- `management` 作为 capability provider：
  - 注册 `management::list_nodes/node_info`；
  - 通过 `BindServer` 绑定 runtime config 后共享到能力中心。
  - 文件：`management/management.go`、`management/go.mod`

# 对应计划任务映射

- `CAPREG-1` → `exec/capability/registry.go`、`exec/capability/registry_test.go`
- `CAPREG-2` → `exec/handler.go`、`exec/capability_permission_test.go`
- `CAPREG-3` → `flow/handler.go`、`flow/go.mod`、`flow/capability_provider_test.go`
- `CAPREG-6` → `varstore/varstore.go`、`varstore/go.mod`、`varstore/capability_provider_test.go`
- `CAPREG-7` → `topicbus/topicbus.go`、`topicbus/go.mod`、`topicbus/capability_provider_test.go`
- `CAPREG-8` → `file/handler.go`、`file/go.mod`、`file/capability_provider_test.go`
- `CAPREG-9` → `management/management.go`、`management/go.mod`、`management/capability_provider_test.go`
- `CAPREG-10` → `flow/handler.go`、`flow/local_capability_test.go`
- `CAPREG-4` → 本文档 “Code Review（3.3）” 小节
- `CAPREG-5` → 本归档文档

# 关键设计决策与权衡

- 决策：能力中心先挂在 `exec` 域（`exec/capability` 子包），不改 wire 协议。
  - 权衡：实现范围可控，兼容现有 `exec.cap_*` 逐级同步链路；后续可再向更底层模块外提。

- 决策：冲突策略使用 fail-fast（同 key 拒绝覆盖）。
  - 权衡：牺牲“最后写入 wins”的灵活性，换取配置/能力声明可审计与可预期性。

- 决策：能力权限采用“双层校验”中的第二层（协议权限保持既有，能力权限新增）。
  - 权衡：调用安全更细粒度；并通过“同节点自动放行”避免本地编排场景被权限噪音阻断。

- 决策：provider 自注册并共享同一 scope（同一 `cfg` 指针）。
  - 权衡：实现简单、接入快；跨模块需新增 `exec/capability` 依赖，发布版本需后续统一对齐。

- 性能关键点：
  - 本地能力列表通过内存 registry 快照刷新，不引入额外 I/O；
  - 权限校验为常量级 map/list 判定，热路径开销可控；
  - provider 调用均在本地节点内执行，不增加跨节点 hop。

# 测试与验证方式 / 结果

- 说明：联测使用 workflow 局部 `go.work`（临时创建并测试后删除），把 `broker/exec/flow/varstore/topicbus/file/management` 与本地 `Core/Proto` 绑定，避免版本缓存干扰。

- 通过：
  - `exec`：`go test ./...`
  - `flow`：`go test ./...`
  - `varstore`：`go test ./...`
  - `topicbus`：`go test ./...`
  - `file`：`go test ./...`
  - `management`：`go test ./...`
  - `broker`：`go test ./...`

- 新增测试：
  - `exec/capability/registry_test.go`
    - `TestRegistryRegisterConflict`
    - `TestRegistryRegisterSameProviderIdempotent`
    - `TestSharedRegistryUsesPointerScope`
  - `exec/capability_permission_test.go`
    - `TestExecCall_EnforcesCapabilityPermission`
    - `TestExecCall_SameNodeBypassesCapabilityPermission`
  - `flow/capability_provider_test.go`
    - `TestFlowRegistersCapabilityRun`
    - `TestFlowCapabilityRunValidatesArgs`
  - `flow/local_capability_test.go`
    - `TestFlowLocalNodeFallsBackToCapabilityRegistry`
    - `TestFlowLocalMethodTakesPrecedenceOverCapabilityRegistry`
  - `varstore/capability_provider_test.go`
    - `TestVarStoreCapabilitySetGetRevoke`
  - `topicbus/capability_provider_test.go`
    - `TestTopicBusPublishCapability`
  - `file/capability_provider_test.go`
    - `TestFileCapabilitiesListReadTextMkdir`
  - `management/capability_provider_test.go`
    - `TestManagementCapabilitiesListNodesNodeInfo`

# Code Review（3.3）

- 需求覆盖：通过
  - 已覆盖通用能力中心、能力级权限、同节点放行，以及 `flow/varstore/topicbus/file/management` provider，并补齐 `flow.local` 直连能力中心。

- 架构合理性：通过
  - 仍保持 `exec` 作为调用控制面；provider 自注册进入共享能力中心，不破坏现有路由协议。

- 性能风险：通过
  - 未引入额外网络往返；本地路径仅内存查询与权限判断，provider 调用复用现有本地逻辑。

- 可读性与一致性：通过
  - 能力定义集中到各 handler 的 `registerCapabilities + invokeCapability*`，测试按模块就近覆盖。

- 可扩展性与配置化：通过
  - 后续协议可沿同模式注册；同节点放行支持配置键覆盖。

- 稳定性与安全：通过
  - 冲突 fail-fast；跨节点仍需 capability permission；同节点仅绕过能力层，不绕过协议层。

- 测试覆盖：通过
  - 已补 capability 冲突、权限 deny/bypass、多协议 provider 基础能力调用。

# 潜在影响与回滚方案

- 潜在影响：
  - `flow/varstore/topicbus/file/management` 新增对 `exec` 模块（`exec/capability`）的编译期依赖；
  - 在不开启 workspace 且未升级对应依赖版本时，离线编译可能需要后续版本对齐。

- 回滚方案：
  1) 回滚 `exec/handler.go` 中 capability registry 接入与能力权限逻辑；
  2) 删除 `exec/capability` 子包及相关测试；
  3) 回滚 `flow/varstore/topicbus/file/management` 的 provider 注册与 `go.mod` 新依赖；
  4) 重新执行相关模块单测确认回退稳定。
