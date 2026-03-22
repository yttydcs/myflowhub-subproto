package management

import protocol "github.com/yttydcs/myflowhub-proto/protocol/management"

const SubProtoManagement uint8 = protocol.SubProtoManagement

const (
	actionNodeEcho        = protocol.ActionNodeEcho
	actionNodeEchoResp    = protocol.ActionNodeEchoResp
	actionNodeInfo        = protocol.ActionNodeInfo
	actionNodeInfoResp    = protocol.ActionNodeInfoResp
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
type nodeInfoReq = protocol.NodeInfoReq
type nodeInfoResp = protocol.NodeInfoResp
type listNodesReq = protocol.ListNodesReq
type configGetReq = protocol.ConfigGetReq
type configSetReq = protocol.ConfigSetReq
type configResp = protocol.ConfigResp
type configListReq = protocol.ConfigListReq
type configListResp = protocol.ConfigListResp
type listSubtreeReq = protocol.ListSubtreeReq

// NOTE:
// `display_name` is rolled out in Proto and SubProto in parallel. Keep the
// local wire struct compatible so management can expose the field before the
// workspace switches to the new Proto module.
type nodeInfo struct {
	NodeID      uint32 `json:"node_id"`
	HasChildren bool   `json:"has_children,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

type listNodesResp struct {
	Code  int        `json:"code"`
	Msg   string     `json:"msg,omitempty"`
	Nodes []nodeInfo `json:"nodes,omitempty"`
}

type listSubtreeResp struct {
	Code  int        `json:"code"`
	Msg   string     `json:"msg,omitempty"`
	Nodes []nodeInfo `json:"nodes,omitempty"`
}
