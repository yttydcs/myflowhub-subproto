# 2026-03-26 SubProto：auth semi-central authority runtime

## 变更背景 / 目标
- 为 auth 增加“半中心”authority 运行时约束：
  - root 下发 `effective_authority_id`
  - 父链在线时保留向上 admission 路由
  - 父链断开时冻结新准入，但允许本地已知身份继续登录
- 避免把 authority lease 持久化到配置，降低断链重启后的陈旧状态风险。

## 具体变更内容
- 修改 [`auth/auth.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/auth.go)
  - 增加半中心 authority runtime state
  - 在 `OnReceive` 前置加入 auth-local `TargetID` 转发判定
  - root 模式下通过 `BindServer` 启动 authority policy 下发
- 新增 [`auth/authority_policy.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/authority_policy.go)
  - 解析 `auth.authority_mode` / `auth.authority_policy_ttl_sec`
  - 维护 in-memory authority lease
  - root 周期性下发 `authority_policy_sync`
- 新增 [`auth/actions_authority_policy.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/actions_authority_policy.go)
  - 仅接受来自父连接的 policy sync
  - 忽略 stale epoch
  - 把最新 policy 继续广播给下游
- 新增 [`auth/auth_forward.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/auth_forward.go)
  - 对 `assist_register / assist_login / assist_query_credential` 增加 `TargetID` 转发
  - 无路由时回 `authority unavailable`
- 修改 [`auth/routing.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/routing.go)
  - `resolveAuthority()` 在半中心模式下优先使用 runtime policy
  - policy 缺失或过期但父链仍在线时，回退为“按父链逐级上送”
- 修改 [`auth/transport.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/transport.go)
  - 新增面向 edge hub 的 targeted assist response
  - 新增带 `SourceID/TargetID` 的 authority request 转发 helper
- 修改 [`auth/actions_register.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/actions_register.go)
  - local request 走 authority-target forwarding
  - intermediate assist path 只做上送，不创建本地 pending/binding
- 修改 [`auth/actions_login.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/actions_login.go)
  - 半中心退化期保留本地已知身份登录
  - 需要上游 authority 的分支在断链时显式失败
- 修改 [`auth/actions_query.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/actions_query.go)
  - `assist_query_credential` 支持同样的多跳 authority forwarding
- 新增 [`auth/authority_policy_test.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/authority_policy_test.go)
  - 覆盖 root 下发、policy apply、stale epoch、header-target forward、degraded login

## Impact
- Requirements impact: `none`
- Specs impact: `updated`
- Lessons impact: `none`
- Related requirements: `none`
- Related specs:
  - [`auth.md`](D:/project/MyFlowHub3/worktrees/server-auth-semi-central-authority/docs/specs/auth.md)
  - [`protocol_map.md`](D:/project/MyFlowHub3/worktrees/proto-auth-semi-central-authority/docs/protocol_map.md)
- Related lessons: `none`

## 对应 plan.md 任务映射
- `AUTHPOL-SUB-1`
- `AUTHPOL-SUB-2`
- `AUTHPOL-SUB-3`

## 经验 / 教训摘要
- 不能把 `authority_policy_sync` 当成启动门禁；否则 parent bootstrap register 会被新 lease 时序反噬。
- 多跳 auth assist 路径不能复用“每跳都落 pending/binding”的旧思路，否则中间 hub 会把子 hub 连接错误绑定成后代设备。

## 可复用排查线索
- 症状
  - 非 root 节点在断链后仍然允许新 register
  - 中间 hub 收到 assist 回包后，child hub 连接的 `nodeID/deviceID` 被污染
  - `authority unavailable` 没有及时返回给 edge hub
- 触发条件
  - 半中心模式下的多跳 admission 请求
  - policy 尚未收到 / 已过期 / 父链断开
- 关键词
  - `auth.authority_mode`
  - `authority_policy_sync`
  - `assist_register`
  - `TargetID`
- 快速检查
  - 查看 [`auth/routing.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/routing.go) 中 `resolveSemiCentralAuthority`
  - 查看 [`auth/auth_forward.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/auth_forward.go) 是否对 assist action 做了 `TargetID` forward
  - 查看 [`auth/authority_policy_test.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/authority_policy_test.go) 的回归用例是否覆盖当前症状

## 关键设计决策与权衡
- 采用“runtime lease + parent-chain fallback”，而不是持久化 `authority.node_id`
  - 这样保留 root 单一 authority 语义，同时不把重启后的 stale authority 带入配置层
- 采用“edge hub 为响应终点，中间节点只转发”的 assist response 语义
  - 这样避免中间节点落本地 pending/binding，降低多跳 metadata 污染风险
- 仅覆盖 admission 上送链路
  - 当前 approve / reject / permit 仍是 authority 本地操作，不假装已经支持全网任意节点远程审批

## 测试与验证方式 / 结果
- 执行：`go test ./... -count=1 -p 1`
  - 环境：`GOWORK=D:/project/MyFlowHub3/.tmp/auth-semi-central-work/go.work`
  - 临时 workspace 指向本地 `MyFlowHub-Core`、Proto worktree 与 auth module
- 结果：通过
- 备注
  - 直接 `GOWORK=off` 时会命中已发布的旧 `core/proto` 版本，无法代表当前 worktree 联调结果

## 潜在影响与回滚方案
- 潜在影响
  - assist 内部响应从“逐跳 `MajorCmd`”切成了“面向 edge hub 的 targeted response”，多跳行为会更依赖 `SourceID/TargetID`
  - root authority lease 会广播到所有非父连接；当前沿用了既有 `perms_snapshot` 广播边界
- 回滚方案
  - 回退 [`auth/auth.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/auth.go)
  - 回退 [`auth/routing.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/routing.go)
  - 回退 [`auth/transport.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/transport.go)
  - 回退 [`auth/actions_register.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/actions_register.go)
  - 回退 [`auth/actions_login.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/actions_login.go)
  - 回退 [`auth/actions_query.go`](D:/project/MyFlowHub3/worktrees/subproto-auth-semi-central-authority/auth/actions_query.go)
  - 删除新增 authority policy / forward / test 文件

## 子Agent执行轨迹
- 本轮未使用子Agent
