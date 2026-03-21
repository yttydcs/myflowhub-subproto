# 2026-03-22 Flow 状态一致性、转发失败响应与 run 保留修复

## 变更背景 / 目标
- 修复 `flow.set/delete` 在文件系统失败场景下的“响应失败但运行态已变更”问题，避免内存态与磁盘态分叉。
- 修复 `run/status/list/get` 的远端转发失败静默丢包问题，保证调用方能收到明确 `*_resp`。
- 为 `runs` 增加按 flow 的索引与定长回收策略，避免长期运行后内存增长与 `status/list` 全表扫描退化。

## 具体变更内容
### 修改
- `flow/config.go`
  - 新增内部配置读取：`flow.max_retained_runs`。
  - 默认保留每个 flow 最近 `32` 个已结束 run。
- `flow/handler.go`
  - `applySetLocal` 改为先持久化成功，再提交 `h.flows` 与 scheduler。
  - `applyDeleteLocal` 改为先删除持久化文件成功，再提交 `h.flows`、scheduler 与 run 取消。
  - 新增 `writeFileAtomic(...)`，用临时文件 + 备份恢复降低覆盖写风险。
  - `run/status/list/get` 在远端无路由、无父节点、无效 route、`hop_limit` 超限、发送失败时返回明确失败响应。
  - 为 `runs` 增加 `runOrderByFlow` 索引。
  - `handleStatus` / `handleList` / `cancelRunsLocked` / `tryStartRun` 改为基于 flow 索引操作，不再全表扫描 `runs`。
  - 新增终态 run 回收逻辑：保留最近有限数量的终态 run，始终保留 `queued/running` run。
- `flow/runtime_fix_test.go`
  - 新增一致性失败、远端失败响应、run 保留与最新 run 查询测试。

### 新增
- `docs/change/2026-03-22_flow-state-route-retention.md`（本文）

### 删除
- 无。

## 对应 `plan.md` 任务映射
- `FLOW-FIX-1` → `flow/handler.go`：`set/delete` 持久化与状态提交顺序修复。
- `FLOW-FIX-2` → `flow/handler.go`：`run/status/list/get` 远端失败显式响应。
- `FLOW-FIX-3` → `flow/config.go`, `flow/handler.go`：run 索引与回收策略。
- `FLOW-FIX-4` → `flow/runtime_fix_test.go`：新增关键回归测试。
- `FLOW-FIX-5` → `plan.md`, `docs/change/2026-03-22_flow-state-route-retention.md`：审查与归档。

## 关键设计决策与权衡
- 持久化优先于运行态提交：
  - 目标是修复“失败后分叉状态”；因此优先保证“失败不生效”。
  - `set` 写盘成功后再切内存；`delete` 删盘成功后再删内存与取消运行。
- 原子写采用最小可控方案：
  - 使用“同目录临时文件 + 旧文件备份恢复”。
  - 不引入更重的 WAL/事务文件，避免超出本轮修复范围。
- run 回收按“每个 flow 的终态历史上限”而不是“全局总量”：
  - 这样可以保持查询局部性，更适合 `status/list` 的访问模式。
  - `queued/running` run 永不因回收被提前删除，避免破坏运行中查询。
- 远端失败响应不改 wire：
  - 继续沿用原有 `*_resp` action 和 `MajorOKResp`。
  - 只补失败分支，不重写既有路由模型。

## 测试与验证方式 / 结果
- 目录：`D:\project\MyFlowHub3\worktrees\subproto-flow-state-route-retention\flow`
- 由于本地 worktree 依赖未发布的 `exec/broker/proto/core` 组合，回归使用临时 `go.test.mod` 做本地 replace：
  - `github.com/yttydcs/myflowhub-subproto/exec => ../exec`
  - `github.com/yttydcs/myflowhub-subproto/broker => ../broker`
  - `github.com/yttydcs/myflowhub-proto => ../../../repo/MyFlowHub-Proto`
  - `github.com/yttydcs/myflowhub-core => ../../../repo/MyFlowHub-Core`
- 命令：
  - `GOWORK=off go test -mod=mod -modfile go.test.mod ./... -count=1`
- 结果：
  - 通过（`ok github.com/yttydcs/myflowhub-subproto/flow`）

## 潜在影响与回滚方案
- 潜在影响：
  - `flow.max_retained_runs` 默认值为 `32`；若调用方依赖更长的 run 历史，需要显式调大。
  - 删除 flow 成功后，历史 run 仍会保留在内存直到被回收；这保持了 `status(run_id)` 的短期可追踪性，但不是永久历史存档。
- 回滚方案：
  1. 回退 `flow/config.go` 中的 run 保留配置读取。
  2. 回退 `flow/handler.go` 中的持久化顺序、远端失败响应和 run 索引改动。
  3. 回退 `flow/runtime_fix_test.go`。

## 子Agent执行轨迹
- 本轮未使用子Agent。
- 原因：
  - 核心改动集中在 `flow/handler.go` 的共享写集上。
  - 状态提交、路由错误语义、run 索引存在强耦合，不适合并行拆分。
- Task ID → Agent → Worktree → 文件 → 验收结果
  - `FLOW-FIX-1` → 主Agent → `D:\project\MyFlowHub3\worktrees\subproto-flow-state-route-retention` → `flow/handler.go` → 通过
  - `FLOW-FIX-2` → 主Agent → `D:\project\MyFlowHub3\worktrees\subproto-flow-state-route-retention` → `flow/handler.go` → 通过
  - `FLOW-FIX-3` → 主Agent → `D:\project\MyFlowHub3\worktrees\subproto-flow-state-route-retention` → `flow/config.go`, `flow/handler.go` → 通过
  - `FLOW-FIX-4` → 主Agent → `D:\project\MyFlowHub3\worktrees\subproto-flow-state-route-retention` → `flow/runtime_fix_test.go` → 通过
  - `FLOW-FIX-5` → 主Agent → `D:\project\MyFlowHub3\worktrees\subproto-flow-state-route-retention` → `plan.md`, `docs/change/2026-03-22_flow-state-route-retention.md` → 通过
