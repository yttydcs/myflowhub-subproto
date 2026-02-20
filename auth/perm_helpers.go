package auth

import (
	"context"
	"encoding/json"
	"sort"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
)

// 权限配置加载
func (h *LoginHandler) loadAuthConfig(cfg core.IConfig) {
	if cfg != nil {
		h.permCfg = permission.SharedConfig(cfg)
	}
	if h.permCfg == nil {
		h.permCfg = permission.NewConfig(nil)
	}
}

func (h *LoginHandler) resolveRole(nodeID uint32) string {
	if h.permCfg == nil {
		return ""
	}
	return h.permCfg.ResolveRole(nodeID)
}

func (h *LoginHandler) resolvePerms(nodeID uint32) []string {
	if h.permCfg == nil {
		return nil
	}
	return h.permCfg.ResolvePerms(nodeID)
}

func (h *LoginHandler) resolveRolePerms(nodeID uint32) (string, []string) {
	return h.resolveRole(nodeID), h.resolvePerms(nodeID)
}

func (h *LoginHandler) applyRolePerms(deviceID string, nodeID uint32, role string, perms []string, conn core.IConnection) {
	if role == "" && len(perms) == 0 {
		return
	}
	if h.permCfg != nil {
		h.permCfg.UpsertNode(nodeID, role, perms)
	}
	h.mu.Lock()
	rec, ok := h.whitelist[deviceID]
	if ok && rec.NodeID == nodeID {
		if role != "" {
			rec.Role = role
		}
		if perms != nil {
			rec.Perms = cloneSlice(perms)
		}
		h.whitelist[deviceID] = rec
	}
	h.mu.Unlock()
	if conn != nil {
		if role != "" {
			conn.SetMeta("role", role)
		}
		if perms != nil {
			conn.SetMeta("perms", cloneSlice(perms))
		}
	}
}

func (h *LoginHandler) lookupByNode(nodeID uint32) (role string, perms []string, ok bool) {
	h.mu.RLock()
	for _, rec := range h.whitelist {
		if rec.NodeID == nodeID {
			role = rec.Role
			perms = cloneSlice(rec.Perms)
			ok = true
			break
		}
	}
	h.mu.RUnlock()
	if ok && role != "" {
		return role, perms, true
	}
	role = h.resolveRole(nodeID)
	perms = h.resolvePerms(nodeID)
	if role == "" && len(perms) == 0 {
		return "", nil, false
	}
	return role, perms, true
}

func (h *LoginHandler) listRolePerms() []rolePermEntry {
	seen := make(map[uint32]bool)
	h.mu.RLock()
	for _, rec := range h.whitelist {
		if rec.NodeID == 0 {
			continue
		}
		seen[rec.NodeID] = true
	}
	h.mu.RUnlock()

	var nodeRoles map[uint32]string
	if h.permCfg != nil {
		nodeRoles = h.permCfg.NodeRoles()
	}
	entries := make([]rolePermEntry, 0, len(seen)+len(nodeRoles))
	for nid := range seen {
		role, perms, ok := h.lookupByNode(nid)
		if !ok {
			continue
		}
		entries = append(entries, rolePermEntry{NodeID: nid, Role: role, Perms: perms})
	}
	for nid, role := range nodeRoles {
		if seen[nid] {
			continue
		}
		perms := h.resolvePerms(nid)
		entries = append(entries, rolePermEntry{NodeID: nid, Role: role, Perms: perms})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].NodeID < entries[j].NodeID })
	return entries
}

func (h *LoginHandler) invalidateCache(nodeIDs []uint32) {
	targets := make(map[uint32]bool)
	for _, id := range nodeIDs {
		if id != 0 {
			targets[id] = true
		}
	}
	if h.permCfg != nil {
		h.permCfg.InvalidateNodes(nodeIDs)
	}
	h.mu.Lock()
	if len(targets) == 0 {
		for k, rec := range h.whitelist {
			rec.Role = ""
			rec.Perms = nil
			h.whitelist[k] = rec
		}
	} else {
		for k, rec := range h.whitelist {
			if targets[rec.NodeID] {
				rec.Role = ""
				rec.Perms = nil
				h.whitelist[k] = rec
			}
		}
	}
	h.mu.Unlock()
}

func (h *LoginHandler) refreshPerms(ctx context.Context, nodeIDs []uint32) {
	if len(nodeIDs) == 0 {
		h.requestPermSnapshot(ctx)
		return
	}
	authority := h.selectAuthority(ctx)
	if authority == nil {
		return
	}
	seen := make(map[uint32]bool)
	for _, id := range nodeIDs {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		req := permsQueryData{NodeID: id}
		h.forward(ctx, authority, actionGetPerms, req)
	}
}

func (h *LoginHandler) requestPermSnapshot(ctx context.Context) {
	authority := h.selectAuthority(ctx)
	if authority == nil {
		return
	}
	h.forward(ctx, authority, actionPermsSnapshot, permission.Snapshot{})
}

func (h *LoginHandler) applyPermSnapshot(ctx context.Context, snap permission.Snapshot) {
	if h.permCfg == nil {
		h.permCfg = permission.NewConfig(nil)
	}
	h.permCfg.ApplySnapshot(snap)
	h.mu.Lock()
	for deviceID, rec := range h.whitelist {
		if rec.NodeID == 0 {
			continue
		}
		rec.Role = h.resolveRole(rec.NodeID)
		rec.Perms = h.resolvePerms(rec.NodeID)
		h.whitelist[deviceID] = rec
	}
	h.mu.Unlock()
	h.refreshConnMetas(ctx)
}

func (h *LoginHandler) refreshConnMetas(ctx context.Context) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	cm := srv.ConnManager()
	if cm == nil {
		return
	}
	cm.Range(func(c core.IConnection) bool {
		nodeMeta, ok := c.GetMeta("nodeID")
		if !ok {
			return true
		}
		nodeID, ok := nodeMeta.(uint32)
		if !ok || nodeID == 0 {
			return true
		}
		role := h.resolveRole(nodeID)
		perms := h.resolvePerms(nodeID)
		if role != "" {
			c.SetMeta("role", role)
		}
		c.SetMeta("perms", cloneSlice(perms))
		return true
	})
}

func (h *LoginHandler) broadcastPermsSnapshot(ctx context.Context, src core.IConnection, raw json.RawMessage) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	cm := srv.ConnManager()
	if cm == nil {
		return
	}
	msg := message{Action: actionPermsSnapshot, Data: raw}
	body, _ := json.Marshal(msg)
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2).WithSourceID(srv.NodeID()).WithTargetID(0)
	cm.Range(func(c core.IConnection) bool {
		if src != nil && c.ID() == src.ID() {
			return true
		}
		if role, ok := c.GetMeta(core.MetaRoleKey); ok {
			if s, ok2 := role.(string); ok2 && s == core.RoleParent {
				return true
			}
		}
		if err := srv.Send(ctx, c.ID(), hdr, body); err != nil {
			h.log.Warn("broadcast perms snapshot failed", "conn", c.ID(), "err", err)
		}
		return true
	})
}
