# 2026-03-21 Flow ID 防护与契约对齐（SubProto）

## 变更背景 / 目标
- 背景：
  - `flow` 子协议已在实现和 proto 中稳定支持 `run/status/list/get`，但对外契约文档未完整展开。
  - `flow_id` 在实现里只做了非空校验，随后直接参与 `<baseDir>/<flow_id>.json` 路径拼接，存在路径穿越风险。
  - DAG 节点写入契约已在实现侧收敛到 `kind=call`，但文档仍保留 `local/exec` 作为正式格式，存在 drift。
- 目标：
  - 修补 `flow_id` 安全校验。
  - 固化“新写入 call-only、历史 local/exec 仅运行兼容”的实现边界。
  - 为 Server 文档补契约提供实现侧对齐依据。

## 具体变更内容（新增 / 修改 / 删除）
### 新增
- `flow/flow_id.go`
  - 新增 `validateFlowID(...)`：统一校验 `flow_id` 为 UUID。
  - 新增 `flowFilePath(...)`：集中生成安全的落盘文件路径。
- `flow/flow_id_test.go`
  - 覆盖合法 UUID、非法 `flow_id`、磁盘加载跳过非法 ID、历史 `local` 数据兼容加载。

### 修改
- `flow/handler.go`
  - `handleSet/delete/run/status/get` 统一复用 `flow_id` 校验。
  - capability `flow::run` 入口改为校验 `flow_id`。
  - `applySetLocal/applyDeleteLocal` 改为复用安全路径 helper。
  - `loadFlowsFromDisk` 改为跳过非法 `flow_id`。
  - 补充注释，明确运行时兼容 `local/exec` 只是历史兼容，不是新写入契约。
- `flow/delete_test.go`
  - 用合法 UUID 更新测试样例，和正式契约对齐。
- `flow/capability_provider_test.go`
  - 用合法 UUID 更新 capability 运行测试样例。
- `flow/resp_ids_test.go`
  - 用合法 UUID 更新响应测试样例。
- `flow/go.sum`
  - 补齐 `github.com/yttydcs/myflowhub-subproto/exec v0.1.1` 的校验条目。

### 删除
- 无。

## 对应 plan.md 任务映射
- `DOC-1`：完成（作为 Server 文档对齐的实现依据）。
- `DOC-2`：完成（实现边界已固定为“写入 call-only，运行兼容旧数据”）。
- `IMPL-1`：完成（`flow_id` UUID 校验 + 安全路径）。
- `IMPL-2`：完成（兼容边界在实现和测试中钉死）。
- `TEST-1`：完成（新增/调整测试并完成回归）。

## 关键设计决策与权衡（尤其性能 / 扩展性）
1. 采用 UUID 白名单校验，而不是“禁用 `../`”式黑名单。
   - 优点：规则更硬、更容易审计，后续不会因为分隔符、空白、大小写等边角输入不断补丁。
2. 新增集中 helper，而不是在各 handler 内手写判断。
   - 优点：入口一致，后续若 ID 规则调整，只需要改一处。
3. 保留 `local/exec` 运行期兼容，不回退到“允许继续写旧格式”。
   - 优点：历史落盘数据仍可运行，同时阻止旧格式继续扩散。
4. 不在本次顺手修改正式依赖版本。
   - 原因：`exec/proto` 的本地代码与已发布 tag 之间存在既有差距，这属于独立依赖治理问题；本次只记录并在验证阶段采用临时测试 modfile 解决。

## 测试与验证方式 / 结果
- 代码格式化：
  - `gofmt -w flow/handler.go flow/flow_id.go flow/flow_id_test.go flow/delete_test.go flow/capability_provider_test.go flow/resp_ids_test.go`
- 模块回归：
  - 目录：`D:\project\MyFlowHub3\worktrees\subproto-id-guard\flow`
  - 命令：
    - `$env:GOWORK='off'`
    - `$env:GOTMPDIR='d:\project\MyFlowHub3\.tmp\gotmp'`
    - `go test -mod=mod -modfile go.test.mod ./... -count=1 -p 1`
  - 结果：通过。
- 验证说明：
  - `go.test.mod` 为临时测试文件，仅用于将 `exec` / `proto` 指向本地仓库；验证后已删除，没有进入正式变更集。

## 潜在影响与回滚方案
### 潜在影响
- 非 UUID 的 `flow_id` 现在会被明确拒绝，旧客户端若继续提交 `flow-1` 这类 ID，会收到 `400`。
- 启动阶段会跳过非法 `flow_id` 的磁盘数据，这会让恶意或脏数据不再进入内存。
- 正式 `go test ./...` 仍受已发布 `exec/proto` tag 落后于本地代码的既有问题影响；本次未扩大修复范围。

### 回滚方案
- 回滚 `flow/flow_id.go`、`flow/handler.go`、相关测试与 `flow/go.sum` 即可。
- 若需临时恢复旧行为，可先回滚 `flow_id` 校验改动；但这会重新暴露路径拼接风险，不建议长期保留。

## 子Agent执行轨迹
- 无子Agent。
- Task ID → Agent → Worktree → 文件 → 验收结果：
  - `IMPL-1` → 主Agent → `D:\project\MyFlowHub3\worktrees\subproto-id-guard` → `flow/handler.go`, `flow/flow_id.go`, `flow/flow_id_test.go` → 通过
  - `IMPL-2` → 主Agent → `D:\project\MyFlowHub3\worktrees\subproto-id-guard` → `flow/handler.go`, `flow/flow_id_test.go`, `flow/delete_test.go`, `flow/capability_provider_test.go`, `flow/resp_ids_test.go` → 通过
  - `TEST-1` → 主Agent → `D:\project\MyFlowHub3\worktrees\subproto-id-guard` → `flow/*_test.go`, `flow/go.sum` → 通过
