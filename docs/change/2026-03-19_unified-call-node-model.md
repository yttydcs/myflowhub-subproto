# 2026-03-19 Flow 节点统一为 call（Exec 远程调用职责收敛）

## 变更背景 / 目标
在能力中心接入后，Flow 中 `local/exec` 双节点语义与协议职责存在重叠。为降低模型复杂度，本次将 Flow 节点写入语义统一为 `call`，并明确：
- `flow` 负责编排执行；
- `exec` 负责远程调用；
- 两者都可作为能力提供者与消费者。

## 具体变更内容

### 新增
- 新增 SubProto 文档：`docs/flow-exec-capability-contract.md`
  - 固化 Flow/Exec 职责边界。
  - 约定 `kind=call` 写入规范与执行语义。
  - 记录“写不兼容旧格式、读兼容旧数据”的迁移策略。

### 修改
- `flow/handler.go`
  - 新增统一 `callSpec` 解析路径。
  - `executeNode` 收敛为单一调用分发：
    - 本地调用：`target` 为空/0/本节点。
    - 远程调用：通过 `exec.call` 链路。
  - 运行期兼容历史节点：`local/exec` 仍可解释执行（仅用于读兼容）。
  - `validateGraph` 增加写入约束：节点 `kind` 必须为 `call`，且 `method` 必填。
- 测试用例同步更新：
  - `flow/graph_test.go`
  - `flow/local_capability_test.go`
  - `flow/capability_provider_test.go`
  - `flow/trigger_test.go`

### 删除
- 无。

## 对应 plan.md 任务映射
- UCN-1：统一 `call` 执行路径 → `flow/handler.go`
- UCN-2：写入校验 call-only → `flow/handler.go`、`flow/graph_test.go`
- UCN-3：旧数据读取执行兼容 → `flow/handler.go`、`flow/local_capability_test.go`
- UCN-4：测试补充 → `flow/*_test.go`
- UCN-5：文档归档 → 本文档 + `docs/flow-exec-capability-contract.md`

## 关键设计决策与权衡
- 采用“单一 `call` + 运行期旧格式解释”策略：
  - 优点：新写入模型统一，后续扩展点集中；历史数据不立即失效。
  - 代价：运行器中保留一段兼容解析分支，后续可在旧数据清理后移除。
- 写入阶段强约束 `kind=call`：
  - 优点：阻止旧模型继续扩散。
  - 影响：旧客户端若继续提交 `local/exec` 会收到 400（预期）。

## 性能与可扩展性
- 性能：
  - 本地调用仍为本地 map/registry 查找，未增加额外网络跳数。
  - 远程调用复用既有 `exec.call`，不新增额外聚合查询。
- 可扩展性：
  - 后续可在 `call.spec` 增加版本/选路字段，不再引入新节点 kind。

## 测试与验证方式 / 结果
- 执行命令（worktree 临时 go.work 环境）：
  - `go test ./... -count=1 -p 1`（目录：`flow/`）
- 结果：通过。

## 3.3 Code Review 结论
- 需求覆盖：通过（`call` 统一模型、写入 call-only、文档更新均已落地）。
- 架构合理性：通过（Flow 编排 / Exec 远程职责边界更清晰）。
- 性能风险：通过（未新增额外网络 hop 或全局扫描）。
- 可读性一致性：通过（执行分支收敛到单一路径，错误语义明确）。
- 可扩展性配置化：通过（后续扩展集中在 `call.spec`）。
- 稳定性与安全：通过（远程仍走 `exec` 权限链；旧数据仅运行期兼容）。
- 测试覆盖：通过（本地、远程、兼容、校验均有覆盖）。

## 潜在影响
- Win 或其他客户端若仍写入 `local/exec`，`flow.set` 将失败并返回 400。
- 历史落盘数据不受写入约束影响，仍可按兼容路径执行。

## 回滚方案
1. 回滚 `flow/handler.go` 的 `validateGraph` call-only 校验。
2. 回滚 `executeNode` 的统一 `call` 分发逻辑。
3. 回滚对应测试改动并恢复旧行为验证。
