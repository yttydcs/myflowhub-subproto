package auth

import (
	"context"

	core "github.com/yttydcs/myflowhub-core"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
)

func (h *LoginHandler) saveBinding(ctx context.Context, conn core.IConnection, deviceID string, nodeID uint32, pubKey []byte) {
	role, perms := h.resolveRolePerms(nodeID)
	h.mu.Lock()
	h.whitelist[deviceID] = bindingRecord{NodeID: nodeID, Role: role, Perms: perms, PubKey: cloneSlice(pubKey)}
	h.mu.Unlock()
	conn.SetMeta("nodeID", nodeID)
	conn.SetMeta("deviceID", deviceID)
	conn.SetMeta("role", role)
	conn.SetMeta("perms", perms)
	if len(pubKey) > 0 {
		conn.SetMeta("pubkey", pubKey)
	}
	if srv := core.ServerFromContext(ctx); srv != nil {
		if cm := srv.ConnManager(); cm != nil {
			cm.UpdateNodeIndex(nodeID, conn)
			cm.UpdateDeviceIndex(deviceID, conn)
			h.addRouteIndex(ctx, nodeID, conn)
		}
	}
}

func (h *LoginHandler) applyHubID(ctx context.Context, conn core.IConnection, hubID uint32) {
	if conn == nil {
		return
	}
	if hubID == 0 {
		hubID = localNodeID(ctx)
	}
	if hubID != 0 {
		conn.SetMeta("hubID", hubID)
	}
}

func (h *LoginHandler) removeBinding(deviceID string) (removed bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.whitelist[deviceID]; !ok {
		return false
	}
	delete(h.whitelist, deviceID)
	return true
}

func (h *LoginHandler) removeIndexes(ctx context.Context, nodeID uint32, conn core.IConnection) {
	if srv := core.ServerFromContext(ctx); srv != nil {
		if cm := srv.ConnManager(); cm != nil {
			if nodeID != 0 {
				cm.UpdateNodeIndex(nodeID, nil)
			}
			h.removeRouteIndex(ctx, nodeID)
		}
	}
}

func (h *LoginHandler) lookup(deviceID string) (bindingRecord, bool) {
	h.mu.RLock()
	rec, ok := h.whitelist[deviceID]
	h.mu.RUnlock()
	if ok && rec.Role == "" {
		rec.Role, rec.Perms = h.resolveRolePerms(rec.NodeID)
		h.mu.Lock()
		h.whitelist[deviceID] = rec
		h.mu.Unlock()
	}
	return rec, ok
}

func (h *LoginHandler) hasPermission(nodeID uint32, perm string) bool {
	if perm == "" || nodeID == 0 {
		return true
	}
	_, perms, ok := h.lookupByNode(nodeID)
	if !ok {
		return false
	}
	for _, entry := range perms {
		if entry == permission.Wildcard || entry == perm {
			return true
		}
	}
	return false
}

func (h *LoginHandler) ensureNodeID(deviceID string) uint32 {
	h.mu.RLock()
	if rec, ok := h.whitelist[deviceID]; ok {
		h.mu.RUnlock()
		return rec.NodeID
	}
	h.mu.RUnlock()
	next := h.nextID.Add(1) - 1
	return next
}

func (h *LoginHandler) sourceMatches(conn core.IConnection, hdr core.IHeader) bool {
	if conn == nil || hdr == nil {
		return false
	}
	meta, ok := conn.GetMeta("nodeID")
	if !ok {
		return false
	}
	nid, ok := meta.(uint32)
	if !ok || nid == 0 {
		return false
	}
	return hdr.SourceID() == nid
}

// persistState 持久化 whitelist 与 trustedNode。
func (h *LoginHandler) persistState() {
	h.mu.RLock()
	bindings := make(map[string]bindingRecord, len(h.whitelist))
	for dev, rec := range h.whitelist {
		bindings[dev] = bindingRecord{
			NodeID: rec.NodeID,
			Role:   rec.Role,
			Perms:  cloneSlice(rec.Perms),
			PubKey: cloneSlice(rec.PubKey),
		}
	}
	trusted := make(map[uint32][]byte, len(h.trustedNode)+len(bindings))
	for id, pk := range h.trustedNode {
		trusted[id] = cloneSlice(pk)
	}
	for _, rec := range bindings {
		if rec.NodeID != 0 && len(rec.PubKey) > 0 {
			if _, ok := trusted[rec.NodeID]; !ok {
				trusted[rec.NodeID] = cloneSlice(rec.PubKey)
			}
		}
	}
	h.mu.RUnlock()
	saveTrustedBindings(bindings, trusted)
}
