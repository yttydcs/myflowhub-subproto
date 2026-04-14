package auth

// 本文件承载 SubProto 中 `auth` 模块里与 `transport` 相关的逻辑。

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
)

// setPending 记录一次需要等待 authority 回包的下游请求。
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

// popPending 取出并消费等待中的请求映射，避免重复回包。
func (h *LoginHandler) popPending(deviceID string) (pendingInfo, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id, ok := h.pendingConn[deviceID]
	if ok {
		delete(h.pendingConn, deviceID)
	}
	return id, ok
}

// buildPendingRespHeader 保留原始 msg/trace id，让回包能和下游请求一一对应。
func (h *LoginHandler) buildPendingRespHeader(ctx context.Context, pending pendingInfo) core.IHeader {
	hdr := h.buildHeader(ctx, nil)
	return hdr.WithMsgID(pending.msgID).WithTraceID(pending.traceID)
}

// sendResp 发送 auth 常规响应，并在必要时自动补当前 HubID。
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

// sendAssistResp 在 assist 链路里优先使用 targeted response，确保响应能回到原始 source。
func (h *LoginHandler) sendAssistResp(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, action string, data respData) {
	if reqHdr != nil && reqHdr.SourceID() != 0 {
		h.sendTargetedResp(ctx, conn, reqHdr, action, data)
		return
	}
	h.sendResp(ctx, conn, reqHdr, action, data)
}

// sendDirectResp 强制走 direct response 语义，用于本地 register/login 等入口。
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

// sendTargetedResp 基于原始 header 构造逐跳可见的 targeted response。
func (h *LoginHandler) sendTargetedResp(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, action string, data respData) {
	if conn == nil || reqHdr == nil {
		return
	}
	msg := message{Action: action}
	raw, _ := json.Marshal(data)
	msg.Data = raw
	payload, _ := json.Marshal(msg)
	if srv := core.ServerFromContext(ctx); srv != nil {
		if data.HubID == 0 {
			data.HubID = srv.NodeID()
			raw, _ = json.Marshal(data)
			msg.Data = raw
			payload, _ = json.Marshal(msg)
		}
		hdr := header.BuildTCPResponse(reqHdr, uint32(len(payload)), 2)
		if err := srv.Send(ctx, conn.ID(), hdr, payload); err != nil && h.log != nil {
			h.log.Warn("send targeted resp failed", "action", action, "err", err)
		}
		return
	}
	codec := header.HeaderTcpCodec{}
	_ = conn.SendWithHeader(header.BuildTCPResponse(reqHdr, uint32(len(payload)), 2), payload, codec)
}

// sendTargetedActionData 是 targeted response 的通用数据版，给 admin action 复用。
func (h *LoginHandler) sendTargetedActionData(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, action string, data any) {
	if conn == nil || reqHdr == nil {
		return
	}
	raw, _ := json.Marshal(data)
	msg := message{Action: action, Data: raw}
	payload, _ := json.Marshal(msg)
	respHdr := header.BuildTCPResponse(reqHdr, uint32(len(payload)), 2)
	if srv := core.ServerFromContext(ctx); srv != nil {
		if err := srv.Send(ctx, conn.ID(), respHdr, payload); err != nil && h.log != nil {
			h.log.Warn("send targeted action data failed", "action", action, "err", err)
		}
		return
	}
	codec := header.HeaderTcpCodec{}
	_ = conn.SendWithHeader(respHdr, payload, codec)
}

// sendActionData 统一封装 admin/list 等 action 的响应发送逻辑。
func (h *LoginHandler) sendActionData(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, action string, data any, direct bool) {
	if conn == nil {
		return
	}
	if direct && reqHdr != nil && reqHdr.TargetID() != 0 && h.routedSourceMatches(ctx, conn, reqHdr) {
		h.sendTargetedActionData(ctx, conn, reqHdr, action, data)
		return
	}
	raw, _ := json.Marshal(data)
	msg := message{Action: action, Data: raw}
	payload, _ := json.Marshal(msg)
	hdr := h.buildHeader(ctx, reqHdr)
	if direct {
		hdr = h.buildDirectRespHeader(ctx, reqHdr)
	}
	if srv := core.ServerFromContext(ctx); srv != nil {
		if err := srv.Send(ctx, conn.ID(), hdr, payload); err != nil && h.log != nil {
			h.log.Warn("send action data failed", "action", action, "err", err)
		}
		return
	}
	codec := header.HeaderTcpCodec{}
	_ = conn.SendWithHeader(hdr, payload, codec)
}

// buildHeader 生成 auth 默认响应头；有请求头时尽量沿用原始链路信息。
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

// buildDirectRespHeader 在 direct response 场景下显式切到 OKResp。
func (h *LoginHandler) buildDirectRespHeader(ctx context.Context, reqHdr core.IHeader) core.IHeader {
	if reqHdr != nil {
		return reqHdr.Clone().WithMajor(header.MajorOKResp)
	}
	return h.buildHeader(ctx, nil)
}

// forward 用于 auth 模块内部的简单单跳转发，不保留原始请求头。
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

// forwardAuthorityRequest 使用当前节点作为 SourceID 向 authority 发起请求。
func (h *LoginHandler) forwardAuthorityRequest(ctx context.Context, authority authoritySelection, reqHdr core.IHeader, action string, data any) bool {
	target := authority.targetNodeID
	if target == 0 {
		target = connectionNodeID(authority.conn)
	}
	source := localNodeID(ctx)
	if source == 0 || target == 0 {
		return false
	}
	return h.sendAuthCmdToTarget(ctx, authority.conn, reqHdr, action, data, source, target, false)
}

// forwardInheritedAuthorityRequest 在转发 admin/assist 请求时保留原始 SourceID。
func (h *LoginHandler) forwardInheritedAuthorityRequest(ctx context.Context, authority authoritySelection, reqHdr core.IHeader, action string, data any) bool {
	target := authority.targetNodeID
	if target == 0 {
		target = connectionNodeID(authority.conn)
	}
	source := uint32(0)
	if reqHdr != nil {
		source = reqHdr.SourceID()
	}
	if source == 0 {
		source = localNodeID(ctx)
	}
	if source == 0 || target == 0 {
		return false
	}
	return h.sendAuthCmdToTarget(ctx, authority.conn, reqHdr, action, data, source, target, true)
}

// sendAuthCmdToTarget 构造带 SourceID/TargetID 的 auth Cmd 帧并发送到下一跳。
func (h *LoginHandler) sendAuthCmdToTarget(ctx context.Context, targetConn core.IConnection, reqHdr core.IHeader, action string, data any, sourceNodeID uint32, targetNodeID uint32, forwarded bool) bool {
	if targetConn == nil || sourceNodeID == 0 || targetNodeID == 0 {
		return false
	}
	payloadData, _ := json.Marshal(data)
	msg := message{Action: action, Data: payloadData}
	payload, _ := json.Marshal(msg)

	var hdr *header.HeaderTcp
	if forwarded {
		var ok bool
		hdr, ok = header.CloneToTCPForForward(reqHdr)
		if !ok {
			return false
		}
	} else if reqHdr != nil {
		hdr = header.CloneToTCP(reqHdr)
	} else {
		hdr = &header.HeaderTcp{}
	}
	hdr.WithMajor(header.MajorCmd).WithSubProto(2).WithSourceID(sourceNodeID).WithTargetID(targetNodeID)

	if srv := core.ServerFromContext(ctx); srv != nil {
		if err := srv.Send(ctx, targetConn.ID(), hdr, payload); err != nil {
			if h.log != nil {
				h.log.Warn("forward authority request failed", "action", action, "target", targetNodeID, "err", err)
			}
			return false
		}
		return true
	}
	codec := header.HeaderTcpCodec{}
	return targetConn.SendWithHeader(hdr, payload, codec) == nil
}

// route index helpers: allow mapping child nodeIDs to the connection carrying them.
// addRouteIndex 在公钥不冲突时把 descendant node 映射到当前连接。
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

// lookupTrustedNodePub 先看持久可信缓存，再退化到连接元数据中的 node_pubkey。
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

// metaPubKey 读取连接上缓存的公钥字节，供路由冲突判断使用。
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

// removeRouteIndex 在节点离线或 revoke 后撤销其树路由索引。
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
