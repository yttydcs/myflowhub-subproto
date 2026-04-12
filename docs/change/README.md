# Change Archive

## Purpose

存放已完成 workflow 的变更结果、验证方式和回滚说明。

## How To Enter This Section

- 实现完成并通过 Code Review 后进入这里
- 写入 change 前先完成 requirement/spec impact 检查

## What Belongs Here

- 变更背景与目标
- 具体改动
- 验证结果
- 回滚方案

## Naming / Maintenance Rules

- 使用 `YYYY-MM-DD_topic.md`
- 新增叶子文档后更新本索引

## Current Docs

- 最新归档：
  - [2026-04-05_flow-drop-legacy-compat.md](2026-04-05_flow-drop-legacy-compat.md)
  - flow 旧格式兼容下线：删除 `local/exec` 运行与落盘恢复兼容，主线改为严格拒绝旧节点。
  - [2026-04-03_flow-transform-node-runtime.md](2026-04-03_flow-transform-node-runtime.md)
  - `transform` 首版运行时：新增结构化表达式树纯计算节点，支持白名单运算、可选 source 缺失兜底和 `foreach` 内使用。
  - [2026-04-02_flow-run-archive-backend-runtime.md](2026-04-02_flow-run-archive-backend-runtime.md)
  - run archive backend：将 retained archive 抽象为独立 store，默认继续支持 file，`Server` 可选注入 PG。
  - [2026-04-02_flow-run-archive-runtime.md](2026-04-02_flow-run-archive-runtime.md)
  - run archive 首版：为 retained window 内的终态 run 增加 local JSON archive，支持重启后继续查询 recent run。
  - [2026-04-02_flow-trigger-dedup-runtime.md](2026-04-02_flow-trigger-dedup-runtime.md)
  - trigger dedup 首版：新增 `dedup_window_ms` 窗口去重，抑制 `event/var_changed` 的短窗口重复启动。
  - [2026-04-02_flow-active-run-limit-runtime.md](2026-04-02_flow-active-run-limit-runtime.md)
  - `max_active_runs` 首版运行时：为手动/trigger 启动补齐统一 active-run gate，并保留 legacy 默认兼容语义。
  - [2026-04-02_flow-retry-backoff-runtime.md](2026-04-02_flow-retry-backoff-runtime.md)
  - retry backoff 首版：新增 `retry_backoff_ms` 固定间隔策略，并让 backoff 等待期间响应取消。
  - [2026-04-02_flow-permission-refinement-runtime.md](2026-04-02_flow-permission-refinement-runtime.md)
  - `flow.run` / `flow.read` 权限细化：统一 run/read 权限 gate，并让 `flow::run` capability 描述与运行时判权保持一致。
  - [2026-04-02_flow-list-runs-runtime.md](2026-04-02_flow-list-runs-runtime.md)
  - `list_runs` 首版运行时：提供 retained run history 查询，按最新到最旧返回 run 摘要。
  - [2026-04-02_flow-cancel-run-runtime.md](2026-04-02_flow-cancel-run-runtime.md)
  - `cancel_run` 首版运行时：新增显式 run 取消动作，并让 `status/detail` 同步体现取消结果。
  - [2026-04-02_flow-delete-permission-baseline.md](2026-04-02_flow-delete-permission-baseline.md)
  - delete 基线修复：对齐 `flow.delete` 测试授权前置，恢复 delete 相关基线回归。
  - [2026-04-02_flow-local-vars-detail-mainline.md](2026-04-02_flow-local-vars-detail-mainline.md)
  - clean branch 收口 `set_var`、`flow_var`、`detail`，并保留目标测试与基线失败隔离记录。
  - [2026-03-28_stream-module.md](2026-03-28_stream-module.md)
  - stream module 首版：新增 `stream` 子协议 module，实现 catalog / delivery / CTRL-DATA-ACK 三层运行时与私有 `delivery_*` helper。
  - [2026-03-28_auth-remote-authority-admin.md](2026-03-28_auth-remote-authority-admin.md)
  - auth remote authority admin：审批与 permit 管理动作复用现有 authority forwarding / targeted response，按真实 `SourceID` 做权限校验。
  - [2026-03-28_subproto-auth-permit-list-runtime.md](2026-03-28_subproto-auth-permit-list-runtime.md)
  - auth register permit list runtime：新增活动 permit list action，复用现有 permit 生命周期，并对缺失 perms 的角色记录做惰性回填。
  - [2026-03-26_auth-semi-central-authority.md](2026-03-26_auth-semi-central-authority.md)
  - auth 半中心 authority runtime：root 下发 authority lease，多跳 assist 仅转发到 edge hub，断链后冻结新准入但保留本地已知身份登录。
  - [2026-03-25_flow-trigger-server-context.md](2026-03-25_flow-trigger-server-context.md)
  - `flow` trigger / capability run 保留 `server context`：恢复本地 capability provider 的实时通知副作用。
  - [2026-03-25_defaultset-deps-release-chain.md](2026-03-25_defaultset-deps-release-chain.md)
  - `defaultset` 依赖链 patch 发布：补齐 `exec` shared packages 与 `WithDeps` 构造路径，供下游 `Server` 在 `GOWORK=off` 模式消费。
  - [2026-03-25_subproto-varstore-capability-schema.md](2026-03-25_subproto-varstore-capability-schema.md)
  - [2026-03-25_flow-set-child-notify.md](2026-03-25_flow-set-child-notify.md)
  - [2026-03-25_subproto-capability-input-schema.md](2026-03-25_subproto-capability-input-schema.md)
  - [2026-03-22_management-node-display-name-followup.md](2026-03-22_management-node-display-name-followup.md)
  - [2026-03-22_management-display-name-and-persistence-hook.md](2026-03-22_management-display-name-and-persistence-hook.md)
  - [2026-03-22_flow-state-route-retention.md](2026-03-22_flow-state-route-retention.md)
  - [2026-03-22_flow-data-dag-runtime.md](2026-03-22_flow-data-dag-runtime.md)
  - [2026-03-21_subproto-flow-delete.md](2026-03-21_subproto-flow-delete.md)
- 历史归档保留在当前目录中，按文件名日期倒序浏览
