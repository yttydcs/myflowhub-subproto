package auth

// Context: This file belongs to the SubProto implementation layer around actions_authority_policy.

import (
	"context"
	"encoding/json"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func (h *LoginHandler) handleAuthorityPolicySync(ctx context.Context, conn core.IConnection, data json.RawMessage) {
	if h == nil || !h.isSemiCentralMode() || !isParentConnLogin(conn) {
		return
	}
	var req authorityPolicySyncData
	if err := json.Unmarshal(data, &req); err != nil {
		if h.log != nil {
			h.log.Warn("invalid authority policy sync", "err", err)
		}
		return
	}
	if !h.applyRuntimeAuthorityPolicy(time.Now().UTC(), req) {
		return
	}
	h.broadcastAuthorityPolicy(ctx, conn, req)
}

func registerAuthorityPolicyActions(h *LoginHandler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionAuthorityPolicySync, func(ctx context.Context, conn core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleAuthorityPolicySync(ctx, conn, data)
		}, kit.WithRequireAuth(true)),
	}
}
