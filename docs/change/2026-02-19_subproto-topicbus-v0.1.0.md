# 2026-02-19 - SubProto：发布独立 module `topicbus`（topicbus/v0.1.0）

## 变更背景 / 目标
为支撑 “子协议可裁切/可组装”，需要将各子协议实现从 `myflowhub-server` 中逐步拆出，形成独立 Go module，使 Server 仅承担装配/编排职责。

本次聚焦 **topicbus 子协议**，目标：
1) 在 `myflowhub-subproto`（A2：单仓多 module）中新增 `topicbus` module；
2) 迁入 topicbus 子协议实现为独立 module：`github.com/yttydcs/myflowhub-subproto/topicbus`；
3) 发布版本 tag：`topicbus/v0.1.0`，供 Server 以 semver 依赖拉取；
4) 保持 wire 与行为不变（仅做代码归属与依赖边界调整）。

## 具体变更内容（新增 / 修改 / 删除）

### 新增
- `topicbus/`（独立 Go module）
  - `topicbus/go.mod`、`topicbus/go.sum`
  - `topicbus/*.go`：topicbus 子协议实现（从 Server 迁入，保持逻辑不变）

### 修改
- 无（本次为新增 module）

### 删除
- 无（Server 侧删除将在对应仓库的变更文档中记录）

## 对应 plan.md 任务映射
- SUBTB0 - 归档旧 plan ✅
- SUBTB1 - 创建 `topicbus` module 并迁入实现 ✅
- SUBTB2 - Code Review ✅
- SUBTB3 - 归档变更（本文档）✅
- SUBTB4 - 发布 tag 并 push ✅（tag：`topicbus/v0.1.0`）

## 关键设计决策与权衡
1) **单仓多 module（A2）**
   - 便于统一管理子协议实现，同时让每个子协议保持独立依赖与独立版本发布；
   - tag 采用 `<moduleDir>/vX.Y.Z`（例如 `topicbus/v0.1.0`），符合 Go 多 module 仓库约定。

2) **依赖边界**
   - `topicbus` module 仅依赖 `myflowhub-core`（框架能力）与 `myflowhub-proto`（协议字典）；
   - 禁止依赖 `myflowhub-server`，避免子协议实现被装配层绑定。

3) **wire 不变**
   - SubProto 值、Action 字符串、payload JSON schema、HeaderTcp 语义与转发策略保持不变；
   - 拆库仅改变源码归属与 import 路径。

## 测试与验证方式 / 结果
- `cd topicbus; GOWORK=off go test ./... -count=1 -p 1`
- 结果：通过。

## 潜在影响与回滚方案

### 潜在影响
- 上游需要新增依赖：`github.com/yttydcs/myflowhub-subproto/topicbus v0.1.0`；
- 若未来对 topicbus 行为做调整，需要通过 `topicbus/v0.x.y` 迭代发布，并同步上游升级依赖。

### 回滚方案
- 上游（Server）可回滚到“自带 topicbus 实现”的方式（具体回滚点在 Server 侧变更文档中说明）；
- 本仓库可通过 `git revert` 回滚迁入代码的提交；若需撤销发布版本，则需要撤回 tag（需谨慎，避免影响已消费版本的仓库）。

