package auth

// 本文件承载 SubProto 中 `auth` 模块里与 `actions_admission` 相关的逻辑。

import (
	"context"
	"encoding/json"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

// registerAdmissionActions 注册 authority 管理面的审批与 permit 动作。
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

// requireActionPermission 提取真实操作者节点并校验单个管理权限。
func (h *LoginHandler) requireActionPermission(conn core.IConnection, hdr core.IHeader, perm string) (uint32, bool) {
	actorID := permission.SourceNodeID(hdr, conn)
	if actorID == 0 {
		return 0, false
	}
	return actorID, h.hasPermission(actorID, perm)
}

// requireAnyActionPermission 用于多个等价权限点共享同一入口的场景。
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

// handleListPendingRegisters 返回当前 authority 侧尚未审批的注册请求。
func (h *LoginHandler) handleListPendingRegisters(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req listPendingRegistersReq
	if len(data) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			h.sendActionData(ctx, conn, hdr, actionListPendingRegistersResp, listPendingRegistersResp{Code: 400, Msg: "invalid request"}, true)
			return
		}
	}
	if h.tryForwardAdminUpstream(ctx, conn, hdr, actionListPendingRegisters, req, actionListPendingRegistersResp, listPendingRegistersResp{Code: 4500, Msg: "authority unavailable"}) {
		return
	}
	if _, ok := h.requireActionPermission(conn, hdr, permission.AuthPendingList); !ok {
		h.sendActionData(ctx, conn, hdr, actionListPendingRegistersResp, listPendingRegistersResp{Code: 4403, Msg: "permission denied"}, true)
		return
	}
	resp := h.listPendingRegisters(req)
	h.sendActionData(ctx, conn, hdr, actionListPendingRegistersResp, resp, true)
}

// handleListRegisterPermits 列出仍然有效的 register permit，并支持上游 authority 转发。
func (h *LoginHandler) handleListRegisterPermits(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req listRegisterPermitsReq
	if len(data) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			h.sendActionData(ctx, conn, hdr, actionListRegisterPermitsResp, listRegisterPermitsResp{Code: 400, Msg: "invalid request"}, true)
			return
		}
	}
	if h.tryForwardAdminUpstream(ctx, conn, hdr, actionListRegisterPermits, req, actionListRegisterPermitsResp, listRegisterPermitsResp{Code: 4500, Msg: "authority unavailable"}) {
		return
	}
	if _, ok := h.requireAnyActionPermission(conn, hdr, permission.AuthPermitIssue, permission.AuthPermitRevoke); !ok {
		h.sendActionData(ctx, conn, hdr, actionListRegisterPermitsResp, listRegisterPermitsResp{Code: 4403, Msg: "permission denied"}, true)
		return
	}
	resp := h.listRegisterPermits(req)
	h.sendActionData(ctx, conn, hdr, actionListRegisterPermitsResp, resp, true)
}

// handleApproveRegister 把 pending register 提升为 approved，等待设备最终完成 register。
func (h *LoginHandler) handleApproveRegister(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req approveRegisterReq
	if err := json.Unmarshal(data, &req); err != nil || strings.TrimSpace(req.RequestID) == "" {
		h.sendActionData(ctx, conn, hdr, actionApproveRegisterResp, approveRegisterResp{Code: 400, Msg: "invalid request_id"}, true)
		return
	}
	if h.tryForwardAdminUpstream(ctx, conn, hdr, actionApproveRegister, req, actionApproveRegisterResp, approveRegisterResp{
		Code:      4500,
		Msg:       "authority unavailable",
		RequestID: req.RequestID,
	}) {
		return
	}
	if _, ok := h.requireActionPermission(conn, hdr, permission.AuthRegisterApprove); !ok {
		h.sendActionData(ctx, conn, hdr, actionApproveRegisterResp, approveRegisterResp{Code: 4403, Msg: "permission denied"}, true)
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

// handleRejectRegister 关闭一个待审批注册请求，并把拒绝结果回给请求方。
func (h *LoginHandler) handleRejectRegister(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req rejectRegisterReq
	if err := json.Unmarshal(data, &req); err != nil || strings.TrimSpace(req.RequestID) == "" {
		h.sendActionData(ctx, conn, hdr, actionRejectRegisterResp, rejectRegisterResp{Code: 400, Msg: "invalid request_id"}, true)
		return
	}
	if h.tryForwardAdminUpstream(ctx, conn, hdr, actionRejectRegister, req, actionRejectRegisterResp, rejectRegisterResp{
		Code:      4500,
		Msg:       "authority unavailable",
		RequestID: req.RequestID,
	}) {
		return
	}
	if _, ok := h.requireActionPermission(conn, hdr, permission.AuthRegisterReject); !ok {
		h.sendActionData(ctx, conn, hdr, actionRejectRegisterResp, rejectRegisterResp{Code: 4403, Msg: "permission denied"}, true)
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

// handleIssueRegisterPermit 生成一次性 permit，供设备绕过人工审批直接 register。
func (h *LoginHandler) handleIssueRegisterPermit(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req issueRegisterPermitReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendActionData(ctx, conn, hdr, actionIssueRegisterPermitResp, issueRegisterPermitResp{Code: 400, Msg: "invalid request"}, true)
		return
	}
	if h.tryForwardAdminUpstream(ctx, conn, hdr, actionIssueRegisterPermit, req, actionIssueRegisterPermitResp, issueRegisterPermitResp{
		Code:     4500,
		Msg:      "authority unavailable",
		DeviceID: strings.TrimSpace(req.DeviceID),
		Role:     strings.TrimSpace(req.Role),
	}) {
		return
	}
	actorID, ok := h.requireActionPermission(conn, hdr, permission.AuthPermitIssue)
	if !ok {
		h.sendActionData(ctx, conn, hdr, actionIssueRegisterPermitResp, issueRegisterPermitResp{Code: 4403, Msg: "permission denied"}, true)
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

// handleRevokeRegisterPermit 撤销尚未消费的 permit，避免后续再次被设备使用。
func (h *LoginHandler) handleRevokeRegisterPermit(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req revokeRegisterPermitReq
	if err := json.Unmarshal(data, &req); err != nil || strings.TrimSpace(req.Permit) == "" {
		h.sendActionData(ctx, conn, hdr, actionRevokeRegisterPermitResp, revokeRegisterPermitResp{Code: 400, Msg: "invalid permit"}, true)
		return
	}
	if h.tryForwardAdminUpstream(ctx, conn, hdr, actionRevokeRegisterPermit, req, actionRevokeRegisterPermitResp, revokeRegisterPermitResp{
		Code:   4500,
		Msg:    "authority unavailable",
		Permit: strings.TrimSpace(req.Permit),
	}) {
		return
	}
	if _, ok := h.requireActionPermission(conn, hdr, permission.AuthPermitRevoke); !ok {
		h.sendActionData(ctx, conn, hdr, actionRevokeRegisterPermitResp, revokeRegisterPermitResp{Code: 4403, Msg: "permission denied"}, true)
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
