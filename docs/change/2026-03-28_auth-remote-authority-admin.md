# 2026-03-28 Auth 远程 Authority 管理动作

## 变更背景 / 目标

- `assist_register / assist_login / assist_query_credential` 已支持 semi-central authority 的多跳转发，但审批与 permit 管理动作仍只能在 authority 本机执行。
- 这导致有权限的远程管理节点在 `sourceId != authorityId` 时，只会看到 timeout、source mismatch，或者被上层 GUI 误收敛成 authority-local 限制。
- 本轮目标是把以下 admin action 接入同一条 remote authority forwarding 链路：
  - `list_pending_registers`
  - `approve_register`
  - `reject_register`
  - `list_register_permits`
  - `issue_register_permit`
  - `revoke_register_permit`

## 具体变更内容

### remote forwarding allowlist

- 更新 `auth/auth_forward.go`
  - 增加 remote authority admin action allowlist
  - `shouldForwardByHeaderTarget(...)` 对上述 6 个 admin action 也按 `TargetID` 判断是否上送 authority
  - 新增 `tryForwardAdminUpstream(...)`
  - generalized `buildForwardError(...)` / `sendForwardError(...)`，让 admin response 也能返回显式 `authority unavailable`

### authority 侧 routed source 接受条件

- 更新 `auth/session.go`
  - 新增 `routedSourceMatches(...)`
  - 新增 `authSourceAllowed(...)`
- 更新 `auth/auth.go`
  - auth-required login actions 不再只接受直连 `sourceMatches`
  - 对 remote authority admin action，允许“当前入站连接确实拥有该 `SourceID` 路由归属”的继承 source 请求进入后续权限判断

### targeted response 与 admission handlers

- 更新 `auth/transport.go`
  - 新增 `sendTargetedActionData(...)`
  - `sendActionData(...)` 在 routed source admin response 场景下，走 targeted response 语义把响应送回原始 source
- 更新 `auth/actions_admission.go`
  - 6 个 authority admin handlers 都改为：
    - 先做请求校验
    - 如果 authority 为 remote，则尝试上送 authority
    - 否则在本地执行
  - remote authority 不可达时，显式返回 `4500 authority unavailable`
  - authority 侧权限判断仍基于请求头里的真实 `SourceID`

### 回归测试

- 新增 `auth/remote_admin_test.go`
  - 覆盖 6 个 admin action 的 `TargetID` forwarding
  - 覆盖 handler fallback forwarding
  - 覆盖 routed-source success with targeted response
  - 覆盖 route ownership mismatch drop
  - 覆盖 original actor permission denied
  - 覆盖 explicit unavailable response

## Requirements impact

- `none`

## Specs impact

- `none`

## Lessons impact

- `none`

## Related requirements

- `none`

## Related specs

- `D:\project\MyFlowHub3\worktrees\feat-server-remote-authority-admin\docs\specs\auth.md`
- `D:\project\MyFlowHub3\worktrees\feat-win-remote-authority-admin\docs\specs\authority-admin-console.md`

## Related lessons

- `D:\project\MyFlowHub3\docs\lessons\authority-local-admin-actions.md`

## 对应 plan.md 任务映射

- `AUTH-REMOTE-SUB-1`
- `AUTH-REMOTE-SUB-2`
- `AUTH-REMOTE-SUB-3`

## 经验 / 教训摘要

- remote authority admin 不需要新 action；复用现有 `TargetID=authority` 语义即可。
- 真正的安全边界不是“只允许 authority 本机”，而是“只接受当前入站连接确实拥有路由归属的继承 source”。
- 若 authority 不可达，应显式返回 `authority unavailable`，不要伪装成 permission denied。

## 可复用排查线索

- 症状
  - remote authority 下 `list_pending_registers` / `list_register_permits` 超时
  - authority 本机成功，远程管理节点失败
  - response 没有回到原始调用方
- 触发条件
  - `sourceId != authorityId`
  - semi-central authority
  - descendant route index 缺失或归属错误
- 关键词
  - `routed source`
  - `source mismatch`
  - `authority unavailable`
  - `TargetID`
- 快速检查
  - 检查 `shouldForwardByHeaderTarget(...)` 是否包含 admin action
  - 检查 authority 侧 `GetByNode(sourceID)` 是否回到当前入站连接
  - 检查 response 是否走 targeted response 路径

## 关键设计决策与权衡

- 复用 authority forwarding / targeted response，而不是引入 Win 专用协议
  - 优点：协议面最小，Server/Win 都能继承同一能力
  - 代价：需要 authority 侧增加更细粒度的 routed source 校验
- 权限始终基于真实 `SourceID`
  - 优点：不会退化成 relay hub 权限
  - 代价：必须保证路由 ownership 验证可靠

## 测试与验证方式 / 结果

- `MyFlowHub-SubProto/auth`
  - `GOWORK=D:\project\MyFlowHub3\.tmp\remote-authority-admin-auth\go.work go test ./... -count=1 -p 1`
  - 结果：通过

## 潜在影响与回滚方案

- 潜在影响
  - remote authority admin 会开始沿既有 forwarding 链路执行
  - 若下游消费者未同步升级，行为仍会停留在旧基线
- 回滚方案
  - 回退 `auth/actions_admission.go`
  - 回退 `auth/auth.go`
  - 回退 `auth/auth_forward.go`
  - 回退 `auth/session.go`
  - 回退 `auth/transport.go`
  - 回退 `auth/remote_admin_test.go`

## 子Agent执行轨迹

- 未使用子Agent
