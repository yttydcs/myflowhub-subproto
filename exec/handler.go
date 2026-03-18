package exec

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/eventbus"
	"github.com/yttydcs/myflowhub-core/header"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	"github.com/yttydcs/myflowhub-core/subproto"
	"github.com/yttydcs/myflowhub-subproto/broker"
)

type MethodFunc func(ctx context.Context, args json.RawMessage) (json.RawMessage, error)

type Handler struct {
	subproto.ActionBaseSubProcess
	log *slog.Logger

	permCfg *permission.Config

	methods map[string]MethodFunc

	capMu        sync.RWMutex
	capLocal     map[string]CapabilityDescriptor
	capChildren  map[uint32]capPeerState
	eventSubOnce sync.Once

	capUpstreamParent uint32
	capUpstreamEpoch  uint64
	capUpstreamReqSeq uint64
	capUpstreamSent   bool
	capUpstreamLastAt time.Time
	capUpstreamCache  map[string]CapabilityDescriptor
}

type capPeerState struct {
	epoch         uint64
	leaseExpireAt time.Time
	caps          map[string]CapabilityDescriptor
}

const (
	defaultCapabilityLease = 60 * time.Second
	maxCapQueryLimit       = 200
	capQueryForwardTimeout = 3 * time.Second
)

func NewHandler(log *slog.Logger) *Handler {
	return NewHandlerWithConfig(nil, log)
}

func NewHandlerWithConfig(cfg core.IConfig, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	h := &Handler{
		log:         log,
		methods:     make(map[string]MethodFunc),
		capLocal:    make(map[string]CapabilityDescriptor),
		capChildren: make(map[uint32]capPeerState),
	}
	if cfg != nil {
		h.permCfg = permission.SharedConfig(cfg)
	}
	if h.permCfg == nil {
		h.permCfg = permission.NewConfig(nil)
	}
	// 内置方法：debug::echo
	h.RegisterMethod("debug::echo", func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
		if len(args) == 0 {
			return json.RawMessage(`{}`), nil
		}
		return args, nil
	})
	return h
}

// AcceptCmd 声明 Cmd 帧在 target!=local 时也需要本地处理一次（用于逐级授权/裁决）。
func (h *Handler) AcceptCmd() bool { return true }

func (h *Handler) SubProto() uint8 { return SubProtoExec }

func (h *Handler) Init() bool {
	h.initActions()
	return true
}

func (h *Handler) initActions() {
	h.ResetActions()
	for _, act := range registerActions(h) {
		h.RegisterAction(act)
	}
}

func (h *Handler) RegisterMethod(method string, fn MethodFunc) {
	method = strings.TrimSpace(method)
	if method == "" || fn == nil {
		return
	}
	h.methods[method] = fn
	desc := CapabilityDescriptor{Method: method}
	h.capMu.Lock()
	h.capLocal[capKey(0, method, "")] = desc
	h.capMu.Unlock()
}

func (h *Handler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	if srv := core.ServerFromContext(ctx); srv != nil {
		h.ensureConnCloseSubscription(srv)
	}
	var msg message
	if err := json.Unmarshal(payload, &msg); err != nil {
		h.log.Warn("exec invalid payload", "err", err)
		return
	}
	h.maybeSyncSnapshotUpstream(ctx, false)
	// *_resp 属于“返回路径”，应按 header.TargetID 逐跳转发到目标节点。
	if isRespAction(msg.Action) {
		if h.forwardRemoteByHeaderTarget(ctx, conn, hdr, payload) {
			return
		}
	}
	entry, ok := h.LookupAction(msg.Action)
	if !ok {
		// 兼容：未知 action 且 target!=local 时，仍按 TargetID 做逐跳转发（可能是新版本指令或返回帧）。
		if h.forwardRemoteByHeaderTarget(ctx, conn, hdr, payload) {
			return
		}
		h.log.Debug("unknown exec action", "action", msg.Action)
		return
	}
	entry.Handle(ctx, conn, hdr, msg.Data)
}

func (h *Handler) forwardRemoteByHeaderTarget(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) bool {
	if hdr == nil || len(payload) == 0 {
		return false
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil || srv.ConnManager() == nil {
		return false
	}
	target := hdr.TargetID()
	if target == 0 || target == srv.NodeID() {
		return false
	}

	var next core.IConnection
	if c, ok := srv.ConnManager().GetByNode(target); ok && c != nil {
		next = c
	} else {
		next = findParentConn(srv.ConnManager())
	}
	if next == nil {
		h.log.Warn("drop exec frame: no route", "target", target, "source", hdr.SourceID())
		return true
	}
	if isParentConn(conn) && isParentConn(next) {
		h.log.Warn("drop exec frame due to invalid route (came from parent)", "target", target, "source", hdr.SourceID())
		return true
	}
	fwdHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		h.log.Warn("drop exec frame due to hop_limit", "target", target, "source", hdr.SourceID())
		return true
	}
	fwdHdr.WithTargetID(target)
	h.sendToConn(ctx, next, fwdHdr, payload)
	return true
}

func (h *Handler) handleCall(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req CallReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendCallResp(ctx, hdr, CallResp{ReqID: req.ReqID, Code: 400, Msg: "invalid call"})
		return
	}
	req.Method = strings.TrimSpace(req.Method)
	req.ReqID = strings.TrimSpace(req.ReqID)
	if req.ReqID == "" || req.ExecutorNode == 0 || req.TargetNode == 0 || req.Method == "" {
		h.sendCallResp(ctx, hdr, CallResp{ReqID: req.ReqID, Code: 400, Msg: "invalid call"})
		return
	}

	srv := core.ServerFromContext(ctx)
	if srv == nil || hdr == nil || conn == nil {
		return
	}
	local := srv.NodeID()
	cm := srv.ConnManager()
	if cm == nil {
		return
	}

	// 来自父节点：下游无条件信任父节点，不做权限判定，直接按最终 target 转交/执行。
	if isParentConn(conn) {
		if req.TargetNode == local {
			h.execLocal(ctx, hdr, req)
			return
		}
		h.forwardDownOrDrop(ctx, srv, hdr, payloadFrom(message{Action: actionCall, Data: mustJSON(req)}), req.TargetNode)
		return
	}

	// 目标为本节点：本节点可作为裁决/目标点，需要权限判定（除非它是下行控制链，已在父分支处理）。
	if req.TargetNode == local {
		if !h.hasPermission(req.ExecutorNode, permExecCall) {
			h.sendCallResp(ctx, hdr, CallResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", ExecutorNode: req.ExecutorNode, TargetNode: req.TargetNode, Method: req.Method})
			return
		}
		h.execLocal(ctx, hdr, req)
		return
	}

	// 目标在本子树内？
	targetConn, ok := cm.GetByNode(req.TargetNode)
	if !ok || targetConn == nil || isParentConn(targetConn) {
		// 不在本子树：上送父节点（若无父则 not found）
		parent := findParentConn(cm)
		if parent == nil {
			h.sendCallResp(ctx, hdr, CallResp{ReqID: req.ReqID, Code: 404, Msg: "not found", ExecutorNode: req.ExecutorNode, TargetNode: req.TargetNode, Method: req.Method})
			return
		}
		parentNode := connNodeID(parent)
		if parentNode == 0 {
			h.sendCallResp(ctx, hdr, CallResp{ReqID: req.ReqID, Code: 500, Msg: "invalid parent route", ExecutorNode: req.ExecutorNode, TargetNode: req.TargetNode, Method: req.Method})
			return
		}
		// 上送必须让父节点进入 handler：TargetID=父节点自身
		upHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			h.sendCallResp(ctx, hdr, CallResp{ReqID: req.ReqID, Code: 500, Msg: "hop limit exceeded", ExecutorNode: req.ExecutorNode, TargetNode: req.TargetNode, Method: req.Method})
			return
		}
		upHdr.WithTargetID(parentNode)
		h.sendToConn(ctx, parent, upHdr, payloadFrom(message{Action: actionCall, Data: mustJSON(req)}))
		return
	}

	// 判定 executor 与 target 是否处于同一 child 分支；若是则下送该 child 继续裁决（本节点非 LCA）。
	execConn, ok2 := cm.GetByNode(req.ExecutorNode)
	if ok2 && execConn != nil && execConn.ID() == targetConn.ID() {
		nextNode := connNodeID(execConn)
		if nextNode == 0 {
			h.sendCallResp(ctx, hdr, CallResp{ReqID: req.ReqID, Code: 500, Msg: "invalid route", ExecutorNode: req.ExecutorNode, TargetNode: req.TargetNode, Method: req.Method})
			return
		}
		childHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			h.sendCallResp(ctx, hdr, CallResp{ReqID: req.ReqID, Code: 500, Msg: "hop limit exceeded", ExecutorNode: req.ExecutorNode, TargetNode: req.TargetNode, Method: req.Method})
			return
		}
		childHdr.WithTargetID(nextNode)
		h.sendToConn(ctx, execConn, childHdr, payloadFrom(message{Action: actionCall, Data: mustJSON(req)}))
		return
	}

	// 本节点为 LCA：判定权限后，按最终 target 下送到对应 child（downstream）。
	if !h.hasPermission(req.ExecutorNode, permExecCall) {
		h.sendCallResp(ctx, hdr, CallResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", ExecutorNode: req.ExecutorNode, TargetNode: req.TargetNode, Method: req.Method})
		return
	}
	downHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		h.sendCallResp(ctx, hdr, CallResp{ReqID: req.ReqID, Code: 500, Msg: "hop limit exceeded", ExecutorNode: req.ExecutorNode, TargetNode: req.TargetNode, Method: req.Method})
		return
	}
	downHdr.WithTargetID(req.TargetNode)
	h.sendToConn(ctx, targetConn, downHdr, payloadFrom(message{Action: actionCall, Data: mustJSON(req)}))
}

func (h *Handler) handleCallResp(_ context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
	var resp CallResp
	if err := json.Unmarshal(data, &resp); err != nil || strings.TrimSpace(resp.ReqID) == "" {
		return
	}
	broker.SharedExecCallBroker().Deliver(resp.ReqID, resp)
}

func (h *Handler) handleCapSnapshot(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req CapSnapshotReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{Code: 400, Msg: "invalid cap_snapshot"})
		return
	}
	from, err := h.resolveSyncSource(conn, hdr, req.FromNode)
	if err != nil {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 403, Msg: err.Error(), FromNode: req.FromNode, Epoch: req.Epoch})
		return
	}
	req.FromNode = from
	if !h.hasPermission(req.FromNode, permExecCapSync) {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", FromNode: req.FromNode, Epoch: req.Epoch})
		return
	}
	if req.Epoch == 0 {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 400, Msg: "epoch required", FromNode: req.FromNode})
		return
	}
	caps, err := sanitizeSnapshotCaps(req.Caps, req.FromNode)
	if err != nil {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 400, Msg: err.Error(), FromNode: req.FromNode, Epoch: req.Epoch})
		return
	}
	lease := normalizeLease(req.LeaseMs)
	now := time.Now()

	h.capMu.Lock()
	prev, ok := h.capChildren[req.FromNode]
	if ok && req.Epoch < prev.epoch {
		h.capMu.Unlock()
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{
			ReqID: req.ReqID, Code: 409, Msg: "stale epoch", FromNode: req.FromNode, Epoch: prev.epoch,
		})
		return
	}
	h.capChildren[req.FromNode] = capPeerState{
		epoch:         req.Epoch,
		leaseExpireAt: now.Add(lease),
		caps:          caps,
	}
	h.capMu.Unlock()

	h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{
		ReqID: req.ReqID, Code: 1, Msg: "ok", FromNode: req.FromNode, Epoch: req.Epoch, Applied: len(caps),
	})
	h.maybeSyncSnapshotUpstream(ctx, true)
}

func (h *Handler) handleCapUpsert(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req CapUpsertReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{Code: 400, Msg: "invalid cap_upsert"})
		return
	}
	from, err := h.resolveSyncSource(conn, hdr, req.FromNode)
	if err != nil {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 403, Msg: err.Error(), FromNode: req.FromNode, Epoch: req.Epoch})
		return
	}
	req.FromNode = from
	if !h.hasPermission(req.FromNode, permExecCapSync) {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", FromNode: req.FromNode, Epoch: req.Epoch})
		return
	}
	if req.Epoch == 0 {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 400, Msg: "epoch required", FromNode: req.FromNode})
		return
	}
	caps, err := sanitizeSnapshotCaps(req.Caps, req.FromNode)
	if err != nil {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 400, Msg: err.Error(), FromNode: req.FromNode, Epoch: req.Epoch})
		return
	}
	lease := normalizeLease(req.LeaseMs)
	now := time.Now()

	h.capMu.Lock()
	state := h.capChildren[req.FromNode]
	if state.epoch != 0 && req.Epoch < state.epoch {
		prev := state.epoch
		h.capMu.Unlock()
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 409, Msg: "stale epoch", FromNode: req.FromNode, Epoch: prev})
		return
	}
	if state.caps == nil || req.Epoch > state.epoch {
		state.caps = make(map[string]CapabilityDescriptor)
	}
	for key, desc := range caps {
		state.caps[key] = desc
	}
	state.epoch = req.Epoch
	state.leaseExpireAt = now.Add(lease)
	h.capChildren[req.FromNode] = state
	h.capMu.Unlock()

	h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{
		ReqID: req.ReqID, Code: 1, Msg: "ok", FromNode: req.FromNode, Epoch: req.Epoch, Applied: len(caps),
	})
	h.maybeSyncSnapshotUpstream(ctx, true)
}

func (h *Handler) handleCapWithdraw(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req CapWithdrawReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{Code: 400, Msg: "invalid cap_withdraw"})
		return
	}
	from, err := h.resolveSyncSource(conn, hdr, req.FromNode)
	if err != nil {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 403, Msg: err.Error(), FromNode: req.FromNode, Epoch: req.Epoch})
		return
	}
	req.FromNode = from
	if !h.hasPermission(req.FromNode, permExecCapSync) {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", FromNode: req.FromNode, Epoch: req.Epoch})
		return
	}
	if req.Epoch == 0 {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 400, Msg: "epoch required", FromNode: req.FromNode})
		return
	}

	h.capMu.Lock()
	state, ok := h.capChildren[req.FromNode]
	if !ok || state.epoch == 0 {
		h.capMu.Unlock()
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{
			ReqID: req.ReqID, Code: 404, Msg: "snapshot not found", FromNode: req.FromNode, Epoch: req.Epoch,
		})
		return
	}
	if req.Epoch < state.epoch {
		prev := state.epoch
		h.capMu.Unlock()
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{
			ReqID: req.ReqID, Code: 409, Msg: "stale epoch", FromNode: req.FromNode, Epoch: prev,
		})
		return
	}
	if req.Epoch > state.epoch {
		state.caps = make(map[string]CapabilityDescriptor)
		state.epoch = req.Epoch
	}
	applied := 0
	for _, key := range req.Keys {
		method := strings.TrimSpace(key.Method)
		if method == "" {
			continue
		}
		provider := key.ProviderNode
		if provider == 0 {
			provider = req.FromNode
		}
		delete(state.caps, capKey(provider, method, key.Version))
		applied++
	}
	state.leaseExpireAt = time.Now().Add(defaultCapabilityLease)
	h.capChildren[req.FromNode] = state
	h.capMu.Unlock()

	h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{
		ReqID: req.ReqID, Code: 1, Msg: "ok", FromNode: req.FromNode, Epoch: state.epoch, Applied: applied,
	})
	h.maybeSyncSnapshotUpstream(ctx, true)
}

func (h *Handler) handleCapHeartbeat(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req CapHeartbeatReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{Code: 400, Msg: "invalid cap_heartbeat"})
		return
	}
	from, err := h.resolveSyncSource(conn, hdr, req.FromNode)
	if err != nil {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 403, Msg: err.Error(), FromNode: req.FromNode, Epoch: req.Epoch})
		return
	}
	req.FromNode = from
	if !h.hasPermission(req.FromNode, permExecCapSync) {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", FromNode: req.FromNode, Epoch: req.Epoch})
		return
	}
	if req.Epoch == 0 {
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{ReqID: req.ReqID, Code: 400, Msg: "epoch required", FromNode: req.FromNode})
		return
	}
	lease := normalizeLease(req.LeaseMs)

	h.capMu.Lock()
	state, ok := h.capChildren[req.FromNode]
	if !ok || state.epoch == 0 {
		h.capMu.Unlock()
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{
			ReqID: req.ReqID, Code: 404, Msg: "snapshot not found", FromNode: req.FromNode, Epoch: req.Epoch,
		})
		return
	}
	if req.Epoch < state.epoch {
		prev := state.epoch
		h.capMu.Unlock()
		h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{
			ReqID: req.ReqID, Code: 409, Msg: "stale epoch", FromNode: req.FromNode, Epoch: prev,
		})
		return
	}
	state.epoch = req.Epoch
	state.leaseExpireAt = time.Now().Add(lease)
	h.capChildren[req.FromNode] = state
	h.capMu.Unlock()

	h.sendCapSyncRespByHeader(ctx, hdr, CapSyncResp{
		ReqID: req.ReqID, Code: 1, Msg: "ok", FromNode: req.FromNode, Epoch: req.Epoch,
	})
	h.maybeSyncSnapshotUpstream(ctx, true)
}

func (h *Handler) handleCapSyncResp(ctx context.Context, conn core.IConnection, _ core.IHeader, data json.RawMessage) {
	var resp CapSyncResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	reqID := strings.TrimSpace(resp.ReqID)
	if reqID == "" || !isUpstreamSyncReqID(reqID) {
		return
	}
	if conn != nil && !isParentConn(conn) {
		return
	}
	if resp.Code == 1 {
		h.capMu.Lock()
		if resp.Epoch > h.capUpstreamEpoch {
			h.capUpstreamEpoch = resp.Epoch
		}
		h.capUpstreamLastAt = time.Now()
		h.capMu.Unlock()
		return
	}

	resync := false
	h.capMu.Lock()
	if resp.Epoch > h.capUpstreamEpoch {
		h.capUpstreamEpoch = resp.Epoch
	}
	switch resp.Code {
	case 404, 409:
		h.capUpstreamSent = false
		h.capUpstreamCache = nil
		h.capUpstreamLastAt = time.Time{}
		resync = true
	default:
		if resp.Code >= 500 {
			h.capUpstreamSent = false
			h.capUpstreamCache = nil
			h.capUpstreamLastAt = time.Time{}
			resync = true
		}
	}
	h.capMu.Unlock()

	h.log.Warn("exec capability sync rejected",
		"req_id", reqID,
		"code", resp.Code,
		"msg", strings.TrimSpace(resp.Msg),
		"epoch", resp.Epoch,
		"resync", resync,
	)
	if resync {
		h.maybeSyncSnapshotUpstream(ctx, true)
	}
}

func (h *Handler) handleCapQuery(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req CapQueryReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendCapQueryRespByHeader(ctx, hdr, CapQueryResp{ReqID: "", Code: 400, Msg: "invalid cap_query"})
		return
	}
	req.ReqID = strings.TrimSpace(req.ReqID)
	if req.ReqID == "" {
		h.sendCapQueryRespByHeader(ctx, hdr, CapQueryResp{Code: 400, Msg: "req_id required"})
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	requester := req.RequesterNode
	if requester == 0 && hdr != nil {
		requester = hdr.SourceID()
	}
	if requester == 0 {
		h.sendCapQueryRespByHeader(ctx, hdr, CapQueryResp{ReqID: req.ReqID, Code: 400, Msg: "requester required"})
		return
	}
	if !h.hasPermission(requester, permExecCapQuery) {
		h.sendCapQueryRespByHeader(ctx, hdr, CapQueryResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied"})
		return
	}
	local := srv.NodeID()
	total, routes := h.queryCapabilityRoutes(req, local)
	if total == 0 && !isParentConn(conn) {
		if upstreamResp, ok := h.queryCapabilityUpstream(ctx, hdr, req); ok {
			h.sendCapQueryRespByHeader(ctx, hdr, upstreamResp)
			return
		}
	}
	h.sendCapQueryRespByHeader(ctx, hdr, CapQueryResp{
		ReqID: req.ReqID, Code: 1, Msg: "ok", ResponderNode: local, Total: total, Routes: routes,
	})
}

func (h *Handler) handleCapQueryResp(_ context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
	var resp CapQueryResp
	if err := json.Unmarshal(data, &resp); err != nil || strings.TrimSpace(resp.ReqID) == "" {
		return
	}
	broker.SharedExecCapQueryBroker().Deliver(resp.ReqID, resp)
}

func (h *Handler) execLocal(ctx context.Context, reqHdr core.IHeader, req CallReq) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	local := srv.NodeID()
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fn, ok := h.methods[req.Method]
	if !ok || fn == nil {
		h.sendCallRespToNode(ctx, reqHdr, req.ExecutorNode, CallResp{ReqID: req.ReqID, Code: 404, Msg: "method not found", ExecutorNode: req.ExecutorNode, TargetNode: local, Method: req.Method})
		return
	}
	res, err := fn(callCtx, req.Args)
	if err != nil {
		code := 500
		if callCtx.Err() == context.DeadlineExceeded {
			code = 408
		}
		h.sendCallRespToNode(ctx, reqHdr, req.ExecutorNode, CallResp{ReqID: req.ReqID, Code: code, Msg: err.Error(), ExecutorNode: req.ExecutorNode, TargetNode: local, Method: req.Method})
		return
	}
	h.sendCallRespToNode(ctx, reqHdr, req.ExecutorNode, CallResp{ReqID: req.ReqID, Code: 1, Msg: "ok", ExecutorNode: req.ExecutorNode, TargetNode: local, Method: req.Method, Result: res})
}

func (h *Handler) sendCallResp(ctx context.Context, reqHdr core.IHeader, resp CallResp) {
	// 尝试从请求头推断 executor：若缺失则不发送
	executor := resp.ExecutorNode
	if executor == 0 && reqHdr != nil {
		// 兜底：按 header.TargetID 可能不可靠；因此这里不做推断
	}
	if executor == 0 {
		return
	}
	h.sendCallRespToNode(ctx, reqHdr, executor, resp)
}

func (h *Handler) sendCallRespToNode(ctx context.Context, reqHdr core.IHeader, target uint32, resp CallResp) {
	if target == 0 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	body, _ := json.Marshal(message{Action: actionCallResp, Data: mustJSON(resp)})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorOKResp).
		WithSubProto(SubProtoExec).
		WithSourceID(srv.NodeID()).
		WithTargetID(target)
	if reqHdr != nil {
		if msgID := reqHdr.GetMsgID(); msgID != 0 {
			hdr = hdr.WithMsgID(msgID)
		}
		if traceID := reqHdr.GetTraceID(); traceID != 0 {
			hdr = hdr.WithTraceID(traceID)
		}
	}

	// 逐跳选择下一跳连接：先命中子树，否则上送父节点
	var next core.IConnection
	if c, ok := srv.ConnManager().GetByNode(target); ok && c != nil {
		next = c
	} else {
		next = findParentConn(srv.ConnManager())
	}
	if next == nil {
		return
	}
	_ = srv.Send(ctx, next.ID(), hdr, body)
}

func (h *Handler) maybeSyncSnapshotUpstream(ctx context.Context, force bool) {
	srv := core.ServerFromContext(ctx)
	if srv == nil || srv.ConnManager() == nil {
		return
	}
	parent := findParentConn(srv.ConnManager())
	if parent == nil {
		h.capMu.Lock()
		h.capUpstreamParent = 0
		h.capUpstreamSent = false
		h.capUpstreamLastAt = time.Time{}
		h.capUpstreamCache = nil
		h.capMu.Unlock()
		return
	}
	parentNode := connNodeID(parent)
	localNode := srv.NodeID()
	if parentNode == 0 || localNode == 0 {
		return
	}
	now := time.Now()

	var (
		snapshotReq  *CapSnapshotReq
		upsertReq    *CapUpsertReq
		withdrawReq  *CapWithdrawReq
		heartbeatReq *CapHeartbeatReq
	)

	h.capMu.Lock()
	if h.capUpstreamParent != parentNode {
		h.capUpstreamParent = parentNode
		h.capUpstreamSent = false
		h.capUpstreamLastAt = time.Time{}
		h.capUpstreamCache = nil
	}

	current := h.collectAggregatedCapsLocked(localNode, now)
	if !h.capUpstreamSent {
		h.capUpstreamEpoch++
		if h.capUpstreamEpoch == 0 {
			h.capUpstreamEpoch = 1
		}
		h.capUpstreamReqSeq++
		req := CapSnapshotReq{
			ReqID:    "capsnapshot-" + strconv.FormatUint(uint64(localNode), 10) + "-" + strconv.FormatUint(h.capUpstreamReqSeq, 10),
			FromNode: localNode,
			Epoch:    h.capUpstreamEpoch,
			LeaseMs:  uint64(defaultCapabilityLease / time.Millisecond),
			Caps:     capabilitiesFromMap(current),
		}
		h.capUpstreamSent = true
		h.capUpstreamCache = cloneCapabilityMap(current)
		h.capUpstreamLastAt = now
		snapshotReq = &req
		h.capMu.Unlock()
	} else {
		upserts, withdraws := diffCapabilityMaps(h.capUpstreamCache, current)
		needHeartbeat := !h.capUpstreamLastAt.IsZero() && now.Sub(h.capUpstreamLastAt) >= defaultCapabilityLease/2
		if force && len(upserts) == 0 && len(withdraws) == 0 {
			needHeartbeat = true
		}
		if len(upserts) == 0 && len(withdraws) == 0 && !needHeartbeat {
			h.capMu.Unlock()
			return
		}
		h.capUpstreamReqSeq++
		reqSeq := h.capUpstreamReqSeq
		if len(upserts) > 0 {
			req := CapUpsertReq{
				ReqID:    "capupsert-" + strconv.FormatUint(uint64(localNode), 10) + "-" + strconv.FormatUint(reqSeq, 10),
				FromNode: localNode,
				Epoch:    h.capUpstreamEpoch,
				LeaseMs:  uint64(defaultCapabilityLease / time.Millisecond),
				Caps:     upserts,
			}
			upsertReq = &req
		}
		if len(withdraws) > 0 {
			req := CapWithdrawReq{
				ReqID:    "capwithdraw-" + strconv.FormatUint(uint64(localNode), 10) + "-" + strconv.FormatUint(reqSeq, 10),
				FromNode: localNode,
				Epoch:    h.capUpstreamEpoch,
				Keys:     withdraws,
			}
			withdrawReq = &req
		}
		if needHeartbeat {
			req := CapHeartbeatReq{
				ReqID:    "capheartbeat-" + strconv.FormatUint(uint64(localNode), 10) + "-" + strconv.FormatUint(reqSeq, 10),
				FromNode: localNode,
				Epoch:    h.capUpstreamEpoch,
				LeaseMs:  uint64(defaultCapabilityLease / time.Millisecond),
			}
			heartbeatReq = &req
		}
		h.capUpstreamCache = cloneCapabilityMap(current)
		h.capUpstreamLastAt = now
		h.capMu.Unlock()
	}

	if snapshotReq != nil {
		h.sendUpstreamCapabilitySync(ctx, parent, localNode, parentNode, actionCapSnapshot, snapshotReq)
		return
	}
	if upsertReq != nil {
		h.sendUpstreamCapabilitySync(ctx, parent, localNode, parentNode, actionCapUpsert, upsertReq)
	}
	if withdrawReq != nil {
		h.sendUpstreamCapabilitySync(ctx, parent, localNode, parentNode, actionCapWithdraw, withdrawReq)
	}
	if heartbeatReq != nil {
		h.sendUpstreamCapabilitySync(ctx, parent, localNode, parentNode, actionCapHeartbeat, heartbeatReq)
	}
}

func (h *Handler) sendUpstreamCapabilitySync(ctx context.Context, parent core.IConnection, localNode, parentNode uint32, action string, req any) {
	if parent == nil || localNode == 0 || parentNode == 0 || strings.TrimSpace(action) == "" || req == nil {
		return
	}
	payload := payloadFrom(message{Action: action, Data: mustJSON(req)})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(localNode).
		WithTargetID(parentNode)
	h.sendToConn(ctx, parent, hdr, payload)
}

func (h *Handler) collectAggregatedCapsLocked(localNode uint32, now time.Time) map[string]CapabilityDescriptor {
	unique := make(map[string]CapabilityDescriptor, len(h.capLocal)+16)
	for _, desc := range h.capLocal {
		copyDesc := cloneCapabilityDescriptor(desc)
		copyDesc.ProviderNode = localNode
		unique[capKey(localNode, copyDesc.Method, copyDesc.Version)] = copyDesc
	}
	for childNode, state := range h.capChildren {
		if !state.leaseExpireAt.IsZero() && now.After(state.leaseExpireAt) {
			delete(h.capChildren, childNode)
			continue
		}
		for key, desc := range state.caps {
			unique[key] = cloneCapabilityDescriptor(desc)
		}
	}
	return unique
}

func capabilitiesFromMap(in map[string]CapabilityDescriptor) []CapabilityDescriptor {
	if len(in) == 0 {
		return nil
	}
	out := make([]CapabilityDescriptor, 0, len(in))
	for _, desc := range in {
		out = append(out, cloneCapabilityDescriptor(desc))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		if out[i].ProviderNode != out[j].ProviderNode {
			return out[i].ProviderNode < out[j].ProviderNode
		}
		return out[i].Version < out[j].Version
	})
	return out
}

func cloneCapabilityMap(in map[string]CapabilityDescriptor) map[string]CapabilityDescriptor {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]CapabilityDescriptor, len(in))
	for key, desc := range in {
		out[key] = cloneCapabilityDescriptor(desc)
	}
	return out
}

func cloneCapabilityDescriptor(in CapabilityDescriptor) CapabilityDescriptor {
	out := CapabilityDescriptor{
		ProviderNode:     in.ProviderNode,
		Method:           strings.TrimSpace(in.Method),
		Version:          strings.TrimSpace(in.Version),
		DefaultTimeoutMs: in.DefaultTimeoutMs,
	}
	if len(in.InputSchema) > 0 {
		out.InputSchema = cloneRaw(in.InputSchema)
	}
	if len(in.OutputSchema) > 0 {
		out.OutputSchema = cloneRaw(in.OutputSchema)
	}
	if len(in.Permissions) > 0 {
		out.Permissions = append([]string(nil), in.Permissions...)
	}
	if len(in.Tags) > 0 {
		out.Tags = make(map[string]string, len(in.Tags))
		for key, val := range in.Tags {
			out.Tags[key] = val
		}
	}
	return out
}

func diffCapabilityMaps(prev, current map[string]CapabilityDescriptor) ([]CapabilityDescriptor, []CapabilityKey) {
	upserts := make([]CapabilityDescriptor, 0)
	withdraws := make([]CapabilityKey, 0)
	for key, desc := range current {
		prevDesc, ok := prev[key]
		if !ok || !capabilityDescriptorEqual(prevDesc, desc) {
			upserts = append(upserts, cloneCapabilityDescriptor(desc))
		}
	}
	for key := range prev {
		if _, ok := current[key]; !ok {
			withdraws = append(withdraws, capKeyToWire(key))
		}
	}
	sort.Slice(upserts, func(i, j int) bool {
		if upserts[i].Method != upserts[j].Method {
			return upserts[i].Method < upserts[j].Method
		}
		if upserts[i].ProviderNode != upserts[j].ProviderNode {
			return upserts[i].ProviderNode < upserts[j].ProviderNode
		}
		return upserts[i].Version < upserts[j].Version
	})
	sort.Slice(withdraws, func(i, j int) bool {
		if withdraws[i].ProviderNode != withdraws[j].ProviderNode {
			return withdraws[i].ProviderNode < withdraws[j].ProviderNode
		}
		if withdraws[i].Method != withdraws[j].Method {
			return withdraws[i].Method < withdraws[j].Method
		}
		return withdraws[i].Version < withdraws[j].Version
	})
	return upserts, withdraws
}

func capabilityDescriptorEqual(a, b CapabilityDescriptor) bool {
	if a.ProviderNode != b.ProviderNode || strings.TrimSpace(a.Method) != strings.TrimSpace(b.Method) || strings.TrimSpace(a.Version) != strings.TrimSpace(b.Version) || a.DefaultTimeoutMs != b.DefaultTimeoutMs {
		return false
	}
	if len(a.InputSchema) != len(b.InputSchema) || string(a.InputSchema) != string(b.InputSchema) {
		return false
	}
	if len(a.OutputSchema) != len(b.OutputSchema) || string(a.OutputSchema) != string(b.OutputSchema) {
		return false
	}
	if len(a.Permissions) != len(b.Permissions) {
		return false
	}
	for idx := range a.Permissions {
		if a.Permissions[idx] != b.Permissions[idx] {
			return false
		}
	}
	if len(a.Tags) != len(b.Tags) {
		return false
	}
	for key, val := range a.Tags {
		if b.Tags[key] != val {
			return false
		}
	}
	return true
}

func capKeyToWire(key string) CapabilityKey {
	parts := strings.SplitN(strings.TrimSpace(key), "|", 3)
	if len(parts) != 3 {
		return CapabilityKey{}
	}
	provider := uint32(0)
	if parsed, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 32); err == nil {
		provider = uint32(parsed)
	}
	return CapabilityKey{
		ProviderNode: provider,
		Method:       strings.TrimSpace(parts[1]),
		Version:      strings.TrimSpace(parts[2]),
	}
}

func (h *Handler) queryCapabilityUpstream(ctx context.Context, reqHdr core.IHeader, req CapQueryReq) (CapQueryResp, bool) {
	if strings.TrimSpace(req.ReqID) == "" {
		return CapQueryResp{}, false
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil || srv.ConnManager() == nil {
		return CapQueryResp{}, false
	}
	parent := findParentConn(srv.ConnManager())
	if parent == nil {
		return CapQueryResp{}, false
	}
	parentNode := connNodeID(parent)
	localNode := srv.NodeID()
	if parentNode == 0 || localNode == 0 {
		return CapQueryResp{}, false
	}

	waitCh, cancel := broker.SharedExecCapQueryBroker().Register(req.ReqID)
	defer cancel()

	forwardHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(localNode).
		WithTargetID(parentNode)
	if reqHdr != nil {
		if trace := reqHdr.GetTraceID(); trace != 0 {
			forwardHdr = forwardHdr.WithTraceID(trace)
		}
	}
	h.sendToConn(ctx, parent, forwardHdr, payloadFrom(message{Action: actionCapQuery, Data: mustJSON(req)}))

	select {
	case resp, ok := <-waitCh:
		if !ok {
			return CapQueryResp{}, false
		}
		return resp, true
	case <-time.After(capQueryForwardTimeout):
		return CapQueryResp{}, false
	}
}

func (h *Handler) sendCapSyncRespByHeader(ctx context.Context, reqHdr core.IHeader, resp CapSyncResp) {
	target := uint32(0)
	if reqHdr != nil && reqHdr.SourceID() != 0 {
		target = reqHdr.SourceID()
	}
	if target == 0 && resp.FromNode != 0 {
		target = resp.FromNode
	}
	if target == 0 {
		return
	}
	if resp.Responder == 0 {
		if srv := core.ServerFromContext(ctx); srv != nil {
			resp.Responder = srv.NodeID()
		}
	}
	h.sendExecRespToNode(ctx, reqHdr, target, actionCapSyncResp, resp)
}

func (h *Handler) sendCapQueryRespByHeader(ctx context.Context, reqHdr core.IHeader, resp CapQueryResp) {
	target := uint32(0)
	if reqHdr != nil && reqHdr.SourceID() != 0 {
		target = reqHdr.SourceID()
	}
	if target == 0 {
		return
	}
	if resp.ResponderNode == 0 {
		if srv := core.ServerFromContext(ctx); srv != nil {
			resp.ResponderNode = srv.NodeID()
		}
	}
	h.sendExecRespToNode(ctx, reqHdr, target, actionCapQueryResp, resp)
}

func (h *Handler) sendExecRespToNode(ctx context.Context, reqHdr core.IHeader, target uint32, action string, data any) {
	if target == 0 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	body, _ := json.Marshal(message{Action: action, Data: mustJSON(data)})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorOKResp).
		WithSubProto(SubProtoExec).
		WithSourceID(srv.NodeID()).
		WithTargetID(target)
	if reqHdr != nil {
		if msgID := reqHdr.GetMsgID(); msgID != 0 {
			hdr = hdr.WithMsgID(msgID)
		}
		if traceID := reqHdr.GetTraceID(); traceID != 0 {
			hdr = hdr.WithTraceID(traceID)
		}
	}

	var next core.IConnection
	if c, ok := srv.ConnManager().GetByNode(target); ok && c != nil {
		next = c
	} else {
		next = findParentConn(srv.ConnManager())
	}
	if next == nil {
		return
	}
	_ = srv.Send(ctx, next.ID(), hdr, body)
}

func (h *Handler) resolveSyncSource(conn core.IConnection, hdr core.IHeader, from uint32) (uint32, error) {
	from = firstNonZero(from, connNodeID(conn))
	if from == 0 && hdr != nil {
		from = hdr.SourceID()
	}
	if from == 0 {
		return 0, errText("from_node required")
	}
	if conn != nil {
		if isParentConn(conn) {
			return 0, errText("sync from parent unsupported")
		}
		if node := connNodeID(conn); node != 0 && node != from {
			return 0, errText("from_node mismatch")
		}
	}
	return from, nil
}

func sanitizeSnapshotCaps(in []CapabilityDescriptor, from uint32) (map[string]CapabilityDescriptor, error) {
	out := make(map[string]CapabilityDescriptor)
	for idx, capDesc := range in {
		desc, err := normalizeCapabilityDescriptor(capDesc, from)
		if err != nil {
			return nil, errText("invalid capability at index " + strconv.Itoa(idx) + ": " + err.Error())
		}
		out[capKey(desc.ProviderNode, desc.Method, desc.Version)] = desc
	}
	return out, nil
}

func normalizeCapabilityDescriptor(in CapabilityDescriptor, from uint32) (CapabilityDescriptor, error) {
	desc := CapabilityDescriptor{
		ProviderNode:     in.ProviderNode,
		Method:           strings.TrimSpace(in.Method),
		Version:          strings.TrimSpace(in.Version),
		DefaultTimeoutMs: in.DefaultTimeoutMs,
	}
	if desc.Method == "" {
		return CapabilityDescriptor{}, errText("method required")
	}
	if desc.ProviderNode == 0 {
		desc.ProviderNode = from
	}
	if desc.ProviderNode != from {
		return CapabilityDescriptor{}, errText("provider_node mismatch")
	}
	if desc.DefaultTimeoutMs < 0 {
		return CapabilityDescriptor{}, errText("default_timeout_ms must be >= 0")
	}
	if len(in.InputSchema) > 0 {
		desc.InputSchema = cloneRaw(in.InputSchema)
	}
	if len(in.OutputSchema) > 0 {
		desc.OutputSchema = cloneRaw(in.OutputSchema)
	}
	if len(in.Permissions) > 0 {
		desc.Permissions = append([]string(nil), in.Permissions...)
	}
	if len(in.Tags) > 0 {
		desc.Tags = make(map[string]string, len(in.Tags))
		for key, val := range in.Tags {
			desc.Tags[strings.TrimSpace(key)] = strings.TrimSpace(val)
		}
	}
	return desc, nil
}

func normalizeLease(leaseMs uint64) time.Duration {
	if leaseMs == 0 {
		return defaultCapabilityLease
	}
	lease := time.Duration(leaseMs) * time.Millisecond
	if lease < 5*time.Second {
		return 5 * time.Second
	}
	return lease
}

func (h *Handler) queryCapabilityRoutes(req CapQueryReq, localNode uint32) (int, []CapabilityRoute) {
	methodFilter := strings.TrimSpace(req.Method)
	limit := req.Limit
	if limit <= 0 {
		limit = maxCapQueryLimit
	}
	if limit > maxCapQueryLimit {
		limit = maxCapQueryLimit
	}

	now := time.Now()
	h.capMu.Lock()
	for node, state := range h.capChildren {
		if !state.leaseExpireAt.IsZero() && now.After(state.leaseExpireAt) {
			delete(h.capChildren, node)
		}
	}
	localCaps := make([]CapabilityDescriptor, 0, len(h.capLocal))
	for _, desc := range h.capLocal {
		copyDesc := desc
		copyDesc.ProviderNode = localNode
		localCaps = append(localCaps, copyDesc)
	}
	childCaps := make([]CapabilityRoute, 0)
	for via, state := range h.capChildren {
		for _, desc := range state.caps {
			childCaps = append(childCaps, capabilityRouteFromDesc(desc, via, state.leaseExpireAt, req.IncludeSchema))
		}
	}
	h.capMu.Unlock()

	routes := make([]CapabilityRoute, 0, len(localCaps)+len(childCaps))
	for _, desc := range localCaps {
		routes = append(routes, capabilityRouteFromDesc(desc, 0, time.Time{}, req.IncludeSchema))
	}
	routes = append(routes, childCaps...)
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Method != routes[j].Method {
			return routes[i].Method < routes[j].Method
		}
		if routes[i].ProviderNode != routes[j].ProviderNode {
			return routes[i].ProviderNode < routes[j].ProviderNode
		}
		return routes[i].Version < routes[j].Version
	})

	filtered := make([]CapabilityRoute, 0, len(routes))
	for _, route := range routes {
		if req.ProviderNode != 0 && route.ProviderNode != req.ProviderNode {
			continue
		}
		if methodFilter != "" {
			if req.Prefix {
				if !strings.HasPrefix(route.Method, methodFilter) {
					continue
				}
			} else if route.Method != methodFilter {
				continue
			}
		}
		filtered = append(filtered, route)
	}
	total := len(filtered)
	if total > limit {
		filtered = filtered[:limit]
	}
	return total, filtered
}

func capabilityRouteFromDesc(desc CapabilityDescriptor, via uint32, leaseExpireAt time.Time, includeSchema bool) CapabilityRoute {
	route := CapabilityRoute{
		ProviderNode:     desc.ProviderNode,
		ViaNode:          via,
		Method:           strings.TrimSpace(desc.Method),
		Version:          strings.TrimSpace(desc.Version),
		DefaultTimeoutMs: desc.DefaultTimeoutMs,
	}
	if len(desc.Permissions) > 0 {
		route.Permissions = append([]string(nil), desc.Permissions...)
	}
	if len(desc.Tags) > 0 {
		route.Tags = make(map[string]string, len(desc.Tags))
		for key, val := range desc.Tags {
			route.Tags[key] = val
		}
	}
	if !leaseExpireAt.IsZero() {
		route.LeaseExpireAt = leaseExpireAt.UnixMilli()
	}
	if includeSchema {
		if len(desc.InputSchema) > 0 {
			route.InputSchema = cloneRaw(desc.InputSchema)
		}
		if len(desc.OutputSchema) > 0 {
			route.OutputSchema = cloneRaw(desc.OutputSchema)
		}
	}
	return route
}

func capKey(provider uint32, method, version string) string {
	return strconv.FormatUint(uint64(provider), 10) + "|" + strings.TrimSpace(method) + "|" + strings.TrimSpace(version)
}

func cloneRaw(in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func isRespAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case actionCallResp, actionCapSyncResp, actionCapQueryResp:
		return true
	default:
		return false
	}
}

func isUpstreamSyncReqID(reqID string) bool {
	reqID = strings.ToLower(strings.TrimSpace(reqID))
	return strings.HasPrefix(reqID, "capsnapshot-") ||
		strings.HasPrefix(reqID, "capupsert-") ||
		strings.HasPrefix(reqID, "capwithdraw-") ||
		strings.HasPrefix(reqID, "capheartbeat-")
}

type errText string

func (e errText) Error() string { return string(e) }

func firstNonZero(values ...uint32) uint32 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

func (h *Handler) ensureConnCloseSubscription(srv core.IServer) {
	if srv == nil {
		return
	}
	h.eventSubOnce.Do(func() {
		eb := srv.EventBus()
		if eb == nil {
			return
		}
		eb.Subscribe("conn.closed", func(evCtx context.Context, evt eventbus.Event) {
			nodeID := connClosedNodeID(evt.Data)
			if nodeID == 0 {
				return
			}
			if h.removeChildCapabilities(nodeID) {
				h.maybeSyncSnapshotUpstream(evCtx, true)
			}
		})
	})
}

func (h *Handler) removeChildCapabilities(nodeID uint32) bool {
	if nodeID == 0 {
		return false
	}
	h.capMu.Lock()
	_, ok := h.capChildren[nodeID]
	if ok {
		delete(h.capChildren, nodeID)
	}
	h.capMu.Unlock()
	return ok
}

func connClosedNodeID(data any) uint32 {
	switch v := data.(type) {
	case map[string]any:
		if nodeID, ok := parseNodeID(v["node_id"]); ok {
			return nodeID
		}
		if nodeID, ok := parseNodeID(v["nodeID"]); ok {
			return nodeID
		}
	}
	return 0
}

func parseNodeID(v any) (uint32, bool) {
	switch vv := v.(type) {
	case uint32:
		return vv, vv != 0
	case uint64:
		if vv == 0 || vv > uint64(^uint32(0)) {
			return 0, false
		}
		return uint32(vv), true
	case int:
		if vv <= 0 {
			return 0, false
		}
		return uint32(vv), true
	case int64:
		if vv <= 0 || vv > int64(^uint32(0)) {
			return 0, false
		}
		return uint32(vv), true
	case float64:
		if vv <= 0 || vv > float64(^uint32(0)) {
			return 0, false
		}
		return uint32(vv), true
	case json.Number:
		if n, err := vv.Int64(); err == nil && n > 0 && n <= int64(^uint32(0)) {
			return uint32(n), true
		}
	case string:
		if parsed, err := strconv.ParseUint(strings.TrimSpace(vv), 10, 32); err == nil && parsed > 0 {
			return uint32(parsed), true
		}
	}
	return 0, false
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

func payloadFrom(msg message) []byte {
	b, _ := json.Marshal(msg)
	return b
}

func (h *Handler) hasPermission(nodeID uint32, perm string) bool {
	if h.permCfg == nil {
		return false
	}
	return h.permCfg.Has(nodeID, perm)
}

func isParentConn(c core.IConnection) bool {
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

func findParentConn(cm core.IConnectionManager) core.IConnection {
	if cm == nil {
		return nil
	}
	var parent core.IConnection
	cm.Range(func(c core.IConnection) bool {
		if isParentConn(c) {
			parent = c
			return false
		}
		return true
	})
	return parent
}

func connNodeID(c core.IConnection) uint32 {
	if c == nil {
		return 0
	}
	if meta, ok := c.GetMeta("nodeID"); ok {
		if nid, ok2 := meta.(uint32); ok2 {
			return nid
		}
	}
	return 0
}

func (h *Handler) forwardDownOrDrop(ctx context.Context, srv core.IServer, hdr core.IHeader, payload []byte, target uint32) {
	if srv == nil || hdr == nil || target == 0 {
		return
	}
	var next core.IConnection
	if c, ok := srv.ConnManager().GetByNode(target); ok && c != nil {
		next = c
	}
	if next == nil {
		return
	}
	fwdHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		return
	}
	fwdHdr.WithTargetID(target)
	_ = srv.Send(ctx, next.ID(), fwdHdr, payload)
}

func (h *Handler) sendToConn(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	if conn == nil || hdr == nil || len(payload) == 0 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		_ = conn.SendWithHeader(hdr, payload, header.HeaderTcpCodec{})
		return
	}
	_ = srv.Send(ctx, conn.ID(), hdr, payload)
}
