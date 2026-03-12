# Plan - MyFlowHub-SubProto/auth：修复 UpLogin sender 公钥毒化与路由自愈

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 分支：`fix/auth-route-index-heal`
- Worktree：`d:\project\MyFlowHub3\worktrees\fix-auth-route-index-heal\MyFlowHub-SubProto`
- Base：`main`
- 关联仓库：`MyFlowHub-Server`（升级依赖并发布 HubServer）

## 项目目标与当前状态
- 目标：修复多 hop 场景中 Root Hub 因 `sourceMismatch` 丢弃后代节点帧的问题（典型日志：`drop frame due to source mismatch subproto=3 hdr_source=11 meta_node=9`），确保能建立 `nodeIndex[descendant] -> childConn`。
- 当前状态：根因已确认：`register` 在 `pubkey` 缺失时填本机公钥导致 trusted/binding “毒化”，进而 `up_login` sender 验签失败、无法写入路由索引。
- 已确认约束：不放宽 Core `sourceMismatch` 门禁；`up_login` 允许自愈但必须满足 `req.SenderID == hdr.SourceID == conn.meta(nodeID)`。
- 明确不做：父链 bootstrap register 携带 pubkey（本轮不做方向 3）。

## 依赖关系
- Auth 模块需要发布新 tag：`auth/v0.1.2`。
- Server 侧需在 `go.mod` 升级 `github.com/yttydcs/myflowhub-subproto/auth` 到新版本，并发布新 Server 版本（是否打 `myflowhub-server` tag 由后续确认）。

## 风险与注意事项
- 安全：自愈属于“纠正历史错误 trusted key”，必须严格约束触发条件并记录审计日志；不满足条件保持拒绝（返回）。
- 持久化：仅更新 `trustedNode` 不够，必须同步修正 `whitelist(binding).PubKey`，否则落盘仍可能写回旧错误 key。
- 测试隔离：`auth.disable_persist=true` 按文档语义应禁止读写 `config/trusted_nodes.json`（需要确认实现一致，避免测试污染）。

## 可执行任务清单（Checklist）

### AUTH-1 修复 register：缺省 pubkey 不再填本机公钥
- 目标：当 register 请求未携带 `pubkey` 时，保持空值，不写 trusted/binding 公钥，避免把 Root 节点公钥错误写入子节点记录。
- 涉及模块/文件：`auth/actions_register.go`
- 验收条件：
  - `pubkey` 为空时不再触发 `addTrustedNode(...)`；
  - 仍可正常分配/绑定 node_id 与 device_id。
- 测试点：新增/更新单测覆盖 `pubkey` 缺失时的行为（不写 trusted）。
- 回滚点：回退本次改动提交。

### AUTH-2 修复 up_login：trusted sender 验签失败时允许 SenderPub 自愈（受限）
- 目标：当 `lookupTrustedNodePub(senderID)` 返回的 pub 验签失败时，若请求携带 `sender_pub` 且满足约束（SenderID/SourceID/conn.meta 一致），则使用 `sender_pub` 二次验签，通过则更新 trusted + 修正对应 binding/pubkey 并继续写入路由索引。
- 涉及模块/文件：
  - `auth/actions_up_login.go`
  - `auth/session.go`（如需暴露/复用“更新 binding 公钥并持久化”的 helper）
- 验收条件：
  - 发生自愈后仍能执行 `AddNodeIndex(req.NodeID, conn)`；
  - 自愈会输出 WARN 审计日志（至少包含 sender_id、conn id、以及“发生自愈/更新”）；
  - 不满足约束或验签失败时不更新 trusted/binding。
- 测试点：
  - trusted 正确：正常通过；
  - trusted 错误 + sender_pub 正确：触发自愈并通过；
  - `req.SenderID != hdr.SourceID`：即使 sender_pub 可验也不得自愈。
- 回滚点：回退本次改动提交。

### AUTH-3 修复/对齐 disable_persist 语义（测试与文档一致性）
- 目标：当配置 `auth.disable_persist=true` 时，不读写 `config/trusted_nodes.json`（与 Server 文档一致）。
- 涉及模块/文件：`auth/session.go`、`auth/node_keys.go`（如需）
- 验收条件：单测运行不会在工作目录产生 `config/trusted_nodes.json` 副作用；运行期关闭持久化时不落盘。
- 测试点：单测中显式设置 `auth.disable_persist=true` 并断言无文件产生（或通过 tempdir 隔离验证）。
- 回滚点：回退该语义对齐提交（若评估为影响面过大，可改为仅测试隔离方案）。

### AUTH-4 回归测试：覆盖“毒化 trusted -> 自愈 -> 建路由”关键链路
- 目标：新增针对 `handleUpLogin` 的回归用例，模拟“sender trusted key 错误但 sender_pub 正确”的场景，确保能够更新 trusted/binding 并写入 `ConnManager.nodeIndex`。
- 涉及模块/文件：`auth/actions_up_login_test.go`（或新增专用 test 文件）
- 验收条件：`cd auth && GOWORK=off go test ./... -count=1 -p 1` 通过。
- 测试点：同 AUTH-2。
- 回滚点：回退新增测试文件/用例。

### AUTH-5 发布：打 tag 并交付给 Server 升级依赖
- 目标：合并前在本仓创建并推送 `auth/v0.1.2`，供 Server 与下游依赖升级。
- 涉及模块/文件：无（git tag）
- 验收条件：`git tag auth/v0.1.2` 存在且可被 `go list -m` 解析。
- 测试点：Server 侧 `go get github.com/yttydcs/myflowhub-subproto/auth@v0.1.2` 可成功。
- 回滚点：删除本地 tag（未推送前）或创建新 patch tag（已推送后不重写）。
