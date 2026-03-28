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
