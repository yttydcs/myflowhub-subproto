package exec

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	core "github.com/yttydcs/myflowhub-core"
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
}

func NewHandler(log *slog.Logger) *Handler {
	return NewHandlerWithConfig(nil, log)
}

func NewHandlerWithConfig(cfg core.IConfig, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	h := &Handler{
		log:     log,
		methods: make(map[string]MethodFunc),
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
}

func (h *Handler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	var msg message
	if err := json.Unmarshal(payload, &msg); err != nil {
		h.log.Warn("exec invalid payload", "err", err)
		return
	}
	// call_resp 属于“返回路径”，应按 header.TargetID 逐跳转发到 executor。
	if msg.Action == actionCallResp {
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
			h.execLocal(ctx, req)
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
		h.execLocal(ctx, req)
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

func (h *Handler) execLocal(ctx context.Context, req CallReq) {
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
		h.sendCallRespToNode(ctx, req.ExecutorNode, CallResp{ReqID: req.ReqID, Code: 404, Msg: "method not found", ExecutorNode: req.ExecutorNode, TargetNode: local, Method: req.Method})
		return
	}
	res, err := fn(callCtx, req.Args)
	if err != nil {
		code := 500
		if callCtx.Err() == context.DeadlineExceeded {
			code = 408
		}
		h.sendCallRespToNode(ctx, req.ExecutorNode, CallResp{ReqID: req.ReqID, Code: code, Msg: err.Error(), ExecutorNode: req.ExecutorNode, TargetNode: local, Method: req.Method})
		return
	}
	h.sendCallRespToNode(ctx, req.ExecutorNode, CallResp{ReqID: req.ReqID, Code: 1, Msg: "ok", ExecutorNode: req.ExecutorNode, TargetNode: local, Method: req.Method, Result: res})
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
	h.sendCallRespToNode(ctx, executor, resp)
}

func (h *Handler) sendCallRespToNode(ctx context.Context, target uint32, resp CallResp) {
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
