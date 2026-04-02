package flow

import protocol "github.com/yttydcs/myflowhub-proto/protocol/flow"

// 子协议：flow（DAG 工作流调度）。
const SubProtoFlow uint8 = protocol.SubProtoFlow

const (
	actionSet           = protocol.ActionSet
	actionSetResp       = protocol.ActionSetResp
	actionDelete        = protocol.ActionDelete
	actionDeleteResp    = protocol.ActionDeleteResp
	actionRun           = protocol.ActionRun
	actionRunResp       = protocol.ActionRunResp
	actionCancelRun     = protocol.ActionCancelRun
	actionCancelRunResp = protocol.ActionCancelRunResp
	actionStatus        = protocol.ActionStatus
	actionStatusResp    = protocol.ActionStatusResp
	actionDetail        = protocol.ActionDetail
	actionDetailResp    = protocol.ActionDetailResp
	actionListRuns      = protocol.ActionListRuns
	actionListRunsResp  = protocol.ActionListRunsResp
	actionList          = protocol.ActionList
	actionListResp      = protocol.ActionListResp
	actionGet           = protocol.ActionGet
	actionGetResp       = protocol.ActionGetResp
)

const (
	permFlowSet    = protocol.PermFlowSet
	permFlowDelete = protocol.PermFlowDelete
	permFlowRun    = protocol.PermFlowRun
	permFlowRead   = protocol.PermFlowRead
)

type message = protocol.Message
type trigger = protocol.Trigger
type graph = protocol.Graph
type node = protocol.Node
type edge = protocol.Edge
type setReq = protocol.SetReq
type setResp = protocol.SetResp
type deleteReq = protocol.DeleteReq
type deleteResp = protocol.DeleteResp
type runReq = protocol.RunReq
type runResp = protocol.RunResp
type cancelRunReq = protocol.CancelRunReq
type cancelRunResp = protocol.CancelRunResp
type statusReq = protocol.StatusReq
type nodeStatus = protocol.NodeStatus
type statusResp = protocol.StatusResp
type detailReq = protocol.DetailReq
type detailResp = protocol.DetailResp
type listRunsReq = protocol.ListRunsReq
type runSummary = protocol.RunSummary
type listRunsResp = protocol.ListRunsResp
type listReq = protocol.ListReq
type flowSummary = protocol.FlowSummary
type listResp = protocol.ListResp
type getReq = protocol.GetReq
type getResp = protocol.GetResp
