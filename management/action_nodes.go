package management

import (
	"context"
	"encoding/json"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func registerListNodesActions(h *ManagementHandler) core.SubProcessAction {
	return kit.NewAction(actionListNodes, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, _ json.RawMessage) {
		srv := core.ServerFromContext(ctx)
		if srv == nil {
			return
		}
		nodes := enumerateDirectNodes(srv.ConnManager())
		h.sendActionResp(ctx, conn, hdr, actionListNodesResp, listNodesResp{Code: 1, Msg: "ok", Nodes: nodes})
	})
}

func registerListSubtreeActions(h *ManagementHandler) core.SubProcessAction {
	return kit.NewAction(actionListSubtree, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, _ json.RawMessage) {
		srv := core.ServerFromContext(ctx)
		if srv == nil {
			return
		}
		nodes := enumerateDirectNodes(srv.ConnManager())
		// 包含自身
		nodes = append(nodes, nodeInfo{NodeID: srv.NodeID(), HasChildren: len(nodes) > 0})
		h.sendActionResp(ctx, conn, hdr, actionListSubtreeResp, listSubtreeResp{Code: 1, Msg: "ok", Nodes: nodes})
	})
}

func enumerateDirectNodes(cm core.IConnectionManager) []nodeInfo {
	if cm == nil {
		return nil
	}
	seen := make(map[uint32]bool)
	nodes := make([]nodeInfo, 0)
	cm.Range(func(c core.IConnection) bool {
		if nidVal, ok := c.GetMeta("nodeID"); ok {
			if nid, ok2 := nidVal.(uint32); ok2 && nid != 0 && !seen[nid] {
				hasChildren := false
				if role, ok := c.GetMeta(core.MetaRoleKey); ok {
					if s, ok2 := role.(string); ok2 && s == core.RoleParent {
						hasChildren = true
					}
				}
				nodes = append(nodes, nodeInfo{NodeID: nid, HasChildren: hasChildren})
				seen[nid] = true
			}
		}
		return true
	})
	return nodes
}

