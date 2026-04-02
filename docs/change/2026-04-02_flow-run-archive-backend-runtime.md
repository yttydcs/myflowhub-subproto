# 2026-04-02_flow-run-archive-backend-runtime

## 变更背景 / 目标

- retained run archive 一期已经支持 local JSON sidecar，但后端仍写死在 `flow/run_archive.go`。
- 本轮目标是把 archive 升级成独立可插拔 backend，为 `Server` 可选注入 PG 留出稳定接口，同时保持未配置 PG 时继续按当前路径正常运行。

## 具体变更内容

### 修改

- `flow/config.go`
  - 新增 `flow.run_archive.backend = off | file | pg`
  - 保留 `flow.run_archive_enabled` 兼容，映射为 `file`
  - 非法 backend 显式报错
- `flow/handler.go`
  - `HandlerOptions` 新增 `RunArchiveStore`
  - `Init()` 在 `backend=pg` 且未注入 store 时 fail-fast
  - archive 相关逻辑改为统一走 `currentRunArchiveStoreLocked()`
  - 保留“手工把 `h.runArchive=true` 打开时默认走 file backend”的测试兼容路径
- `flow/run_archive.go`
  - 新增 `RunArchiveStore`
  - 新增 `FileRunArchiveStore`
  - `persist/load/prune` 改为基于 store 的 `LoadAll/Save/Delete`
  - 归档记录新增内部排序键 `archived_at_ns`，避免 reload 后在同毫秒终态 run 上退化到 `run_id` 排序
- `flow/config_test.go`
  - 覆盖默认值、legacy bool、backend override、`pg` 和非法值
- `flow/run_archive_store_test.go`
  - 覆盖 injected archive store 的 `load/save/delete` 与 `pg` 缺 store 的失败路径
- `flow/runtime_fix_test.go`
  - archive preload helper 改为 backend-agnostic `loadArchivedRuns()`

### 删除

- 无

## Requirements impact

- `updated`
  - `D:\project\MyFlowHub3\worktrees\server-run-archive-backend\docs\requirements\flow_data_dag.md`

## Specs impact

- `updated`
  - `D:\project\MyFlowHub3\worktrees\server-run-archive-backend\docs\specs\flow.md`

## Lessons impact

- `none`
  - 本轮主要是既有 archive 路径的抽象化与可选 PG 接口扩展，没有新增独立 lesson 文档

## Related requirements

- `D:\project\MyFlowHub3\worktrees\server-run-archive-backend\docs\requirements\flow_data_dag.md`

## Related specs

- `D:\project\MyFlowHub3\worktrees\server-run-archive-backend\docs\specs\flow.md`

## Related lessons

- `none`

## 对应 plan.md 任务映射

- `RA-SUB-1`
  - `flow/config.go`
  - `flow/handler.go`
- `RA-SUB-2`
  - `flow/run_archive.go`
  - `flow/config_test.go`
  - `flow/run_archive_store_test.go`
  - `flow/runtime_fix_test.go`
- `RA-VER-1`
  - cross-repo validation

## 经验 / 教训摘要

- run archive backend 最稳的切口仍然是“保留查询面完全不变，只替换 preload/save/delete 的存储后端”。
- legacy bool 兼容不能只看配置文件；测试里常有手工把 `h.runArchive=true` 直接打开的路径，也要保留默认 file fallback。
- retained run reload 若只按 `start/end/run_id` 排序，在同毫秒终态场景会出现顺序漂移；需要一个仅归档内部使用的稳定排序键。

## 可复用排查线索

- 症状：
  - `flow.run_archive.backend=pg` 时 handler 初始化直接失败
  - archive reload 后 recent run 顺序与执行顺序不一致
  - legacy `flow.run_archive_enabled=true` 不再落本地 `_runs`
- 触发条件：
  - `backend=pg` 但 `Server` 没有注入 archive store
  - retained runs 在同一毫秒内终态，reload 排序退化到 `run_id`
  - backend 兼容层只处理新 key，没有覆盖 legacy bool / 手工启用路径
- 关键词 / 错误文本：
  - `flow.run_archive.backend`
  - `flow run archive backend requires injected store`
  - `RunArchiveStore`
  - `archived_at_ns`
- 快速检查：
  1. 看 `flow/config.go` 是否同时支持 `flow.run_archive.backend` 和 `flow.run_archive_enabled`
  2. 看 `flow/handler.go` 是否通过 `currentRunArchiveStoreLocked()` 统一选择 backend
  3. 看 `flow/run_archive.go` 是否仍保持 `archive -> prune` 顺序
  4. 看 `flow/run_archive_store_test.go` 是否覆盖 injected store 和 pg-no-store fail-fast

## 关键设计决策与权衡

- 继续把 archive 与 definition persistence 分离
  - 好处：职责边界清晰，不把 retained run 与 flow 定义耦合到同一个接口
  - 代价：`Server` 需要额外装配一个 archive store
- `backend=pg` 时要求 injected store 显式存在
  - 好处：错误配置会立即暴露，不会误退化到 file/off
  - 代价：直接裸用 `SubProto` handler 时若显式写了 `pg` 配置，必须配套 `Server` 或测试注入
- 新增 `archived_at_ns`
  - 好处：reload 后 retained run 顺序稳定
  - 代价：archive record 新增一个内部字段，但不影响协议面

## 测试与验证方式 / 结果

- `D:\project\MyFlowHub3`
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-run-archive-backend\go.work go test github.com/yttydcs/myflowhub-subproto/flow -count=1 -p 1`
- 结果：通过

## 潜在影响

- 未配置 PG 的环境仍可继续使用 `off` 或 `file`，默认行为不变。
- archive record 新增内部排序字段 `archived_at_ns`；旧 file archive 无此字段时仍可 fallback。
- 显式配置 `backend=pg` 但未装配 PG store 的环境会在 init 时直接失败，而不是默默退回 file/off。

## 回滚方案

1. 回退 `flow/config.go`
2. 回退 `flow/handler.go`
3. 回退 `flow/run_archive.go`
4. 回退 `flow/config_test.go` / `flow/run_archive_store_test.go` / `flow/runtime_fix_test.go`
5. 恢复“archive 固定为 local JSON sidecar + legacy bool”的实现

## 子Agent执行轨迹

- 本轮未使用子Agent
