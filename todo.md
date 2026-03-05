# TODO - SubProto：修复跨级 VarStore 目标节点路由丢失（2 -> 10 set 返回 code=4）

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 分支：`fix/subproto-uplogin-sender-pub`
- Worktree：`d:\project\MyFlowHub3\repo\MyFlowHub-SubProto\worktrees\fix-subproto-uplogin-sender-pub\MyFlowHub-SubProto`
- 触发问题：拓扑 `1 -> (2,9) -> 10`，由 2(Win) 对 owner=10 执行 VarStore set 报 `code=4 not found`。

## 项目目标与当前状态
- 目标：
  - 修复 node10 向上登录传播（up_login）链路中 sender 公钥携带错误，避免父节点无法校验 sender 签名进而丢失路由索引。
  - 让 `2 -> 1 -> 9 -> 10` 的 VarStore set 能正确命中 owner=10 路由。
- 当前状态（已定位）：
  - `auth/sendUpLogin` 当前把 `SenderPub` 错误设置为“登录设备(10)公钥”，而不是“sender 节点(9)公钥”；
  - 当父节点缺少 sender 已信任公钥时，会依赖 `SenderPub` 回填校验，导致校验失败并不建立 `node10` 路由。

## 可执行任务清单（Checklist）

- [x] UPLOGIN-1：修复 sendUpLogin 的 SenderPub 取值
  - 目标：`SenderPub` 必须携带 sender 节点公钥（`h.nodePubB64`），而非被登录设备公钥。
  - 涉及文件：
    - `auth/actions_up_login.go`
  - 验收条件：
    - 构造的 `up_login` 数据中，`PubKey` 仍是登录节点公钥；`SenderPub` 为 sender 节点公钥。
  - 测试点：
    - 单测断言 `SenderPub != PubKey`（在不同公钥输入下）。
  - 回滚点：
    - 回滚 `actions_up_login.go` 本任务变更。

- [x] UPLOGIN-2：补充单测覆盖关键构包逻辑
  - 目标：防止未来回归把 `SenderPub` 再次误绑到目标节点公钥。
  - 涉及文件：
    - `auth/actions_up_login_test.go`（新增）
  - 验收条件：
    - `go test ./auth -count=1 -p 1` 通过；
    - 用例覆盖 `SenderPub` 与 `PubKey` 字段语义。
  - 回滚点：
    - 删除新增测试文件。

- [x] UPLOGIN-3：验证、Code Review、归档
  - 目标：完成质量闭环与审计文档。
  - 涉及文件：
    - `docs/change/2026-03-05_auth-up-login-sender-pub-fix.md`
  - 验收条件：
    - 测试命令通过并记录；
    - Review 清单逐项给出通过/不通过结论；
    - docs/change 归档完整（背景、任务映射、权衡、验证、回滚）。
  - 测试点：
    - `go test ./auth -count=1 -p 1`
  - 回滚点：
    - 按提交回滚或按任务粒度回滚。

## 依赖关系
- `UPLOGIN-1 -> UPLOGIN-2 -> UPLOGIN-3`

## 风险与注意事项
- 本修复影响 Auth 上行校验与路由传播，属于链路关键路径；必须保持 wire 字段不变，仅修正字段值来源。
- 不改 VarStore/Win 协议调用参数，避免引入跨仓耦合变更。

## 当前执行状态
- 已完成：UPLOGIN-1、UPLOGIN-2、UPLOGIN-3
- 进行中：无
- 待完成：无
