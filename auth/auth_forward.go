package auth

import (
	"context"
	"encoding/json"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
)

func (h *LoginHandler) shouldForwardByHeaderTarget(ctx context.Context, hdr core.IHeader, action string) bool {
	if h == nil || !h.isSemiCentralMode() || hdr == nil || hdr.Major() != header.MajorCmd || hdr.TargetID() == 0 {
		return false
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil || hdr.TargetID() == srv.NodeID() {
		return false
	}
	switch action {
	case actionAssistRegister, actionAssistLogin, actionAssistQueryCred:
		return true
	default:
		return false
	}
}

func (h *LoginHandler) forwardCmdByHeaderTarget(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) (bool, int, string) {
	if conn == nil || hdr == nil || len(payload) == 0 {
		return false, 4500, "authority unavailable"
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil || srv.ConnManager() == nil {
		return false, 4500, "authority unavailable"
	}
	target := hdr.TargetID()
	if target == 0 || target == srv.NodeID() {
		return false, 4500, "authority unavailable"
	}

	cm := srv.ConnManager()
	var next core.IConnection
	if c, ok := cm.GetByNode(target); ok && c != nil {
		next = c
	} else {
		next = h.selectAuthorityConn(ctx)
	}
	if next == nil || next.ID() == conn.ID() {
		return false, 4500, "authority unavailable"
	}
	if isParentConnLogin(conn) && isParentConnLogin(next) {
		return false, 4500, "authority unavailable"
	}

	fwdHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		return false, 4500, "authority unavailable"
	}
	fwdHdr.WithTargetID(target)
	if err := srv.Send(ctx, next.ID(), fwdHdr, payload); err != nil {
		if h.log != nil {
			h.log.Warn("forward auth frame failed", "target", target, "source", hdr.SourceID(), "err", err)
		}
		return false, 4500, "authority unavailable"
	}
	return true, 0, ""
}

func (h *LoginHandler) sendForwardError(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, frame message, code int, msg string) {
	if conn == nil || reqHdr == nil {
		return
	}
	action, data := buildForwardError(frame.Action, frame.Data, code, msg)
	if action == "" {
		return
	}
	h.sendAssistResp(ctx, conn, reqHdr, action, data)
}

func buildForwardError(action string, data json.RawMessage, code int, msg string) (string, respData) {
	build := func(deviceID string) respData {
		resp := authorityUnavailableResp(deviceID)
		if code != 0 {
			resp.Code = code
		}
		if msg != "" {
			resp.Msg = msg
			resp.Reason = msg
		}
		return resp
	}
	switch action {
	case actionAssistRegister:
		var req registerData
		_ = json.Unmarshal(data, &req)
		return actionAssistRegisterResp, build(req.DeviceID)
	case actionAssistLogin:
		var req loginData
		_ = json.Unmarshal(data, &req)
		return actionAssistLoginResp, build(req.DeviceID)
	case actionAssistQueryCred:
		var req queryCredData
		_ = json.Unmarshal(data, &req)
		return actionAssistQueryCredResp, build(req.DeviceID)
	default:
		return "", respData{}
	}
}

func (h *LoginHandler) tryForwardAssistUpstream(ctx context.Context, conn core.IConnection, hdr core.IHeader, action string, data any, respAction string, deviceID string) bool {
	if h == nil || !h.isSemiCentralMode() || hdr == nil || hdr.SourceID() == 0 {
		return false
	}
	authority := h.resolveAuthority(ctx)
	if authority.local() {
		return false
	}
	if authority.unavailable() || authority.targetNodeID == 0 || authority.targetNodeID == localNodeID(ctx) {
		h.sendAssistResp(ctx, conn, hdr, respAction, authorityUnavailableResp(deviceID))
		return true
	}
	if h.forwardInheritedAuthorityRequest(ctx, authority, hdr, action, data) {
		return true
	}
	h.sendAssistResp(ctx, conn, hdr, respAction, authorityUnavailableResp(deviceID))
	return true
}
