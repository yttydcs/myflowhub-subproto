# Lessons

## Purpose

存放本仓可复用的排查经验、根因模式和快速检查线索。

## How To Enter This Section

- 当问题可能复发
- 当排查路径非显而易见
- 当根因体现为结构性规则，而不是一次性笔误

## What Belongs Here

- 症状与关键词
- 触发条件
- 根因与修复方式
- 下次优先检查项

## Naming / Maintenance Rules

- 使用稳定文件名，不加日期前缀
- 新增或调整 lesson 后更新本索引

## Current Docs

- [capability-provider-observable-side-effects.md](capability-provider-observable-side-effects.md)
  - 线索：`flow set 成功但 varpool 不更新`、`invokeCapabilitySet`、`propagateChange`
- [flow-trigger-run-missing-server-context.md](flow-trigger-run-missing-server-context.md)
  - 线索：`trigger flow 不推送但刷新可见`、`tryStartRunWithTrigger`、`core.ServerFromContext(ctx)`
