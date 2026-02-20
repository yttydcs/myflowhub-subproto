package auth

import (
	"context"
	"encoding/json"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func registerRegisterActions(h *LoginHandler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionRegister, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleRegister(ctx, conn, hdr, data, false)
		}),
		kit.NewAction(actionAssistRegister, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleRegister(ctx, conn, hdr, data, true)
		}),
		kit.NewAction(actionRegisterResp, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleRegisterResp(ctx, data)
		}),
		kit.NewAction(actionAssistRegisterResp, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleRegisterResp(ctx, data)
		}),
	}
}

func (h *LoginHandler) handleRegister(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	send := h.sendDirectResp
	if assisted {
		send = h.sendResp
	}
	var req registerData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		send(ctx, conn, hdr, actionRegisterResp, respData{Code: 400, Msg: "invalid register data"})
		return
	}
	if strings.TrimSpace(req.PubKey) == "" && strings.TrimSpace(h.nodePubB64) != "" {
		req.PubKey = h.nodePubB64
	}
	req.NodePub = req.PubKey
	var pubRaw []byte
	if strings.TrimSpace(req.PubKey) != "" {
		if _, raw, err := parseECPubKey(req.PubKey); err != nil {
			send(ctx, conn, hdr, actionRegisterResp, respData{Code: 400, Msg: "invalid pubkey"})
			return
		} else {
			pubRaw = raw
		}
	}
	if assisted {
		// being processed at authority
		nodeID := h.ensureNodeID(req.DeviceID)
		h.saveBinding(ctx, conn, req.DeviceID, nodeID, pubRaw)
		h.addRouteIndex(ctx, nodeID, conn)
		if strings.TrimSpace(req.PubKey) != "" {
			h.addTrustedNode(nodeID, req.PubKey)
		}
		send(ctx, conn, hdr, actionAssistRegisterResp, respData{
			Code:     1,
			Msg:      "ok",
			DeviceID: req.DeviceID,
			NodeID:   nodeID,
			Role:     h.resolveRole(nodeID),
			Perms:    h.resolvePerms(nodeID),
			PubKey:   req.PubKey,
			NodePub:  req.PubKey,
		})
		h.persistState()
		return
	}
	authority := h.selectAuthority(ctx)
	if authority != nil {
		h.setPending(req.DeviceID, conn.ID(), hdr)
		h.forward(ctx, authority, actionAssistRegister, req)
		return
	}
	// self authority
	nodeID := h.ensureNodeID(req.DeviceID)
	h.saveBinding(ctx, conn, req.DeviceID, nodeID, pubRaw)
	h.applyHubID(ctx, conn, localNodeID(ctx))
	if strings.TrimSpace(req.PubKey) != "" {
		h.addTrustedNode(nodeID, req.PubKey)
	}
	send(ctx, conn, hdr, actionRegisterResp, respData{
		Code:     1,
		Msg:      "ok",
		DeviceID: req.DeviceID,
		NodeID:   nodeID,
		HubID:    localNodeID(ctx),
		Role:     h.resolveRole(nodeID),
		Perms:    h.resolvePerms(nodeID),
		PubKey:   req.PubKey,
		NodePub:  req.PubKey,
	})
	h.persistState()
}

func (h *LoginHandler) handleRegisterResp(ctx context.Context, data json.RawMessage) {
	var resp respData
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	if resp.DeviceID == "" {
		return
	}
	pending, ok := h.popPending(resp.DeviceID)
	if !ok {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	if c, found := srv.ConnManager().Get(pending.connID); found {
		var pubRaw []byte
		if pk := strings.TrimSpace(resp.PubKey); pk != "" {
			if _, raw, err := parseECPubKey(pk); err == nil {
				pubRaw = raw
			}
		}
		h.saveBinding(ctx, c, resp.DeviceID, resp.NodeID, pubRaw)
		h.applyRolePerms(resp.DeviceID, resp.NodeID, resp.Role, resp.Perms, c)
		h.applyHubID(ctx, c, resp.HubID)
		if strings.TrimSpace(resp.NodePub) != "" {
			h.addTrustedNode(resp.NodeID, resp.NodePub)
		}
		if resp.HubID == 0 {
			resp.HubID = srv.NodeID()
		}
		h.sendResp(ctx, c, h.buildPendingRespHeader(ctx, pending), actionRegisterResp, resp)
	}
}
