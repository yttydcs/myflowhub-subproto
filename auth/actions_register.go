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
	respAction := actionRegisterResp
	if assisted {
		send = h.sendResp
		respAction = actionAssistRegisterResp
	}
	var req registerData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		send(ctx, conn, hdr, respAction, respData{Code: 400, Msg: "invalid register data"})
		return
	}
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.RequestedRole = strings.TrimSpace(req.RequestedRole)
	req.JoinPermit = strings.TrimSpace(req.JoinPermit)
	req.DisplayName = normalizeDisplayName(req.DisplayName)
	req.NodePub = req.PubKey
	var pubRaw []byte
	if strings.TrimSpace(req.PubKey) != "" {
		if _, raw, err := parseECPubKey(req.PubKey); err != nil {
			send(ctx, conn, hdr, respAction, respData{Code: 400, Msg: "invalid pubkey"})
			return
		} else {
			pubRaw = raw
		}
	}
	if !assisted {
		authority := h.resolveAuthority(ctx)
		if authority.remote() {
			h.setPending(req.DeviceID, conn.ID(), hdr)
			h.forward(ctx, authority.conn, actionAssistRegister, req)
			return
		}
		if authority.unavailable() {
			send(ctx, conn, hdr, respAction, authorityUnavailableResp(req.DeviceID))
			return
		}
	}
	if existing, ok := h.lookup(req.DeviceID); ok && existing.NodeID != 0 {
		if len(pubRaw) == 0 && len(existing.PubKey) > 0 {
			pubRaw = cloneSlice(existing.PubKey)
		}
		h.finishRegisterSuccess(ctx, conn, hdr, send, respAction, req, existing.NodeID, existing.Role, pubRaw, "")
		return
	}
	if req.JoinPermit != "" {
		permit, err := h.consumeRegisterPermit(req.JoinPermit, req.DeviceID)
		if err != nil {
			send(ctx, conn, hdr, respAction, respData{
				Code:      4001,
				Msg:       "register rejected",
				DeviceID:  req.DeviceID,
				Status:    admissionStatusRejected,
				Reason:    err.Error(),
				RequestID: "",
			})
			return
		}
		h.finishRegisterSuccess(ctx, conn, hdr, send, respAction, req, 0, permit.Role, pubRaw, "")
		return
	}
	if approved, ok := h.consumeApprovedRegister(req.DeviceID); ok {
		h.finishRegisterSuccess(ctx, conn, hdr, send, respAction, req, approved.NodeID, approved.Role, pubRaw, approved.RequestID)
		return
	}
	if h.requireApproval {
		pending, err := h.savePendingRegister(req)
		if err != nil {
			send(ctx, conn, hdr, respAction, respData{Code: 4500, Msg: "save pending register failed", DeviceID: req.DeviceID})
			return
		}
		send(ctx, conn, hdr, respAction, respData{
			Code:      202,
			Msg:       "pending approval",
			DeviceID:  req.DeviceID,
			Status:    admissionStatusPending,
			RequestID: pending.RequestID,
			Reason:    "approval required",
		})
		return
	}
	h.finishRegisterSuccess(ctx, conn, hdr, send, respAction, req, 0, "", pubRaw, "")
}

func (h *LoginHandler) finishRegisterSuccess(ctx context.Context, conn core.IConnection, hdr core.IHeader, send func(context.Context, core.IConnection, core.IHeader, string, respData), respAction string, req registerData, nodeID uint32, roleOverride string, pubRaw []byte, requestID string) {
	if nodeID == 0 {
		nodeID = h.ensureNodeID(req.DeviceID)
	}
	roleOverride = strings.TrimSpace(roleOverride)
	if roleOverride != "" && h.permCfg != nil {
		h.permCfg.UpsertNode(nodeID, roleOverride, nil)
	}
	h.saveBinding(ctx, conn, req.DeviceID, nodeID, pubRaw)
	if strings.TrimSpace(req.PubKey) != "" {
		h.addTrustedNode(nodeID, req.PubKey)
	}
	if respAction == actionRegisterResp {
		h.applyHubID(ctx, conn, localNodeID(ctx))
		applyDisplayNameMeta(conn, req.DisplayName)
	}
	send(ctx, conn, hdr, respAction, respData{
		Code:        1,
		Msg:         "ok",
		DeviceID:    req.DeviceID,
		NodeID:      nodeID,
		HubID:       localNodeID(ctx),
		Role:        h.resolveRole(nodeID),
		Perms:       h.resolvePerms(nodeID),
		PubKey:      req.PubKey,
		NodePub:     req.PubKey,
		DisplayName: req.DisplayName,
		Status:      admissionStatusApproved,
		RequestID:   requestID,
	})
	h.persistState()
}

func registerRespApproved(resp respData) bool {
	status := strings.ToLower(strings.TrimSpace(resp.Status))
	if status == "" {
		return resp.Code == 1 && resp.NodeID != 0
	}
	return status == admissionStatusApproved && resp.Code == 1 && resp.NodeID != 0
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
		if registerRespApproved(resp) {
			var pubRaw []byte
			if pk := strings.TrimSpace(resp.PubKey); pk != "" {
				if _, raw, err := parseECPubKey(pk); err == nil {
					pubRaw = raw
				}
			}
			h.saveBinding(ctx, c, resp.DeviceID, resp.NodeID, pubRaw)
			h.applyRolePerms(resp.DeviceID, resp.NodeID, resp.Role, resp.Perms, c)
			h.applyHubID(ctx, c, resp.HubID)
			applyDisplayNameMeta(c, resp.DisplayName)
			if strings.TrimSpace(resp.NodePub) != "" {
				h.addTrustedNode(resp.NodeID, resp.NodePub)
			}
		}
		if resp.HubID == 0 {
			resp.HubID = srv.NodeID()
		}
		h.sendResp(ctx, c, h.buildPendingRespHeader(ctx, pending), actionRegisterResp, resp)
	}
}
