package auth

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func (h *LoginHandler) handleUpLogin(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req upLoginData
	if err := json.Unmarshal(data, &req); err != nil || req.NodeID == 0 {
		return
	}
	pub, raw, err := parseECPubKey(req.PubKey)
	if err != nil || pub == nil || len(raw) == 0 {
		return
	}
	// 验证设备签名链路
	if strings.TrimSpace(req.DeviceSig) == "" || !strings.EqualFold(strings.TrimSpace(req.DeviceAlg), defaultAlgES256) {
		return
	}
	ld := loginData{
		DeviceID: req.DeviceID,
		NodeID:   req.NodeID,
		TS:       req.DeviceTS,
		Nonce:    req.DeviceNonce,
		Alg:      req.DeviceAlg,
		Sig:      req.DeviceSig,
	}
	if !verifyEcdsaSig(pub, loginSignBytes(ld), req.DeviceSig) {
		return
	}
	// 验证 sender 签名
	if strings.TrimSpace(req.SenderSig) == "" || !strings.EqualFold(strings.TrimSpace(req.SenderAlg), defaultAlgES256) || req.SenderID == 0 {
		return
	}
	senderPub := h.lookupTrustedNodePub(req.SenderID, conn)
	if senderPub == nil && strings.TrimSpace(req.SenderPub) != "" {
		if pub, raw, err := parseECPubKey(req.SenderPub); err == nil {
			senderPub = pub
			h.addTrustedNode(req.SenderID, req.SenderPub)
			conn.SetMeta("node_pubkey", raw)
		}
	}
	if senderPub == nil || !verifyEcdsaSig(senderPub, upLoginSenderSignBytes(req), req.SenderSig) {
		return
	}
	// 检查路由冲突
	if !h.canAddRoute(ctx, req.NodeID, raw) {
		return
	}
	conn.SetMeta("pubkey", raw)
	h.addRouteIndex(ctx, req.NodeID, conn)
	h.sendResp(ctx, conn, hdr, actionUpLoginResp, respData{Code: 1, Msg: "ok", NodeID: req.NodeID, PubKey: req.PubKey})
}

func (h *LoginHandler) sendUpLogin(ctx context.Context, conn core.IConnection, deviceID string, nodeID uint32, pubKey []byte, devSig, devAlg string, devTS int64, devNonce string) {
	parent := h.selectAuthorityConn(ctx)
	if parent == nil || nodeID == 0 || len(pubKey) == 0 || strings.TrimSpace(devSig) == "" || !strings.EqualFold(strings.TrimSpace(devAlg), defaultAlgES256) {
		return
	}
	if h.nodePriv == nil || strings.TrimSpace(h.nodePubB64) == "" {
		return
	}
	data := upLoginData{
		NodeID:      nodeID,
		DeviceID:    deviceID,
		HubID:       localNodeID(ctx),
		PubKey:      encodePubKey(pubKey),
		TS:          time.Now().Unix(),
		DeviceTS:    devTS,
		DeviceNonce: devNonce,
		DeviceSig:   devSig,
		DeviceAlg:   devAlg,
		SenderID:    localNodeID(ctx),
		SenderTS:    time.Now().Unix(),
		SenderNonce: "",
		SenderAlg:   defaultAlgES256,
		SenderPub:   encodePubKey(pubKey),
		Alg:         defaultAlgES256,
	}
	data.SenderSig = signWithNodeKey(h.nodePriv, upLoginSenderSignBytes(data))
	raw, _ := json.Marshal(data)
	payload, _ := json.Marshal(message{Action: actionUpLogin, Data: raw})
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2)
	if srv := core.ServerFromContext(ctx); srv != nil {
		hdr.WithSourceID(srv.NodeID())
	}
	if conn != nil {
		if nid, ok := conn.GetMeta("nodeID"); ok {
			if v, ok2 := nid.(uint32); ok2 {
				hdr.WithTargetID(v)
			}
		}
	}
	if srv := core.ServerFromContext(ctx); srv != nil {
		_ = srv.Send(ctx, parent.ID(), hdr, payload)
		return
	}
}

func registerUpLoginActions(h *LoginHandler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionUpLogin, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleUpLogin(ctx, conn, hdr, data)
		}, kit.WithRequireAuth(true)),
		kit.NewAction(actionUpLoginResp, nil, kit.WithRequireAuth(true)),
	}
}
