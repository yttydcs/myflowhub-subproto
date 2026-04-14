package exec

// 本文件承载 SubProto 中 `exec` 模块里与 `types` 相关的逻辑。

import protocol "github.com/yttydcs/myflowhub-proto/protocol/exec"

// 子协议：exec（网络特殊能力调用）。
const SubProtoExec uint8 = protocol.SubProtoExec

const (
	actionCall     = protocol.ActionCall
	actionCallResp = protocol.ActionCallResp

	actionCapSnapshot  = protocol.ActionCapSnapshot
	actionCapUpsert    = protocol.ActionCapUpsert
	actionCapWithdraw  = protocol.ActionCapWithdraw
	actionCapHeartbeat = protocol.ActionCapHeartbeat
	actionCapSyncResp  = protocol.ActionCapSyncResp
	actionCapQuery     = protocol.ActionCapQuery
	actionCapQueryResp = protocol.ActionCapQueryResp
)

const (
	permExecCall     = protocol.PermExecCall
	permExecCapSync  = protocol.PermExecCapSync
	permExecCapQuery = protocol.PermExecCapQuery
)

type message = protocol.Message
type CallReq = protocol.CallReq
type CallResp = protocol.CallResp
type CapabilityDescriptor = protocol.CapabilityDescriptor
type CapabilityKey = protocol.CapabilityKey
type CapSnapshotReq = protocol.CapSnapshotReq
type CapUpsertReq = protocol.CapUpsertReq
type CapWithdrawReq = protocol.CapWithdrawReq
type CapHeartbeatReq = protocol.CapHeartbeatReq
type CapSyncResp = protocol.CapSyncResp
type CapQueryReq = protocol.CapQueryReq
type CapabilityRoute = protocol.CapabilityRoute
type CapQueryResp = protocol.CapQueryResp
