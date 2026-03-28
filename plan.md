# Plan - remote-authority-auth-release-subproto

## Workflow Information
- Repo: `MyFlowHub-SubProto`
- Branch: `chore/remote-authority-auth-release`
- Base: `main`
- Worktree: `D:\project\MyFlowHub3\worktrees\chore-subproto-remote-authority-auth-release`
- Current Stage: `3.1`

## Stage Records

### Initialization
- guide.md:
  - 已读取 `D:\project\MyFlowHub3\guide.md`
  - 约束确认：
    - commit 信息使用中文
    - 所有 worktree 必须位于 `D:\project\MyFlowHub3\worktrees`
    - 子协议稳定文档以 `repo\MyFlowHub-Server\docs` 为准
- base/worktree confirmation:
  - 主仓控制面路径仅用于 worktree / merge / release 管理
  - 本轮实现仅在当前 worktree 内完成
  - 关联 worktree：
    - `D:\project\MyFlowHub3\worktrees\chore-server-remote-authority-auth-release`

### Stage 1 - Requirements Analysis
#### Goal
- 发布包含 remote authority admin 能力的 `myflowhub-subproto/auth` 新版本，作为 Server 消费升级的上游前置。

#### Scope
- 必须：
  - 确认 `auth/v0.1.4` 之后的 auth 模块改动已具备发布条件
  - 对齐并验证 `auth` module 的依赖与测试
  - 推送分支并发布 `auth/v0.1.5`
- 可选：
  - 如验证发现依赖声明仍缺失，补齐最小必要 `go.mod/go.sum`
- 不做：
  - 不修改 `MyFlowHub-Win`
  - 不引入新的 auth 行为变更
  - 不在本轮新增 requirement/spec 真值文档

#### Use Cases
- Server 需要拉取包含 remote authority admin / permit list / semi-central authority policy / first-register bootstrap 的 auth 模块新版本。
- 下游需要在 `GOWORK=off` 场景解析到已发布 tag，而不是依赖本地 worktree。

#### Functional Requirements
- `auth/v0.1.4..HEAD` 的 auth 改动必须形成可发布 patch 版本。
- 新 tag 必须遵循单仓多 module 规则：`auth/vX.Y.Z`。
- 发布后下游必须可通过 semver 拉取该版本。

#### Non-functional Requirements
- 变更保持最小发布面，仅覆盖 auth 模块发版所需内容。
- 测试必须可审计，可复现，不能仅依赖主工作区 `go.work` 偶然通过。
- 发布顺序遵循依赖方向，避免下游解析到未发布上游。

#### Inputs / Outputs
- 输入：
  - 稳定需求：`D:\project\MyFlowHub3\docs\requirements\auth-controlled-admission.md`
  - 稳定规格：`D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\auth.md`
  - 经验：`D:\project\MyFlowHub3\docs\lessons\cross-repo-semver-release.md`
  - 当前源码：`auth/v0.1.4..HEAD`
- 输出：
  - 已 push 的分支 `origin/chore/remote-authority-auth-release`
  - 已 push 的 module tag `auth/v0.1.5`
  - 可供 Server 升级消费的发布版本

#### Edge Cases
- 主工作区 `go.work` 不能直接证明可发布；需防止被本地联调掩盖真实依赖问题。
- 若 `GOWORK=off` 直接测试失败，需要通过临时 `go.work` 验证 auth 与上游源码兼容，再判断是否需要补版本声明。
- 已存在 `auth/v0.1.4` tag，不允许重写；若发现发布问题只能继续升 patch。

#### Acceptance Criteria
- `auth` 模块完成最小必要修正并通过验证。
- 远端存在可解析的 `auth/v0.1.5` tag。
- 后续 Server worktree 可基于该 tag 继续完成依赖升级。

#### Risks
- 真实上游 semver 版本与本地源码之间可能仍有漂移。
- 若只在本地 workspace 验证，容易误判为可发布。
- tag 发布后不可回收，必须先完成最小充分验证。

#### Issue List
- 无

### Stage 2 - Architecture Design
#### Overall Solution
- 采用最小发布链方案：
  - 先在 `MyFlowHub-SubProto` worktree 内确认 auth 模块源码与依赖声明
  - 用临时 `go.work` 连接 `MyFlowHub-Core`、`MyFlowHub-Proto` 与本 worktree 的 `auth` 做本地真实源码验证
  - 如需要，再用发布版依赖做补充核验
  - 完成 commit / push / tag / push tag

#### Alternatives Considered
- 备选 1：直接发 tag，不补验证
  - 否决：风险过高，tag 不可重写
- 备选 2：先在 Server 里用 sibling replace 验证再回头发 tag
  - 否决：不符合依赖方向，也无法证明 semver 可解析

#### Module Responsibilities
- `MyFlowHub-SubProto/auth`
  - 提供 remote authority admin、permit list、authority policy、bootstrap 等 auth 行为实现
- `MyFlowHub-Core`
  - 提供 auth 依赖的权限/路由/配置基础能力
- `MyFlowHub-Proto`
  - 提供 auth 协议字段与动作契约
- `MyFlowHub-Server`
  - 下一阶段消费已发布的 auth tag，本计划只记录其依赖关系，不在此 worktree 中修改

#### Data / Call Flow
- 本地 auth 源码验证：
  - 临时 `go.work` -> `Core` + `Proto` + 当前 `auth` worktree
  - `go test ./... -count=1 -p 1`
- 发布验证：
  - push branch -> 创建 `auth/v0.1.5` annotated tag -> push tag
  - 在干净上下文执行 `go list -m github.com/yttydcs/myflowhub-subproto/auth@v0.1.5`

#### Interface Drafts
- 无新增接口；保持现有 auth module 对外 API 与协议契约

#### Error Handling and Safety
- 如果测试、push、tag 任一环节失败，停止进入下游 Server 升级。
- 不重写既有 tag；若出现问题，采用新 patch 版本修复。

#### Performance and Testing Strategy
- 仅做 module 级测试与版本解析验证，不引入额外运行时路径。
- 优先执行：
  - 临时 `go.work` 下 `go test ./... -count=1 -p 1`
  - tag 发布后 `go list -m ...@v0.1.5`

#### Extensibility Design Points
- 保持单仓多 module 发布规则，便于后续其他 subproto module 按同样方式发 patch。
- 计划与归档显式记录跨仓版本链，便于后续查错。

#### Issue List
- 无

### Stage 3.1 - Planning
#### Project Goal and Current State
- 目标：
  - 将 `auth/v0.1.4` 之后已合入的 remote authority admin 相关改动发布为 `auth/v0.1.5`
- 当前状态：
  - 分支与 worktree 已创建
  - `auth/v0.1.4..HEAD` 已包含 remote authority admin、permit list、semi-central authority policy、first-register bootstrap 等变更
  - 当前尚无本 worktree 的 `plan.md`
  - 最新现有 tag 为 `auth/v0.1.4`

#### Docs Governance Routing Decision
- 使用 `$m-docs` 完成路由与影响判断
- Requirements impact: `none`
- Specs impact: `none`
- Related requirements:
  - `D:\project\MyFlowHub3\docs\requirements\auth-controlled-admission.md`
- Related specs:
  - `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\auth.md`
- Related lessons:
  - `D:\project\MyFlowHub3\docs\lessons\cross-repo-semver-release.md`
- 结论：
  - 本轮属于既有需求/规格下的发版收口，不新增稳定真值
  - 本 worktree 根 `plan.md` 为控制面例外；完成后归档进入 `docs/change`

#### Related Requirements / Specs / Lessons
- Requirements:
  - `D:\project\MyFlowHub3\docs\requirements\auth-controlled-admission.md`
- Specs:
  - `D:\project\MyFlowHub3\repo\MyFlowHub-Server\docs\specs\auth.md`
- Lessons:
  - `D:\project\MyFlowHub3\docs\lessons\cross-repo-semver-release.md`

#### Executable Task List
- [ ] `AUTHREL-1` 校验 `auth/v0.1.4..HEAD` 的发布内容与依赖现状
- [ ] `AUTHREL-2` 按最小必要原则修正 `auth/go.mod` 与 `auth/go.sum`（如验证需要）
- [ ] `AUTHREL-3` 通过临时 `go.work` / 版本解析完成验证
- [ ] `AUTHREL-4` 提交、推送分支、创建并推送 `auth/v0.1.5`

#### Task Details
##### AUTHREL-1 - 校验 auth 发布面
- Owner: 主代理
- Worktree: `D:\project\MyFlowHub3\worktrees\chore-subproto-remote-authority-auth-release`
- Plan Path: `D:\project\MyFlowHub3\worktrees\chore-subproto-remote-authority-auth-release\plan.md`
- Goal:
  - 明确 `auth/v0.1.4` 之后的改动范围和本轮发布边界
- Files / Modules:
  - `auth/*`
- Write Set:
  - 只读，必要时转入 `AUTHREL-2`
- Acceptance:
  - 形成可审计的发布差异与边界说明
- Test Points:
  - `git log auth/v0.1.4..HEAD`
  - `git diff --stat auth/v0.1.4..HEAD -- auth`
- Rollback:
  - 无代码写入，无需回滚

##### AUTHREL-2 - 修正 auth 依赖声明
- Owner: 主代理
- Worktree: `D:\project\MyFlowHub3\worktrees\chore-subproto-remote-authority-auth-release`
- Plan Path: `D:\project\MyFlowHub3\worktrees\chore-subproto-remote-authority-auth-release\plan.md`
- Goal:
  - 在不扩大变更面的前提下，补齐 auth module 的最小必要依赖声明
- Files / Modules:
  - `auth/go.mod`
  - `auth/go.sum`
- Write Set:
  - `auth/go.mod`
  - `auth/go.sum`
- Acceptance:
  - 依赖版本与当前源码兼容，且不引入计划外升级
- Test Points:
  - `go test ./... -count=1 -p 1`
- Rollback:
  - 回退本次依赖声明提交

##### AUTHREL-3 - 验证 auth 可发布
- Owner: 主代理
- Worktree: `D:\project\MyFlowHub3\worktrees\chore-subproto-remote-authority-auth-release`
- Plan Path: `D:\project\MyFlowHub3\worktrees\chore-subproto-remote-authority-auth-release\plan.md`
- Goal:
  - 证明 auth 模块在真实依赖链下可测试、可发布、可解析
- Files / Modules:
  - 临时验证文件：`D:\project\MyFlowHub3\.tmp\remote-authority-auth-release\go.work`
- Write Set:
  - `.tmp` 下临时验证文件
- Acceptance:
  - 测试通过，tag 发布后可解析到 `v0.1.5`
- Test Points:
  - 临时 `go.work` 下 `go test ./... -count=1 -p 1`
  - `go list -m github.com/yttydcs/myflowhub-subproto/auth@v0.1.5`
- Rollback:
  - 删除临时验证文件；若测试失败则停止发版

##### AUTHREL-4 - 推送分支与发布 tag
- Owner: 主代理
- Worktree: `D:\project\MyFlowHub3\worktrees\chore-subproto-remote-authority-auth-release`
- Plan Path: `D:\project\MyFlowHub3\worktrees\chore-subproto-remote-authority-auth-release\plan.md`
- Goal:
  - 形成下游可消费的正式发布版本
- Files / Modules:
  - git branch / tag 元数据
- Write Set:
  - 当前 worktree 提交历史
  - 远端分支与 tag
- Acceptance:
  - `origin/chore/remote-authority-auth-release` 已推送
  - `auth/v0.1.5` 已推送
- Test Points:
  - `git push origin chore/remote-authority-auth-release`
  - `git push origin auth/v0.1.5`
- Rollback:
  - 未合并前可回退分支提交；已发布 tag 只能追加新 patch 修复

#### Dependencies
- 上游依赖：
  - `MyFlowHub-Core`
  - `MyFlowHub-Proto`
- 下游依赖：
  - `MyFlowHub-Server` worktree 依赖本 worktree 先完成 tag 发布

#### Risks and Notes
- `go.work` 联调通过不代表 semver 发布可用，必须做真实版本解析检查。
- 若 `auth/go.mod` 仍能保持较低最小版本且测试通过，不强制同步到 `Server` 当前版本，以避免无意义升级。
- 本轮不修改稳定 specs；如验证显示协议契约仍有漂移，需回到阶段 1/2。

#### Parallelism Assessment
- 当前不派发子 Agent。
- 原因：
  - 发布链是串行依赖：必须先完成 auth tag，再进入 Server 升级
  - 写集高度集中，主代理直接执行更低风险

#### Issue List
- 无

阻塞：否
进入 3.2
