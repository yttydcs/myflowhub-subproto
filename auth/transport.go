package auth

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
)

func (h *LoginHandler) setPending(deviceID, connID string, hdr core.IHeader) {
	var msgID uint32
	var traceID uint32
	if hdr != nil {
		msgID = hdr.GetMsgID()
		traceID = hdr.GetTraceID()
	}
	h.mu.Lock()
	h.pendingConn[deviceID] = pendingInfo{
		connID:  connID,
		msgID:   msgID,
		traceID: traceID,
	}
	h.mu.Unlock()
}

func (h *LoginHandler) popPending(deviceID string) (pendingInfo, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id, ok := h.pendingConn[deviceID]
	if ok {
		delete(h.pendingConn, deviceID)
	}
	return id, ok
}

func (h *LoginHandler) buildPendingRespHeader(ctx context.Context, pending pendingInfo) core.IHeader {
	hdr := h.buildHeader(ctx, nil)
	return hdr.WithMsgID(pending.msgID).WithTraceID(pending.traceID)
}

func (h *LoginHandler) sendResp(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, action string, data respData) {
	msg := message{Action: action}
	raw, _ := json.Marshal(data)
	msg.Data = raw
	payload, _ := json.Marshal(msg)
	hdr := h.buildHeader(ctx, reqHdr)
	if srv := core.ServerFromContext(ctx); srv != nil {
		if data.HubID == 0 {
			data.HubID = srv.NodeID()
			raw, _ = json.Marshal(data)
			msg.Data = raw
			payload, _ = json.Marshal(msg)
		}
		if conn != nil {
			if err := srv.Send(ctx, conn.ID(), hdr, payload); err != nil {
				h.log.Warn("send resp failed", "err", err)
			}
			return
		}
	}
	if conn != nil {
		codec := header.HeaderTcpCodec{}
		_ = conn.SendWithHeader(hdr, payload, codec)
	}
}

func (h *LoginHandler) sendDirectResp(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, action string, data respData) {
	msg := message{Action: action}
	raw, _ := json.Marshal(data)
	msg.Data = raw
	payload, _ := json.Marshal(msg)
	hdr := h.buildDirectRespHeader(ctx, reqHdr)
	if srv := core.ServerFromContext(ctx); srv != nil {
		if data.HubID == 0 {
			data.HubID = srv.NodeID()
			raw, _ = json.Marshal(data)
			msg.Data = raw
			payload, _ = json.Marshal(msg)
		}
		if conn != nil {
			if err := srv.Send(ctx, conn.ID(), hdr, payload); err != nil {
				h.log.Warn("send resp failed", "err", err)
			}
			return
		}
	}
	if conn != nil {
		codec := header.HeaderTcpCodec{}
		_ = conn.SendWithHeader(hdr, payload, codec)
	}
}

func (h *LoginHandler) buildHeader(ctx context.Context, reqHdr core.IHeader) core.IHeader {
	if reqHdr != nil {
		return reqHdr.Clone()
	}
	base := &header.HeaderTcp{}
	src := uint32(0)
	if srv := core.ServerFromContext(ctx); srv != nil {
		src = srv.NodeID()
	}
	return base.WithMajor(header.MajorOKResp).WithSubProto(2).WithSourceID(src).WithTargetID(0)
}

func (h *LoginHandler) buildDirectRespHeader(ctx context.Context, reqHdr core.IHeader) core.IHeader {
	if reqHdr != nil {
		return reqHdr.Clone().WithMajor(header.MajorOKResp)
	}
	return h.buildHeader(ctx, nil)
}

func (h *LoginHandler) forward(ctx context.Context, targetConn core.IConnection, action string, data any) {
	if targetConn == nil {
		return
	}
	payloadData, _ := json.Marshal(data)
	msg := message{Action: action, Data: payloadData}
	payload, _ := json.Marshal(msg)
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2)
	if srv := core.ServerFromContext(ctx); srv != nil {
		hdr.WithSourceID(srv.NodeID())
	}
	if nid, ok := targetConn.GetMeta("nodeID"); ok {
		if v, ok2 := nid.(uint32); ok2 {
			hdr.WithTargetID(v)
		}
	}
	if srv := core.ServerFromContext(ctx); srv != nil {
		_ = srv.Send(ctx, targetConn.ID(), hdr, payload)
		return
	}
	codec := header.HeaderTcpCodec{}
	_ = targetConn.SendWithHeader(hdr, payload, codec)
}

// route index helpers: allow mapping child nodeIDs to the connection carrying them.
func (h *LoginHandler) addRouteIndex(ctx context.Context, nodeID uint32, conn core.IConnection) {
	if nodeID == 0 || conn == nil {
		return
	}
	if srv := core.ServerFromContext(ctx); srv != nil {
		if cm := srv.ConnManager(); cm != nil {
			if !h.canAddRoute(ctx, nodeID, metaPubKey(conn)) {
				return
			}
			cm.AddNodeIndex(nodeID, conn)
		}
	}
}

func (h *LoginHandler) lookupTrustedNodePub(nodeID uint32, conn core.IConnection) *ecdsa.PublicKey {
	if nodeID == 0 {
		return nil
	}
	if raw, ok := h.trustedNode[nodeID]; ok && len(raw) > 0 {
		if pub, err := parseECPubKeyRaw(raw); err == nil {
			return pub
		}
	}
	// 尝试从连接元数据获取
	if conn != nil {
		if v, ok := conn.GetMeta("node_pubkey"); ok {
			if b, ok2 := v.([]byte); ok2 && len(b) > 0 {
				if pub, err := parseECPubKeyRaw(b); err == nil {
					return pub
				}
			}
		}
	}
	return nil
}

// canAddRoute 检查同 nodeID 是否已存在不同公钥的路由，防止占用。
func (h *LoginHandler) canAddRoute(ctx context.Context, nodeID uint32, newPub []byte) bool {
	if nodeID == 0 {
		return false
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return true
	}
	cm := srv.ConnManager()
	if cm == nil {
		return true
	}
	existing, ok := cm.GetByNode(nodeID)
	if !ok || existing == nil {
		return true
	}
	oldPub := metaPubKey(existing)
	if len(oldPub) == 0 || len(newPub) == 0 {
		return true
	}
	if len(oldPub) == len(newPub) {
		match := true
		for i := range oldPub {
			if oldPub[i] != newPub[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	if h.log != nil {
		h.log.Warn("reject route update due to pubkey conflict", "node", nodeID)
	}
	return false
}

func metaPubKey(conn core.IConnection) []byte {
	if conn == nil {
		return nil
	}
	if v, ok := conn.GetMeta("pubkey"); ok {
		if b, ok2 := v.([]byte); ok2 {
			return b
		}
	}
	return nil
}

func (h *LoginHandler) removeRouteIndex(ctx context.Context, nodeID uint32) {
	if nodeID == 0 {
		return
	}
	if srv := core.ServerFromContext(ctx); srv != nil {
		if cm := srv.ConnManager(); cm != nil {
			cm.RemoveNodeIndex(nodeID)
		}
	}
}
