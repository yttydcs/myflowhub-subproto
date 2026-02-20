# 2026-02-20 - SubProto：拆剩余子协议为独立 Go module（含 broker）

## 变更背景 / 目标

此前 `MyFlowHub-Server` 仓库内仍包含多项子协议实现（`subproto/auth|varstore|file|forward|exec|flow`），导致：
- Server 既做“运行时装配层”，又承载了大量子协议实现细节，边界不够硬；
- 子协议在源代码层难以“自由裁切与组装”（未来需要按需组合时成本高）；
- `flow -> exec` 的“同进程等待/投递”能力位于 Server 私有包（`internal/broker`），阻碍 `exec/flow` 拆成独立 module。

本次变更目标：
1) 采用 A2（单仓多 module）在 `MyFlowHub-SubProto` 中一次性新增并发布剩余子协议 module：
   - `auth`、`varstore`、`file`、`forward`、`exec`、`flow`（均首发 `v0.1.0`）。
2) 新增共享 module：`broker`（`broker/v0.1.0`），用于承载 `exec/flow` 依赖的“同进程投递器”，彻底移除对 Server 私有包的依赖。
3) **wire 不改**：SubProto 值 / Action 字符串 / JSON struct / HeaderTcp 语义与路由规则均保持不变（仅代码归属与依赖边界调整）。

## 总体方案（A2：单仓多 module）

- 每个子协议一个目录 + 独立 `go.mod`：
  - `broker/`、`auth/`、`varstore/`、`file/`、`forward/`、`exec/`、`flow/`
- 版本 tag 形如：`<moduleDir>/vX.Y.Z`，对应 module path：
  - 例如：`auth/v0.1.0` ↔ `github.com/yttydcs/myflowhub-subproto/auth`

## 具体变更内容

### 1) 新增共享 module：`broker`

- Module：`github.com/yttydcs/myflowhub-subproto/broker`
- Tag：`broker/v0.1.0`
- 依赖：`myflowhub-proto`（仅类型依赖，wire 无关）
- 内容：
  - `Broker[T]`：进程内 `reqID -> 响应` 投递器（`Register/Deliver`）
  - `SharedExecCallBroker()`：为 `exec.call_resp` 提供本进程共享投递器（类型使用 `myflowhub-proto/protocol/exec.CallResp`）

**说明（语义边界）**
- 这是“同进程等待/唤醒”，不是网络 pending；网络响应仍通过 Core 路由到达目标节点后再进入该投递器。
- 通道缓冲 1：避免 Deliver 侧阻塞；Deliver 后关闭通道提示完成。

### 2) 新增子协议 modules（从 Server 迁入，行为不变）

> 下列 module 均遵循依赖约束：只依赖 `myflowhub-core` + `myflowhub-proto`（以及标准库）；禁止依赖 `myflowhub-server` / `myflowhub-win`。

- `auth`：
  - Module：`github.com/yttydcs/myflowhub-subproto/auth`
  - Tag：`auth/v0.1.0`
  - 来源：`MyFlowHub-Server/subproto/auth`
- `varstore`：
  - Module：`github.com/yttydcs/myflowhub-subproto/varstore`
  - Tag：`varstore/v0.1.0`
  - 来源：`MyFlowHub-Server/subproto/varstore`
- `file`：
  - Module：`github.com/yttydcs/myflowhub-subproto/file`
  - Tag：`file/v0.1.0`
  - 来源：`MyFlowHub-Server/subproto/file`
- `forward`：
  - Module：`github.com/yttydcs/myflowhub-subproto/forward`
  - Tag：`forward/v0.1.0`
  - 来源：`MyFlowHub-Server/subproto/forward`
- `exec`：
  - Module：`github.com/yttydcs/myflowhub-subproto/exec`
  - Tag：`exec/v0.1.0`
  - 来源：`MyFlowHub-Server/subproto/exec`
  - 关键调整：将 `myflowhub-server/internal/broker` 替换为 `myflowhub-subproto/broker`
- `flow`：
  - Module：`github.com/yttydcs/myflowhub-subproto/flow`
  - Tag：`flow/v0.1.0`
  - 来源：`MyFlowHub-Server/subproto/flow`
  - 关键调整：
    - 将 `myflowhub-server/internal/broker` 替换为 `myflowhub-subproto/broker`
    - 将 `myflowhub-server/protocol/exec` 替换为 `myflowhub-proto/protocol/exec`（避免依赖 Server 兼容壳）

## 对 `MyFlowHub-Server` 的要求（协同变更）

Server 侧需要：
- `go.mod` 增加上述 module 依赖（均为 `v0.1.0`）；
- `modules/defaultset` 与 `tests` 的 import 切换到 `myflowhub-subproto/*`；
- 删除 Server 内置的：
  - `subproto/auth|varstore|file|forward|exec|flow`
  - `internal/broker`

对应 Server 侧归档文档（短引用）：
- `MyFlowHub-Server/docs/change/2026-02-20_server-use-subproto-remaining-modules.md`

## 测试与验证

统一约束：验收使用 `GOWORK=off`，避免本地 `go.work` 干扰审计。

建议命令（逐 module 在各自目录执行）：
```powershell
$env:GOTMPDIR='d:\\project\\MyFlowHub3\\.tmp\\gotmp'
New-Item -ItemType Directory -Force -Path $env:GOTMPDIR | Out-Null
$env:GOWORK='off'
go test ./... -count=1 -p 1
```

结果：`broker/auth/varstore/file/forward/exec/flow` 均 `go test` 通过。

## 潜在影响

- 依赖方（例如 Server）需要显式依赖子协议 module 的 semver 版本；不再通过 “Server 内置 subproto 目录”间接获得实现。
- 子协议拆分后，跨协议共享能力必须通过明确的 shared module（例如本次的 `broker`）承载，避免回退到 Server 私有包，防止边界塌陷。

## 回滚方案

- Server 侧回滚：
  - revert “依赖切换 + 删除目录”提交，恢复 `subproto/*` 与 `internal/broker`（风险：重新引入边界耦合）。
- SubProto 侧回滚：
  - 若某个 module 发布后发现缺陷：按 semver 发布补丁版本（例如 `exec/v0.1.1`），避免重写已发布 tag。

## 计划任务映射

- SUBALL0：归档旧 plan（已完成）
- SUBALL1：新增 `broker` module（已完成，tag：`broker/v0.1.0`）
- SUBALL2~SUBALL7：新增并迁入 `auth/varstore/file/forward/exec/flow`（已完成，tags：`*/v0.1.0`）
- SUBALL8：逐 module `GOWORK=off go test`（已完成）
- SUBALL9：发布 tags 并 push（已完成）

