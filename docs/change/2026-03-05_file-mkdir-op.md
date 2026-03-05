# 2026-03-05 - SubProto/file：新增 `op=mkdir`（创建目录）

## 变更背景 / 目标
- 背景：File 子协议现有 write 仅支持 `op=offer`，缺少“显式创建目录”控制操作。
- 目标：在不改变既有 wire action（`write/write_resp`）的前提下，新增 `op=mkdir`，支持在目标节点 `BaseDir` 下创建目录。

## 具体变更内容（新增 / 修改 / 删除）

### 修改
- `file/types.go`
  - 新增操作常量：`opMkdir = "mkdir"`。

- `file/handler.go`
  - `handleWriteRequest` 从“仅支持 offer”扩展为 `switch`：
    - `op=offer`：保持现有逻辑；
    - `op=mkdir`：走 `file.write` 权限链路与 `routeCtrlRequest`；
    - 其他 op：仍返回 `invalid op`。
  - 新增 `handleMkdirLocal`：
    - 本地执行目录创建并回 `write_resp(op=mkdir)`；
    - 成功时返回 `provider/consumer`；失败返回明确错误码和消息。

### 新增
- `file/mkdir.go`
  - 新增 `mkdirLocalDir(baseDir, dir, name)`：
    - 校验 `dir/name`（复用 `sanitizeDir/sanitizeName`）；
    - 通过 `resolvePaths` 保证路径落在 `BaseDir` 范围内；
    - 处理目录创建、已存在目录幂等、同名文件冲突与异常错误映射。

- `file/mkdir_test.go`
  - 覆盖关键场景：
    - 正常创建成功；
    - 已存在目录幂等成功；
    - 同名文件冲突（409 exists）；
    - 非法输入（400）。

### 删除
- 无。

## 对应任务映射（todo.md）
- `MKDIR-1` 扩展操作常量与写请求分发：✅
- `MKDIR-2` 本地 mkdir 处理逻辑：✅
- `MKDIR-3` 测试覆盖：✅
- `MKDIR-4` 归档：✅（本文）

## 关键设计决策与权衡（性能 / 扩展性）
- 复用 `write` action，不新增 action 字符串：
  - 降低 wire 变更面与兼容风险；
  - 新能力通过 `op` 扩展，保持同一路由/权限模型。
- 路径安全优先：
  - 统一走 `sanitize + resolvePaths`，避免目录穿越。
- 幂等语义：
  - 目录已存在返回成功（`code=1`），减少重复创建失败噪音；
  - 与“同名普通文件存在”区分为冲突（`409 exists`）。

## 测试与验证方式 / 结果
```powershell
cd d:\project\MyFlowHub3\worktrees\MyFlowHub-SubProto-file-mkdir\file
$env:GOWORK='off'
go test ./... -count=1
```
- 结果：通过。

## 潜在影响与回滚方案

### 潜在影响
- 客户端若发送 `op=mkdir` 但未提供 `target`，会返回 `400 target required`。
- 协议说明文档（Server 仓 `docs/5-file.md`）应在后续同步补充 `mkdir`，避免文档滞后。

### 回滚方案
- 回滚以下文件即可撤销本次能力：
  - `file/types.go`
  - `file/handler.go`
  - `file/mkdir.go`
  - `file/mkdir_test.go`

