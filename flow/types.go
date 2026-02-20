package flow

import protocol "github.com/yttydcs/myflowhub-proto/protocol/flow"

// 子协议：flow（DAG 工作流调度）。
const SubProtoFlow uint8 = protocol.SubProtoFlow

const (
	actionSet        = protocol.ActionSet
	actionSetResp    = protocol.ActionSetResp
	actionRun        = protocol.ActionRun
	actionRunResp    = protocol.ActionRunResp
	actionStatus     = protocol.ActionStatus
	actionStatusResp = protocol.ActionStatusResp
	actionList       = protocol.ActionList
	actionListResp   = protocol.ActionListResp
	actionGet        = protocol.ActionGet
	actionGetResp    = protocol.ActionGetResp
)

const permFlowSet = protocol.PermFlowSet

type message = protocol.Message
type trigger = protocol.Trigger
type graph = protocol.Graph
type node = protocol.Node
type edge = protocol.Edge
type setReq = protocol.SetReq
type setResp = protocol.SetResp
type runReq = protocol.RunReq
type runResp = protocol.RunResp
type statusReq = protocol.StatusReq
type nodeStatus = protocol.NodeStatus
type statusResp = protocol.StatusResp
type listReq = protocol.ListReq
type flowSummary = protocol.FlowSummary
type listResp = protocol.ListResp
type getReq = protocol.GetReq
type getResp = protocol.GetResp
