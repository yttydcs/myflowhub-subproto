package auth

import (
	"context"
	"encoding/json"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func registerAdmissionActions(h *LoginHandler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionListPendingRegisters, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleListPendingRegisters(ctx, conn, hdr, data)
		}, kit.WithRequireAuth(true)),
		kit.NewAction(actionListRegisterPermits, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleListRegisterPermits(ctx, conn, hdr, data)
		}, kit.WithRequireAuth(true)),
		kit.NewAction(actionApproveRegister, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleApproveRegister(ctx, conn, hdr, data)
		}, kit.WithRequireAuth(true)),
		kit.NewAction(actionRejectRegister, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleRejectRegister(ctx, conn, hdr, data)
		}, kit.WithRequireAuth(true)),
		kit.NewAction(actionIssueRegisterPermit, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleIssueRegisterPermit(ctx, conn, hdr, data)
		}, kit.WithRequireAuth(true)),
		kit.NewAction(actionRevokeRegisterPermit, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleRevokeRegisterPermit(ctx, conn, hdr, data)
		}, kit.WithRequireAuth(true)),
	}
}

func (h *LoginHandler) requireActionPermission(conn core.IConnection, hdr core.IHeader, perm string) (uint32, bool) {
	actorID := permission.SourceNodeID(hdr, conn)
	if actorID == 0 {
		return 0, false
	}
	return actorID, h.hasPermission(actorID, perm)
}

func (h *LoginHandler) requireAnyActionPermission(conn core.IConnection, hdr core.IHeader, perms ...string) (uint32, bool) {
	actorID := permission.SourceNodeID(hdr, conn)
	if actorID == 0 {
		return 0, false
	}
	for _, perm := range perms {
		if strings.TrimSpace(perm) == "" {
			continue
		}
		if h.hasPermission(actorID, perm) {
			return actorID, true
		}
	}
	return actorID, false
}

func (h *LoginHandler) handleListPendingRegisters(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	if _, ok := h.requireActionPermission(conn, hdr, permission.AuthPendingList); !ok {
		h.sendActionData(ctx, conn, hdr, actionListPendingRegistersResp, listPendingRegistersResp{Code: 4403, Msg: "permission denied"}, true)
		return
	}
	var req listPendingRegistersReq
	if len(data) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			h.sendActionData(ctx, conn, hdr, actionListPendingRegistersResp, listPendingRegistersResp{Code: 400, Msg: "invalid request"}, true)
			return
		}
	}
	resp := h.listPendingRegisters(req)
	h.sendActionData(ctx, conn, hdr, actionListPendingRegistersResp, resp, true)
}

func (h *LoginHandler) handleListRegisterPermits(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	if _, ok := h.requireAnyActionPermission(conn, hdr, permission.AuthPermitIssue, permission.AuthPermitRevoke); !ok {
		h.sendActionData(ctx, conn, hdr, actionListRegisterPermitsResp, listRegisterPermitsResp{Code: 4403, Msg: "permission denied"}, true)
		return
	}
	var req listRegisterPermitsReq
	if len(data) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			h.sendActionData(ctx, conn, hdr, actionListRegisterPermitsResp, listRegisterPermitsResp{Code: 400, Msg: "invalid request"}, true)
			return
		}
	}
	resp := h.listRegisterPermits(req)
	h.sendActionData(ctx, conn, hdr, actionListRegisterPermitsResp, resp, true)
}

func (h *LoginHandler) handleApproveRegister(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	if _, ok := h.requireActionPermission(conn, hdr, permission.AuthRegisterApprove); !ok {
		h.sendActionData(ctx, conn, hdr, actionApproveRegisterResp, approveRegisterResp{Code: 4403, Msg: "permission denied"}, true)
		return
	}
	var req approveRegisterReq
	if err := json.Unmarshal(data, &req); err != nil || strings.TrimSpace(req.RequestID) == "" {
		h.sendActionData(ctx, conn, hdr, actionApproveRegisterResp, approveRegisterResp{Code: 400, Msg: "invalid request_id"}, true)
		return
	}
	approved, err := h.approvePendingRegister(req.RequestID, req.Role)
	if err != nil {
		code := 4001
		if strings.Contains(err.Error(), "unknown role") {
			code = 400
		}
		h.sendActionData(ctx, conn, hdr, actionApproveRegisterResp, approveRegisterResp{Code: code, Msg: err.Error(), RequestID: req.RequestID}, true)
		return
	}
	h.sendActionData(ctx, conn, hdr, actionApproveRegisterResp, approveRegisterResp{
		Code:      1,
		Msg:       "ok",
		RequestID: approved.RequestID,
		DeviceID:  approved.DeviceID,
		NodeID:    approved.NodeID,
		Role:      approved.Role,
		Status:    admissionStatusApproved,
	}, true)
}

func (h *LoginHandler) handleRejectRegister(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	if _, ok := h.requireActionPermission(conn, hdr, permission.AuthRegisterReject); !ok {
		h.sendActionData(ctx, conn, hdr, actionRejectRegisterResp, rejectRegisterResp{Code: 4403, Msg: "permission denied"}, true)
		return
	}
	var req rejectRegisterReq
	if err := json.Unmarshal(data, &req); err != nil || strings.TrimSpace(req.RequestID) == "" {
		h.sendActionData(ctx, conn, hdr, actionRejectRegisterResp, rejectRegisterResp{Code: 400, Msg: "invalid request_id"}, true)
		return
	}
	rejected, err := h.rejectPendingRegister(req.RequestID)
	if err != nil {
		h.sendActionData(ctx, conn, hdr, actionRejectRegisterResp, rejectRegisterResp{Code: 4001, Msg: err.Error(), RequestID: req.RequestID}, true)
		return
	}
	h.sendActionData(ctx, conn, hdr, actionRejectRegisterResp, rejectRegisterResp{
		Code:      1,
		Msg:       "ok",
		RequestID: rejected.RequestID,
		DeviceID:  rejected.DeviceID,
		Status:    admissionStatusRejected,
		Reason:    strings.TrimSpace(req.Reason),
	}, true)
}

func (h *LoginHandler) handleIssueRegisterPermit(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	actorID, ok := h.requireActionPermission(conn, hdr, permission.AuthPermitIssue)
	if !ok {
		h.sendActionData(ctx, conn, hdr, actionIssueRegisterPermitResp, issueRegisterPermitResp{Code: 4403, Msg: "permission denied"}, true)
		return
	}
	var req issueRegisterPermitReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendActionData(ctx, conn, hdr, actionIssueRegisterPermitResp, issueRegisterPermitResp{Code: 400, Msg: "invalid request"}, true)
		return
	}
	record, err := h.issueRegisterPermit(req.DeviceID, req.Role, req.ExpiresAt, actorID)
	if err != nil {
		h.sendActionData(ctx, conn, hdr, actionIssueRegisterPermitResp, issueRegisterPermitResp{Code: 400, Msg: err.Error()}, true)
		return
	}
	h.sendActionData(ctx, conn, hdr, actionIssueRegisterPermitResp, issueRegisterPermitResp{
		Code:      1,
		Msg:       "ok",
		Permit:    record.Permit,
		DeviceID:  record.DeviceID,
		Role:      record.Role,
		ExpiresAt: record.ExpiresAt,
	}, true)
}

func (h *LoginHandler) handleRevokeRegisterPermit(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	if _, ok := h.requireActionPermission(conn, hdr, permission.AuthPermitRevoke); !ok {
		h.sendActionData(ctx, conn, hdr, actionRevokeRegisterPermitResp, revokeRegisterPermitResp{Code: 4403, Msg: "permission denied"}, true)
		return
	}
	var req revokeRegisterPermitReq
	if err := json.Unmarshal(data, &req); err != nil || strings.TrimSpace(req.Permit) == "" {
		h.sendActionData(ctx, conn, hdr, actionRevokeRegisterPermitResp, revokeRegisterPermitResp{Code: 400, Msg: "invalid permit"}, true)
		return
	}
	record, ok := h.revokeRegisterPermit(req.Permit)
	if !ok {
		h.sendActionData(ctx, conn, hdr, actionRevokeRegisterPermitResp, revokeRegisterPermitResp{Code: 4001, Msg: "permit not found", Permit: strings.TrimSpace(req.Permit)}, true)
		return
	}
	h.sendActionData(ctx, conn, hdr, actionRevokeRegisterPermitResp, revokeRegisterPermitResp{
		Code:     1,
		Msg:      "ok",
		Permit:   record.Permit,
		DeviceID: record.DeviceID,
		Role:     record.Role,
	}, true)
}
