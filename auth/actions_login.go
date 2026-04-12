package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func (h *LoginHandler) handleLoginResp(ctx context.Context, data json.RawMessage) {
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
		if resp.Code == 1 {
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
			if strings.TrimSpace(resp.PubKey) != "" {
				h.addTrustedNode(resp.NodeID, resp.PubKey)
			}
			// 此分支没有原始 device 签名，避免上行；由实际验证节点负责上报
		}
		if resp.HubID == 0 {
			resp.HubID = srv.NodeID()
		}
		h.sendResp(ctx, c, h.buildPendingRespHeader(ctx, pending), actionLoginResp, resp)
	}
}

func (h *LoginHandler) handleLogin(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	send := h.sendDirectResp
	if assisted {
		send = h.sendAssistResp
	}
	var req loginData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		send(ctx, conn, hdr, actionLoginResp, respData{Code: 400, Msg: "invalid login data"})
		return
	}
	req.DisplayName = normalizeDisplayName(req.DisplayName)
	if assisted && h.tryForwardAssistUpstream(ctx, conn, hdr, actionAssistLogin, req, actionAssistLoginResp, req.DeviceID) {
		return
	}
	authority := h.resolveAuthority(ctx)
	if assisted {
		rec, ok := h.lookup(req.DeviceID)
		if !ok || len(rec.PubKey) == 0 {
			if authority.remote() {
				// 向上查询公钥
				h.setPending(req.DeviceID, conn.ID(), hdr)
				if h.forwardAuthorityRequest(ctx, authority, hdr, actionAssistQueryCred, queryCredData{DeviceID: req.DeviceID, NodeID: req.NodeID}) {
					return
				}
				h.popPending(req.DeviceID)
				h.sendAssistResp(ctx, conn, hdr, actionAssistLoginResp, authorityUnavailableResp(req.DeviceID))
				return
			}
			if authority.unavailable() {
				h.sendAssistResp(ctx, conn, hdr, actionAssistLoginResp, authorityUnavailableResp(req.DeviceID))
				return
			}
		}
		valid := false
		if ok && len(rec.PubKey) > 0 && strings.EqualFold(strings.TrimSpace(req.Alg), defaultAlgES256) && strings.TrimSpace(req.Sig) != "" {
			if pub, err := parseECPubKeyRaw(rec.PubKey); err == nil {
				valid = verifyEcdsaSig(pub, loginSignBytes(req), req.Sig)
			}
		}
		if !ok || !valid {
			h.sendResp(ctx, conn, hdr, actionAssistLoginResp, respData{Code: 4001, Msg: "invalid signature"})
			return
		}
		if len(rec.PubKey) > 0 {
			conn.SetMeta("pubkey", rec.PubKey)
		}
		if isSameConnectionNode(conn, rec.NodeID) {
			applyDisplayNameMeta(conn, req.DisplayName)
		}
		role, perms, _ := h.lookupByNode(rec.NodeID)
		h.addRouteIndex(ctx, rec.NodeID, conn)
		h.sendAssistResp(ctx, conn, hdr, actionAssistLoginResp, respData{
			Code:        1,
			Msg:         "ok",
			DeviceID:    req.DeviceID,
			NodeID:      rec.NodeID,
			HubID:       localNodeID(ctx),
			Role:        role,
			Perms:       perms,
			PubKey:      base64.StdEncoding.EncodeToString(rec.PubKey),
			NodePub:     base64.StdEncoding.EncodeToString(rec.PubKey),
			DisplayName: req.DisplayName,
		})
		go h.sendUpLogin(ctx, conn, req.DeviceID, rec.NodeID, rec.PubKey, req.Sig, req.Alg, req.TS, req.Nonce)
		return
	}
	// local check
	if rec, ok := h.lookup(req.DeviceID); ok {
		if len(rec.PubKey) == 0 {
			if authority.remote() {
				h.setPending(req.DeviceID, conn.ID(), hdr)
				if h.forwardAuthorityRequest(ctx, authority, hdr, actionAssistQueryCred, queryCredData{DeviceID: req.DeviceID, NodeID: req.NodeID}) {
					return
				}
				h.popPending(req.DeviceID)
				send(ctx, conn, hdr, actionLoginResp, authorityUnavailableResp(req.DeviceID))
				return
			}
			if authority.unavailable() {
				send(ctx, conn, hdr, actionLoginResp, authorityUnavailableResp(req.DeviceID))
				return
			}
		}
		valid := false
		if len(rec.PubKey) > 0 && strings.EqualFold(strings.TrimSpace(req.Alg), defaultAlgES256) && strings.TrimSpace(req.Sig) != "" {
			if pub, err := parseECPubKeyRaw(rec.PubKey); err == nil {
				valid = verifyEcdsaSig(pub, loginSignBytes(req), req.Sig)
			}
		}
		if valid {
			h.saveBinding(ctx, conn, req.DeviceID, rec.NodeID, rec.PubKey)
			h.applyHubID(ctx, conn, localNodeID(ctx))
			applyDisplayNameMeta(conn, req.DisplayName)
			role, perms, _ := h.lookupByNode(rec.NodeID)
			send(ctx, conn, hdr, actionLoginResp, respData{
				Code:        1,
				Msg:         "ok",
				DeviceID:    req.DeviceID,
				NodeID:      rec.NodeID,
				HubID:       localNodeID(ctx),
				Role:        role,
				Perms:       perms,
				PubKey:      base64.StdEncoding.EncodeToString(rec.PubKey),
				NodePub:     base64.StdEncoding.EncodeToString(rec.PubKey),
				DisplayName: req.DisplayName,
			})
			go h.sendUpLogin(ctx, conn, req.DeviceID, rec.NodeID, rec.PubKey, req.Sig, req.Alg, req.TS, req.Nonce)
			return
		}
		send(ctx, conn, hdr, actionLoginResp, respData{Code: 4001, Msg: "invalid signature"})
		return
	}
	// not found locally, try authority
	if authority.remote() {
		h.setPending(req.DeviceID, conn.ID(), hdr)
		if h.forwardAuthorityRequest(ctx, authority, hdr, actionAssistLogin, req) {
			return
		}
		h.popPending(req.DeviceID)
		send(ctx, conn, hdr, actionLoginResp, authorityUnavailableResp(req.DeviceID))
		return
	}
	if authority.unavailable() {
		send(ctx, conn, hdr, actionLoginResp, authorityUnavailableResp(req.DeviceID))
		return
	}
	send(ctx, conn, hdr, actionLoginResp, respData{Code: 4001, Msg: "invalid signature"})
}

func registerLoginActions(h *LoginHandler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionLogin, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleLogin(ctx, conn, hdr, data, false)
		}),
		kit.NewAction(actionAssistLogin, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleLogin(ctx, conn, hdr, data, true)
		}),
		kit.NewAction(actionLoginResp, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleLoginResp(ctx, data)
		}),
		kit.NewAction(actionAssistLoginResp, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleLoginResp(ctx, data)
		}),
	}
}
