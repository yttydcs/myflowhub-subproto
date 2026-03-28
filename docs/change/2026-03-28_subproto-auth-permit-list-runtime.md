# 2026-03-28 SubProto：auth register permit list runtime

## 变更背景 / 目标

- auth runtime 已经具备 permit 的 issue / revoke / consume / persist 语义，但还缺少“列出当前活动 permit”的 action。
- 本轮目标是在不改变既有 permit 生命周期语义的前提下，补齐活动 permit 列表能力，并保证权限判断和测试覆盖稳定。

## 具体变更内容

- 修改 `auth/types.go`
  - 接入 permit list 相关 Proto alias
- 修改 `auth/actions_admission.go`
  - 注册 `list_register_permits`
  - 新增 `handleListRegisterPermits`
  - 新增 `requireAnyActionPermission(...)`
- 修改 `auth/admission.go`
  - 新增 `listRegisterPermits(req)`
  - list 前执行 `cleanupExpiredAdmissionLocked()`
  - 支持 `device_id` 过滤、排序和分页
- 修改 `auth/perm_helpers.go`
  - 当 whitelist 记录有 `role` 但 `perms` 为空时，按当前角色配置补回权限
  - 避免 permit list 权限判断把旧绑定或缺失 perms 的记录误判成无权限
- 修改 `auth/admission_test.go`
  - 新增 permission denied 用例
  - 新增 revoke 权限即可列出 permit 的回归
  - 新增 revoke / consume / expiry 后列表变化回归

## Impact

- Requirements impact: `none`
- Specs impact: `none`
- Lessons impact: `none`
- Related requirements: `none`
- Related specs: `none`
- Related lessons: `none`

## 对应 plan.md 任务映射

- `SUBPROTO-PERMIT-1`
- `SUBPROTO-PERMIT-2`
- `SUBPROTO-PERMIT-3`
- `REVIEW-SUBPROTO-PERMIT-1`
- `ARCHIVE-SUBPROTO-PERMIT-1`

## 经验 / 教训摘要

- permit list 必须直接基于 `registerPermits` 活动态构建，不能额外引入第二份状态来源。
- 权限判断不能假设 whitelist 里的 `Role` 和 `Perms` 永远同时存在；对旧持久化或手工构造记录，缺失 `Perms` 时需要按角色配置回填。

## 可复用排查线索

- 症状
  - permit list action 返回 `4403 permission denied`，但节点角色明明具备 `auth.permit.revoke`
  - 过期 permit 仍出现在列表里
  - revoke / consume 后列表没有消失
- 触发条件
  - whitelist 记录只写了 `role`，没有 `perms`
  - list 实现没有先做过期清理
- 关键词
  - `list_register_permits`
  - `requireAnyActionPermission`
  - `lookupByNode`
  - `cleanupExpiredAdmissionLocked`
- 快速检查
  - 查看 `auth/actions_admission.go` 是否允许 `auth.permit.issue` 或 `auth.permit.revoke`
  - 查看 `auth/perm_helpers.go` 是否会对缺失 perms 的角色记录做回填
  - 查看 `auth/admission.go` 是否在 list 前清理过期 permit

## 关键设计决策与权衡

- permit list 权限复用现有 `issue/revoke` 权限，而不是新增 list 权限
  - 优点：和现有 Core 权限模型一致，改动面最小
  - 代价：列表权限不再单独细分
- 对缺失 perms 的绑定记录做惰性回填，而不是直接拒绝
  - 优点：兼容旧持久化状态，减少“角色存在但无权限”的伪回归
  - 代价：`lookupByNode` 增加了少量修复逻辑

## 测试与验证方式 / 结果

- 临时 workspace 下执行 `go test ./... -count=1 -p 1`
  - 环境：临时 `go.work` 指向本地 `MyFlowHub-Core`、Proto worktree 和 auth module
  - 结果：通过

## 潜在影响与回滚方案

- 潜在影响
  - `lookupByNode` 现在会对缺失 perms 的角色记录做惰性修复，权限行为会更接近当前配置而不是旧脏状态
- 回滚方案
  - 回退 `auth/types.go`
  - 回退 `auth/actions_admission.go`
  - 回退 `auth/admission.go`
  - 回退 `auth/perm_helpers.go`
  - 回退相关测试

## 子Agent执行轨迹

- 本轮未使用子Agent
