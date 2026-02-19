# MyFlowHub-SubProto

本仓库用于承载 **MyFlowHub 子协议实现（SubProto Implementations）**，采用 **单仓多 Go module（A2）** 的组织方式：
- 每个子协议一个独立目录（例如 `management/`），并拥有自己的 `go.mod`；
- Server 以 Go module 依赖的方式按需组合这些子协议（可裁切/可组装）；
- 子协议实现 **只依赖**：
  - `github.com/yttydcs/myflowhub-core`（框架与运行时能力）
  - `github.com/yttydcs/myflowhub-proto`（协议字典）

## 目录结构
- `management/`：management 子协议（Go module：`github.com/yttydcs/myflowhub-subproto/management`）
- `docs/change/`：变更归档（按日期命名）

## 版本与发布（重要）
本仓库是多 module 仓库，因此 **tag 必须带模块前缀**：
- management module 的发布 tag：`management/v0.1.0`

示例（在仓库根目录）：
```bash
git tag -a management/v0.1.0 -m "发布 management v0.1.0"
git push origin management/v0.1.0
```

> 上游（例如 `myflowhub-server`）在 `go.mod` 中依赖时写：  
> `github.com/yttydcs/myflowhub-subproto/management v0.1.0`

## 本地验证
```bash
cd management
GOWORK=off go test ./... -count=1 -p 1
```

