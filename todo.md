# Plan - MyFlowHub-SubProto：file 子协议新增 mkdir 操作

## Workflow 信息
- 仓库：`MyFlowHub-SubProto`
- 分支：`feat/file-mkdir-op`
- Worktree：`d:\project\MyFlowHub3\worktrees\MyFlowHub-SubProto-file-mkdir`
- Base：`main`
- 当前状态：已完成（待你确认是否结束 workflow）

## 项目目标与当前状态
- 目标：
  - 在 `SubProto=5 file` 中新增 `action=write, op=mkdir`，支持创建目录。
  - 保持现有 `offer/list/pull/read_text` 行为不变。
- 现状：
  - `handleWriteRequest` 仅支持 `op=offer`；
  - 无显式创建目录控制操作。

## 可执行任务清单（Checklist）

- [x] `MKDIR-1` 扩展 file 操作常量与写请求分发
  - 目标：识别 `op=mkdir` 并纳入 write 路由。
  - 涉及文件：
    - `file/types.go`
    - `file/handler.go`
  - 验收条件：
    - `handleWriteRequest` 能区分 `offer/mkdir`；
    - `mkdir` 走 `file.write` 权限链路。
  - 测试点：
    - 非法 op 仍返回 `invalid op`；
    - 既有 `offer` 行为无回归。
  - 回滚点：
    - 回滚上述文件对 `op=mkdir` 的分发逻辑。

- [x] `MKDIR-2` 新增本地 mkdir 处理逻辑（安全默认）
  - 目标：实现 `handleMkdirLocal`，仅允许在 `BaseDir` 下创建目录。
  - 涉及文件：
    - `file/handler.go`
    - `file/path.go`（如需复用/补充路径辅助函数）
  - 验收条件：
    - `dir/name` 均做校验；
    - 同名文件冲突返回明确错误；
    - 成功返回 `code=1`。
  - 测试点：
    - 成功创建；
    - 已存在目录幂等处理；
    - 非法输入拦截。
  - 回滚点：
    - 回滚 `handleMkdirLocal` 及调用路径。

- [x] `MKDIR-3` 补充测试覆盖
  - 目标：新增 mkdir 关键路径测试，确保回归可控。
  - 涉及文件：
    - `file/handler_mkdir_test.go`（新增）
  - 验收条件：
    - go test 通过；
    - 关键边界（合法/非法/冲突）被覆盖。
  - 回滚点：
    - 删除新增测试文件。

- [x] `MKDIR-4` 文档与变更归档
  - 目标：归档本次协议变更（请求/响应语义、错误码、兼容性）。
  - 涉及文件：
    - `docs/change/2026-03-05_file-mkdir-op.md`（新增）
  - 验收条件：
    - 文档包含目标、变更细节、测试结果、回滚方案。
  - 回滚点：
    - 仅文档回滚，无运行时影响。

## 依赖关系
- `MKDIR-1` -> `MKDIR-2` -> `MKDIR-3` -> `MKDIR-4`

## 风险与注意事项
- `WriteReq` 原先为 `offer` 设计，`mkdir` 复用时需明确“非相关字段忽略”。
- 目录创建应保持路径安全，禁止绝对路径/`..` 越界。
- 必须保证 wire 兼容：不破坏既有字段与 action 命名。
