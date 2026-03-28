package auth

import (
	"context"
	"encoding/json"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
)

func isRemoteAuthorityAdminAction(action string) bool {
	switch action {
	case actionListPendingRegisters,
		actionApproveRegister,
		actionRejectRegister,
		actionListRegisterPermits,
		actionIssueRegisterPermit,
		actionRevokeRegisterPermit:
		return true
	default:
		return false
	}
}

func isAuthorityTargetForwardAction(action string) bool {
	switch action {
	case actionAssistRegister, actionAssistLogin, actionAssistQueryCred:
		return true
	default:
		return isRemoteAuthorityAdminAction(action)
	}
}

func (h *LoginHandler) shouldForwardByHeaderTarget(ctx context.Context, hdr core.IHeader, action string) bool {
	if h == nil || !h.isSemiCentralMode() || hdr == nil || hdr.Major() != header.MajorCmd || hdr.TargetID() == 0 {
		return false
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil || hdr.TargetID() == srv.NodeID() {
		return false
	}
	return isAuthorityTargetForwardAction(action)
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
	action, data, direct := buildForwardError(frame.Action, frame.Data, code, msg)
	if action == "" {
		return
	}
	if direct {
		h.sendActionData(ctx, conn, reqHdr, action, data, true)
		return
	}
	if resp, ok := data.(respData); ok {
		h.sendAssistResp(ctx, conn, reqHdr, action, resp)
	}
}

func buildForwardError(action string, data json.RawMessage, code int, msg string) (string, any, bool) {
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
		return actionAssistRegisterResp, build(req.DeviceID), false
	case actionAssistLogin:
		var req loginData
		_ = json.Unmarshal(data, &req)
		return actionAssistLoginResp, build(req.DeviceID), false
	case actionAssistQueryCred:
		var req queryCredData
		_ = json.Unmarshal(data, &req)
		return actionAssistQueryCredResp, build(req.DeviceID), false
	case actionListPendingRegisters:
		return actionListPendingRegistersResp, listPendingRegistersResp{Code: code, Msg: msg}, true
	case actionListRegisterPermits:
		return actionListRegisterPermitsResp, listRegisterPermitsResp{Code: code, Msg: msg}, true
	case actionApproveRegister:
		var req approveRegisterReq
		_ = json.Unmarshal(data, &req)
		return actionApproveRegisterResp, approveRegisterResp{
			Code:      code,
			Msg:       msg,
			RequestID: strings.TrimSpace(req.RequestID),
		}, true
	case actionRejectRegister:
		var req rejectRegisterReq
		_ = json.Unmarshal(data, &req)
		return actionRejectRegisterResp, rejectRegisterResp{
			Code:      code,
			Msg:       msg,
			RequestID: strings.TrimSpace(req.RequestID),
		}, true
	case actionIssueRegisterPermit:
		var req issueRegisterPermitReq
		_ = json.Unmarshal(data, &req)
		return actionIssueRegisterPermitResp, issueRegisterPermitResp{
			Code:     code,
			Msg:      msg,
			DeviceID: strings.TrimSpace(req.DeviceID),
			Role:     strings.TrimSpace(req.Role),
		}, true
	case actionRevokeRegisterPermit:
		var req revokeRegisterPermitReq
		_ = json.Unmarshal(data, &req)
		return actionRevokeRegisterPermitResp, revokeRegisterPermitResp{
			Code:   code,
			Msg:    msg,
			Permit: strings.TrimSpace(req.Permit),
		}, true
	default:
		return "", nil, false
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

func (h *LoginHandler) tryForwardAdminUpstream(ctx context.Context, conn core.IConnection, hdr core.IHeader, action string, data any, respAction string, unavailableResp any) bool {
	if h == nil {
		return false
	}
	authority := h.resolveAuthority(ctx)
	if authority.local() {
		return false
	}
	if authority.unavailable() || authority.targetNodeID == 0 || authority.targetNodeID == localNodeID(ctx) {
		h.sendActionData(ctx, conn, hdr, respAction, unavailableResp, true)
		return true
	}
	useInheritedSource := hdr != nil && hdr.SourceID() != 0 && hdr.SourceID() != localNodeID(ctx)
	if useInheritedSource {
		if h.forwardInheritedAuthorityRequest(ctx, authority, hdr, action, data) {
			return true
		}
	} else if h.forwardAuthorityRequest(ctx, authority, hdr, action, data) {
		return true
	}
	h.sendActionData(ctx, conn, hdr, respAction, unavailableResp, true)
	return true
}
