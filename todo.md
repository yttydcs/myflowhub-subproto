# TODO - SubProto：VarStore 跨层（1->9->10）set/get 路由修复

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 分支：`fix/varstore-crosshop-routing`
- Worktree：`d:\project\MyFlowHub3\worktrees\fix-subproto-varstore-crosshop-routing`
- 触发问题：拓扑 `1 -> (2,9) -> 10`，Win(2) 对 owner=10 执行 var set 返回 `not found (code=4)`；同层（1直连10）可用。

## 项目目标与当前状态
- 目标：
  - 修复 VarStore 在多跳拓扑下的目标转发能力，确保跨层 owner 可达。
  - 保持同层行为与现有 wire 协议兼容。
- 当前状态（已定位）：
  - `varstore` 当前未像 management 一样处理 `hdr.TargetID != local` 的命令转发；
  - 跨层 notify/command 在中间节点可能被本地消费，导致链路中断与值不一致。

## 可执行任务清单（Checklist）

- [x] VSROUTE-1：补齐 VarStore 的按 target 转发入口
  - 目标：当 `MajorCmd` 且 `TargetID != local` 时，按路由索引转发到下一跳（或父节点），本地不消费。
  - 涉及文件：
    - `varstore/varstore.go`
  - 验收条件：
    - 多跳场景下，中间节点不再错误本地处理目标非本机的 varstore 命令。
  - 测试点：
    - 新增单测覆盖 target-forward 成功/未命中分支。
  - 回滚点：
    - 回滚 `varstore/varstore.go` 中新增的 forward 入口。

- [x] VSROUTE-2：补充回归测试覆盖 1->9->10 关键路径
  - 目标：锁定跨层 set/get/revoke 至少一条关键路径不回归。
  - 涉及文件：
    - `varstore/*_test.go`（新增或修改）
  - 验收条件：
    - `go test ./... -count=1 -p 1` 通过。
  - 回滚点：
    - 删除新增测试并回滚相关断言。

- [x] VSROUTE-3：Code Review、归档与发布准备
  - 目标：完成审计闭环，并给出下游依赖升级建议。
  - 涉及文件：
    - `docs/change/2026-03-05_varstore-crosshop-routing.md`
  - 验收条件：
    - Review 清单逐项通过；
    - docs/change 完整记录背景、任务映射、验证、回滚。
  - 回滚点：
    - 回滚文档与版本发布动作。

## 依赖关系
- `VSROUTE-1 -> VSROUTE-2 -> VSROUTE-3`

## 风险与注意事项
- 转发逻辑位于子协议关键路径，必须避免回环与 hop-limit 违规。
- 不改协议字段，仅补齐行为；确保与既有 Win/Android 客户端兼容。
