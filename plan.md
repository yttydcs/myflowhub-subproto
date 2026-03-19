# Plan - SubProto：通用能力注册中心（挂在 exec）与 flow 能力提供者

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 分支：`feat/capability-registry-ext`
- Worktree：`d:\project\MyFlowHub3\repo\MyFlowHub-SubProto\repo\MyFlowHub-SubProto\worktrees\feat-capability-registry-ext`
- Base：`main`

## 项目目标与当前状态
- 目标：
  - 在 `exec` 域下提供“可被多子协议复用”的能力注册中心（非 flow 专用）。
  - 补齐 `exec.call` 的能力级权限校验，并支持“同节点自动放行”。
  - 让 `flow` 成为能力提供者，提供“调用指定 flow”的公开能力。
- 当前状态：
  - `exec` 已有逐级 `cap_snapshot/cap_query` 聚合机制，但本地能力来源主要是 `exec.RegisterMethod`。
  - `CapabilityDescriptor.permissions` 已可上报/查询，`exec.call` 尚未对其做强制校验。
  - `flow` 当前仅支持 DAG 本地 `local/exec` 节点执行，不作为公开能力提供者。

## 范围
- 必须：
  - 增加通用能力注册中心（冲突策略 fail-fast，key=`method+version`，大小写敏感）。
  - `exec` 改为从能力中心读取本地能力并执行本地调用。
  - 新增能力级权限校验：
    - 先过协议权限（既有）；
    - 再过能力权限（descriptor.permissions）；
    - 同节点 `caller_node == provider_node` 自动放行能力权限。
  - `flow` 注册至少一个公开能力（调用指定 flow）。
- 可选：
  - 为更多子协议补充 provider 注册（本轮若不引入高风险改动可后续增量）。
- 不做：
  - 不改 wire 协议结构；
  - 不引入自动选路替代显式 target（`exec` 仍严格指定目标节点）。

## 验收标准
- `exec`：
  - 本地 capability 能力来源来自通用注册中心；
  - 能力权限在 `exec.call` 生效；
  - 同节点能力调用在 capability permission 层自动放行。
- `flow`：
  - 通过能力中心可发现 `flow` 提供的方法；
  - 通过 `exec.call` 在目标节点执行该能力可触发对应 flow 运行。
- 测试：
  - `exec`、`flow`、`broker` 相关单测通过。

## 3.1) 计划拆分（Checklist）

### CAPREG-1 - 新增通用能力注册中心
- 目标：在 `exec/capability` 子包实现 registry（注册/查询/冲突检测）。
- 涉及文件：
  - `exec/capability/registry.go`
  - `exec/capability/registry_test.go`
- 验收条件：
  - 同 key 冲突可拒绝；
  - 同 provider 同描述重复注册幂等。
- 回滚点：revert 本任务提交。

### CAPREG-2 - exec 接入能力中心并补齐能力权限
- 目标：
  - `exec` 本地方法执行改为基于 registry；
  - `cap_query`/上行聚合读取 registry 本地能力；
  - 增加 capability permission 校验与同节点自动放行。
- 涉及文件：
  - `exec/handler.go`
- 验收条件：
  - `exec.call` 在目标节点本地执行时生效能力权限；
  - 同节点自动放行能力权限（协议权限仍生效）。
- 测试点：
  - 新增/更新 `exec` 单测覆盖 allow/deny/bypass 场景。
- 回滚点：revert 本任务提交。

### CAPREG-3 - flow 作为能力提供者（调用指定 flow）
- 目标：`flow` 注册公开能力并实现本地运行入口。
- 涉及文件：
  - `flow/handler.go`
  - `flow/go.mod`（如需新增 `exec` 模块依赖）
- 验收条件：
  - capability 列表可见 flow 能力；
  - 调用后返回 run_id 并可在 status 查询。
- 测试点：
  - 新增 `flow` 单测覆盖 capability 调用成功/参数校验。
- 回滚点：revert 本任务提交。

### CAPREG-4 - Code Review（强制）
- 逐项审查：需求覆盖/架构/性能风险/可读性/扩展性/稳定性与安全/测试覆盖。

### CAPREG-5 - 归档变更（强制）
- 输出：`docs/change/2026-03-19_exec-capability-registry-ext-flow-provider.md`

## 追加迭代（Round-2：按“全开”补齐 provider）

### 追加背景与回退原因
- 在 CAPREG-1~5 基础上，用户追加要求“provider 全开”，即除 `flow` 外继续补齐 `varstore/topicbus/file/management` 的能力注册。
- 该范围超出原 CAPREG-3（仅 flow provider），按规则回到 3.1 增补计划任务后再编码。

### CAPREG-6 - varstore 能力提供者
- 目标：注册 `varstore::set/get/revoke` 本地能力，能力执行作用于当前节点本地 var 数据。
- 涉及文件：
  - `varstore/varstore.go`
  - `varstore/go.mod`
  - `varstore/capability_provider_test.go`
- 验收条件：
  - 能力可被 registry 查询到；
  - set/get/revoke 基础链路可用（参数校验、not found）。

### CAPREG-7 - topicbus 能力提供者
- 目标：注册 `topicbus::publish` 能力，支持发布事件并复用既有 topicbus 分发链路。
- 涉及文件：
  - `topicbus/topicbus.go`
  - `topicbus/go.mod`
  - `topicbus/capability_provider_test.go`
- 验收条件：
  - 能力可查询；
  - 发布参数校验与成功返回可用。

### CAPREG-8 - file 能力提供者
- 目标：注册 `file::list/read_text/mkdir` 本地能力，复用现有路径安全策略。
- 涉及文件：
  - `file/handler.go`
  - `file/go.mod`
  - `file/capability_provider_test.go`
- 验收条件：
  - 能力可查询；
  - list/read_text/mkdir 基础链路可用。

### CAPREG-9 - management 能力提供者
- 目标：注册 `management::list_nodes/node_info` 能力，支持从 server context 查询本机管理信息。
- 涉及文件：
  - `management/management.go`
  - `management/go.mod`
  - `management/capability_provider_test.go`
- 验收条件：
  - 能力可查询；
  - 在有 server context 时返回节点与信息数据。

## 追加迭代（Round-3：flow local 直连能力中心）

### 追加背景与回退原因
- 在 CAPREG-1~9 基础上，用户追加要求：`flow` 的 `local` 节点也可调用通用能力中心（方案 A：直接调用，不经 `exec(target=self)` 桥接）。
- 该范围超出原 Round-2（仅 provider 注册与 exec 调用路径），按规则回到 3.1 增补计划任务后再编码。

### CAPREG-10 - flow local 支持能力中心调用（A 方案）
- 目标：
  - `local` 节点执行时，保留现有 `localMethods` 语义，同时支持从 capability registry 查找并调用方法。
- 涉及文件：
  - `flow/handler.go`
  - `flow/capability_provider_test.go`（或新增邻近测试文件）
- 验收条件：
  - `local` 可调用 `varstore/topicbus/file/management/flow` 已注册能力；
  - 历史 `debug::echo/debug::fail` 行为保持兼容；
  - 未找到方法时仍返回明确错误。
- 测试点：
  - 使用同一 cfg 作用域初始化 provider + flow，验证 `local` 节点调用外部能力成功；
  - 验证 `localMethods` 优先级与回退路径。

### CAPREG-11 - Round-3 Code Review 与归档补充
- 目标：
  - 对 Round-3 变更追加 review 结论，并补充到本次变更归档文档。
- 涉及文件：
  - `docs/change/2026-03-19_exec-capability-registry-ext-flow-provider.md`
- 验收条件：
  - 文档包含 CAPREG-10 的任务映射、设计权衡、测试结果、回滚点。
