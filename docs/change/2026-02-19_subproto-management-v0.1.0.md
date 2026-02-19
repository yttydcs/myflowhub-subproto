# 2026-02-19 - SubProto：发布独立 module `management`（management/v0.1.0）

## 变更背景 / 目标
为支撑 “子协议可裁切/可组装” 的长期目标，需要将各子协议实现从 `myflowhub-server` 中逐步拆出，形成独立 Go module，使 Server 仅承担装配/编排职责。

本次聚焦 **management 子协议**，目标：
1) 建立新仓库 `myflowhub-subproto`（单仓多 module，A2）；
2) 迁入 management 子协议实现为独立 module：`github.com/yttydcs/myflowhub-subproto/management`；
3) 发布版本 tag：`management/v0.1.0`，供 Server 以 semver 依赖拉取；
4) 保持 wire 与行为不变（仅做代码归属与依赖边界调整）。

## 具体变更内容（新增 / 修改 / 删除）

### 新增
- `README.md`：说明单仓多 module 的目录与 tag 规则
- `management/`（独立 Go module）
  - `management/go.mod`、`management/go.sum`
  - `management/*.go`：management 子协议实现（从 Server 迁入，保持逻辑不变）

### 修改
- 无（新仓首批提交）

### 删除
- 无（Server 侧删除将在对应仓库的变更文档中记录）

## 对应 plan.md 任务映射
- SUBMGMT0 - 初始化仓库骨架 ✅
- SUBMGMT1 - 创建 `management` module 并迁入实现 ✅
- SUBMGMT2 - Code Review ✅
- SUBMGMT3 - 归档变更（本文档）✅
- SUBMGMT4 - 发布 tag 并 push ✅（tag：`management/v0.1.0`）

## 关键设计决策与权衡
1) **单仓多 module（A2）**
   - 便于统一管理子协议实现，同时让每个子协议保持独立依赖与独立版本发布；
   - tag 采用 `<moduleDir>/vX.Y.Z`（例如 `management/v0.1.0`），符合 Go 多 module 仓库约定。

2) **依赖边界**
   - `management` module 仅依赖 `myflowhub-core`（框架能力）与 `myflowhub-proto`（协议字典）；
   - 禁止依赖 `myflowhub-server`，避免子协议实现被装配层绑定。

3) **wire 不变**
   - SubProto 值、Action 字符串、payload JSON schema、HeaderTcp 语义与转发策略保持不变；
   - 拆库仅改变源码归属与 import 路径。

## 测试与验证方式 / 结果
- `cd management; GOWORK=off go test ./... -count=1 -p 1`
- 结果：通过。

## 潜在影响与回滚方案

### 潜在影响
- 上游需要新增依赖：`github.com/yttydcs/myflowhub-subproto/management v0.1.0`；
- 若未来对 management 行为做调整，需要通过 `management/v0.x.y` 迭代发布，并同步上游升级依赖。

### 回滚方案
- 上游（Server）可回滚到“自带 management 实现”的方式（具体回滚点在 Server 侧变更文档中说明）；
- 本仓库可通过 `git revert` 回滚迁入代码的提交；若需撤销发布版本，则需要撤回 tag（需谨慎，避免影响已消费版本的仓库）。

