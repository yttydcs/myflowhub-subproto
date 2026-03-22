# 2026-03-22 Management Node Display Name Follow-up

## 变更背景 / 目标

此前 `management` 已能从 effective config / connection metadata 返回 `display_name`，但仍缺两条会话内闭环：

- auth 建链成功后，直连父节点不一定拿得到 child 的名称缓存
- direct-child 改名后，当前会话里的后续 `list_nodes` 还不会立即刷新

本次目标是在不引入 `list_nodes` 额外 I/O 的前提下，用 auth bootstrap 和 `config_set_resp` 回程刷新把这条链路补齐。

## 具体变更内容

- `auth/types.go`
  - 本地兼容 `register/login/resp` 的 `display_name` 字段
- `auth/actions_register.go`
  - direct-child register 成功与 `register_resp` 回程透传 `display_name`
- `auth/actions_login.go`
  - direct-child login 成功、受保护 assist 路径、以及 `login_resp` 回程透传 / 刷新 `display_name`
- `auth/session.go`
  - 增加显示名规范化、metadata helper、直连 child 判定 helper
  - 空字符串会清空旧 metadata，避免清名后残留旧值
- `management/management.go`
  - 在 `config_set_resp(key=node.display_name, code=1)` 回程上刷新 direct-child metadata
  - 限制 `hdr.SourceID == conn.meta(nodeID)`，避免 descendant rename 污染中间连接
- 新增测试
  - `auth/display_name_test.go`
  - `management/management_response_path_test.go`

## 对应计划任务映射

- `SUB1`
- `SUB2`
- `SUB3`

## 关键设计决策与权衡

- `list_nodes` 继续只读连接 metadata，不增加 per-child `node_info` 查询。
- auth 侧使用本地兼容 wire struct，不强制等待 Proto 版本切换。
- `assist_register` 前绑定阶段缺少稳定的“直连 child 本人”判定，因此 authority 侧首次 bootstrap 以 login 成功路径和后续 rename refresh 为主。
- 空字符串仅在成功的 auth / management 路径上清空 metadata，不会因为缺失字段误清已有值。

## Requirements / Specs 影响检查

- Requirements impact：`none`
- Specs impact：`none`
- Related requirements：
  - [management-node-display-name.md](/D:/project/MyFlowHub3/worktrees/MyFlowHub3-feat-node-display-name-followup/docs/requirements/management-node-display-name.md)
- Related specs：
  - [management-config-layering.md](/D:/project/MyFlowHub3/worktrees/MyFlowHub3-feat-node-display-name-followup/docs/specs/management-config-layering.md)
  - [auth.md](/D:/project/MyFlowHub3/worktrees/MyFlowHub-Server-feat-node-display-name-followup/docs/specs/auth.md)
- Lessons：`none`

## 测试与验证方式 / 结果

- `auth`: `GOWORK=off go test ./... -count=1`：通过
- `management`: 通过临时 `modfile + replace` 指向本地 `exec` module 后 `go test ./... -count=1`：通过
- 直接 `GOWORK=off go test ./...`：失败
  - 阻塞点：当前已发布 `github.com/yttydcs/myflowhub-subproto/exec v0.1.1` 不包含 `exec/capability`
  - 结论：属于现有 module 依赖解析问题，不是本轮 `management` / auth 显示名逻辑回归

## 潜在影响与回滚方案

### 潜在影响

- authority 侧在 `assist_register` 时不会盲目缓存 `display_name`，首次显示依赖后续 login 或 rename refresh。
- `management` module 的独立 `GOWORK=off` 测试仍需要本地 `exec` module 才能完整跑通。

### 回滚方案

- 回退 `auth/types.go`
- 回退 `auth/actions_register.go`
- 回退 `auth/actions_login.go`
- 回退 `auth/session.go`
- 回退 `management/management.go`
- 回退新增测试文件

## 子 Agent 执行轨迹

- `SUB1` / `SUB2` -> `Arendt (019d1618-11cf-7923-8e62-babfc05af8bb)` 初始执行超时，主Agent接管收口并补齐空字符串清理与 assist 保护 -> `D:\project\MyFlowHub3\worktrees\MyFlowHub-SubProto-feat-node-display-name-followup`
  - 文件：`auth/types.go`、`auth/actions_register.go`、`auth/actions_login.go`、`auth/session.go`、`management/management.go`、测试文件
  - 验收：`auth` / `management` 关键测试通过；独立 `GOWORK=off` 阻塞点已定位为既有依赖发布问题
