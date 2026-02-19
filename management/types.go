package management

import protocol "github.com/yttydcs/myflowhub-proto/protocol/management"

const SubProtoManagement uint8 = protocol.SubProtoManagement

const (
	actionNodeEcho        = protocol.ActionNodeEcho
	actionNodeEchoResp    = protocol.ActionNodeEchoResp
	actionListNodes       = protocol.ActionListNodes
	actionListNodesResp   = protocol.ActionListNodesResp
	actionListSubtree     = protocol.ActionListSubtree
	actionListSubtreeResp = protocol.ActionListSubtreeResp
	actionConfigGet       = protocol.ActionConfigGet
	actionConfigGetResp   = protocol.ActionConfigGetResp
	actionConfigSet       = protocol.ActionConfigSet
	actionConfigSetResp   = protocol.ActionConfigSetResp
	actionConfigList      = protocol.ActionConfigList
	actionConfigListResp  = protocol.ActionConfigListResp
)

type mgmtMessage = protocol.Message
type nodeEchoReq = protocol.NodeEchoReq
type nodeEchoResp = protocol.NodeEchoResp
type listNodesReq = protocol.ListNodesReq
type listNodesResp = protocol.ListNodesResp
type configGetReq = protocol.ConfigGetReq
type configSetReq = protocol.ConfigSetReq
type configResp = protocol.ConfigResp
type configListReq = protocol.ConfigListReq
type configListResp = protocol.ConfigListResp
type nodeInfo = protocol.NodeInfo
type listSubtreeReq = protocol.ListSubtreeReq
type listSubtreeResp = protocol.ListSubtreeResp

