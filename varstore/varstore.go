package varstore

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/eventbus"
	"github.com/yttydcs/myflowhub-core/header"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	"github.com/yttydcs/myflowhub-core/subproto"
)

type VarStoreHandler struct {
	subproto.ActionBaseSubProcess
	log *slog.Logger

	mu      sync.RWMutex
	records map[string]varRecord           // key: owner:name
	pending map[pendingKey][]pendingWaiter // (owner,name,kind) -> waiting downstream requests
	cache   map[string]map[uint32]bool     // name -> owners known

	subscriptions map[string]map[uint32]map[string]struct{} // key -> subscriber node -> connIDs
	connSubs      map[string]map[string]struct{}            // connID -> key set
	upstreamSubs  map[string]bool                           // key -> subscribed upstream
	pendingSubs   map[pendingKey][]pendingSubscriber        // pending subscribe (with subscriber id)
	eventSubOnce  sync.Once

	permCfg *permission.Config
}

func NewVarStoreHandler(log *slog.Logger) *VarStoreHandler {
	return NewVarStoreHandlerWithConfig(nil, log)
}

func NewVarStoreHandlerWithConfig(cfg core.IConfig, log *slog.Logger) *VarStoreHandler {
	if log == nil {
		log = slog.Default()
	}
	if cfg == nil {
		cfg = coreconfig.NewMap(map[string]string{
			coreconfig.KeyAuthDefaultPerms: "",
		})
	}
	h := &VarStoreHandler{
		log:           log,
		records:       make(map[string]varRecord),
		pending:       make(map[pendingKey][]pendingWaiter),
		cache:         make(map[string]map[uint32]bool),
		subscriptions: make(map[string]map[uint32]map[string]struct{}),
		connSubs:      make(map[string]map[string]struct{}),
		upstreamSubs:  make(map[string]bool),
		pendingSubs:   make(map[pendingKey][]pendingSubscriber),
	}
	if cfg != nil {
		h.permCfg = permission.SharedConfig(cfg)
	}
	if h.permCfg == nil {
		h.permCfg = permission.NewConfig(nil)
	}
	return h
}

// AcceptCmd 声明 Cmd 帧在 target!=local 时也需要本地处理一次。
func (h *VarStoreHandler) AcceptCmd() bool { return true }

func (h *VarStoreHandler) SubProto() uint8 { return 3 }

func (h *VarStoreHandler) Init() bool {
	h.initActions()
	return true
}

func (h *VarStoreHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	h.ensureConnCloseSubscription(ctx)
	var msg varMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		h.log.Warn("varstore invalid payload", "err", err)
		return
	}
	entry, ok := h.LookupAction(msg.Action)
	if !ok {
		h.log.Debug("unknown varstore action", "action", msg.Action)
		return
	}
	entry.Handle(ctx, conn, hdr, msg.Data)
}

// set / assist_set
func (h *VarStoreHandler) handleSet(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req setReq
	if err := json.Unmarshal(data, &req); err != nil || !validVarName(req.Name) || strings.TrimSpace(req.Value) == "" {
		h.sendResp(ctx, conn, hdr, chooseSetResp(assisted), varResp{Code: 2, Msg: "invalid set", Name: req.Name, Owner: req.Owner})
		return
	}
	owner := req.Owner
	if owner == 0 {
		if owners, ok := h.cache[req.Name]; ok && len(owners) == 1 {
			for o := range owners {
				owner = o
			}
		}
	}
	owner = firstNonZero(owner, hdr.SourceID())
	if owner == 0 {
		h.sendResp(ctx, conn, hdr, chooseSetResp(assisted), varResp{Code: 2, Msg: "owner required", Name: req.Name})
		return
	}
	req.Owner = owner
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}

	// 判断是否在当前子树
	if !h.ownerInSubtree(ctx, owner) {
		if parent := h.findParent(ctx); parent != nil {
			h.addPending(owner, req.Name, conn.ID(), pendingKindSet, hdr)
			h.forward(ctx, parent, varActionAssistSet, req, srv.NodeID())
			return
		}
		h.sendResp(ctx, conn, hdr, chooseSetResp(assisted), varResp{Code: 4, Msg: "not found", Name: req.Name, Owner: owner})
		return
	}

	actorID := permission.SourceNodeID(hdr, conn)
	existing, found := h.lookupOwned(owner, req.Name)
	wasPublic := existing.IsPublic

	// 创建仅 owner
	if !found && actorID != owner {
		h.sendResp(ctx, conn, hdr, chooseSetResp(assisted), varResp{Code: 3, Msg: "owner required", Name: req.Name, Owner: owner})
		return
	}
	// private 更新权限
	nextVis := strings.TrimSpace(req.Visibility)
	if nextVis == "" {
		if found {
			nextVis = existing.Visibility
		} else if existing.IsPublic {
			nextVis = visibilityPublic
		}
	}
	if strings.ToLower(nextVis) != visibilityPublic && actorID != owner && !h.hasPermission(actorID, permission.VarPrivateSet) {
		h.sendResp(ctx, conn, hdr, chooseSetResp(assisted), varResp{Code: 3, Msg: "permission denied", Name: req.Name, Owner: owner})
		return
	}

	rec := existing
	rec.Owner = owner
	rec.Value = req.Value
	if strings.TrimSpace(req.Type) != "" {
		rec.Type = req.Type
	} else if rec.Type == "" {
		rec.Type = "string"
	}
	if strings.TrimSpace(req.Visibility) != "" {
		rec.Visibility = strings.TrimSpace(req.Visibility)
		rec.IsPublic = strings.ToLower(req.Visibility) == visibilityPublic
	} else if rec.Visibility == "" {
		rec.Visibility = visibilityPrivate
		rec.IsPublic = false
	}

	h.saveRecord(req.Name, rec)

	// 可见性降级为私有：视为删除，清理订阅并下发删除通知
	if wasPublic && !rec.IsPublic {
		h.handleVisibilityDowngrade(ctx, owner, req.Name, actorID, hdr)
	} else {
		h.propagateChange(ctx, owner, req.Name, rec, actorID, owner, hdr.SourceID())
	}

	// 向上同步缓存
	if parent := h.findParent(ctx); parent != nil {
		h.forward(ctx, parent, varActionUpSet, req, srv.NodeID())
	}

	// 响应与通知：始终回请求者；若请求者!=owner，则额外通知 owner
	h.sendResp(ctx, conn, hdr, chooseSetResp(assisted), varResp{
		Code:       1,
		Msg:        "ok",
		Name:       req.Name,
		Owner:      owner,
		Visibility: rec.Visibility,
		Type:       rec.Type,
	})
	if actorID != owner {
		h.sendNotifySet(ctx, owner, req.Name, rec)
	}
}

// get / assist_get
func (h *VarStoreHandler) handleGet(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req getReq
	if err := json.Unmarshal(data, &req); err != nil || !validVarName(req.Name) {
		h.sendResp(ctx, conn, hdr, chooseGetResp(assisted), varResp{Code: 2, Msg: "invalid get"})
		return
	}
	owner := firstNonZero(req.Owner, hdr.SourceID())
	if owner == 0 {
		h.sendResp(ctx, conn, hdr, chooseGetResp(assisted), varResp{Code: 2, Msg: "owner required"})
		return
	}
	req.Owner = owner
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}

	if rec, ok := h.lookupOwned(owner, req.Name); ok {
		if rec.IsPublic || owner == hdr.SourceID() || h.hasPermission(permission.SourceNodeID(hdr, conn), permission.VarPrivateSet) {
			h.sendResp(ctx, conn, hdr, chooseGetResp(assisted), varResp{
				Code:       1,
				Msg:        "ok",
				Name:       req.Name,
				Value:      rec.Value,
				Owner:      owner,
				Visibility: rec.Visibility,
				Type:       rec.Type,
			})
			return
		}
		h.sendResp(ctx, conn, hdr, chooseGetResp(assisted), varResp{Code: 3, Msg: "forbidden"})
		return
	}

	if parent := h.findParent(ctx); parent != nil {
		h.addPending(owner, req.Name, conn.ID(), pendingKindGet, hdr)
		h.forward(ctx, parent, varActionAssistGet, req, srv.NodeID())
		return
	}
	h.sendResp(ctx, conn, hdr, chooseGetResp(assisted), varResp{Code: 4, Msg: "not found", Name: req.Name, Owner: owner})
}

// list / assist_list
func (h *VarStoreHandler) handleList(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req listReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendResp(ctx, conn, hdr, chooseListResp(assisted), varResp{Code: 2, Msg: "invalid list"})
		return
	}
	owner := firstNonZero(req.Owner, hdr.SourceID())
	if owner == 0 {
		h.sendResp(ctx, conn, hdr, chooseListResp(assisted), varResp{Code: 2, Msg: "owner required"})
		return
	}
	req.Owner = owner
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}

	names := h.listPublicNames(owner)
	if len(names) > 0 {
		h.sendResp(ctx, conn, hdr, chooseListResp(assisted), varResp{Code: 1, Msg: "ok", Owner: owner, Names: names})
		return
	}

	if parent := h.findParent(ctx); parent != nil {
		h.addPending(owner, "", conn.ID(), pendingKindList, hdr)
		h.forward(ctx, parent, varActionAssistList, req, srv.NodeID())
		return
	}
	h.sendResp(ctx, conn, hdr, chooseListResp(assisted), varResp{Code: 4, Msg: "not found", Owner: owner})
}

// revoke / assist_revoke
func (h *VarStoreHandler) handleRevoke(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req getReq
	if err := json.Unmarshal(data, &req); err != nil || !validVarName(req.Name) {
		h.sendResp(ctx, conn, hdr, chooseRevokeResp(assisted), varResp{Code: 2, Msg: "invalid revoke"})
		return
	}
	owner := firstNonZero(req.Owner, hdr.SourceID())
	if owner == 0 {
		h.sendResp(ctx, conn, hdr, chooseRevokeResp(assisted), varResp{Code: 2, Msg: "owner required"})
		return
	}
	req.Owner = owner
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}

	if !h.ownerInSubtree(ctx, owner) {
		if parent := h.findParent(ctx); parent != nil {
			h.addPending(owner, req.Name, conn.ID(), pendingKindRevoke, hdr)
			h.forward(ctx, parent, varActionAssistRevoke, req, srv.NodeID())
			return
		}
		h.sendResp(ctx, conn, hdr, chooseRevokeResp(assisted), varResp{Code: 4, Msg: "not found", Name: req.Name, Owner: owner})
		return
	}

	actorID := permission.SourceNodeID(hdr, conn)
	if actorID != owner && !h.hasPermission(actorID, permission.VarRevoke) {
		h.sendResp(ctx, conn, hdr, chooseRevokeResp(assisted), varResp{Code: 3, Msg: "forbidden", Name: req.Name, Owner: owner})
		return
	}

	if _, ok := h.lookupOwned(owner, req.Name); !ok {
		// 尝试继续向上查询
		if parent := h.findParent(ctx); parent != nil {
			h.addPending(owner, req.Name, conn.ID(), pendingKindRevoke, hdr)
			h.forward(ctx, parent, varActionAssistRevoke, req, srv.NodeID())
			return
		}
		h.sendResp(ctx, conn, hdr, chooseRevokeResp(assisted), varResp{Code: 4, Msg: "not found", Name: req.Name, Owner: owner})
		return
	}

	h.deleteRecord(owner, req.Name)
	h.handleDeletion(ctx, owner, req.Name, actorID, hdrSourceID(hdr), owner)

	if parent := h.findParent(ctx); parent != nil {
		h.forward(ctx, parent, varActionUpRevoke, req, srv.NodeID())
	}

	h.sendResp(ctx, conn, hdr, chooseRevokeResp(assisted), varResp{Code: 1, Msg: "ok", Name: req.Name, Owner: owner})
	if actorID != owner {
		h.sendNotifyRevoke(ctx, owner, req.Name)
	}
}

// responses from upstream
func (h *VarStoreHandler) handleSetResp(ctx context.Context, data json.RawMessage) {
	var resp varResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	waiters := h.popPending(resp.Owner, resp.Name, pendingKindSet)
	if resp.Code == 1 {
		rec := varRecord{
			Value:      resp.Value,
			Owner:      resp.Owner,
			Visibility: resp.Visibility,
			Type:       resp.Type,
			IsPublic:   strings.ToLower(resp.Visibility) == visibilityPublic,
		}
		h.saveRecord(resp.Name, rec)
	}
	h.broadcastPendingResp(ctx, waiters, varActionSetResp, resp)
}

func (h *VarStoreHandler) handleGetResp(ctx context.Context, data json.RawMessage) {
	var resp varResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	waiters := h.popPending(resp.Owner, resp.Name, pendingKindGet)
	if resp.Code == 1 {
		rec := varRecord{
			Value:      resp.Value,
			Owner:      resp.Owner,
			Visibility: resp.Visibility,
			Type:       resp.Type,
			IsPublic:   strings.ToLower(resp.Visibility) == visibilityPublic,
		}
		h.saveRecord(resp.Name, rec)
	}
	h.broadcastPendingResp(ctx, waiters, varActionGetResp, resp)
}

func (h *VarStoreHandler) handleListResp(ctx context.Context, data json.RawMessage) {
	var resp varResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	waiters := h.popPending(resp.Owner, "", pendingKindList)
	h.broadcastPendingResp(ctx, waiters, varActionListResp, resp)
}

func (h *VarStoreHandler) handleRevokeResp(ctx context.Context, data json.RawMessage) {
	var resp varResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	waiters := h.popPending(resp.Owner, resp.Name, pendingKindRevoke)
	if resp.Code == 1 {
		h.deleteRecord(resp.Owner, resp.Name)
		h.handleDeletion(ctx, resp.Owner, resp.Name)
	}
	h.broadcastPendingResp(ctx, waiters, varActionRevokeResp, resp)
}

// subscribe handlers
func (h *VarStoreHandler) handleSubscribe(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req subscribeReq
	if err := json.Unmarshal(data, &req); err != nil || !validVarName(req.Name) || req.Owner == 0 {
		h.sendResp(ctx, conn, hdr, chooseSubscribeResp(assisted), varResp{Code: 2, Msg: "invalid subscribe", Name: req.Name, Owner: req.Owner})
		return
	}
	subscriber := req.Subscriber
	if subscriber == 0 {
		subscriber = permission.SourceNodeID(hdr, conn)
	}
	if subscriber == 0 {
		h.sendResp(ctx, conn, hdr, chooseSubscribeResp(assisted), varResp{Code: 2, Msg: "subscriber required", Name: req.Name, Owner: req.Owner})
		return
	}
	req.Subscriber = subscriber
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}

	if rec, ok := h.lookupOwned(req.Owner, req.Name); ok {
		if !rec.IsPublic && subscriber != req.Owner && !h.hasPermission(subscriber, permission.VarSubscribe) {
			h.sendResp(ctx, conn, hdr, chooseSubscribeResp(assisted), varResp{Code: 3, Msg: "forbidden", Name: req.Name, Owner: req.Owner})
			return
		}
		h.addSubscription(req.Owner, req.Name, subscriber, conn.ID())
		h.sendResp(ctx, conn, hdr, chooseSubscribeResp(assisted), varResp{
			Code:       1,
			Msg:        "ok",
			Name:       req.Name,
			Owner:      req.Owner,
			Visibility: rec.Visibility,
			Type:       rec.Type,
		})
		return
	}

	if parent := h.findParent(ctx); parent != nil {
		alreadyPending := h.hasPendingSubscribe(req.Owner, req.Name)
		h.addPendingSubscribe(req.Owner, req.Name, subscriber, conn.ID(), hdr)
		if !alreadyPending {
			h.forward(ctx, parent, varActionAssistSubscribe, req, srv.NodeID())
		}
		return
	}

	h.sendResp(ctx, conn, hdr, chooseSubscribeResp(assisted), varResp{Code: 4, Msg: "not found", Name: req.Name, Owner: req.Owner})
}

func (h *VarStoreHandler) handleSubscribeResp(ctx context.Context, hdr core.IHeader, data json.RawMessage) {
	var resp varResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	pending := h.popPendingSubscribe(resp.Owner, resp.Name)
	key := h.key(resp.Owner, resp.Name)
	if resp.Code != 1 {
		for _, p := range pending {
			reqHdr := (&header.HeaderTcp{}).WithMsgID(p.msgID).WithTraceID(p.traceID)
			h.sendResp(ctx, h.lookupConn(ctx, p.connID), reqHdr, chooseSubscribeResp(false), varResp{
				Code:  resp.Code,
				Msg:   resp.Msg,
				Name:  resp.Name,
				Owner: resp.Owner,
			})
		}
		if resp.Code == 4 {
			h.markUpstreamSubscribed(key, false)
		}
		return
	}
	rec := varRecord{
		Value:      resp.Value,
		Owner:      resp.Owner,
		Visibility: resp.Visibility,
		Type:       resp.Type,
		IsPublic:   strings.ToLower(resp.Visibility) == visibilityPublic,
	}
	h.saveRecord(resp.Name, rec)
	if h.findParent(ctx) != nil {
		h.markUpstreamSubscribed(key, true)
	}
	for _, p := range pending {
		if !rec.IsPublic && p.subscriber != resp.Owner && !h.hasPermission(p.subscriber, permission.VarSubscribe) {
			reqHdr := (&header.HeaderTcp{}).WithMsgID(p.msgID).WithTraceID(p.traceID)
			h.sendResp(ctx, h.lookupConn(ctx, p.connID), reqHdr, chooseSubscribeResp(false), varResp{
				Code:  3,
				Msg:   "forbidden",
				Name:  resp.Name,
				Owner: resp.Owner,
			})
			continue
		}
		h.addSubscription(resp.Owner, resp.Name, p.subscriber, p.connID)
		reqHdr := (&header.HeaderTcp{}).WithMsgID(p.msgID).WithTraceID(p.traceID)
		h.sendResp(ctx, h.lookupConn(ctx, p.connID), reqHdr, chooseSubscribeResp(false), varResp{
			Code:       1,
			Msg:        "ok",
			Name:       resp.Name,
			Owner:      resp.Owner,
			Visibility: resp.Visibility,
			Type:       resp.Type,
		})
	}
}

func (h *VarStoreHandler) handleUnsubscribe(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req subscribeReq
	if err := json.Unmarshal(data, &req); err != nil || !validVarName(req.Name) || req.Owner == 0 {
		return
	}
	subscriber := req.Subscriber
	if subscriber == 0 {
		subscriber = permission.SourceNodeID(hdr, conn)
	}
	if subscriber == 0 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	key := h.key(req.Owner, req.Name)
	h.removeSubscription(req.Owner, req.Name, subscriber, conn.ID())
	if h.subscriberCount(key) == 0 && h.upstreamSubscribed(key) {
		if parent := h.findParent(ctx); parent != nil {
			h.forward(ctx, parent, varActionAssistUnsubscribe, req, srv.NodeID())
		}
		h.markUpstreamSubscribed(key, false)
	}
	if !assisted {
		h.sendResp(ctx, conn, hdr, chooseSubscribeResp(false), varResp{Code: 1, Msg: "ok", Name: req.Name, Owner: req.Owner})
	}
}

// change notifications
func (h *VarStoreHandler) handleVarChanged(ctx context.Context, hdr core.IHeader, data json.RawMessage) {
	var resp varResp
	if err := json.Unmarshal(data, &resp); err != nil || resp.Owner == 0 || resp.Name == "" {
		return
	}
	rec := varRecord{
		Value:      resp.Value,
		Owner:      resp.Owner,
		Visibility: resp.Visibility,
		Type:       resp.Type,
		IsPublic:   strings.ToLower(resp.Visibility) == visibilityPublic,
	}
	h.saveRecord(resp.Name, rec)
	h.propagateChange(ctx, resp.Owner, resp.Name, rec, resp.Owner, hdrSourceID(hdr))
}

func (h *VarStoreHandler) handleVarDeleted(ctx context.Context, hdr core.IHeader, data json.RawMessage) {
	var resp varResp
	if err := json.Unmarshal(data, &resp); err != nil || resp.Owner == 0 || resp.Name == "" {
		return
	}
	h.deleteRecord(resp.Owner, resp.Name)
	h.handleDeletion(ctx, resp.Owner, resp.Name, hdrSourceID(hdr), resp.Owner)
}

// up_* handlers
func (h *VarStoreHandler) handleUpSet(ctx context.Context, hdr core.IHeader, data json.RawMessage) {
	var req setReq
	if err := json.Unmarshal(data, &req); err != nil || req.Owner == 0 || req.Name == "" {
		return
	}
	existing, _ := h.lookupOwned(req.Owner, req.Name)
	wasPublic := existing.IsPublic
	rec := existing
	rec.Owner = req.Owner
	rec.Value = req.Value
	rec.Visibility = strings.TrimSpace(req.Visibility)
	rec.Type = req.Type
	rec.IsPublic = strings.ToLower(req.Visibility) == visibilityPublic
	h.saveRecord(req.Name, rec)
	if wasPublic && !rec.IsPublic {
		h.handleVisibilityDowngrade(ctx, req.Owner, req.Name, hdrSourceID(hdr), hdr)
	} else {
		h.propagateChange(ctx, req.Owner, req.Name, rec, req.Owner, hdrSourceID(hdr))
	}
	if parent := h.findParent(ctx); parent != nil {
		srv := core.ServerFromContext(ctx)
		if srv != nil {
			h.forward(ctx, parent, varActionUpSet, req, srv.NodeID())
		}
	}
}

func (h *VarStoreHandler) handleUpRevoke(ctx context.Context, hdr core.IHeader, data json.RawMessage) {
	var req getReq
	if err := json.Unmarshal(data, &req); err != nil || req.Owner == 0 || req.Name == "" {
		return
	}
	h.deleteRecord(req.Owner, req.Name)
	h.handleDeletion(ctx, req.Owner, req.Name, hdrSourceID(hdr))
	if parent := h.findParent(ctx); parent != nil {
		srv := core.ServerFromContext(ctx)
		if srv != nil {
			h.forward(ctx, parent, varActionUpRevoke, req, srv.NodeID())
		}
	}
}

// notify handlers
func (h *VarStoreHandler) handleNotifySet(ctx context.Context, hdr core.IHeader, data json.RawMessage) {
	var resp varResp
	if err := json.Unmarshal(data, &resp); err != nil || resp.Name == "" || resp.Owner == 0 {
		return
	}
	existing, _ := h.lookupOwned(resp.Owner, resp.Name)
	wasPublic := existing.IsPublic
	rec := existing
	rec.Value = resp.Value
	rec.Owner = resp.Owner
	rec.Visibility = resp.Visibility
	rec.Type = resp.Type
	rec.IsPublic = strings.ToLower(resp.Visibility) == visibilityPublic
	h.saveRecord(resp.Name, rec)
	if wasPublic && !rec.IsPublic {
		h.handleVisibilityDowngrade(ctx, resp.Owner, resp.Name, hdrSourceID(hdr), hdr)
	} else {
		h.propagateChange(ctx, resp.Owner, resp.Name, rec, resp.Owner, hdrSourceID(hdr))
	}
}

func (h *VarStoreHandler) handleNotifyRevoke(ctx context.Context, hdr core.IHeader, data json.RawMessage) {
	var resp varResp
	if err := json.Unmarshal(data, &resp); err != nil || resp.Owner == 0 || resp.Name == "" {
		return
	}
	h.deleteRecord(resp.Owner, resp.Name)
	h.handleDeletion(ctx, resp.Owner, resp.Name, hdrSourceID(hdr))
}

// helpers
func (h *VarStoreHandler) ownerInSubtree(ctx context.Context, owner uint32) bool {
	if owner == 0 {
		return false
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return false
	}
	if srv.NodeID() == owner {
		return true
	}
	if _, ok := srv.ConnManager().GetByNode(owner); ok {
		return true
	}
	return false
}

func (h *VarStoreHandler) lookupOwned(owner uint32, name string) (varRecord, bool) {
	if owner == 0 || name == "" {
		return varRecord{}, false
	}
	h.mu.RLock()
	rec, ok := h.records[h.key(owner, name)]
	h.mu.RUnlock()
	return rec, ok
}

func (h *VarStoreHandler) saveRecord(name string, rec varRecord) {
	if name == "" || rec.Owner == 0 {
		return
	}
	key := h.key(rec.Owner, name)
	h.mu.Lock()
	h.records[key] = rec
	h.addOwnerCache(name, rec.Owner)
	h.mu.Unlock()
}

func (h *VarStoreHandler) listPublicNames(owner uint32) []string {
	if owner == 0 {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	var names []string
	prefix := strconv.FormatUint(uint64(owner), 10) + ":"
	for key, rec := range h.records {
		if !rec.IsPublic {
			continue
		}
		if strings.HasPrefix(key, prefix) {
			if parts := strings.SplitN(key, ":", 2); len(parts) == 2 && parts[1] != "" {
				names = append(names, parts[1])
			}
		}
	}
	return names
}

func (h *VarStoreHandler) key(owner uint32, name string) string {
	return strconv.FormatUint(uint64(owner), 10) + ":" + name
}

func (h *VarStoreHandler) addOwnerCache(name string, owner uint32) {
	if owner == 0 || name == "" {
		return
	}
	if _, ok := h.cache[name]; !ok {
		h.cache[name] = make(map[uint32]bool)
	}
	h.cache[name][owner] = true
}

func (h *VarStoreHandler) deleteRecord(owner uint32, name string) {
	if owner == 0 || name == "" {
		return
	}
	k := h.key(owner, name)
	h.mu.Lock()
	delete(h.records, k)
	if owners, ok := h.cache[name]; ok {
		delete(owners, owner)
		if len(owners) == 0 {
			delete(h.cache, name)
		} else {
			h.cache[name] = owners
		}
	}
	h.mu.Unlock()
}

func (h *VarStoreHandler) addSubscription(owner uint32, name string, subscriber uint32, connID string) {
	if owner == 0 || name == "" || subscriber == 0 || connID == "" {
		return
	}
	key := h.key(owner, name)
	h.mu.Lock()
	subs := h.subscriptions[key]
	if subs == nil {
		subs = make(map[uint32]map[string]struct{})
	}
	conns := subs[subscriber]
	if conns == nil {
		conns = make(map[string]struct{})
	}
	conns[connID] = struct{}{}
	subs[subscriber] = conns
	h.subscriptions[key] = subs

	connIndex := h.connSubs[connID]
	if connIndex == nil {
		connIndex = make(map[string]struct{})
	}
	connIndex[key] = struct{}{}
	h.connSubs[connID] = connIndex
	h.mu.Unlock()
}

func (h *VarStoreHandler) removeSubscription(owner uint32, name string, subscriber uint32, connID string) bool {
	if owner == 0 || name == "" {
		return false
	}
	return h.removeSubscriptionByKey(h.key(owner, name), subscriber, connID)
}

func (h *VarStoreHandler) removeSubscriptionByKey(key string, subscriber uint32, connID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if connID != "" {
		if keys, ok := h.connSubs[connID]; ok {
			delete(keys, key)
			if len(keys) == 0 {
				delete(h.connSubs, connID)
			} else {
				h.connSubs[connID] = keys
			}
		}
	}
	subs := h.subscriptions[key]
	if subs == nil {
		return false
	}
	if subscriber == 0 {
		for _, conns := range subs {
			for id := range conns {
				if idx, ok := h.connSubs[id]; ok {
					delete(idx, key)
					if len(idx) == 0 {
						delete(h.connSubs, id)
					} else {
						h.connSubs[id] = idx
					}
				}
			}
		}
		delete(h.subscriptions, key)
		return true
	}
	if conns, ok := subs[subscriber]; ok {
		if connID != "" {
			delete(conns, connID)
		} else {
			for id := range conns {
				if idx, ok := h.connSubs[id]; ok {
					delete(idx, key)
					if len(idx) == 0 {
						delete(h.connSubs, id)
					} else {
						h.connSubs[id] = idx
					}
				}
			}
		}
		if len(conns) == 0 {
			delete(subs, subscriber)
		} else {
			subs[subscriber] = conns
		}
	}
	if len(subs) == 0 {
		delete(h.subscriptions, key)
		return true
	}
	h.subscriptions[key] = subs
	return len(subs) == 0
}

func (h *VarStoreHandler) removeConnSubscriptions(connID string) []string {
	if connID == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	keysSet := make(map[string]struct{})
	if keys, ok := h.connSubs[connID]; ok {
		for key := range keys {
			keysSet[key] = struct{}{}
			if subs, ok2 := h.subscriptions[key]; ok2 {
				for node, conns := range subs {
					delete(conns, connID)
					if len(conns) == 0 {
						delete(subs, node)
					} else {
						subs[node] = conns
					}
				}
				if len(subs) == 0 {
					delete(h.subscriptions, key)
				} else {
					h.subscriptions[key] = subs
				}
			}
		}
		delete(h.connSubs, connID)
	}
	return keysFromSet(keysSet)
}

func keysFromSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

func (h *VarStoreHandler) subscriberCount(key string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	subs := h.subscriptions[key]
	if len(subs) == 0 {
		return 0
	}
	count := 0
	for _, conns := range subs {
		count += len(conns)
	}
	return count
}

func (h *VarStoreHandler) subscribersFor(key string, exclude uint32) map[uint32][]string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	subs := h.subscriptions[key]
	if len(subs) == 0 {
		return nil
	}
	out := make(map[uint32][]string)
	for node, conns := range subs {
		if node == exclude {
			continue
		}
		for id := range conns {
			out[node] = append(out[node], id)
		}
	}
	return out
}

func (h *VarStoreHandler) clearSubscriptionsExcept(key string, keepNode uint32) []uint32 {
	h.mu.Lock()
	defer h.mu.Unlock()
	var affected []uint32
	subs := h.subscriptions[key]
	if len(subs) == 0 {
		return nil
	}
	for node, conns := range subs {
		if node == keepNode {
			continue
		}
		affected = append(affected, node)
		for id := range conns {
			if idx, ok := h.connSubs[id]; ok {
				delete(idx, key)
				if len(idx) == 0 {
					delete(h.connSubs, id)
				} else {
					h.connSubs[id] = idx
				}
			}
		}
		delete(subs, node)
	}
	if len(subs) == 0 || (len(subs) == 1 && keepNode != 0 && len(subs[keepNode]) == 0) {
		delete(h.subscriptions, key)
	} else {
		h.subscriptions[key] = subs
	}
	return affected
}

func (h *VarStoreHandler) markUpstreamSubscribed(key string, v bool) {
	h.mu.Lock()
	if v {
		h.upstreamSubs[key] = true
	} else {
		delete(h.upstreamSubs, key)
	}
	h.mu.Unlock()
}

func (h *VarStoreHandler) upstreamSubscribed(key string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.upstreamSubs[key]
}

func (h *VarStoreHandler) addPendingSubscribe(owner uint32, name string, subscriber uint32, connID string, hdr core.IHeader) {
	if owner == 0 || name == "" || subscriber == 0 || connID == "" {
		return
	}
	var msgID uint32
	var traceID uint32
	if hdr != nil {
		msgID = hdr.GetMsgID()
		traceID = hdr.GetTraceID()
	}
	k := pendingKey{owner: owner, name: name, kind: pendingKindSubscribe}
	h.mu.Lock()
	h.pendingSubs[k] = append(h.pendingSubs[k], pendingSubscriber{connID: connID, subscriber: subscriber, msgID: msgID, traceID: traceID})
	h.mu.Unlock()
}

func (h *VarStoreHandler) popPendingSubscribe(owner uint32, name string) []pendingSubscriber {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := pendingKey{owner: owner, name: name, kind: pendingKindSubscribe}
	items := h.pendingSubs[k]
	delete(h.pendingSubs, k)
	return items
}

func (h *VarStoreHandler) hasPendingSubscribe(owner uint32, name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	k := pendingKey{owner: owner, name: name, kind: pendingKindSubscribe}
	_, ok := h.pendingSubs[k]
	return ok
}

func (h *VarStoreHandler) maybeUnsubscribeUpstream(ctx context.Context, keys []string) {
	if len(keys) == 0 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	parent := h.findParent(ctx)
	for _, key := range keys {
		if !h.upstreamSubscribed(key) || h.subscriberCount(key) > 0 {
			continue
		}
		if parent == nil {
			h.markUpstreamSubscribed(key, false)
			continue
		}
		owner, name := splitKey(key)
		if owner == 0 || name == "" {
			h.markUpstreamSubscribed(key, false)
			continue
		}
		req := subscribeReq{Name: name, Owner: owner, Subscriber: srv.NodeID()}
		h.forward(ctx, parent, varActionAssistUnsubscribe, req, srv.NodeID())
		h.markUpstreamSubscribed(key, false)
	}
}

func (h *VarStoreHandler) ensureConnCloseSubscription(ctx context.Context) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	h.eventSubOnce.Do(func() {
		eb := srv.EventBus()
		if eb == nil {
			return
		}
		eb.Subscribe("conn.closed", func(evCtx context.Context, evt eventbus.Event) {
			connID, _ := parseConnClosed(evt.Data)
			if connID == "" {
				return
			}
			keys := h.removeConnSubscriptions(connID)
			h.maybeUnsubscribeUpstream(evCtx, keys)
		})
	})
}

func parseConnClosed(data any) (string, uint32) {
	switch v := data.(type) {
	case connClosedEvent:
		return v.ConnID, v.NodeID
	case *connClosedEvent:
		return v.ConnID, v.NodeID
	case map[string]any:
		connID := ""
		nodeID := uint32(0)
		if c, ok := v["conn_id"]; ok {
			if s, ok2 := c.(string); ok2 {
				connID = s
			}
		}
		if c, ok := v["connID"]; ok {
			if s, ok2 := c.(string); ok2 {
				connID = s
			}
		}
		if n, ok := v["node_id"]; ok {
			switch vv := n.(type) {
			case uint32:
				nodeID = vv
			case uint64:
				nodeID = uint32(vv)
			case int:
				if vv >= 0 {
					nodeID = uint32(vv)
				}
			case int64:
				if vv >= 0 {
					nodeID = uint32(vv)
				}
			case float64:
				if vv >= 0 {
					nodeID = uint32(vv)
				}
			}
		}
		return connID, nodeID
	default:
		return "", 0
	}
}

func hdrSourceID(h core.IHeader) uint32 {
	if h == nil {
		return 0
	}
	return h.SourceID()
}

func (h *VarStoreHandler) propagateChange(ctx context.Context, owner uint32, name string, rec varRecord, excludes ...uint32) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	key := h.key(owner, name)
	skip := make(map[uint32]struct{}, len(excludes))
	for _, id := range excludes {
		if id != 0 {
			skip[id] = struct{}{}
		}
	}
	subs := h.subscribersFor(key, 0)
	if len(subs) == 0 {
		return
	}
	raw, _ := json.Marshal(varResp{
		Code:       1,
		Name:       name,
		Owner:      owner,
		Value:      rec.Value,
		Visibility: rec.Visibility,
		Type:       rec.Type,
	})
	payload, _ := json.Marshal(varMessage{Action: varActionVarChanged, Data: raw})
	baseHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(srv.NodeID())
	for node, conns := range subs {
		if _, ok := skip[node]; ok {
			continue
		}
		if node == owner {
			continue
		}
		hdr := baseHdr.Clone().WithTargetID(node)
		for _, cid := range conns {
			_ = srv.Send(ctx, cid, hdr.Clone(), payload)
		}
	}
}

func (h *VarStoreHandler) propagateDelete(ctx context.Context, owner uint32, name string, keepNode uint32, excludes ...uint32) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	key := h.key(owner, name)
	skip := make(map[uint32]struct{}, len(excludes)+1)
	for _, id := range excludes {
		if id != 0 {
			skip[id] = struct{}{}
		}
	}
	if keepNode != 0 {
		skip[keepNode] = struct{}{}
	}
	subs := h.subscribersFor(key, 0)
	if len(subs) > 0 {
		raw, _ := json.Marshal(varResp{
			Code:  1,
			Name:  name,
			Owner: owner,
		})
		payload, _ := json.Marshal(varMessage{Action: varActionVarDeleted, Data: raw})
		baseHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(3).
			WithSourceID(srv.NodeID())
		for node, conns := range subs {
			if _, ok := skip[node]; ok {
				continue
			}
			hdr := baseHdr.Clone().WithTargetID(node)
			for _, cid := range conns {
				_ = srv.Send(ctx, cid, hdr.Clone(), payload)
			}
		}
	}
	h.clearSubscriptionsExcept(key, keepNode)
	h.maybeUnsubscribeUpstream(ctx, []string{key})
}

func (h *VarStoreHandler) handleVisibilityDowngrade(ctx context.Context, owner uint32, name string, actor uint32, hdr core.IHeader) {
	h.propagateDelete(ctx, owner, name, owner, actor, hdrSourceID(hdr))
}

func (h *VarStoreHandler) handleDeletion(ctx context.Context, owner uint32, name string, excludes ...uint32) {
	h.propagateDelete(ctx, owner, name, 0, excludes...)
}

func splitKey(key string) (uint32, string) {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return 0, ""
	}
	owner, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return 0, ""
	}
	return uint32(owner), parts[1]
}

func (h *VarStoreHandler) addPending(owner uint32, name, connID string, kind string, hdr core.IHeader) {
	var msgID uint32
	var traceID uint32
	if hdr != nil {
		msgID = hdr.GetMsgID()
		traceID = hdr.GetTraceID()
	}
	h.mu.Lock()
	k := pendingKey{owner: owner, name: name, kind: kind}
	h.pending[k] = append(h.pending[k], pendingWaiter{connID: connID, msgID: msgID, traceID: traceID})
	h.mu.Unlock()
}

func (h *VarStoreHandler) popPending(owner uint32, name string, kind string) []pendingWaiter {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := pendingKey{owner: owner, name: name, kind: kind}
	conns := h.pending[k]
	delete(h.pending, k)
	return conns
}

func (h *VarStoreHandler) broadcastPendingResp(ctx context.Context, waiters []pendingWaiter, action string, resp varResp) {
	if len(waiters) == 0 {
		return
	}
	for _, pending := range waiters {
		reqHdr := (&header.HeaderTcp{}).WithMsgID(pending.msgID).WithTraceID(pending.traceID)
		h.sendResp(ctx, h.lookupConn(ctx, pending.connID), reqHdr, action, resp)
	}
}

func (h *VarStoreHandler) lookupConn(ctx context.Context, id string) core.IConnection {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return nil
	}
	if c, ok := srv.ConnManager().Get(id); ok {
		return c
	}
	return nil
}

func (h *VarStoreHandler) sendResp(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, action string, data varResp) {
	msg := varMessage{Action: action}
	raw, _ := json.Marshal(data)
	msg.Data = raw
	payload, _ := json.Marshal(msg)
	hdr := h.buildRespHeader(ctx, reqHdr, data.Owner)
	if srv := core.ServerFromContext(ctx); srv != nil && conn != nil {
		_ = srv.Send(ctx, conn.ID(), hdr, payload)
		return
	}
	if conn != nil {
		codec := header.HeaderTcpCodec{}
		_ = conn.SendWithHeader(hdr, payload, codec)
	}
}

func (h *VarStoreHandler) buildRespHeader(ctx context.Context, reqHdr core.IHeader, target uint32) core.IHeader {
	var base core.IHeader = &header.HeaderTcp{}
	if reqHdr != nil {
		base = reqHdr.Clone()
	}
	src := uint32(0)
	if srv := core.ServerFromContext(ctx); srv != nil {
		src = srv.NodeID()
	}
	if target == 0 && reqHdr != nil && reqHdr.SourceID() != 0 {
		target = reqHdr.SourceID()
	}
	return base.WithMajor(header.MajorOKResp).WithSubProto(3).WithSourceID(src).WithTargetID(target)
}

func (h *VarStoreHandler) forward(ctx context.Context, target core.IConnection, action string, data any, srcID uint32) {
	if target == nil {
		return
	}
	payloadData, _ := json.Marshal(data)
	msg := varMessage{Action: action, Data: payloadData}
	payload, _ := json.Marshal(msg)
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(3)
	if srv := core.ServerFromContext(ctx); srv != nil {
		if srcID != 0 {
			hdr.WithSourceID(srcID)
		} else {
			hdr.WithSourceID(srv.NodeID())
		}
	}
	if nid, ok := target.GetMeta("nodeID"); ok {
		if v, ok2 := nid.(uint32); ok2 {
			hdr.WithTargetID(v)
		}
	}
	if srv := core.ServerFromContext(ctx); srv != nil {
		_ = srv.Send(ctx, target.ID(), hdr, payload)
		return
	}
	codec := header.HeaderTcpCodec{}
	_ = target.SendWithHeader(hdr, payload, codec)
}

func (h *VarStoreHandler) sendNotifySet(ctx context.Context, owner uint32, name string, rec varRecord) {
	if owner == 0 || name == "" {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	resp := varResp{
		Code:       1,
		Name:       name,
		Owner:      owner,
		Value:      rec.Value,
		Visibility: rec.Visibility,
		Type:       rec.Type,
	}
	raw, _ := json.Marshal(resp)
	msg := varMessage{Action: varActionNotifySet, Data: raw}
	payload, _ := json.Marshal(msg)
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(srv.NodeID()).
		WithTargetID(owner)
	_ = srv.Send(ctx, ownerConnID(ctx, srv, owner), hdr, payload)
}

func (h *VarStoreHandler) sendNotifyRevoke(ctx context.Context, owner uint32, name string) {
	if owner == 0 || name == "" {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	resp := varResp{
		Code:  1,
		Name:  name,
		Owner: owner,
	}
	raw, _ := json.Marshal(resp)
	msg := varMessage{Action: varActionNotifyRevoke, Data: raw}
	payload, _ := json.Marshal(msg)
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(srv.NodeID()).
		WithTargetID(owner)
	_ = srv.Send(ctx, ownerConnID(ctx, srv, owner), hdr, payload)
}

func (h *VarStoreHandler) findParent(ctx context.Context) core.IConnection {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return nil
	}
	if c, ok := findParentConnVar(srv.ConnManager()); ok {
		return c
	}
	return nil
}

func findParentConnVar(cm core.IConnectionManager) (core.IConnection, bool) {
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

func (h *VarStoreHandler) hasPermission(nodeID uint32, perm string) bool {
	if h.permCfg == nil {
		return false
	}
	return h.permCfg.Has(nodeID, perm)
}

func (h *VarStoreHandler) initActions() {
	h.ResetActions()
	for _, act := range registerVarActions(h) {
		h.RegisterAction(act)
	}
}
