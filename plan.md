# Plan - SubProto(file)：Hub（node1）File Console root list 报 not found 修复（BaseDir exeDir 解析 + 自动创建）

## Workflow 信息
- Repo：`MyFlowHub-SubProto`
- 分支：`fix/hub-file-console-base-dir`
- Worktree：`d:\project\MyFlowHub3\worktrees\fix-hub-file-console-base-dir\MyFlowHub-SubProto`
- Base：`main`
- 关联仓库（同一 workflow）：`MyFlowHub-Server`（同名分支/独占 worktree）
- 参考：
  - `d:\project\MyFlowHub3\repos.md`
  - `d:\project\MyFlowHub3\guide.md`（commit 信息中文）

## 背景 / 问题陈述（事实，可审计）
- Win 的 File Console 访问 Hub（node1）时提示：`not found`。
- Hub 侧使用 `myflowhub-subproto/file` 处理 list：
  - 默认配置 `file.base_dir=./file`；
  - `handleListLocal` 对 `BaseDir + dir` 执行 `os.ReadDir`；
  - 当 `./file` 目录不存在时，`os.ReadDir` 失败被统一映射为 `404 not found`。
- 额外风险：`file.base_dir` 为相对路径时以 CWD 为基准，`hub_server` 从不同目录启动可能导致 `file/` 目录散落到不同位置（与你“目录最好在软件同一目录下”的诉求冲突）。

## 目标
1) Hub（node1）root list（`dir==""`）在 `BaseDir` 不存在时：自动创建目录并返回空列表（成功）。
2) `file.base_dir` 为相对路径时：以 **可执行文件目录（exeDir）** 为基准解析，默认落点稳定为 `exeDir/file`。
3) 发布 `github.com/yttydcs/myflowhub-subproto/file v0.1.1`（tag：`file/v0.1.1`），供 Server 升级依赖。

## 非目标
- 不改 wire：SubProto 值 / Action 字符串 / JSON schema 不变。
- 不改变非 root 子目录的语义：`dir!="“` 且目录不存在时，仍返回 `not found`（不隐式创建任意子目录）。
- 不引入“浏览整机文件系统”；仍以 `BaseDir` 为沙箱根目录。

## 约束（边界）
- 仅修改 `myflowhub-subproto/file` module。
- 测试与验收命令统一使用 `GOWORK=off`（避免本地 `go.work` 干扰可审计性）。

## 验收标准
- Windows 下直接运行 `hub_server`（未配置 `file.base_dir`）：
  - 首次打开 Hub（node1）File Console 根目录：不再 `not found`，返回空列表；
  - 自动在 `hub_server.exe` 同目录创建 `file/`。
- 单测通过：
  - `cd file; $env:GOWORK='off'; go test ./... -count=1 -p 1`
- 发布可用：
  - tag `file/v0.1.1` 已 push；
  - `go list -m github.com/yttydcs/myflowhub-subproto/file@v0.1.1` 可解析。

---

## 3.1) 计划拆分（Checklist）

### FILE0 - 归档旧 plan（已执行）
- 目标：避免历史 plan 覆盖本 workflow。
- 已执行：`git mv plan.md docs/plan_archive/plan_archive_2026-03-04_subproto-management-children-only.md`
- 验收条件：归档文件存在且可阅读。
- 回滚点：撤销该 `git mv`。

### FILE1 - 配置：BaseDir 相对路径以 exeDir 为基准解析
**目标**
- `file.base_dir` 为相对路径（例如 `./file`）时，运行期解析为 `exeDir/<base_dir>`，避免随 CWD 漂移。

**涉及模块 / 文件**
- `file/config.go`（新增解析函数/缓存）

**验收条件**
- 同一进程内切换 CWD，不影响 `BaseDir` 的最终解析结果。

**测试点**
- 单测覆盖：相对路径解析到 `os.Executable()` 目录；绝对路径保持不变。

**回滚点**
- revert 本任务提交。

### FILE2 - 行为：root list 自动创建 BaseDir
**目标**
- `dir==""`（root list）时，若 `BaseDir` 不存在则 `MkdirAll(BaseDir)`，并返回空列表（成功）。

**涉及模块 / 文件**
- `file/handler.go`（`handleListLocal`）
- （如需提高可测性）抽取最小的本地 list helper（同 module 内新文件）

**验收条件**
- root list 不再把 “BaseDir 不存在” 映射为 `404 not found`。

**测试点**
- 单测覆盖：BaseDir 不存在时可被创建；返回 code 为 ok 且 dirs/files 为空。

**回滚点**
- revert 本任务提交。

### FILE3 - 测试：补充关键路径单测
**目标**
- 覆盖 BaseDir 解析与 root list 自动创建的关键路径，防回归。

**涉及模块 / 文件**
- `file/*_test.go`（新增）

**验收条件**
- `cd file; $env:GOWORK='off'; go test ./... -count=1 -p 1` 通过。

**回滚点**
- revert 测试提交（不影响功能）。

### FILE4 - 发布：tag `file/v0.1.1`
**目标**
- 发布可被 Server 拉取的版本。

**步骤**
1) 合入上述改动并 push 分支
2) 打 tag：`file/v0.1.1`
3) push tag：`git push origin file/v0.1.1`

**验收条件**
- tag 在本地与远端均存在；`go list -m ...@v0.1.1` 可解析。

**回滚点**
- 已发布 tag 不建议删除；如需回滚，发布 `file/v0.1.2` 修正或在 Server 回退依赖版本。

### FILE5 - Code Review（强制）+ 归档变更（强制）
**目标**
- 完成审查清单并输出 `docs/change/YYYY-MM-DD_*.md`。

**验收条件**
- Review 通过；变更文档包含任务映射、关键权衡（尤其 BaseDir 解析策略）、验证结果与回滚方案。

