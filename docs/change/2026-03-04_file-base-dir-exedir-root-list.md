# MyFlowHub-SubProto（file）：Hub File Console root list 修复（BaseDir exeDir 解析 + 自动创建）

## 变更背景 / 目标

### 现象
- Win 的 File Console 访问 Hub（node1）时提示：`not found`。

### 根因（事实，可审计）
- `myflowhub-subproto/file` 的 `handleListLocal` 直接对 `BaseDir + dir` 执行 `os.ReadDir(...)`。
- 默认配置 `file.base_dir=./file`，首次运行时该目录通常不存在，导致 `os.ReadDir` 报错并被映射为 `404 not found`。
- 同时，`file.base_dir` 为相对路径时以 **进程 CWD** 为基准，`hub_server` 从不同目录启动会导致 `file/` 目录散落（与“目录最好在软件同一目录下”的诉求冲突）。

### 目标
1) root list（`dir==""`）在 `BaseDir` 不存在时：自动创建目录并返回空列表（成功）。
2) `file.base_dir` 为相对路径时：以 **可执行文件目录（exeDir）** 为基准解析，默认落点稳定为 `exeDir/file`。
3) 发布版本：tag `file/v0.1.1`，供 `MyFlowHub-Server` 升级依赖。

---

## 具体变更内容

### 1) BaseDir 运行时解析（相对路径 → exeDir 基准）
- 文件：`file/config.go`
- 新增：
  - `executableDir()`：通过 `os.Executable()` 获取并缓存 `exeDir`
  - `resolveRuntimeBaseDir(baseDir string)`：绝对路径保持不变；相对路径拼接为 `exeDir/baseDir`
- 调整：`loadConfig(...)` 的 `BaseDir` 读取后统一经过 `resolveRuntimeBaseDir(...)`

**关键决策 / 权衡**
- 选择 exeDir 作为相对路径基准，避免 CWD 漂移导致目录散落。
- 行为变化：相对路径不再以 CWD 为基准（见“潜在影响”）。

### 2) root list 自动创建 BaseDir（仅限根目录）
- 文件：`file/handler.go`
- 调整：`handleListLocal(...)`
  - 当 `dir==""`（root list）时，先 `os.MkdirAll(root, 0o755)` 再 `os.ReadDir(root)`
  - 若创建失败：返回 `Code=500 Msg="mkdir failed"`，并记录 warn 日志（不把路径下发到远端）
  - 若 root `ReadDir` 失败：返回 `Code=500 Msg="read failed"`，并记录 warn 日志
  - 非 root 子目录：保持现有语义（`ReadDir` 失败 → `404 not found`），**不隐式创建任意子目录**

**关键决策 / 权衡**
- 只在 root list 创建 `BaseDir`，避免“列目录即隐式创建任意目录”的安全/语义问题。

### 3) 测试覆盖
- 文件：`file/config_test.go`
  - 覆盖：默认 `./file` 与显式相对路径会解析到 `exeDir/file`
  - 覆盖：绝对 `base_dir` 保持不变
- 文件：`file/handler_list_test.go`
  - 覆盖：root list 会创建缺失的 `BaseDir`
  - 覆盖：列子目录不会创建 `BaseDir`

### 4) 发布
- 新增 tag：`file/v0.1.1`

---

## 对应 plan.md 任务映射
- FILE0：归档旧 plan 并建立本次计划
- FILE1：BaseDir 相对路径以 exeDir 为基准解析
- FILE2：root list 自动创建 BaseDir
- FILE3：补充关键路径单测
- FILE4：发布 tag `file/v0.1.1`
- FILE5：Code Review + 归档变更（本文）

---

## 测试与验证
- 单测：
  - `cd file; $env:GOWORK='off'; go test ./... -count=1 -p 1`
  - 结果：通过

---

## 潜在影响与回滚方案

### 潜在影响
- **行为变化**：`file.base_dir` 为相对路径时，不再以 CWD 为基准，而是以 exeDir 为基准。
- 若 `hub_server.exe` 所在目录不可写（例如放在受保护目录），root list 可能返回 `500 mkdir failed`。此时应显式配置：
  - `file.base_dir` 为可写的绝对路径（推荐）。

### 回滚方案
- `MyFlowHub-Server` 可将依赖回退到 `github.com/yttydcs/myflowhub-subproto/file v0.1.0`（恢复旧行为）。
- 若已发布 tag 不建议删除；如需修正，发布新版本（例如 `file/v0.1.2`）进行前进修复。

