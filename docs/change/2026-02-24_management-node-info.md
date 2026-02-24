# 变更说明：SubProto/Management 增加 node_info（节点自描述信息）

## 变更背景 / 目标
为支持 Win 的 `Devices` “点击节点查看设备信息”，management 子协议需要新增一个动作，由目标节点本地采集基础信息并响应（满足“数据来自节点本身”）。

## 具体变更内容（新增 / 修改 / 删除）
- 新增：
  - `management/action_node_info.go`：新增 `node_info` 处理器，采集并返回 KV（平台/版本/commit 等）
- 修改：
  - `management/actions.go`：注册 `node_info`
  - `management/types.go`：补充 action 常量与请求/响应类型别名
  - `management/management.go`：补充 forward 失败时的 `node_info_resp` 错误响应映射

## 对应 plan.md 任务映射
- `worktrees/node-info/MyFlowHub-Win/plan.md`
  - T3. SubProto(management)：实现 `node_info` handler（并发布 `management/v0.1.1`）

## 关键设计决策与权衡（性能 / 扩展性）
- 返回结构采用 `items: map[string]string`：
  - 便于未来扩展字段（不需要改 UI 与 struct 字段），适合“通用信息面板”。
- 信息采集使用 `runtime` + `debug.ReadBuildInfo()`：
  - 采集成本低（常量级字符串处理），不会引入明显性能开销。
  - 版本号可能为 `(devel)`，因此同时返回 `commit/vcs_time/vcs_modified` 作为可定位信息（后续可在发布流水线注入 semver）。

## 测试与验证方式 / 结果
- 已在本地执行：`go test ./...`（`myflowhub-subproto/management` module）

## 潜在影响与回滚方案
- 影响：仅新增 action，不改变既有 wire；旧节点仍可工作（但不支持 node_info 时 Win 侧可能超时，需要 UI 提示）。
- 回滚：revert 本提交；若已发布 tag，建议走补丁版本而不是删除 tag。

