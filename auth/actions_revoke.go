package auth

import (
	"context"
	"encoding/json"

	core "github.com/yttydcs/myflowhub-core"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func registerRevokeActions(h *LoginHandler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionRevoke, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleRevoke(ctx, conn, hdr, data)
		}, kit.WithRequireAuth(true)),
	}
}

// revoke handling: broadcast; respond only if deleted
func (h *LoginHandler) handleRevoke(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req revokeData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		return
	}
	actorID := permission.SourceNodeID(hdr, conn)
	if !h.hasPermission(actorID, permission.AuthRevoke) {
		h.sendDirectResp(ctx, conn, hdr, actionRevokeResp, respData{Code: 4403, Msg: "permission denied", DeviceID: req.DeviceID, NodeID: req.NodeID})
		return
	}
	removed := h.removeBinding(req.DeviceID)
	if removed {
		h.sendDirectResp(ctx, conn, hdr, actionRevokeResp, respData{Code: 1, Msg: "ok", DeviceID: req.DeviceID, NodeID: req.NodeID})
	}
	// broadcast downstream and upstream except source
	h.broadcast(ctx, conn, actionRevoke, req)
}
