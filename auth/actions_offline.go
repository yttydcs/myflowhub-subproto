package auth

import (
	"context"
	"encoding/json"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func (h *LoginHandler) handleOffline(ctx context.Context, conn core.IConnection, data json.RawMessage, assisted bool) {
	var req offlineData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		return
	}
	h.removeBinding(req.DeviceID)
	h.removeIndexes(ctx, req.NodeID, conn)
	if !assisted {
		// forward to parent
		if parent := h.selectAuthorityConn(ctx); parent != nil && (conn == nil || parent.ID() != conn.ID()) {
			h.forward(ctx, parent, actionAssistOffline, req)
		}
	}
}

func registerOfflineActions(h *LoginHandler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionOffline, func(ctx context.Context, conn core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleOffline(ctx, conn, data, false)
		}, kit.WithRequireAuth(true)),
		kit.NewAction(actionAssistOffline, func(ctx context.Context, conn core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleOffline(ctx, conn, data, true)
		}, kit.WithRequireAuth(true)),
	}
}
