# 2026-04-02 Flow Delete Permission Baseline

## 变更背景 / 目标

- `2026-04-02_flow-local-vars-detail-mainline` 回归时，`flow` 模块只剩 4 个 delete 相关基线失败：
  - `TestFlowDeleteSuccess`
  - `TestFlowDeleteNotFound`
  - `TestFlowDeleteInterruptsActiveRun`
  - `TestFlowDeleteFileFailureKeepsState`
- 复现后发现 4 个用例都在进入 `applyDeleteLocal` 之前统一返回 `403 permission denied`，并非 delete 路由、持久化或 run cancel 逻辑回退。
- 本轮目标是把 delete 测试基线与当前稳定 `flow.delete` 权限模型重新对齐。

## 具体变更内容

### 修改

- `flow/delete_test.go`
  - 调整 `newDeleteTestEnv(...)` 的默认 happy-path 权限前置
  - 默认改为显式使用具备 `flow.delete` 的 `admin` 角色
  - 补充注释，说明 `auth.default_perms="*"` 不会覆盖已有 `role_perms[node]`

### 删除

- 无

## Requirements impact

- `none`

## Specs impact

- `none`

## Lessons impact

- `none`

## Related requirements

- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\requirements\flow_data_dag.md`

## Related specs

- `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\flow.md`

## Related lessons

- 无

## 对应 plan.md 任务映射

- `DEL-BL-1`
  - `flow/delete_test.go`
- `DEL-BL-2`
  - delete 目标测试
  - 全量 `flow` 模块回归
- `DEL-BL-3`
  - review 与当前归档

## 经验 / 教训摘要

- `flow.delete` 的稳定语义没有回退，真正漂移的是测试默认权限前置。
- `2026-03-26` 之后默认 `node` 角色只保留普通工作节点权限，不再天然拥有 `flow.delete`。
- 当 `role_perms[node]` 已定义时，`auth.default_perms` 不能再被当成“覆盖 node 角色权限”的快捷方式。

## 可复用排查线索

- 症状：
  - delete 成功 / not found / 文件失败 / 中断运行用例全部先返回 `403`
  - 只有 `permission denied` 用例仍符合预期
- 触发条件：
  - 测试 helper 仍默认使用 `node` 角色
  - 测试把 `auth.default_perms="*"` 当成 delete 授权来源
- 关键词 / 错误文本：
  - `permission denied`
  - `flow.delete`
  - `auth.default_perms`
  - `auth.role_perms`
- 快速检查：
  1. 看 `flow/delete_test.go` 的 helper 默认角色是否具备 `flow.delete`
  2. 看 `MyFlowHub-Core/config.DefaultAuthRolePerms` 中 `node` / `admin` 的权限集合
  3. 看 `permission.Config.ResolvePerms()` 是否优先命中 `role_perms[role]`

## 关键设计决策与权衡

- 保持 delete 运行时权限判定不变，只修正测试前置
  - 好处：与稳定 requirements/specs 保持一致，不意外放宽默认权限
  - 代价：delete 成功路径测试需要显式使用拥有 `flow.delete` 的角色

## 测试与验证方式 / 结果

- delete 目标测试：
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-delete-baseline\go.work go test github.com/yttydcs/myflowhub-subproto/flow/... -run 'TestFlowDeleteSuccess|TestFlowDeleteNotFound|TestFlowDeleteInterruptsActiveRun|TestFlowDeleteFileFailureKeepsState|TestFlowDeletePermissionDenied' -count=1 -p 1`
  - 结果：通过
- 全量 `flow` 模块：
  - `GOWORK=D:\project\MyFlowHub3\.tmp\verify-delete-baseline\go.work go test github.com/yttydcs/myflowhub-subproto/flow/... -count=1 -p 1`
  - 结果：通过
- `git diff --check`
  - 结果：通过

## 潜在影响

- 仅收敛测试基线，不改变运行时 delete 权限、删除语义或持久化行为

## 回滚方案

1. 回退 `flow/delete_test.go` 中 helper 默认角色改动
2. 重新执行 delete 相关测试确认旧基线表现

## 子Agent执行轨迹

- 本轮未使用子Agent
