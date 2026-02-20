package auth

import (
	"context"
	"encoding/json"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/header"
)

func localNodeID(ctx context.Context) uint32 {
	if srv := core.ServerFromContext(ctx); srv != nil {
		return srv.NodeID()
	}
	return 0
}

func (h *LoginHandler) selectAuthority(ctx context.Context) core.IConnection {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return nil
	}
	if h.authNode == 0 && srv.Config() != nil {
		if raw, ok := srv.Config().Get(coreconfig.KeyParentAddr); ok && raw != "" {
			// no explicit node id, use parent conn if exists
		}
		if raw, ok := srv.Config().Get("authority.node_id"); ok {
			if id, err := parseUint32(raw); err == nil && id != 0 {
				h.authNode = id
			}
		}
	}
	if h.authNode != 0 {
		if c, ok := srv.ConnManager().GetByNode(h.authNode); ok {
			return c
		}
	}
	if parent := h.selectAuthorityConn(ctx); parent != nil {
		return parent
	}
	return nil
}

func (h *LoginHandler) selectAuthorityConn(ctx context.Context) core.IConnection {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return nil
	}
	if c, ok := findParentConnLogin(srv.ConnManager()); ok {
		return c
	}
	return nil
}

func findParentConnLogin(cm core.IConnectionManager) (core.IConnection, bool) {
	if cm == nil {
		return nil, false
	}
	var parent core.IConnection
	cm.Range(func(c core.IConnection) bool {
		if role, ok := c.GetMeta(core.MetaRoleKey); ok {
			if s, ok2 := role.(string); ok2 && s == core.RoleParent {
				parent = c
				return false
			}
		}
		return true
	})
	return parent, parent != nil
}

func (h *LoginHandler) broadcast(ctx context.Context, src core.IConnection, action string, data any) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	payloadData, _ := json.Marshal(data)
	msg := message{Action: action, Data: payloadData}
	payload, _ := json.Marshal(msg)
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2)
	if srv != nil {
		hdr.WithSourceID(srv.NodeID())
	}
	srv.ConnManager().Range(func(c core.IConnection) bool {
		if src != nil && c.ID() == src.ID() {
			return true
		}
		if err := srv.Send(ctx, c.ID(), hdr, payload); err != nil {
			h.log.Warn("broadcast revoke failed", "conn", c.ID(), "err", err)
		}
		return true
	})
}

func filterRolePerms(entries []rolePermEntry, req listRolesReq) ([]rolePermEntry, int) {
	roleFilter := strings.TrimSpace(req.Role)
	nodeFilter := make(map[uint32]bool)
	for _, id := range req.NodeIDs {
		if id != 0 {
			nodeFilter[id] = true
		}
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	filtered := make([]rolePermEntry, 0, len(entries))
	for _, e := range entries {
		if roleFilter != "" && e.Role != roleFilter {
			continue
		}
		if len(nodeFilter) > 0 && !nodeFilter[e.NodeID] {
			continue
		}
		filtered = append(filtered, e)
	}
	total := len(filtered)
	if offset >= total {
		return []rolePermEntry{}, total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return filtered[offset:end], total
}
