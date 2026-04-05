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
type nodeKind = protocol.NodeKind
type bindingSourceKind = protocol.BindingSourceKind
type branchMatchOp = protocol.BranchMatchOp
type inputBinding = protocol.InputBinding
type bindingSource = protocol.BindingSource
type callSpec = protocol.CallSpec
type composeSpec = protocol.ComposeSpec
type setVarSpec = protocol.SetVarSpec
type transformExpr = protocol.TransformExpr
type transformSpec = protocol.TransformSpec
type branchMatch = protocol.BranchMatch
type branchCase = protocol.BranchCase
type branchSpec = protocol.BranchSpec
type foreachSpec = protocol.ForeachSpec
type subflowSpec = protocol.SubflowSpec

const (
	nodeKindCall      = protocol.NodeKindCall
	nodeKindCompose   = protocol.NodeKindCompose
	nodeKindTransform = protocol.NodeKindTransform
	nodeKindSetVar    = protocol.NodeKindSetVar
	nodeKindBranch    = protocol.NodeKindBranch
	nodeKindForeach   = protocol.NodeKindForeach
	nodeKindSubflow   = protocol.NodeKindSubflow
)

const (
	bindingSourceNodeResult = protocol.BindingSourceNodeResult
	bindingSourceTrigger    = protocol.BindingSourceTrigger
	bindingSourceFlowMeta   = protocol.BindingSourceFlowMeta
	bindingSourceRunMeta    = protocol.BindingSourceRunMeta
	bindingSourceLoopItem   = protocol.BindingSourceLoopItem
	bindingSourceLoopIndex  = protocol.BindingSourceLoopIndex
	bindingSourceFlowVar    = protocol.BindingSourceFlowVar
)

const (
	branchMatchEq     = protocol.BranchMatchEq
	branchMatchNe     = protocol.BranchMatchNe
	branchMatchGt     = protocol.BranchMatchGt
	branchMatchGte    = protocol.BranchMatchGte
	branchMatchLt     = protocol.BranchMatchLt
	branchMatchLte    = protocol.BranchMatchLte
	branchMatchExists = protocol.BranchMatchExists
)
