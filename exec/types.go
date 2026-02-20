package exec

import protocol "github.com/yttydcs/myflowhub-proto/protocol/exec"

// 子协议：exec（网络特殊能力调用）。
const SubProtoExec uint8 = protocol.SubProtoExec

const (
	actionCall     = protocol.ActionCall
	actionCallResp = protocol.ActionCallResp
)

const permExecCall = protocol.PermExecCall

type message = protocol.Message
type CallReq = protocol.CallReq
type CallResp = protocol.CallResp
