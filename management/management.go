package management

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
	"github.com/yttydcs/myflowhub-core/subproto"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

type ManagementHandler struct {
	subproto.ActionBaseSubProcess
	log *slog.Logger
}

func NewHandler(log *slog.Logger) *ManagementHandler {
	if log == nil {
		log = slog.Default()
	}
	h := &ManagementHandler{log: log}
	return h
}

func (h *ManagementHandler) SubProto() uint8 { return SubProtoManagement }

func (h *ManagementHandler) Init() bool {
	h.initActions()
	return true
}

func (h *ManagementHandler) initActions() {
	h.ResetActions()
	for _, act := range registerActions(h) {
		h.RegisterAction(act)
	}
}

func (h *ManagementHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	var frame mgmtMessage
	if err := json.Unmarshal(payload, &frame); err != nil {
		h.log.Warn("management invalid payload", "err", err)
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	if hdr != nil && hdr.TargetID() != 0 && hdr.TargetID() != srv.NodeID() {
		forwarded, code, msg := h.forwardCmdByHeaderTarget(ctx, conn, hdr, payload)
		if forwarded {
			return
		}
		h.sendForwardError(ctx, conn, hdr, frame, code, msg)
		return
	}
	entry, ok := h.LookupAction(frame.Action)
	if !ok {
		h.log.Debug("unknown management action", "action", frame.Action)
		return
	}
	entry.Handle(ctx, conn, hdr, frame.Data)
}

// 内部响应工具
func (h *ManagementHandler) sendActionResp(ctx context.Context, conn core.IConnection, req core.IHeader, action string, data any) {
	resp := mgmtMessage{Action: action}
	raw, _ := json.Marshal(data)
	resp.Data = raw
	body, _ := json.Marshal(resp)
	kit.SendResponse(ctx, h.log, conn, req, body, h.SubProto())
}

func (h *ManagementHandler) forwardCmdByHeaderTarget(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) (bool, int, string) {
	if conn == nil || hdr == nil || len(payload) == 0 {
		return false, 500, "invalid frame"
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil || srv.ConnManager() == nil {
		return false, 500, "no server context"
	}
	target := hdr.TargetID()
	if target == 0 || target == srv.NodeID() {
		return false, 500, "invalid target"
	}

	cm := srv.ConnManager()
	var next core.IConnection
	if c, ok := cm.GetByNode(target); ok && c != nil {
		next = c
	} else {
		next = findParentConn(cm)
	}
	if next == nil {
		return false, 404, "not found"
	}
	if isParentConn(conn) && isParentConn(next) {
		h.log.Warn("drop management frame due to invalid route (came from parent)", "target", target, "source", hdr.SourceID())
		return false, 404, "not found"
	}
	fwdHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		h.log.Warn("drop management frame due to hop_limit", "target", target, "source", hdr.SourceID())
		return false, 500, "hop limit exceeded"
	}
	fwdHdr.WithTargetID(target)
	if err := srv.Send(ctx, next.ID(), fwdHdr, payload); err != nil {
		h.log.Error("forward management frame failed", "err", err, "target", target, "source", hdr.SourceID())
		return false, 500, "forward failed"
	}
	return true, 0, ""
}

func (h *ManagementHandler) sendForwardError(ctx context.Context, conn core.IConnection, req core.IHeader, frame mgmtMessage, code int, msg string) {
	if conn == nil || req == nil {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	action, data := buildForwardError(frame.Action, frame.Data, code, msg)
	if action == "" {
		return
	}
	resp := mgmtMessage{Action: action}
	raw, _ := json.Marshal(data)
	resp.Data = raw
	body, _ := json.Marshal(resp)

	respHdr := header.BuildTCPResponse(req, uint32(len(body)), h.SubProto())
	respHdr.WithSourceID(srv.NodeID())
	_ = srv.Send(ctx, conn.ID(), respHdr, body)
}

func buildForwardError(action string, data json.RawMessage, code int, msg string) (string, any) {
	switch action {
	case actionNodeEcho:
		return actionNodeEchoResp, nodeEchoResp{Code: code, Msg: msg}
	case actionListNodes:
		return actionListNodesResp, listNodesResp{Code: code, Msg: msg}
	case actionListSubtree:
		return actionListSubtreeResp, listSubtreeResp{Code: code, Msg: msg}
	case actionConfigGet:
		var req configGetReq
		_ = json.Unmarshal(data, &req)
		return actionConfigGetResp, configResp{Code: code, Msg: msg, Key: strings.TrimSpace(req.Key)}
	case actionConfigSet:
		var req configSetReq
		_ = json.Unmarshal(data, &req)
		return actionConfigSetResp, configResp{Code: code, Msg: msg, Key: strings.TrimSpace(req.Key), Value: req.Value}
	case actionConfigList:
		return actionConfigListResp, configListResp{Code: code, Msg: msg}
	default:
		return "", nil
	}
}

func isParentConn(c core.IConnection) bool {
	if c == nil {
		return false
	}
	if role, ok := c.GetMeta(core.MetaRoleKey); ok {
		if s, ok2 := role.(string); ok2 && s == core.RoleParent {
			return true
		}
	}
	return false
}

func findParentConn(cm core.IConnectionManager) core.IConnection {
	if cm == nil {
		return nil
	}
	var parent core.IConnection
	cm.Range(func(c core.IConnection) bool {
		if isParentConn(c) {
			parent = c
			return false
		}
		return true
	})
	return parent
}

