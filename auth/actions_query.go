package auth

// 本文件承载 SubProto 中 `auth` 模块里与 `actions_query` 相关的逻辑。

import (
	"context"
	"encoding/json"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func (h *LoginHandler) handleAssistQuery(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req queryCredData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		h.sendAssistResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 400, Msg: "invalid query"})
		return
	}
	if h.tryForwardAssistUpstream(ctx, conn, hdr, actionAssistQueryCred, req, actionAssistQueryCredResp, req.DeviceID) {
		return
	}
	if rec, ok := h.lookup(req.DeviceID); ok {
		nodePub := ""
		nodePub = encodePubKey(rec.PubKey)
		h.sendAssistResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{
			Code:     1,
			Msg:      "ok",
			DeviceID: req.DeviceID,
			NodeID:   rec.NodeID,
			Role:     rec.Role,
			Perms:    rec.Perms,
			PubKey:   encodePubKey(rec.PubKey),
			NodePub:  nodePub,
		})
		return
	}
	h.sendAssistResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 4001, Msg: "not found", DeviceID: req.DeviceID})
}

func (h *LoginHandler) handleAssistQueryResp(ctx context.Context, data json.RawMessage) {
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
			h.addRouteIndex(ctx, resp.NodeID, c)
			if strings.TrimSpace(resp.PubKey) != "" {
				h.addTrustedNode(resp.NodeID, resp.PubKey)
			}
			h.sendResp(ctx, c, h.buildPendingRespHeader(ctx, pending), actionLoginResp, respData{Code: 1, Msg: "ok", DeviceID: resp.DeviceID, NodeID: resp.NodeID, Role: resp.Role, Perms: resp.Perms, PubKey: encodePubKey(pubRaw), NodePub: resp.PubKey})
			return
		}
		h.sendResp(ctx, c, h.buildPendingRespHeader(ctx, pending), actionLoginResp, respData{Code: resp.Code, Msg: resp.Msg})
	}
}

func registerAssistQueryActions(h *LoginHandler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionAssistQueryCred, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleAssistQuery(ctx, conn, hdr, data)
		}, kit.WithRequireAuth(true)),
		kit.NewAction(actionAssistQueryCredResp, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleAssistQueryResp(ctx, data)
		}),
	}
}
