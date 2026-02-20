package auth

import (
	"context"
	"encoding/json"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func (h *LoginHandler) handleGetPerms(ctx context.Context, conn core.IConnection, data json.RawMessage) {
	var req permsQueryData
	if err := json.Unmarshal(data, &req); err != nil || req.NodeID == 0 {
		h.sendResp(ctx, conn, nil, actionGetPermsResp, respData{Code: 400, Msg: "invalid node id"})
		return
	}
	role, perms, ok := h.lookupByNode(req.NodeID)
	if !ok {
		h.sendResp(ctx, conn, nil, actionGetPermsResp, respData{Code: 4404, Msg: "not found", NodeID: req.NodeID})
		return
	}
	h.sendResp(ctx, conn, nil, actionGetPermsResp, respData{Code: 1, Msg: "ok", NodeID: req.NodeID, Role: role, Perms: perms})
}

func (h *LoginHandler) handleListRoles(ctx context.Context, conn core.IConnection, data json.RawMessage) {
	var req listRolesReq
	_ = json.Unmarshal(data, &req)
	snapshot := h.listRolePerms()
	filtered, total := filterRolePerms(snapshot, req)
	resp := struct {
		Code  int             `json:"code"`
		Msg   string          `json:"msg,omitempty"`
		Total int             `json:"total"`
		Roles []rolePermEntry `json:"roles,omitempty"`
	}{
		Code:  1,
		Msg:   "ok",
		Total: total,
		Roles: filtered,
	}
	raw, _ := json.Marshal(resp)
	msg := message{Action: actionListRolesResp, Data: raw}
	body, _ := json.Marshal(msg)
	hdr := h.buildHeader(ctx, nil)
	if srv := core.ServerFromContext(ctx); srv != nil && conn != nil {
		_ = srv.Send(ctx, conn.ID(), hdr, body)
		return
	}
	if conn != nil {
		_ = conn.SendWithHeader(hdr, body, header.HeaderTcpCodec{})
	}
}

func (h *LoginHandler) handlePermsInvalidate(ctx context.Context, data json.RawMessage) {
	var req invalidateData
	_ = json.Unmarshal(data, &req)
	h.invalidateCache(req.NodeIDs)
	if req.Refresh {
		h.refreshPerms(ctx, req.NodeIDs)
	}
	// 清理当前连接的 meta
	if srv := core.ServerFromContext(ctx); srv != nil {
		if cm := srv.ConnManager(); cm != nil {
			targets := make(map[uint32]bool)
			for _, id := range req.NodeIDs {
				if id != 0 {
					targets[id] = true
				}
			}
			cm.Range(func(c core.IConnection) bool {
				if len(targets) == 0 {
					c.SetMeta("role", "")
					c.SetMeta("perms", []string(nil))
					return true
				}
				if nid, ok := c.GetMeta("nodeID"); ok {
					if v, ok2 := nid.(uint32); ok2 && targets[v] {
						c.SetMeta("role", "")
						c.SetMeta("perms", []string(nil))
					}
				}
				return true
			})
		}
		// 广播给子节点（不回父）
		srv.ConnManager().Range(func(c core.IConnection) bool {
			if role, ok := c.GetMeta(core.MetaRoleKey); ok {
				if s, ok2 := role.(string); ok2 && s == core.RoleParent {
					return true
				}
			}
			msg := message{Action: actionPermsInvalidate, Data: data}
			body, _ := json.Marshal(msg)
			hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2).WithSourceID(srv.NodeID()).WithTargetID(0)
			_ = srv.Send(ctx, c.ID(), hdr, body)
			return true
		})
	}
}

func (h *LoginHandler) handlePermsSnapshot(ctx context.Context, conn core.IConnection, data json.RawMessage) {
	if len(data) == 0 {
		return
	}
	var snap permission.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		h.log.Warn("invalid perms snapshot", "err", err)
		return
	}
	h.applyPermSnapshot(ctx, snap)
	// forward downstream except parent
	h.broadcastPermsSnapshot(ctx, conn, data)
}

func registerPermActions(h *LoginHandler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionGetPerms, func(ctx context.Context, conn core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleGetPerms(ctx, conn, data)
		}, kit.WithRequireAuth(true)),
		kit.NewAction(actionListRoles, func(ctx context.Context, conn core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleListRoles(ctx, conn, data)
		}, kit.WithRequireAuth(true)),
		kit.NewAction(actionPermsInvalidate, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handlePermsInvalidate(ctx, data)
		}, kit.WithRequireAuth(true)),
		kit.NewAction(actionPermsSnapshot, func(ctx context.Context, conn core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handlePermsSnapshot(ctx, conn, data)
		}, kit.WithRequireAuth(true)),
	}
}
