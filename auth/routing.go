package auth

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/header"
)

type authorityMode uint8

const (
	authorityModeLocal authorityMode = iota
	authorityModeRemote
	authorityModeUnavailable
)

type authoritySelection struct {
	mode         authorityMode
	conn         core.IConnection
	targetNodeID uint32
}

func localNodeID(ctx context.Context) uint32 {
	if srv := core.ServerFromContext(ctx); srv != nil {
		return srv.NodeID()
	}
	return 0
}

func (h *LoginHandler) resolveAuthority(ctx context.Context) authoritySelection {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return authoritySelection{mode: authorityModeUnavailable}
	}
	if h.isSemiCentralMode() {
		return h.resolveSemiCentralAuthority(ctx, srv)
	}
	explicitAuthority := h.explicitAuthorityNodeID(srv.Config())
	if explicitAuthority != 0 {
		if c, ok := srv.ConnManager().GetByNode(explicitAuthority); ok {
			return authoritySelection{mode: authorityModeRemote, conn: c, targetNodeID: explicitAuthority}
		}
		return authoritySelection{mode: authorityModeUnavailable}
	}
	if parent := h.selectAuthorityConn(ctx); parent != nil {
		return authoritySelection{mode: authorityModeRemote, conn: parent, targetNodeID: connectionNodeID(parent)}
	}
	if h.parentConfigured(srv.Config()) {
		return authoritySelection{mode: authorityModeUnavailable}
	}
	return authoritySelection{mode: authorityModeLocal}
}

func (h *LoginHandler) resolveSemiCentralAuthority(ctx context.Context, srv core.IServer) authoritySelection {
	if h == nil || srv == nil {
		return authoritySelection{mode: authorityModeUnavailable}
	}
	if parent := h.selectAuthorityConn(ctx); parent != nil {
		if policy, ok := h.currentRuntimeAuthorityPolicy(time.Now().UTC()); ok && policy.effectiveAuthorityID != 0 && policy.effectiveAuthorityID != srv.NodeID() {
			return authoritySelection{
				mode:         authorityModeRemote,
				conn:         parent,
				targetNodeID: policy.effectiveAuthorityID,
			}
		}
		target := connectionNodeID(parent)
		if target == 0 {
			return authoritySelection{mode: authorityModeUnavailable}
		}
		return authoritySelection{
			mode:         authorityModeRemote,
			conn:         parent,
			targetNodeID: target,
		}
	}
	if h.parentConfigured(srv.Config()) {
		return authoritySelection{mode: authorityModeUnavailable}
	}
	return authoritySelection{mode: authorityModeLocal}
}

func (h *LoginHandler) explicitAuthorityNodeID(cfg core.IConfig) uint32 {
	if h == nil {
		return 0
	}
	if h.authNode != 0 {
		return h.authNode
	}
	if cfg == nil {
		return 0
	}
	if raw, ok := cfg.Get("authority.node_id"); ok {
		if id, err := parseUint32(raw); err == nil && id != 0 {
			h.authNode = id
			return id
		}
	}
	return 0
}

func (h *LoginHandler) parentConfigured(cfg core.IConfig) bool {
	if cfg == nil {
		return false
	}
	rawAddr, ok := cfg.Get(coreconfig.KeyParentAddr)
	if !ok || strings.TrimSpace(rawAddr) == "" {
		return false
	}
	rawEnable, ok := cfg.Get(coreconfig.KeyParentEnable)
	if !ok {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(rawEnable)) {
	case "", "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func (s authoritySelection) local() bool {
	return s.mode == authorityModeLocal
}

func (s authoritySelection) remote() bool {
	return s.mode == authorityModeRemote && s.conn != nil
}

func (s authoritySelection) unavailable() bool {
	return s.mode == authorityModeUnavailable
}

func authorityUnavailableResp(deviceID string) respData {
	return respData{
		Code:     4500,
		Msg:      "authority unavailable",
		DeviceID: strings.TrimSpace(deviceID),
		Reason:   "authority unavailable",
	}
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
		if isParentConnLogin(c) {
			parent = c
			return false
		}
		return true
	})
	return parent, parent != nil
}

func isParentConnLogin(c core.IConnection) bool {
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
