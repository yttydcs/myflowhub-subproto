# 2026-03-05 - Auth：修复 up_login SenderPub 误用导致跨级路由丢失

## 变更背景 / 目标
- 背景：在拓扑 `1 -> (2,9) -> 10` 中，2(Win) 对 owner=10 执行 VarStore set 可能返回 `code=4 not found`。
- 目标：修复 Auth 上行登录传播（`up_login`）中的 sender 公钥字段错误，确保父节点可校验 sender 签名并建立下游节点路由索引。

## 具体变更内容

### 修改
- `auth/actions_up_login.go`
  - `sendUpLogin` 改为调用 `buildUpLoginData(...)` 统一构包与校验。
  - 修复 `SenderPub` 取值：从错误的“登录节点公钥”改为正确的“sender 节点公钥（h.nodePubB64）”。
  - 增加 `SenderSig` 为空时的保护返回，避免发送无效上行登录帧。

### 新增
- `auth/actions_up_login_test.go`
  - `TestBuildUpLoginData_UsesSenderNodePub`：验证 `PubKey` 与 `SenderPub` 字段语义不混淆，且 sender 签名可被 sender 公钥验证。
  - `TestBuildUpLoginData_MissingSenderPubRejected`：验证 sender 公钥缺失时构包失败。

### 删除
- 无。

## 对应 todo 任务映射
- UPLOGIN-1：修复 sendUpLogin 的 SenderPub 取值 -> 完成
- UPLOGIN-2：补充单测覆盖关键构包逻辑 -> 完成
- UPLOGIN-3：验证、Code Review、归档 -> 完成

## 关键设计决策与权衡（性能 / 扩展性）
- 决策：新增 `buildUpLoginData` 聚合构包规则与校验，避免字段语义在调用点散落。
- 性能：仅登录传播路径增加常数级字段校验与签名空值判断，对正常数据面（VarStore set/get）无额外开销。
- 扩展性：后续若上行登录增加字段约束，可集中在构包函数演进，降低回归风险。

## 测试与验证方式 / 结果
- 执行命令：`cd auth && GOWORK=off go test ./... -count=1 -p 1`
- 结果：通过。

## Code Review（3.3）结论
- 需求覆盖：通过
- 架构合理性：通过（修复点集中于 Auth 上行构包）
- 性能风险：通过（无新增 I/O、无循环放大）
- 可读性与一致性：通过（字段语义显式）
- 可扩展性与配置化：通过（构包逻辑集中）
- 稳定性与安全：通过（避免错误 sender 公钥导致签名链断裂）
- 测试覆盖情况：通过（新增关键语义单测 + 模块测试通过）

## 潜在影响与回滚方案
- 潜在影响：
  - 父节点将更稳定地识别并接纳下游节点上行登录传播，改善跨级路由可达性。
- 回滚方案：
  - 回滚文件：
    - `auth/actions_up_login.go`
    - `auth/actions_up_login_test.go`
  - 若已发布后发现兼容问题，采用新补丁版本前滚修复，不建议删除历史 tag。
