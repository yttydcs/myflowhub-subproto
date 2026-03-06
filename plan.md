# Plan - MyFlowHub-SubProto：VarStore 逐跳可见与并发匹配对齐

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 分支：`refactor/varstore-hop-align`
- Worktree：`d:\project\MyFlowHub3\worktrees\varstore-hop-align\subproto`
- Base：`main`
- 关联仓库：`MyFlowHub-SDK`、`MyFlowHub-Win`、`MyFlowHub-Server`

## 项目目标与当前状态
- 目标：按已确认规范完成 VarStore 行为对齐，覆盖逐跳回程可见、并发写一一对应、notify 下行“转发+本地处理”、SourceID 端到端保留。
- 当前状态：已完成阶段 1/2 文档（`varstore_requirements.md`、`varstore_architecture.md`）；本仓已完成 VS-1~VS-6 核心改造与关键单测（VS-7 部分），待联动 SDK/Win 与归档。

## 依赖关系
- 先做本仓核心改造（本计划 VS-1~VS-7），再联动 SDK/Win await 兼容验证。
- Server 文档同步可并行，但最终验收需以本仓行为为准。

## 风险与注意事项
- `MajorCmd` 回程会提高 handler 处理频次，需谨慎处理 pending 生命周期，避免泄漏。
- 并发写入映射表若缺少超时/异常清理，容易造成状态堆积。
- 不得破坏 wire（action/data schema/SubProto 不变）。

## 可执行任务清单（Checklist）

### VS-1 统一响应逐跳可见（`MajorCmd`）
- 目标：`*_resp` 与 `assist_*_resp` 改为逐跳可见，不再依赖 `MajorOKResp` 快速转发。
- 涉及模块/文件：`varstore/varstore.go`
- 验收条件：多 hop 场景中间节点能进入 `handle*Resp`；pending 可逐跳出队。
- 测试点：构造两跳链路请求，验证中间节点收到并处理 resp。
- 回滚点：恢复 `buildRespHeader` 的旧 major 策略。

### VS-2 SourceID 端到端保留
- 目标：`assist_* / up_* / notify_* / *_resp` 转发不改写原始 actor SourceID。
- 涉及模块/文件：`varstore/varstore.go`（`forward`/resp 构建相关）
- 验收条件：跨 hop 抓包或日志可见 SourceID 恒等于原请求发起方。
- 测试点：转发链路断言 SourceID 不变。
- 回滚点：恢复旧 forward 源 ID 赋值逻辑。

### VS-3 写操作并发一一对应（策略 C）
- 目标：`set/revoke` 使用“上行 msg_id + 映射表”，回程按请求粒度匹配并恢复下游 msg_id。
- 涉及模块/文件：`varstore/types.go`、`varstore/varstore.go`
- 验收条件：同 `(owner,name)` 并发两次写入时，不错配、不吞响应。
- 测试点：并发 set/revoke 的映射命中、超时清理、断连清理。
- 回滚点：恢复旧 pendingKey 方案（仅 owner/name/kind）。

### VS-4 查询去重扇出与沿途缓存
- 目标：`get/list/subscribe` 保持去重上送，一次回程可扇出全部等待者；成功响应按规则沿途更新缓存。
- 涉及模块/文件：`varstore/varstore.go`
- 验收条件：同 key 并发查询仅上送一次 assist，所有等待者都收到正确响应。
- 测试点：去重命中、扇出数量、缓存写入内容完整性。
- 回滚点：恢复旧 pending 处理逻辑。

### VS-5 notify 下行“转发 + 本地处理”
- 目标：`notify_set/notify_revoke` 在 `TargetID!=local` 场景仍执行本地缓存/订阅推送，再继续转发。
- 涉及模块/文件：`varstore/varstore.go`
- 验收条件：中间节点收到 notify 时，本地缓存变更与下游推送均生效。
- 测试点：notify 多 hop 链路中间节点状态变化。
- 回滚点：恢复旧“转发即 return”路径。

### VS-6 规则收敛：`set_resp/list/subscriber/private`
- 目标：落实已确认规则：
  - `set_resp code=1` 必带 `value`，失败不更新缓存；
  - owner 自查 list 包含 private，空集合 `code=1 names=[]`；
  - `set.value` 禁止空串/纯空白；
  - `subscriber` 只允许 0 或等于 SourceID；
  - private 允许权限例外。
- 涉及模块/文件：`varstore/varstore.go`
- 验收条件：规则对应分支全部可触发且返回码正确。
- 测试点：每条规则至少 1 个正例 + 1 个反例。
- 回滚点：按规则维度逐项回退。

### VS-7 测试与归档
- 目标：补齐关键单测并形成本仓变更归档。
- 涉及模块/文件：`varstore/*_test.go`、`docs/change/2026-03-06_varstore-hop-align-subproto.md`
- 验收条件：`go test ./... -count=1 -p 1` 通过；归档文档完整映射 VS-1~VS-6。
- 测试点：优先跑 `varstore` module，再跑仓库全量。
- 回滚点：测试与文档改动可独立回退，不影响功能提交。
