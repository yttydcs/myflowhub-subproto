package flow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	"github.com/yttydcs/myflowhub-core/subproto"

	"github.com/yttydcs/myflowhub-subproto/broker"
	protocolexec "github.com/yttydcs/myflowhub-proto/protocol/exec"
)

type LocalMethodFunc func(ctx context.Context, args json.RawMessage) (json.RawMessage, error)

type Handler struct {
	subproto.ActionBaseSubProcess
	log *slog.Logger
	cfg core.IConfig
	srv core.IServer

	permCfg *permission.Config

	mu sync.Mutex

	baseDir string
	flows   map[string]setReq
	runs    map[string]*runState // run_id -> state

	schedulers map[string]*flowScheduler // flow_id -> scheduler

	schedStarted bool

	localMethods map[string]LocalMethodFunc
}

type flowScheduler struct {
	stop chan struct{}
}

type runState struct {
	mu sync.Mutex

	flowID string
	runID  string
	status string
	nodes  map[string]nodeStatus
	start  time.Time
	end    time.Time
}

func NewHandler(log *slog.Logger) *Handler {
	return NewHandlerWithConfig(nil, log)
}

func NewHandlerWithConfig(cfg core.IConfig, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	h := &Handler{
		log:          log,
		cfg:          cfg,
		flows:        make(map[string]setReq),
		runs:         make(map[string]*runState),
		schedulers:   make(map[string]*flowScheduler),
		localMethods: make(map[string]LocalMethodFunc),
	}
	if cfg != nil {
		h.permCfg = permission.SharedConfig(cfg)
	}
	if h.permCfg == nil {
		h.permCfg = permission.NewConfig(nil)
	}
	// 内置 local 方法：debug::echo / debug::fail
	h.RegisterLocalMethod("debug::echo", func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
		if len(args) == 0 {
			return json.RawMessage(`{}`), nil
		}
		return args, nil
	})
	h.RegisterLocalMethod("debug::fail", func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return nil, errors.New("forced failure")
	})
	return h
}

// BindServer 用于让 handler 在非 OnReceive 触发的场景（例如 interval）也能获取发送能力。
func (h *Handler) BindServer(srv core.IServer) {
	h.mu.Lock()
	h.srv = srv
	h.mu.Unlock()
	h.startSchedulers()
}

// AcceptCmd 声明 Cmd 帧在 target!=local 时也需要本地处理一次（用于逐级授权/裁决）。
func (h *Handler) AcceptCmd() bool { return true }

func (h *Handler) SubProto() uint8 { return SubProtoFlow }

func (h *Handler) Init() bool {
	h.baseDir = loadConfig(h.cfg).BaseDir
	_ = os.MkdirAll(h.baseDir, 0o755)
	h.loadFlowsFromDisk()
	h.initActions()
	return true
}

func (h *Handler) initActions() {
	h.ResetActions()
	for _, act := range registerActions(h) {
		h.RegisterAction(act)
	}
}

func (h *Handler) RegisterLocalMethod(method string, fn LocalMethodFunc) {
	method = strings.TrimSpace(method)
	if method == "" || fn == nil {
		return
	}
	h.localMethods[method] = fn
}

func (h *Handler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	if srv := core.ServerFromContext(ctx); srv != nil {
		h.BindServer(srv)
		// 仅在首次拿到 server 后启动 scheduler（避免 interval 场景无法发送）。
		h.startSchedulers()
	}
	var msg message
	if err := json.Unmarshal(payload, &msg); err != nil {
		h.log.Warn("flow invalid payload", "err", err)
		return
	}
	entry, ok := h.LookupAction(msg.Action)
	if !ok {
		// 兼容：flow 的响应帧（*_resp）不需要本节点理解；target!=local 时按 header.TargetID 逐跳转发即可。
		if h.forwardRemoteByHeaderTarget(ctx, conn, hdr, payload) {
			return
		}
		h.log.Debug("unknown flow action", "action", msg.Action)
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
		h.log.Warn("drop flow frame: no route", "target", target, "source", hdr.SourceID())
		return true
	}
	if isParentConn(conn) && isParentConn(next) {
		h.log.Warn("drop flow frame due to invalid route (came from parent)", "target", target, "source", hdr.SourceID())
		return true
	}
	fwdHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		h.log.Warn("drop flow frame due to hop_limit", "target", target, "source", hdr.SourceID())
		return true
	}
	fwdHdr.WithTargetID(target)
	_ = srv.Send(ctx, next.ID(), fwdHdr, payload)
	return true
}

func (h *Handler) handleSet(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req setReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendSetResp(ctx, hdr, 400, "invalid set", "")
		return
	}
	req.ReqID = strings.TrimSpace(req.ReqID)
	req.FlowID = strings.TrimSpace(req.FlowID)
	req.Trigger.Type = strings.ToLower(strings.TrimSpace(req.Trigger.Type))
	if req.ReqID == "" || req.FlowID == "" || req.Trigger.Type != "interval" || req.Trigger.EveryMs == 0 {
		h.sendSetResp(ctx, hdr, 400, "invalid set", req.FlowID)
		return
	}

	srv := core.ServerFromContext(ctx)
	if srv == nil || hdr == nil || conn == nil {
		// interval 触发可能不在带 server 的 ctx 中
		h.mu.Lock()
		srv = h.srv
		h.mu.Unlock()
		if srv == nil || hdr == nil || conn == nil {
			return
		}
	}
	local := srv.NodeID()
	cm := srv.ConnManager()
	if cm == nil {
		return
	}

	origin := req.OriginNode
	if origin == 0 {
		origin = hdr.SourceID()
	}
	executor := req.ExecutorNode
	if executor == 0 {
		executor = local
	}
	req.OriginNode = origin
	req.ExecutorNode = executor

	// 来自父节点：下游无条件信任父节点，视为已授权，直接将请求转交到 executor（或本地落盘）。
	if isParentConn(conn) {
		if executor == local {
			h.applySetLocal(ctx, req, origin)
			return
		}
		if !h.forwardDown(ctx, srv, hdr, message{Action: actionSet, Data: mustJSON(req)}, executor) {
			h.sendSetRespToNode(ctx, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "forward failed", FlowID: req.FlowID})
		}
		return
	}

	// executor 为本节点：本节点即 LCA+executor，执行权限判定并落盘生效。
	if executor == local {
		if !h.hasPermission(origin, permFlowSet) {
			h.sendSetRespToNode(ctx, origin, setResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", FlowID: req.FlowID})
			return
		}
		h.applySetLocal(ctx, req, origin)
		return
	}

	// executor 在本子树内？
	execConn, ok := cm.GetByNode(executor)
	if !ok || execConn == nil || isParentConn(execConn) {
		// 不在本子树：上送父节点（若无父则 not found）
		parent := findParentConn(cm)
		if parent == nil {
			h.sendSetRespToNode(ctx, origin, setResp{ReqID: req.ReqID, Code: 404, Msg: "not found", FlowID: req.FlowID})
			return
		}
		parentNode := connNodeID(parent)
		if parentNode == 0 {
			h.sendSetRespToNode(ctx, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "invalid parent route", FlowID: req.FlowID})
			return
		}
		// 上送必须让父节点进入 handler：TargetID=父节点自身
		upHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			h.sendSetRespToNode(ctx, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "hop limit exceeded", FlowID: req.FlowID})
			return
		}
		upHdr.WithTargetID(parentNode)
		h.sendToConn(ctx, parent, upHdr, payloadFrom(message{Action: actionSet, Data: mustJSON(req)}))
		return
	}

	// 判定 origin 与 executor 是否处于同一 child 分支；若是则下送该 child 继续裁决（本节点非 LCA）。
	originConn, ok2 := cm.GetByNode(origin)
	if ok2 && originConn != nil && originConn.ID() == execConn.ID() {
		nextNode := connNodeID(originConn)
		if nextNode == 0 {
			h.sendSetRespToNode(ctx, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "invalid route", FlowID: req.FlowID})
			return
		}
		childHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			h.sendSetRespToNode(ctx, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "hop limit exceeded", FlowID: req.FlowID})
			return
		}
		childHdr.WithTargetID(nextNode)
		h.sendToConn(ctx, originConn, childHdr, payloadFrom(message{Action: actionSet, Data: mustJSON(req)}))
		return
	}

	// 本节点为 LCA：判定权限后，向下转发到 executor（转发即同意）。
	if !h.hasPermission(origin, permFlowSet) {
		h.sendSetRespToNode(ctx, origin, setResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", FlowID: req.FlowID})
		return
	}
	downHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		h.sendSetRespToNode(ctx, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "hop limit exceeded", FlowID: req.FlowID})
		return
	}
	downHdr.WithTargetID(executor)
	h.sendToConn(ctx, execConn, downHdr, payloadFrom(message{Action: actionSet, Data: mustJSON(req)}))
}

func (h *Handler) applySetLocal(ctx context.Context, req setReq, origin uint32) {
	if err := validateGraph(req.Graph); err != nil {
		h.sendSetRespToNode(ctx, origin, setResp{ReqID: req.ReqID, Code: 400, Msg: err.Error(), FlowID: req.FlowID})
		return
	}
	h.mu.Lock()
	h.flows[req.FlowID] = req
	base := h.baseDir
	h.mu.Unlock()

	if err := os.MkdirAll(base, 0o755); err != nil {
		h.sendSetRespToNode(ctx, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "mkdir failed", FlowID: req.FlowID})
		return
	}
	path := filepath.Join(base, req.FlowID+".json")
	raw, _ := json.MarshalIndent(req, "", "  ")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		h.sendSetRespToNode(ctx, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "write failed", FlowID: req.FlowID})
		return
	}
	h.restartScheduler(req.FlowID)
	h.sendSetRespToNode(ctx, origin, setResp{ReqID: req.ReqID, Code: 1, Msg: "ok", FlowID: req.FlowID})
}

func (h *Handler) handleRun(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req runReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendRunResp(ctx, hdr, runResp{ReqID: req.ReqID, Code: 400, Msg: "invalid run"})
		return
	}
	req.ReqID = strings.TrimSpace(req.ReqID)
	req.FlowID = strings.TrimSpace(req.FlowID)
	if req.ReqID == "" || req.FlowID == "" {
		h.sendRunResp(ctx, hdr, runResp{ReqID: req.ReqID, Code: 400, Msg: "invalid run"})
		return
	}
	srv := h.getServer(ctx)
	if srv == nil || hdr == nil || conn == nil {
		return
	}
	local := srv.NodeID()
	executor := req.ExecutorNode
	if executor == 0 {
		executor = local
	}
	origin := req.OriginNode
	if origin == 0 {
		origin = hdr.SourceID()
	}
	req.OriginNode = origin
	req.ExecutorNode = executor

	if executor != local {
		h.forwardToExecutorNoPerm(ctx, srv, conn, hdr, executor, origin, message{Action: actionRun, Data: mustJSON(req)}, func() {})
		return
	}

	// 来自父节点：视为已授权/已路由到本执行者，直接执行。
	if isParentConn(conn) {
		h.runLocal(ctx, hdr, req)
		return
	}

	h.runLocal(ctx, hdr, req)
}

func (h *Handler) runLocal(ctx context.Context, hdr core.IHeader, req runReq) {
	h.mu.Lock()
	flow, ok := h.flows[req.FlowID]
	h.mu.Unlock()
	if !ok || strings.TrimSpace(flow.FlowID) == "" {
		h.sendRunResp(ctx, hdr, runResp{ReqID: req.ReqID, Code: 404, Msg: "not found", FlowID: req.FlowID})
		return
	}

	runID := newUUID()
	state := &runState{
		flowID: flow.FlowID,
		runID:  runID,
		status: "queued",
		nodes:  make(map[string]nodeStatus),
		start:  time.Now(),
	}
	h.mu.Lock()
	h.runs[runID] = state
	h.mu.Unlock()
	go h.executeFlow(ctx, flow, state)

	h.sendRunResp(ctx, hdr, runResp{ReqID: req.ReqID, Code: 1, Msg: "ok", FlowID: req.FlowID, RunID: runID})
}

func (h *Handler) handleStatus(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req statusReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendStatusResp(ctx, hdr, statusResp{ReqID: req.ReqID, Code: 400, Msg: "invalid status"})
		return
	}
	req.ReqID = strings.TrimSpace(req.ReqID)
	req.FlowID = strings.TrimSpace(req.FlowID)
	req.RunID = strings.TrimSpace(req.RunID)
	if req.ReqID == "" || req.FlowID == "" {
		h.sendStatusResp(ctx, hdr, statusResp{ReqID: req.ReqID, Code: 400, Msg: "invalid status"})
		return
	}
	srv := h.getServer(ctx)
	if srv == nil || hdr == nil || conn == nil {
		return
	}
	local := srv.NodeID()
	executor := req.ExecutorNode
	if executor == 0 {
		executor = local
	}
	origin := req.OriginNode
	if origin == 0 {
		origin = hdr.SourceID()
	}
	req.OriginNode = origin
	req.ExecutorNode = executor
	if executor != local {
		h.forwardToExecutorNoPerm(ctx, srv, conn, hdr, executor, origin, message{Action: actionStatus, Data: mustJSON(req)}, func() {})
		return
	}

	var state *runState
	h.mu.Lock()
	if req.RunID != "" {
		state = h.runs[req.RunID]
	} else {
		// 取最新一次 run（按 start 排序）
		var latest *runState
		for _, r := range h.runs {
			if r == nil || r.flowID != req.FlowID {
				continue
			}
			if latest == nil || r.start.After(latest.start) {
				latest = r
			}
		}
		state = latest
	}
	h.mu.Unlock()
	if state == nil {
		h.sendStatusResp(ctx, hdr, statusResp{ReqID: req.ReqID, Code: 404, Msg: "not found", FlowID: req.FlowID})
		return
	}
	state.mu.Lock()
	nodes := make([]nodeStatus, 0, len(state.nodes))
	for _, st := range state.nodes {
		nodes = append(nodes, st)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	resp := statusResp{
		ReqID:        req.ReqID,
		Code:         1,
		Msg:          "ok",
		ExecutorNode: executor,
		FlowID:       state.flowID,
		RunID:        state.runID,
		Status:       state.status,
		Nodes:        nodes,
	}
	state.mu.Unlock()
	h.sendStatusResp(ctx, hdr, resp)
}

func (h *Handler) handleList(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req listReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendListResp(ctx, hdr, listResp{ReqID: req.ReqID, Code: 400, Msg: "invalid list"})
		return
	}
	req.ReqID = strings.TrimSpace(req.ReqID)
	if req.ReqID == "" {
		h.sendListResp(ctx, hdr, listResp{ReqID: req.ReqID, Code: 400, Msg: "invalid list"})
		return
	}
	srv := h.getServer(ctx)
	if srv == nil || hdr == nil || conn == nil {
		return
	}
	local := srv.NodeID()
	executor := req.ExecutorNode
	if executor == 0 {
		executor = local
	}
	origin := req.OriginNode
	if origin == 0 {
		origin = hdr.SourceID()
	}
	req.OriginNode = origin
	req.ExecutorNode = executor
	if executor != local {
		h.forwardToExecutorNoPerm(ctx, srv, conn, hdr, executor, origin, message{Action: actionList, Data: mustJSON(req)}, func() {})
		return
	}
	h.mu.Lock()
	flows := make([]flowSummary, 0, len(h.flows))
	for _, f := range h.flows {
		id := strings.TrimSpace(f.FlowID)
		if id == "" {
			continue
		}
		sum := flowSummary{FlowID: id, Name: strings.TrimSpace(f.Name), EveryMs: f.Trigger.EveryMs}
		// latest run for this flow
		var latest *runState
		for _, r := range h.runs {
			if r == nil || r.flowID != id {
				continue
			}
			if latest == nil || r.start.After(latest.start) {
				latest = r
			}
		}
		if latest != nil {
			latest.mu.Lock()
			sum.LastRunID = latest.runID
			sum.LastStatus = latest.status
			latest.mu.Unlock()
		}
		flows = append(flows, sum)
	}
	h.mu.Unlock()
	sort.Slice(flows, func(i, j int) bool {
		if flows[i].Name != flows[j].Name {
			return flows[i].Name < flows[j].Name
		}
		return flows[i].FlowID < flows[j].FlowID
	})
	h.sendListResp(ctx, hdr, listResp{ReqID: req.ReqID, Code: 1, Msg: "ok", ExecutorNode: executor, Flows: flows})
}

func (h *Handler) handleGet(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req getReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendGetResp(ctx, hdr, getResp{ReqID: req.ReqID, Code: 400, Msg: "invalid get"})
		return
	}
	req.ReqID = strings.TrimSpace(req.ReqID)
	req.FlowID = strings.TrimSpace(req.FlowID)
	if req.ReqID == "" || req.FlowID == "" {
		h.sendGetResp(ctx, hdr, getResp{ReqID: req.ReqID, Code: 400, Msg: "invalid get"})
		return
	}
	srv := h.getServer(ctx)
	if srv == nil || hdr == nil || conn == nil {
		return
	}
	local := srv.NodeID()
	executor := req.ExecutorNode
	if executor == 0 {
		executor = local
	}
	origin := req.OriginNode
	if origin == 0 {
		origin = hdr.SourceID()
	}
	req.OriginNode = origin
	req.ExecutorNode = executor
	if executor != local {
		h.forwardToExecutorNoPerm(ctx, srv, conn, hdr, executor, origin, message{Action: actionGet, Data: mustJSON(req)}, func() {})
		return
	}
	h.mu.Lock()
	f, ok := h.flows[req.FlowID]
	h.mu.Unlock()
	if !ok || strings.TrimSpace(f.FlowID) == "" {
		h.sendGetResp(ctx, hdr, getResp{ReqID: req.ReqID, Code: 404, Msg: "not found", ExecutorNode: executor, FlowID: req.FlowID})
		return
	}
	h.sendGetResp(ctx, hdr, getResp{ReqID: req.ReqID, Code: 1, Msg: "ok", ExecutorNode: executor, FlowID: f.FlowID, Name: f.Name, Trigger: f.Trigger, Graph: f.Graph})
}

func (h *Handler) forwardToExecutorNoPerm(ctx context.Context, srv core.IServer, conn core.IConnection, hdr core.IHeader, executor, origin uint32, msg message, localFn func()) {
	if srv == nil || conn == nil || hdr == nil || executor == 0 {
		return
	}
	local := srv.NodeID()
	cm := srv.ConnManager()
	if cm == nil {
		return
	}
	// 来自父节点：信任父，直接向下转交到 executor（或本地）。
	if isParentConn(conn) {
		if executor == local && localFn != nil {
			localFn()
			return
		}
		h.forwardDown(ctx, srv, hdr, msg, executor)
		return
	}
	if executor == local {
		if localFn != nil {
			localFn()
		}
		return
	}
	execConn, ok := cm.GetByNode(executor)
	if !ok || execConn == nil || isParentConn(execConn) {
		parent := findParentConn(cm)
		if parent == nil {
			return
		}
		parentNode := connNodeID(parent)
		if parentNode == 0 {
			return
		}
		upHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			h.log.Warn("drop flow frame due to hop_limit", "target", parentNode, "source", hdr.SourceID())
			return
		}
		upHdr.WithTargetID(parentNode)
		h.sendToConn(ctx, parent, upHdr, payloadFrom(msg))
		return
	}
	originConn, ok2 := cm.GetByNode(origin)
	if ok2 && originConn != nil && originConn.ID() == execConn.ID() {
		nextNode := connNodeID(originConn)
		if nextNode == 0 {
			return
		}
		childHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			h.log.Warn("drop flow frame due to hop_limit", "target", nextNode, "source", hdr.SourceID())
			return
		}
		childHdr.WithTargetID(nextNode)
		h.sendToConn(ctx, originConn, childHdr, payloadFrom(msg))
		return
	}
	downHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		h.log.Warn("drop flow frame due to hop_limit", "target", executor, "source", hdr.SourceID())
		return
	}
	downHdr.WithTargetID(executor)
	h.sendToConn(ctx, execConn, downHdr, payloadFrom(msg))
}

func (h *Handler) sendListResp(ctx context.Context, hdr core.IHeader, resp listResp) {
	target := uint32(0)
	if hdr != nil {
		target = hdr.SourceID()
	}
	if target == 0 {
		return
	}
	h.sendCtrlToNode(ctx, target, message{Action: actionListResp, Data: mustJSON(resp)})
}

func (h *Handler) sendGetResp(ctx context.Context, hdr core.IHeader, resp getResp) {
	target := uint32(0)
	if hdr != nil {
		target = hdr.SourceID()
	}
	if target == 0 {
		return
	}
	h.sendCtrlToNode(ctx, target, message{Action: actionGetResp, Data: mustJSON(resp)})
}

func (h *Handler) getServer(ctx context.Context) core.IServer {
	if srv := core.ServerFromContext(ctx); srv != nil {
		h.BindServer(srv)
		return srv
	}
	h.mu.Lock()
	srv := h.srv
	h.mu.Unlock()
	return srv
}

func (h *Handler) executeFlow(ctx context.Context, flow setReq, state *runState) {
	order, err := topoOrder(flow.Graph)
	state.mu.Lock()
	if err != nil {
		state.status = "failed"
		state.end = time.Now()
		state.mu.Unlock()
		return
	}
	state.status = "running"
	state.mu.Unlock()

	for _, n := range order {
		if n == nil {
			continue
		}
		id := strings.TrimSpace(n.ID)
		if id == "" {
			continue
		}
		retry := 1
		if n.Retry != nil {
			retry = *n.Retry
		}
		if retry < 0 {
			retry = 0
		}
		timeoutMs := 3000
		if n.TimeoutMs != nil {
			timeoutMs = *n.TimeoutMs
		}
		if timeoutMs <= 0 {
			timeoutMs = 3000
		}

		var lastErr error
		var lastCode int
		for attempt := 0; attempt <= retry; attempt++ {
			nodeCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
			lastCode, lastErr = h.executeNode(nodeCtx, flow, *n)
			cancel()
			if lastErr == nil && lastCode == 1 {
				break
			}
		}
		ns := nodeStatus{ID: id}
		if lastErr == nil && lastCode == 1 {
			ns.Status = "succeeded"
			ns.Code = 1
		} else {
			ns.Status = "failed"
			if lastCode != 0 {
				ns.Code = lastCode
			} else {
				ns.Code = 500
			}
			if lastErr != nil {
				ns.Msg = lastErr.Error()
			}
		}
		state.mu.Lock()
		state.nodes[id] = ns
		state.mu.Unlock()

		if ns.Status != "succeeded" && !n.AllowFail {
			state.mu.Lock()
			state.status = "failed"
			state.end = time.Now()
			state.mu.Unlock()
			return
		}
	}
	state.mu.Lock()
	state.status = "succeeded"
	state.end = time.Now()
	state.mu.Unlock()
}

type localSpec struct {
	Method string          `json:"method"`
	Args   json.RawMessage `json:"args,omitempty"`
}

type execSpec struct {
	Target uint32          `json:"target"`
	Method string          `json:"method"`
	Args   json.RawMessage `json:"args,omitempty"`
}

func (h *Handler) executeNode(ctx context.Context, flow setReq, n node) (code int, err error) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		h.mu.Lock()
		srv = h.srv
		h.mu.Unlock()
	}
	if srv == nil {
		return 500, errors.New("no server")
	}
	local := srv.NodeID()
	kind := strings.ToLower(strings.TrimSpace(n.Kind))
	switch kind {
	case "local":
		var spec localSpec
		if err := json.Unmarshal(n.Spec, &spec); err != nil {
			return 400, errors.New("invalid local spec")
		}
		method := strings.TrimSpace(spec.Method)
		if method == "" {
			return 400, errors.New("local method required")
		}
		fn := h.localMethods[method]
		if fn == nil {
			return 404, fmt.Errorf("local method not found: %s", method)
		}
		_, err := fn(ctx, spec.Args)
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return 408, err
			}
			return 500, err
		}
		return 1, nil
	case "exec":
		var spec execSpec
		if err := json.Unmarshal(n.Spec, &spec); err != nil {
			return 400, errors.New("invalid exec spec")
		}
		target := spec.Target
		method := strings.TrimSpace(spec.Method)
		if target == 0 || method == "" {
			return 400, errors.New("exec target/method required")
		}
		timeoutMs := 3000
		if n.TimeoutMs != nil && *n.TimeoutMs > 0 {
			timeoutMs = *n.TimeoutMs
		}
		reqID := newUUID()
		ch, cancel := broker.SharedExecCallBroker().Register(reqID)
		defer cancel()

		call := protocolexec.CallReq{
			ReqID:        reqID,
			ExecutorNode: local,
			TargetNode:   target,
			Method:       method,
			Args:         spec.Args,
			TimeoutMs:    timeoutMs,
		}
		if err := h.sendExecCall(ctx, srv, call); err != nil {
			return 500, err
		}
		select {
		case resp, ok := <-ch:
			if !ok {
				return 500, errors.New("exec response closed")
			}
			if resp.Code != 1 {
				return resp.Code, errors.New(strings.TrimSpace(resp.Msg))
			}
			return 1, nil
		case <-ctx.Done():
			return 408, errors.New("timeout")
		}
	default:
		return 400, fmt.Errorf("unknown node kind: %s", kind)
	}
}

func (h *Handler) sendExecCall(ctx context.Context, srv core.IServer, call protocolexec.CallReq) error {
	if srv == nil {
		return errors.New("no server")
	}
	local := srv.NodeID()
	if call.ExecutorNode == 0 {
		call.ExecutorNode = local
	}
	env := struct {
		Action string          `json:"action"`
		Data   json.RawMessage `json:"data"`
	}{Action: protocolexec.ActionCall, Data: mustJSON(call)}
	body, _ := json.Marshal(env)
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(protocolexec.SubProtoExec).
		WithSourceID(local)

	cm := srv.ConnManager()
	if cm == nil {
		return errors.New("no conn manager")
	}
	// downstream 免检：若目标在本子树方向（下一跳为子连接），则直接向下发送
	if targetConn, ok := cm.GetByNode(call.TargetNode); ok && targetConn != nil && !isParentConn(targetConn) {
		hdr = hdr.WithTargetID(call.TargetNode)
		return srv.Send(ctx, targetConn.ID(), hdr, body)
	}
	// 否则：按模型向直接父节点发送，使其进入 handler 逐级上报
	parent := findParentConn(cm)
	if parent == nil {
		return errors.New("no parent")
	}
	parentNode := connNodeID(parent)
	if parentNode == 0 {
		return errors.New("invalid parent node")
	}
	hdr = hdr.WithTargetID(parentNode)
	return srv.Send(ctx, parent.ID(), hdr, body)
}

func (h *Handler) sendSetResp(ctx context.Context, hdr core.IHeader, code int, msg string, flowID string) {
	target := uint32(0)
	if hdr != nil {
		target = hdr.SourceID()
	}
	if target == 0 {
		return
	}
	h.sendSetRespToNode(ctx, target, setResp{ReqID: "", Code: code, Msg: msg, FlowID: flowID})
}

func (h *Handler) sendSetRespToNode(ctx context.Context, target uint32, resp setResp) {
	h.sendCtrlToNode(ctx, target, message{Action: actionSetResp, Data: mustJSON(resp)})
}

func (h *Handler) sendRunResp(ctx context.Context, hdr core.IHeader, resp runResp) {
	target := uint32(0)
	if hdr != nil {
		target = hdr.SourceID()
	}
	if target == 0 {
		return
	}
	h.sendCtrlToNode(ctx, target, message{Action: actionRunResp, Data: mustJSON(resp)})
}

func (h *Handler) sendStatusResp(ctx context.Context, hdr core.IHeader, resp statusResp) {
	target := uint32(0)
	if hdr != nil {
		target = hdr.SourceID()
	}
	if target == 0 {
		return
	}
	h.sendCtrlToNode(ctx, target, message{Action: actionStatusResp, Data: mustJSON(resp)})
}

func (h *Handler) sendCtrlToNode(ctx context.Context, target uint32, msg message) {
	if target == 0 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	src := srv.NodeID()
	body, _ := json.Marshal(msg)

	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorOKResp).
		WithSubProto(SubProtoFlow).
		WithSourceID(src).
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

func (h *Handler) forwardDown(ctx context.Context, srv core.IServer, hdr core.IHeader, msg message, target uint32) bool {
	if srv == nil || hdr == nil || target == 0 {
		return false
	}
	body, _ := json.Marshal(msg)
	var next core.IConnection
	if c, ok := srv.ConnManager().GetByNode(target); ok && c != nil {
		next = c
	}
	if next == nil {
		return false
	}
	fwdHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		h.log.Warn("drop flow frame due to hop_limit", "target", target, "source", hdr.SourceID())
		return false
	}
	fwdHdr.WithTargetID(target)
	_ = srv.Send(ctx, next.ID(), fwdHdr, body)
	return true
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

func validateGraph(g graph) error {
	if len(g.Nodes) == 0 {
		return errors.New("empty graph")
	}
	seen := make(map[string]bool)
	for _, n := range g.Nodes {
		id := strings.TrimSpace(n.ID)
		if id == "" {
			return errors.New("node id required")
		}
		if seen[id] {
			return fmt.Errorf("duplicate node id: %s", id)
		}
		seen[id] = true
	}
	// 基础拓扑校验（无环）
	_, err := topoOrder(g)
	return err
}

func topoOrder(g graph) ([]*node, error) {
	nodes := make(map[string]*node, len(g.Nodes))
	inDeg := make(map[string]int, len(g.Nodes))
	next := make(map[string][]string)
	for i := range g.Nodes {
		id := strings.TrimSpace(g.Nodes[i].ID)
		g.Nodes[i].ID = id
		nodes[id] = &g.Nodes[i]
		inDeg[id] = 0
	}
	for _, e := range g.Edges {
		from := strings.TrimSpace(e.From)
		to := strings.TrimSpace(e.To)
		if from == "" || to == "" {
			return nil, errors.New("invalid edge")
		}
		if nodes[from] == nil || nodes[to] == nil {
			return nil, errors.New("edge references unknown node")
		}
		next[from] = append(next[from], to)
		inDeg[to]++
	}
	q := make([]string, 0, len(nodes))
	for id, d := range inDeg {
		if d == 0 {
			q = append(q, id)
		}
	}
	sort.Strings(q)
	out := make([]*node, 0, len(nodes))
	for len(q) > 0 {
		id := q[0]
		q = q[1:]
		out = append(out, nodes[id])
		for _, to := range next[id] {
			inDeg[to]--
			if inDeg[to] == 0 {
				q = append(q, to)
			}
		}
		sort.Strings(q)
	}
	if len(out) != len(nodes) {
		return nil, errors.New("graph has cycle")
	}
	return out, nil
}

func (h *Handler) loadFlowsFromDisk() {
	base := strings.TrimSpace(h.baseDir)
	if base == "" {
		return
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e == nil || e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			continue
		}
		var req setReq
		if err := json.Unmarshal(raw, &req); err != nil {
			continue
		}
		if strings.TrimSpace(req.FlowID) == "" {
			continue
		}
		h.mu.Lock()
		h.flows[req.FlowID] = req
		h.mu.Unlock()
	}
}

func (h *Handler) startSchedulers() {
	h.mu.Lock()
	if h.srv == nil || h.schedStarted {
		h.mu.Unlock()
		return
	}
	h.schedStarted = true
	ids := make([]string, 0, len(h.flows))
	for id := range h.flows {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	for _, id := range ids {
		h.restartScheduler(id)
	}
}

func (h *Handler) restartScheduler(flowID string) {
	flowID = strings.TrimSpace(flowID)
	if flowID == "" {
		return
	}
	h.mu.Lock()
	if old := h.schedulers[flowID]; old != nil {
		close(old.stop)
		delete(h.schedulers, flowID)
	}
	flow, ok := h.flows[flowID]
	if !ok {
		h.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	h.schedulers[flowID] = &flowScheduler{stop: stop}
	h.mu.Unlock()

	every := time.Duration(flow.Trigger.EveryMs) * time.Millisecond
	if every <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				// interval 触发：避免并发重入（同一 flow 同时仅一个 run）
				h.tryStartScheduledRun(flowID)
			}
		}
	}()
}

func (h *Handler) tryStartScheduledRun(flowID string) {
	flowID = strings.TrimSpace(flowID)
	if flowID == "" {
		return
	}
	h.mu.Lock()
	flow, ok := h.flows[flowID]
	if !ok {
		h.mu.Unlock()
		return
	}
	// 判断是否已有 running
	for _, r := range h.runs {
		if r != nil && r.flowID == flowID {
			r.mu.Lock()
			st := r.status
			r.mu.Unlock()
			if st == "running" || st == "queued" {
				h.mu.Unlock()
				return
			}
		}
	}
	runID := newUUID()
	state := &runState{
		flowID: flow.FlowID,
		runID:  runID,
		status: "queued",
		nodes:  make(map[string]nodeStatus),
		start:  time.Now(),
	}
	h.runs[runID] = state
	h.mu.Unlock()
	go h.executeFlow(context.Background(), flow, state)
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	// v4
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:])
}
