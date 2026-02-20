package forward

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

// DefaultForwardHandler 丢弃未知子协议，或按配置转发到指定节点；若未配置则默认尝试转发给父节点。
type DefaultForwardHandler struct {
	log            *slog.Logger
	forward        bool
	subTargets     map[uint8]uint32
	fallbackParent bool
}

func NewDefaultForwardHandler(cfg core.IConfig, log *slog.Logger) *DefaultForwardHandler {
	if log == nil {
		log = slog.Default()
	}
	h := &DefaultForwardHandler{
		log:            log,
		subTargets:     make(map[uint8]uint32),
		fallbackParent: true, // 未配置时默认转发给父节点
	}
	if cfg != nil {
		if raw, ok := cfg.Get(coreconfig.KeyDefaultForwardEnable); ok {
			if strings.TrimSpace(raw) != "" { // 显式配置才覆盖默认行为
				h.forward = core.ParseBool(raw, false)
				h.fallbackParent = false
			}
		}
		if raw, ok := cfg.Get(coreconfig.KeyDefaultForwardTarget); ok {
			if id, err := parseUint32(raw); err == nil {
				h.subTargets[0] = id
			}
		}
		if raw, ok := cfg.Get(coreconfig.KeyDefaultForwardMap); ok {
			h.loadMap(raw)
		}
	}
	return h
}

func (h *DefaultForwardHandler) loadMap(raw string) {
	pairs := strings.Split(raw, ";")
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		subID, err1 := strconv.ParseUint(strings.TrimSpace(kv[0]), 10, 8)
		nodeID, err2 := parseUint32(kv[1])
		if err1 != nil || err2 != nil {
			continue
		}
		h.subTargets[uint8(subID)] = nodeID
	}
}

func (h *DefaultForwardHandler) SubProto() uint8 { return 0 }

func (h *DefaultForwardHandler) Init() bool { return true }

func (h *DefaultForwardHandler) AcceptCmd() bool { return false }

func (h *DefaultForwardHandler) AllowSourceMismatch() bool { return false }

func (h *DefaultForwardHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	if hdr == nil {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		h.log.Warn("no server context, cannot forward", "conn", conn.ID())
		return
	}
	targetNode := h.resolveTarget(hdr.SubProto())
	if h.forward && targetNode != 0 {
		targetHeader := kit.CloneWithTarget(hdr, targetNode)
		if targetHeader == nil {
			h.log.Warn("cannot clone header for forwarding")
			return
		}
		targetHeader.WithSourceID(srv.NodeID())
		h.forwardToNode(ctx, srv, targetHeader, payload)
		return
	}
	if h.forward && targetNode == 0 {
		h.log.Debug("no default route for subproto", "subproto", hdr.SubProto())
		return
	}
	// 未配置或未命中：尝试转发给父节点
	if h.fallbackParent {
		h.forwardToParent(ctx, srv, hdr, payload)
		return
	}
	h.log.Debug("unknown subproto dropped", "subproto", hdr.SubProto(), "conn", conn.ID())
}

func (h *DefaultForwardHandler) resolveTarget(sub uint8) uint32 {
	if target, ok := h.subTargets[sub]; ok {
		return target
	}
	return h.subTargets[0]
}

func (h *DefaultForwardHandler) forwardToNode(ctx context.Context, srv core.IServer, hdr core.IHeader, payload []byte) {
	cm := srv.ConnManager()
	if conn, ok := cm.GetByNode(hdr.TargetID()); ok {
		if err := srv.Send(ctx, conn.ID(), hdr, payload); err != nil {
			h.log.Error("default forward failed", "err", err, "target", hdr.TargetID())
		}
		return
	}
	// fallback scan
	var forwarded bool
	cm.Range(func(conn core.IConnection) bool {
		if nodeID, ok := conn.GetMeta("nodeID"); ok {
			if nid, ok2 := nodeID.(uint32); ok2 && nid == hdr.TargetID() {
				forwarded = true
				if err := srv.Send(ctx, conn.ID(), hdr, payload); err != nil {
					h.log.Error("default forward failed", "err", err, "target", hdr.TargetID())
				}
				return false
			}
		}
		return true
	})
	if !forwarded {
		h.log.Warn("default target not found", "target", hdr.TargetID())
	}
}

func (h *DefaultForwardHandler) forwardToParent(ctx context.Context, srv core.IServer, hdr core.IHeader, payload []byte) {
	parent, ok := findParentConn(srv.ConnManager())
	if !ok {
		h.log.Debug("no parent connection, drop unknown subproto", "subproto", hdr.SubProto())
		return
	}
	clone := kit.CloneRequest(hdr)
	if clone == nil {
		h.log.Warn("cannot clone header for parent forward")
		return
	}
	clone.WithSourceID(srv.NodeID())
	if err := srv.Send(ctx, parent.ID(), clone, payload); err != nil {
		h.log.Error("forward to parent failed", "err", err, "conn", parent.ID())
	}
}

func findParentConn(cm core.IConnectionManager) (core.IConnection, bool) {
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

func parseUint32(v string) (uint32, error) {
	val, err := strconv.ParseUint(strings.TrimSpace(v), 10, 32)
	return uint32(val), err
}
