package flow

import (
	"bytes"
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
	"strconv"
	"strings"
	"sync"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/eventbus"
	"github.com/yttydcs/myflowhub-core/header"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	"github.com/yttydcs/myflowhub-core/subproto"

	protocolexec "github.com/yttydcs/myflowhub-proto/protocol/exec"
	"github.com/yttydcs/myflowhub-subproto/broker"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
	"github.com/yttydcs/myflowhub-subproto/exec/runtimedeps"
)

type LocalMethodFunc func(ctx context.Context, args json.RawMessage) (json.RawMessage, error)

type HandlerOptions struct {
	RuntimeDeps     runtimedeps.Deps
	Persistence     Persistence
	RunArchiveStore RunArchiveStore
}

type Handler struct {
	subproto.ActionBaseSubProcess
	log *slog.Logger
	cfg core.IConfig
	srv core.IServer

	permCfg *permission.Config

	mu sync.Mutex

	baseDir                 string
	maxRetainedRuns         int
	runArchive              bool
	runArchiveBackend       string
	persistence             Persistence
	explicitStore           bool
	runArchiveStore         RunArchiveStore
	explicitRunArchiveStore bool
	configErr               error
	flows                   map[string]setReq
	runs                    map[string]*runState // run_id -> state
	runOrderByFlow          map[string][]string  // flow_id -> ordered run_ids (oldest -> newest)
	triggerDedup            map[string]map[string]time.Time

	schedulers map[string]*flowScheduler // flow_id -> scheduler

	schedStarted bool
	eventSubOnce sync.Once

	localMethods map[string]LocalMethodFunc
	capRegistry  *execcap.Registry
}

type flowScheduler struct {
	stop chan struct{}
}

type runState struct {
	mu sync.Mutex

	flowID       string
	runID        string
	status       string
	start        time.Time
	end          time.Time
	archivedAtNs int64

	cancel       context.CancelFunc
	cancelReason string
	runtime      runContext
}

type runStartSource string

const (
	triggerTypeInterval   = "interval"
	triggerTypeCron       = "cron"
	triggerTypeEvent      = "event"
	triggerTypeVarChanged = "var_changed"
	triggerTypeSubflow    = "subflow"

	eventModePublish  = "publish"
	eventModeReceived = "received"
	eventModeAny      = "any"

	capabilityProviderFlow = "flow"
	capabilityMethodRun    = "flow::run"

	runCancelMsgCancelled   = "cancelled"
	runCancelMsgFlowDeleted = "interrupted by flow delete"
	runCancelMsgManual      = "cancelled by cancel_run"
	runStartSourceManual    = runStartSource("manual")
	runStartSourceTrigger   = runStartSource("trigger")

	varChangeOpChanged = "changed"
	varChangeOpDeleted = "deleted"
)

type topicPublishEvent struct {
	Topic string          `json:"topic"`
	Name  string          `json:"name"`
	TS    int64           `json:"ts,omitempty"`
	Data  json.RawMessage `json:"payload,omitempty"`
}

type varChangedEvent struct {
	Owner uint32 `json:"owner"`
	Name  string `json:"name"`
}

func NewHandler(log *slog.Logger) *Handler {
	return NewHandlerWithOptions(nil, HandlerOptions{}, log)
}

func NewHandlerWithConfig(cfg core.IConfig, log *slog.Logger) *Handler {
	return NewHandlerWithOptions(cfg, HandlerOptions{}, log)
}

func NewHandlerWithDeps(cfg core.IConfig, deps runtimedeps.Deps, log *slog.Logger) *Handler {
	return NewHandlerWithOptions(cfg, HandlerOptions{RuntimeDeps: deps}, log)
}

func NewHandlerWithOptions(cfg core.IConfig, opts HandlerOptions, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	deps := runtimedeps.Resolve(cfg, opts.RuntimeDeps)
	loadedCfg := loadConfig(cfg)
	h := &Handler{
		log:                     log,
		cfg:                     cfg,
		baseDir:                 loadedCfg.BaseDir,
		maxRetainedRuns:         loadedCfg.MaxRetainedRuns,
		runArchive:              loadedCfg.RunArchive,
		runArchiveBackend:       loadedCfg.RunArchiveBackend,
		persistence:             opts.Persistence,
		explicitStore:           opts.Persistence != nil,
		runArchiveStore:         opts.RunArchiveStore,
		explicitRunArchiveStore: opts.RunArchiveStore != nil,
		configErr:               loadedCfg.ConfigErr,
		flows:                   make(map[string]setReq),
		runs:                    make(map[string]*runState),
		runOrderByFlow:          make(map[string][]string),
		triggerDedup:            make(map[string]map[string]time.Time),
		schedulers:              make(map[string]*flowScheduler),
		localMethods:            make(map[string]LocalMethodFunc),
		capRegistry:             deps.CapRegistry,
	}
	h.permCfg = deps.PermConfig
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
	h.registerCapabilities()
	return h
}

// BindServer 用于让 handler 在非 OnReceive 触发的场景（例如 interval）也能获取发送能力。
func (h *Handler) BindServer(srv core.IServer) {
	h.mu.Lock()
	h.srv = srv
	h.mu.Unlock()
	h.startSchedulers()
	h.ensureTriggerSubscriptions(srv)
}

// AcceptCmd 声明 Cmd 帧在 target!=local 时也需要本地处理一次（用于逐级授权/裁决）。
func (h *Handler) AcceptCmd() bool { return true }

func (h *Handler) SubProto() uint8 { return SubProtoFlow }

func (h *Handler) Init() bool {
	cfg := loadConfig(h.cfg)
	h.baseDir = cfg.BaseDir
	h.maxRetainedRuns = cfg.MaxRetainedRuns
	h.runArchive = cfg.RunArchive
	h.runArchiveBackend = cfg.RunArchiveBackend
	h.configErr = cfg.ConfigErr
	if h.configErr != nil {
		h.log.Warn("flow config invalid", "err", h.configErr)
		return false
	}
	if h.runArchiveBackend == runArchiveBackendPG && !h.explicitRunArchiveStore {
		h.log.Warn("flow run archive backend requires injected store", "backend", h.runArchiveBackend)
		return false
	}
	if err := h.loadFlowsFromDisk(); err != nil {
		h.log.Warn("flow load persisted state failed", "err", err)
		return false
	}
	if err := h.loadArchivedRuns(); err != nil {
		h.log.Warn("flow load archived runs failed", "err", err)
	}
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

func (h *Handler) registerCapabilities() {
	if h.capRegistry == nil {
		return
	}
	err := h.capRegistry.Register(execcap.Descriptor{
		Provider: capabilityProviderFlow,
		Method:   capabilityMethodRun,
		Permissions: []string{
			permFlowRun,
		},
		Tags: map[string]string{
			"subproto": "flow",
		},
		InputSchema:  json.RawMessage(`{"type":"object","required":["flow_id"],"properties":{"flow_id":{"type":"string","minLength":1}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["flow_id","run_id"],"properties":{"flow_id":{"type":"string"},"run_id":{"type":"string"}}}`),
	}, execcap.InvokeFunc(h.invokeCapabilityRun))
	if err != nil {
		h.log.Warn("flow register capability failed", "method", capabilityMethodRun, "err", err)
	}
}

func (h *Handler) invokeCapabilityRun(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		FlowID string `json:"flow_id"`
	}
	if err := json.Unmarshal(args, &req); err != nil {
		return nil, errors.New("invalid flow::run args")
	}
	validFlowID, err := validateFlowID(req.FlowID)
	if err != nil {
		return nil, err
	}
	req.FlowID = validFlowID

	h.mu.Lock()
	flow, ok := h.flows[req.FlowID]
	h.mu.Unlock()
	if !ok || strings.TrimSpace(flow.FlowID) == "" {
		return nil, errors.New("flow not found")
	}

	runID := h.enqueueRun(ctx, flow)
	return mustJSON(map[string]string{
		"flow_id": flow.FlowID,
		"run_id":  runID,
	}), nil
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

func triggerType(t trigger) string {
	return strings.ToLower(strings.TrimSpace(t.Type))
}

func normalizeTrigger(t *trigger) {
	if t == nil {
		return
	}
	t.Type = triggerType(*t)
	t.Cron = strings.TrimSpace(t.Cron)
	t.EventMode = strings.ToLower(strings.TrimSpace(t.EventMode))
	if t.Type == triggerTypeEvent && t.EventMode == "" {
		t.EventMode = eventModePublish
	}
	t.EventName = strings.TrimSpace(t.EventName)
	t.EventTopic = strings.TrimSpace(t.EventTopic)
	t.VarName = strings.TrimSpace(t.VarName)
}

func normalizeEventMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", eventModePublish:
		return eventModePublish
	case eventModeReceived:
		return eventModeReceived
	case eventModeAny:
		return eventModeAny
	default:
		return ""
	}
}

func validateTrigger(t trigger) error {
	dedupWindowMs := 0
	if t.DedupWindowMs != nil {
		if *t.DedupWindowMs < 0 {
			return errors.New("trigger dedup_window_ms must be >= 0")
		}
		dedupWindowMs = *t.DedupWindowMs
	}
	switch triggerType(t) {
	case triggerTypeInterval:
		if t.EveryMs == 0 {
			return errors.New("trigger interval every_ms required")
		}
		if dedupWindowMs > 0 {
			return errors.New("trigger interval does not support dedup_window_ms")
		}
		return nil
	case triggerTypeCron:
		if strings.TrimSpace(t.Cron) == "" {
			return errors.New("trigger cron required")
		}
		if _, err := parseCronExpr(t.Cron); err != nil {
			return fmt.Errorf("trigger cron invalid: %w", err)
		}
		if dedupWindowMs > 0 {
			return errors.New("trigger cron does not support dedup_window_ms")
		}
		return nil
	case triggerTypeEvent:
		if normalizeEventMode(t.EventMode) == "" {
			return errors.New("trigger event_mode unsupported")
		}
		if strings.TrimSpace(t.EventName) == "" && strings.TrimSpace(t.EventTopic) == "" {
			return errors.New("trigger event requires event_name or event_topic")
		}
		return nil
	case triggerTypeVarChanged:
		return nil
	default:
		return errors.New("trigger type unsupported")
	}
}

func validateFlowRunConfig(req setReq) error {
	if req.MaxActiveRuns != nil && *req.MaxActiveRuns < 0 {
		return errors.New("max_active_runs must be >= 0")
	}
	return nil
}

func decodeEventData(data any, out any) bool {
	if data == nil || out == nil {
		return false
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return false
	}
	return true
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
	validFlowID, err := validateFlowID(req.FlowID)
	normalizeTrigger(&req.Trigger)
	if req.ReqID == "" {
		h.sendSetResp(ctx, hdr, 400, "invalid set", "")
		return
	}
	if err != nil {
		h.sendSetResp(ctx, hdr, 400, err.Error(), "")
		return
	}
	req.FlowID = validFlowID
	if err := validateTrigger(req.Trigger); err != nil {
		h.sendSetResp(ctx, hdr, 400, err.Error(), req.FlowID)
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
			h.applySetLocal(ctx, hdr, req, origin)
			return
		}
		if !h.forwardDown(ctx, srv, hdr, message{Action: actionSet, Data: mustJSON(req)}, executor) {
			h.sendSetRespToNode(ctx, hdr, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "forward failed", FlowID: req.FlowID})
		}
		return
	}

	// executor 为本节点：本节点即 LCA+executor，执行权限判定并落盘生效。
	if executor == local {
		if !h.hasPermission(origin, permFlowSet) {
			h.sendSetRespToNode(ctx, hdr, origin, setResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", FlowID: req.FlowID})
			return
		}
		h.applySetLocal(ctx, hdr, req, origin)
		return
	}

	// executor 在本子树内？
	execConn, ok := cm.GetByNode(executor)
	if !ok || execConn == nil || isParentConn(execConn) {
		// 不在本子树：上送父节点（若无父则 not found）
		parent := findParentConn(cm)
		if parent == nil {
			h.sendSetRespToNode(ctx, hdr, origin, setResp{ReqID: req.ReqID, Code: 404, Msg: "not found", FlowID: req.FlowID})
			return
		}
		parentNode := connNodeID(parent)
		if parentNode == 0 {
			h.sendSetRespToNode(ctx, hdr, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "invalid parent route", FlowID: req.FlowID})
			return
		}
		// 上送必须让父节点进入 handler：TargetID=父节点自身
		upHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			h.sendSetRespToNode(ctx, hdr, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "hop limit exceeded", FlowID: req.FlowID})
			return
		}
		upHdr.WithTargetID(parentNode)
		_ = h.sendToConn(ctx, parent, upHdr, payloadFrom(message{Action: actionSet, Data: mustJSON(req)}))
		return
	}

	// 判定 origin 与 executor 是否处于同一 child 分支；若是则下送该 child 继续裁决（本节点非 LCA）。
	originConn, ok2 := cm.GetByNode(origin)
	if ok2 && originConn != nil && originConn.ID() == execConn.ID() {
		nextNode := connNodeID(originConn)
		if nextNode == 0 {
			h.sendSetRespToNode(ctx, hdr, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "invalid route", FlowID: req.FlowID})
			return
		}
		childHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			h.sendSetRespToNode(ctx, hdr, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "hop limit exceeded", FlowID: req.FlowID})
			return
		}
		childHdr.WithTargetID(nextNode)
		_ = h.sendToConn(ctx, originConn, childHdr, payloadFrom(message{Action: actionSet, Data: mustJSON(req)}))
		return
	}

	// 本节点为 LCA：判定权限后，向下转发到 executor（转发即同意）。
	if !h.hasPermission(origin, permFlowSet) {
		h.sendSetRespToNode(ctx, hdr, origin, setResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", FlowID: req.FlowID})
		return
	}
	downHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		h.sendSetRespToNode(ctx, hdr, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "hop limit exceeded", FlowID: req.FlowID})
		return
	}
	downHdr.WithTargetID(executor)
	_ = h.sendToConn(ctx, execConn, downHdr, payloadFrom(message{Action: actionSet, Data: mustJSON(req)}))
}

func (h *Handler) applySetLocal(ctx context.Context, reqHdr core.IHeader, req setReq, origin uint32) {
	if err := validateFlowRunConfig(req); err != nil {
		h.sendSetRespToNode(ctx, reqHdr, origin, setResp{ReqID: req.ReqID, Code: 400, Msg: err.Error(), FlowID: req.FlowID})
		return
	}
	if err := validateGraphForFlow(req.FlowID, req.Graph); err != nil {
		h.sendSetRespToNode(ctx, reqHdr, origin, setResp{ReqID: req.ReqID, Code: 400, Msg: err.Error(), FlowID: req.FlowID})
		return
	}
	h.mu.Lock()
	store := h.currentPersistenceLocked()
	h.mu.Unlock()
	if err := store.Save(ctx, FlowDocument(req)); err != nil {
		h.sendSetRespToNode(ctx, reqHdr, origin, setResp{ReqID: req.ReqID, Code: 500, Msg: "write failed", FlowID: req.FlowID})
		return
	}
	h.mu.Lock()
	h.flows[req.FlowID] = req
	delete(h.triggerDedup, req.FlowID)
	h.mu.Unlock()
	h.restartScheduler(req.FlowID)
	h.sendSetRespToNode(ctx, reqHdr, origin, setResp{ReqID: req.ReqID, Code: 1, Msg: "ok", FlowID: req.FlowID})
}

func (h *Handler) handleDelete(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req deleteReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendDeleteResp(ctx, hdr, deleteResp{ReqID: req.ReqID, Code: 400, Msg: "invalid delete"})
		return
	}
	req.ReqID = strings.TrimSpace(req.ReqID)
	validFlowID, err := validateFlowID(req.FlowID)
	if req.ReqID == "" {
		h.sendDeleteResp(ctx, hdr, deleteResp{ReqID: req.ReqID, Code: 400, Msg: "invalid delete"})
		return
	}
	if err != nil {
		h.sendDeleteResp(ctx, hdr, deleteResp{ReqID: req.ReqID, Code: 400, Msg: err.Error()})
		return
	}
	req.FlowID = validFlowID

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

	// 来自父节点：下游无条件信任父节点，视为已授权，直接将请求转交到 executor（或本地删除）。
	if isParentConn(conn) {
		if executor == local {
			h.applyDeleteLocal(ctx, hdr, req, origin)
			return
		}
		if !h.forwardDown(ctx, srv, hdr, message{Action: actionDelete, Data: mustJSON(req)}, executor) {
			h.sendDeleteRespToNode(ctx, hdr, origin, deleteResp{ReqID: req.ReqID, Code: 500, Msg: "forward failed", FlowID: req.FlowID})
		}
		return
	}

	// executor 为本节点：本节点即 LCA+executor，执行权限判定并本地删除。
	if executor == local {
		if !h.hasPermission(origin, permFlowDelete) {
			h.sendDeleteRespToNode(ctx, hdr, origin, deleteResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", FlowID: req.FlowID})
			return
		}
		h.applyDeleteLocal(ctx, hdr, req, origin)
		return
	}

	// executor 在本子树内？
	execConn, ok := cm.GetByNode(executor)
	if !ok || execConn == nil || isParentConn(execConn) {
		// 不在本子树：上送父节点（若无父则 not found）
		parent := findParentConn(cm)
		if parent == nil {
			h.sendDeleteRespToNode(ctx, hdr, origin, deleteResp{ReqID: req.ReqID, Code: 404, Msg: "not found", FlowID: req.FlowID})
			return
		}
		parentNode := connNodeID(parent)
		if parentNode == 0 {
			h.sendDeleteRespToNode(ctx, hdr, origin, deleteResp{ReqID: req.ReqID, Code: 500, Msg: "invalid parent route", FlowID: req.FlowID})
			return
		}
		// 上送必须让父节点进入 handler：TargetID=父节点自身
		upHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			h.sendDeleteRespToNode(ctx, hdr, origin, deleteResp{ReqID: req.ReqID, Code: 500, Msg: "hop limit exceeded", FlowID: req.FlowID})
			return
		}
		upHdr.WithTargetID(parentNode)
		_ = h.sendToConn(ctx, parent, upHdr, payloadFrom(message{Action: actionDelete, Data: mustJSON(req)}))
		return
	}

	// 判定 origin 与 executor 是否处于同一 child 分支；若是则下送该 child 继续裁决（本节点非 LCA）。
	originConn, ok2 := cm.GetByNode(origin)
	if ok2 && originConn != nil && originConn.ID() == execConn.ID() {
		nextNode := connNodeID(originConn)
		if nextNode == 0 {
			h.sendDeleteRespToNode(ctx, hdr, origin, deleteResp{ReqID: req.ReqID, Code: 500, Msg: "invalid route", FlowID: req.FlowID})
			return
		}
		childHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			h.sendDeleteRespToNode(ctx, hdr, origin, deleteResp{ReqID: req.ReqID, Code: 500, Msg: "hop limit exceeded", FlowID: req.FlowID})
			return
		}
		childHdr.WithTargetID(nextNode)
		_ = h.sendToConn(ctx, originConn, childHdr, payloadFrom(message{Action: actionDelete, Data: mustJSON(req)}))
		return
	}

	// 本节点为 LCA：判定权限后，向下转发到 executor（转发即同意）。
	if !h.hasPermission(origin, permFlowDelete) {
		h.sendDeleteRespToNode(ctx, hdr, origin, deleteResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", FlowID: req.FlowID})
		return
	}
	downHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		h.sendDeleteRespToNode(ctx, hdr, origin, deleteResp{ReqID: req.ReqID, Code: 500, Msg: "hop limit exceeded", FlowID: req.FlowID})
		return
	}
	downHdr.WithTargetID(executor)
	_ = h.sendToConn(ctx, execConn, downHdr, payloadFrom(message{Action: actionDelete, Data: mustJSON(req)}))
}

func (h *Handler) applyDeleteLocal(ctx context.Context, reqHdr core.IHeader, req deleteReq, origin uint32) {
	flowID, err := validateFlowID(req.FlowID)
	if err != nil {
		h.sendDeleteRespToNode(ctx, reqHdr, origin, deleteResp{ReqID: req.ReqID, Code: 400, Msg: err.Error()})
		return
	}

	h.mu.Lock()
	if _, ok := h.flows[flowID]; !ok {
		h.mu.Unlock()
		h.sendDeleteRespToNode(ctx, reqHdr, origin, deleteResp{ReqID: req.ReqID, Code: 404, Msg: "not found", FlowID: flowID})
		return
	}
	store := h.currentPersistenceLocked()
	h.mu.Unlock()
	if err := store.Delete(ctx, flowID); err != nil {
		h.log.Warn("flow delete persistence failed", "flow_id", flowID, "err", err)
		h.sendDeleteRespToNode(ctx, reqHdr, origin, deleteResp{ReqID: req.ReqID, Code: 500, Msg: "delete file failed", FlowID: flowID})
		return
	}
	h.mu.Lock()
	delete(h.flows, flowID)
	delete(h.triggerDedup, flowID)
	if old := h.schedulers[flowID]; old != nil {
		close(old.stop)
		delete(h.schedulers, flowID)
	}
	h.cancelRunsLocked(flowID, runCancelMsgFlowDeleted)
	h.mu.Unlock()
	h.sendDeleteRespToNode(ctx, reqHdr, origin, deleteResp{ReqID: req.ReqID, Code: 1, Msg: "ok", FlowID: flowID})
}

func (h *Handler) handleRun(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req runReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendRunResp(ctx, hdr, runResp{ReqID: req.ReqID, Code: 400, Msg: "invalid run"})
		return
	}
	req.ReqID = strings.TrimSpace(req.ReqID)
	validFlowID, err := validateFlowID(req.FlowID)
	if req.ReqID == "" {
		h.sendRunResp(ctx, hdr, runResp{ReqID: req.ReqID, Code: 400, Msg: "invalid run"})
		return
	}
	if err != nil {
		h.sendRunResp(ctx, hdr, runResp{ReqID: req.ReqID, Code: 400, Msg: err.Error()})
		return
	}
	req.FlowID = validFlowID
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
	_, code, msgText := h.forwardToExecutorWithPerm(ctx, srv, conn, hdr, executor, origin, permFlowRun, message{Action: actionRun, Data: mustJSON(req)}, func() {
		h.runLocal(ctx, hdr, req)
	})
	if code != 0 {
		h.sendRunResp(ctx, hdr, runResp{ReqID: req.ReqID, Code: code, Msg: msgText, FlowID: req.FlowID})
	}
}

func (h *Handler) runLocal(ctx context.Context, hdr core.IHeader, req runReq) {
	h.mu.Lock()
	flow, ok := h.flows[req.FlowID]
	if !ok || strings.TrimSpace(flow.FlowID) == "" {
		h.mu.Unlock()
		h.sendRunResp(ctx, hdr, runResp{ReqID: req.ReqID, Code: 404, Msg: "not found", FlowID: req.FlowID})
		return
	}
	state, runCtx, allowed := h.prepareQueuedRunLocked(flow, nil, runStartSourceManual)
	h.mu.Unlock()
	if !allowed {
		h.sendRunResp(ctx, hdr, runResp{ReqID: req.ReqID, Code: 409, Msg: "active run limit reached", FlowID: req.FlowID})
		return
	}
	runID := state.runID
	go h.executeFlow(runCtx, flow, state)

	h.sendRunResp(ctx, hdr, runResp{ReqID: req.ReqID, Code: 1, Msg: "ok", FlowID: req.FlowID, RunID: runID})
}

func (h *Handler) handleCancelRun(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req cancelRunReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendCancelRunResp(ctx, hdr, cancelRunResp{ReqID: req.ReqID, Code: 400, Msg: "invalid cancel_run"})
		return
	}
	req.ReqID = strings.TrimSpace(req.ReqID)
	validFlowID, err := validateFlowID(req.FlowID)
	validRunID, runErr := validateRunID(req.RunID)
	if req.ReqID == "" {
		h.sendCancelRunResp(ctx, hdr, cancelRunResp{ReqID: req.ReqID, Code: 400, Msg: "invalid cancel_run"})
		return
	}
	if err != nil {
		h.sendCancelRunResp(ctx, hdr, cancelRunResp{ReqID: req.ReqID, Code: 400, Msg: err.Error()})
		return
	}
	if runErr != nil {
		h.sendCancelRunResp(ctx, hdr, cancelRunResp{ReqID: req.ReqID, Code: 400, Msg: runErr.Error(), FlowID: validFlowID})
		return
	}
	req.FlowID = validFlowID
	req.RunID = validRunID
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
	_, code, msgText := h.forwardToExecutorWithPerm(ctx, srv, conn, hdr, executor, origin, permFlowRun, message{Action: actionCancelRun, Data: mustJSON(req)}, func() {
		h.cancelRunLocal(ctx, hdr, req)
	})
	if code != 0 {
		h.sendCancelRunResp(ctx, hdr, cancelRunResp{
			ReqID:        req.ReqID,
			Code:         code,
			Msg:          msgText,
			ExecutorNode: executor,
			FlowID:       req.FlowID,
			RunID:        req.RunID,
		})
	}
}

func (h *Handler) cancelRunLocal(ctx context.Context, hdr core.IHeader, req cancelRunReq) {
	h.mu.Lock()
	state := h.runs[req.RunID]
	h.mu.Unlock()
	if state == nil {
		h.sendCancelRunResp(ctx, hdr, cancelRunResp{
			ReqID:        req.ReqID,
			Code:         404,
			Msg:          "not found",
			ExecutorNode: req.ExecutorNode,
			FlowID:       req.FlowID,
			RunID:        req.RunID,
		})
		return
	}

	state.mu.Lock()
	if strings.TrimSpace(state.flowID) != req.FlowID {
		state.mu.Unlock()
		h.sendCancelRunResp(ctx, hdr, cancelRunResp{
			ReqID:        req.ReqID,
			Code:         404,
			Msg:          "not found",
			ExecutorNode: req.ExecutorNode,
			FlowID:       req.FlowID,
			RunID:        req.RunID,
		})
		return
	}
	state.mu.Unlock()

	status, ok := cancelRunState(state, runCancelMsgManual)
	if !ok {
		state.mu.Lock()
		status = state.status
		state.mu.Unlock()
		h.sendCancelRunResp(ctx, hdr, cancelRunResp{
			ReqID:        req.ReqID,
			Code:         409,
			Msg:          "run already terminal",
			ExecutorNode: req.ExecutorNode,
			FlowID:       req.FlowID,
			RunID:        req.RunID,
			Status:       status,
		})
		return
	}

	h.sendCancelRunResp(ctx, hdr, cancelRunResp{
		ReqID:        req.ReqID,
		Code:         1,
		Msg:          runCancelMsgManual,
		ExecutorNode: req.ExecutorNode,
		FlowID:       req.FlowID,
		RunID:        req.RunID,
		Status:       status,
	})
}

func (h *Handler) handleStatus(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req statusReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendStatusResp(ctx, hdr, statusResp{ReqID: req.ReqID, Code: 400, Msg: "invalid status"})
		return
	}
	req.ReqID = strings.TrimSpace(req.ReqID)
	validFlowID, err := validateFlowID(req.FlowID)
	req.RunID = strings.TrimSpace(req.RunID)
	if req.ReqID == "" {
		h.sendStatusResp(ctx, hdr, statusResp{ReqID: req.ReqID, Code: 400, Msg: "invalid status"})
		return
	}
	if err != nil {
		h.sendStatusResp(ctx, hdr, statusResp{ReqID: req.ReqID, Code: 400, Msg: err.Error()})
		return
	}
	req.FlowID = validFlowID
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
	_, code, msgText := h.forwardToExecutorWithPerm(ctx, srv, conn, hdr, executor, origin, permFlowRead, message{Action: actionStatus, Data: mustJSON(req)}, func() {
		h.handleStatusLocal(ctx, hdr, req)
	})
	if code != 0 {
		h.sendStatusResp(ctx, hdr, statusResp{
			ReqID:        req.ReqID,
			Code:         code,
			Msg:          msgText,
			ExecutorNode: executor,
			FlowID:       req.FlowID,
			RunID:        req.RunID,
		})
	}
}

func (h *Handler) handleStatusLocal(ctx context.Context, hdr core.IHeader, req statusReq) {
	var state *runState
	h.mu.Lock()
	if req.RunID != "" {
		state = h.runs[req.RunID]
	} else {
		state = h.latestRunStateLocked(req.FlowID)
	}
	h.mu.Unlock()
	if state == nil {
		h.sendStatusResp(ctx, hdr, statusResp{ReqID: req.ReqID, Code: 404, Msg: "not found", FlowID: req.FlowID})
		return
	}
	state.mu.Lock()
	if strings.TrimSpace(state.flowID) != req.FlowID {
		state.mu.Unlock()
		h.sendStatusResp(ctx, hdr, statusResp{ReqID: req.ReqID, Code: 404, Msg: "not found", FlowID: req.FlowID, RunID: req.RunID})
		return
	}
	nodes := state.snapshotNodeStatusesLocked()
	resp := statusResp{
		ReqID:        req.ReqID,
		Code:         1,
		Msg:          "ok",
		ExecutorNode: req.ExecutorNode,
		FlowID:       state.flowID,
		RunID:        state.runID,
		Status:       state.status,
		Nodes:        nodes,
	}
	if state.status == "cancelled" && strings.TrimSpace(state.cancelReason) != "" {
		resp.Msg = state.cancelReason
	}
	state.mu.Unlock()
	h.sendStatusResp(ctx, hdr, resp)
}

func (h *Handler) handleDetail(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req detailReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendDetailResp(ctx, hdr, detailResp{ReqID: req.ReqID, Code: 400, Msg: "invalid detail"})
		return
	}
	req.ReqID = strings.TrimSpace(req.ReqID)
	validFlowID, err := validateFlowID(req.FlowID)
	req.RunID = strings.TrimSpace(req.RunID)
	req.NodeID = strings.TrimSpace(req.NodeID)
	req.Path = strings.TrimSpace(req.Path)
	if req.ReqID == "" {
		h.sendDetailResp(ctx, hdr, detailResp{ReqID: req.ReqID, Code: 400, Msg: "invalid detail"})
		return
	}
	if err != nil {
		h.sendDetailResp(ctx, hdr, detailResp{ReqID: req.ReqID, Code: 400, Msg: err.Error()})
		return
	}
	if req.NodeID == "" {
		h.sendDetailResp(ctx, hdr, detailResp{ReqID: req.ReqID, Code: 400, Msg: "node_id required"})
		return
	}
	if _, err := parseJSONPointer(req.Path); err != nil {
		h.sendDetailResp(ctx, hdr, detailResp{ReqID: req.ReqID, Code: 400, Msg: "invalid detail path"})
		return
	}
	req.FlowID = validFlowID
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
	_, code, msgText := h.forwardToExecutorWithPerm(ctx, srv, conn, hdr, executor, origin, permFlowRead, message{Action: actionDetail, Data: mustJSON(req)}, func() {
		h.handleDetailLocal(ctx, hdr, req)
	})
	if code != 0 {
		h.sendDetailResp(ctx, hdr, detailResp{
			ReqID:        req.ReqID,
			Code:         code,
			Msg:          msgText,
			ExecutorNode: executor,
			FlowID:       req.FlowID,
			RunID:        req.RunID,
			Path:         req.Path,
		})
	}
}

func (h *Handler) handleDetailLocal(ctx context.Context, hdr core.IHeader, req detailReq) {
	var state *runState
	h.mu.Lock()
	if req.RunID != "" {
		state = h.runs[req.RunID]
	} else {
		state = h.latestRunStateLocked(req.FlowID)
	}
	h.mu.Unlock()
	if state == nil {
		h.sendDetailResp(ctx, hdr, detailResp{ReqID: req.ReqID, Code: 404, Msg: "not found", ExecutorNode: req.ExecutorNode, FlowID: req.FlowID, RunID: req.RunID, Path: req.Path})
		return
	}

	state.mu.Lock()
	if strings.TrimSpace(state.flowID) != req.FlowID {
		state.mu.Unlock()
		h.sendDetailResp(ctx, hdr, detailResp{ReqID: req.ReqID, Code: 404, Msg: "not found", ExecutorNode: req.ExecutorNode, FlowID: req.FlowID, RunID: req.RunID, Path: req.Path})
		return
	}
	nodeData, ok := state.runtime.Nodes[req.NodeID]
	resp := detailResp{
		ReqID:        req.ReqID,
		Code:         1,
		Msg:          "ok",
		ExecutorNode: req.ExecutorNode,
		FlowID:       state.flowID,
		RunID:        state.runID,
		Path:         req.Path,
	}
	if state.status == "cancelled" && strings.TrimSpace(state.cancelReason) != "" {
		resp.Msg = state.cancelReason
	}
	if ok {
		resp.Node = &nodeStatus{
			ID:     req.NodeID,
			Status: nodeData.Status,
			Code:   nodeData.Code,
			Msg:    nodeData.Msg,
		}
	}
	raw := cloneRawJSON(nodeData.Result)
	state.mu.Unlock()
	if !ok {
		resp.Code = 404
		resp.Msg = "not found"
		h.sendDetailResp(ctx, hdr, resp)
		return
	}
	if req.Path == "" {
		resp.Result = raw
		h.sendDetailResp(ctx, hdr, resp)
		return
	}
	if len(raw) == 0 {
		resp.Code = 404
		resp.Msg = "not found"
		h.sendDetailResp(ctx, hdr, resp)
		return
	}
	value, found, err := readJSONSourceValue(raw, req.Path)
	if err != nil {
		h.sendDetailResp(ctx, hdr, detailResp{
			ReqID:        req.ReqID,
			Code:         500,
			Msg:          "invalid node result json",
			ExecutorNode: req.ExecutorNode,
			FlowID:       resp.FlowID,
			RunID:        resp.RunID,
			Path:         req.Path,
			Node:         resp.Node,
		})
		return
	}
	if !found {
		resp.Code = 404
		resp.Msg = "not found"
		h.sendDetailResp(ctx, hdr, resp)
		return
	}
	result, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		h.sendDetailResp(ctx, hdr, detailResp{
			ReqID:        req.ReqID,
			Code:         500,
			Msg:          "detail result marshal failed",
			ExecutorNode: req.ExecutorNode,
			FlowID:       resp.FlowID,
			RunID:        resp.RunID,
			Path:         req.Path,
			Node:         resp.Node,
		})
		return
	}
	resp.Result = result
	h.sendDetailResp(ctx, hdr, resp)
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
	_, code, msgText := h.forwardToExecutorWithPerm(ctx, srv, conn, hdr, executor, origin, permFlowRead, message{Action: actionList, Data: mustJSON(req)}, func() {
		h.handleListLocal(ctx, hdr, req)
	})
	if code != 0 {
		h.sendListResp(ctx, hdr, listResp{ReqID: req.ReqID, Code: code, Msg: msgText, ExecutorNode: executor})
	}
}

func (h *Handler) handleListLocal(ctx context.Context, hdr core.IHeader, req listReq) {
	h.mu.Lock()
	flows := make([]flowSummary, 0, len(h.flows))
	for _, f := range h.flows {
		id := strings.TrimSpace(f.FlowID)
		if id == "" {
			continue
		}
		sum := flowSummary{FlowID: id, Name: strings.TrimSpace(f.Name), EveryMs: f.Trigger.EveryMs}
		latest := h.latestRunStateLocked(id)
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
	h.sendListResp(ctx, hdr, listResp{ReqID: req.ReqID, Code: 1, Msg: "ok", ExecutorNode: req.ExecutorNode, Flows: flows})
}

func (h *Handler) handleListRuns(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req listRunsReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendListRunsResp(ctx, hdr, listRunsResp{ReqID: req.ReqID, Code: 400, Msg: "invalid list_runs"})
		return
	}
	req.ReqID = strings.TrimSpace(req.ReqID)
	validFlowID, err := validateFlowID(req.FlowID)
	if req.ReqID == "" {
		h.sendListRunsResp(ctx, hdr, listRunsResp{ReqID: req.ReqID, Code: 400, Msg: "invalid list_runs"})
		return
	}
	if err != nil {
		h.sendListRunsResp(ctx, hdr, listRunsResp{ReqID: req.ReqID, Code: 400, Msg: err.Error()})
		return
	}
	req.FlowID = validFlowID
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
	_, code, msgText := h.forwardToExecutorWithPerm(ctx, srv, conn, hdr, executor, origin, permFlowRead, message{Action: actionListRuns, Data: mustJSON(req)}, func() {
		h.handleListRunsLocal(ctx, hdr, req)
	})
	if code != 0 {
		h.sendListRunsResp(ctx, hdr, listRunsResp{ReqID: req.ReqID, Code: code, Msg: msgText, ExecutorNode: executor, FlowID: req.FlowID})
	}
}

func (h *Handler) handleListRunsLocal(ctx context.Context, hdr core.IHeader, req listRunsReq) {
	h.mu.Lock()
	ids := append([]string(nil), h.runOrderByFlow[req.FlowID]...)
	_, flowExists := h.flows[req.FlowID]
	h.mu.Unlock()
	if len(ids) == 0 && !flowExists {
		h.sendListRunsResp(ctx, hdr, listRunsResp{ReqID: req.ReqID, Code: 404, Msg: "not found", ExecutorNode: req.ExecutorNode, FlowID: req.FlowID})
		return
	}

	limit := int(req.Limit)
	runs := make([]runSummary, 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		runID := ids[i]
		h.mu.Lock()
		state := h.runs[runID]
		h.mu.Unlock()
		if state == nil {
			continue
		}
		state.mu.Lock()
		if strings.TrimSpace(state.flowID) != req.FlowID {
			state.mu.Unlock()
			continue
		}
		runs = append(runs, state.snapshotRunSummaryLocked())
		state.mu.Unlock()
		if limit > 0 && len(runs) >= limit {
			break
		}
	}
	h.sendListRunsResp(ctx, hdr, listRunsResp{ReqID: req.ReqID, Code: 1, Msg: "ok", ExecutorNode: req.ExecutorNode, FlowID: req.FlowID, Runs: runs})
}

func (h *Handler) handleGet(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req getReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendGetResp(ctx, hdr, getResp{ReqID: req.ReqID, Code: 400, Msg: "invalid get"})
		return
	}
	req.ReqID = strings.TrimSpace(req.ReqID)
	validFlowID, err := validateFlowID(req.FlowID)
	if req.ReqID == "" {
		h.sendGetResp(ctx, hdr, getResp{ReqID: req.ReqID, Code: 400, Msg: "invalid get"})
		return
	}
	if err != nil {
		h.sendGetResp(ctx, hdr, getResp{ReqID: req.ReqID, Code: 400, Msg: err.Error()})
		return
	}
	req.FlowID = validFlowID
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
	_, code, msgText := h.forwardToExecutorWithPerm(ctx, srv, conn, hdr, executor, origin, permFlowRead, message{Action: actionGet, Data: mustJSON(req)}, func() {
		h.handleGetLocal(ctx, hdr, req)
	})
	if code != 0 {
		h.sendGetResp(ctx, hdr, getResp{ReqID: req.ReqID, Code: code, Msg: msgText, ExecutorNode: executor, FlowID: req.FlowID})
	}
}

func (h *Handler) handleGetLocal(ctx context.Context, hdr core.IHeader, req getReq) {
	h.mu.Lock()
	f, ok := h.flows[req.FlowID]
	h.mu.Unlock()
	if !ok || strings.TrimSpace(f.FlowID) == "" {
		h.sendGetResp(ctx, hdr, getResp{ReqID: req.ReqID, Code: 404, Msg: "not found", ExecutorNode: req.ExecutorNode, FlowID: req.FlowID})
		return
	}
	h.sendGetResp(ctx, hdr, getResp{
		ReqID:         req.ReqID,
		Code:          1,
		Msg:           "ok",
		ExecutorNode:  req.ExecutorNode,
		FlowID:        f.FlowID,
		Name:          f.Name,
		MaxActiveRuns: f.MaxActiveRuns,
		Trigger:       f.Trigger,
		Graph:         f.Graph,
	})
}

func (h *Handler) forwardToExecutorWithPerm(ctx context.Context, srv core.IServer, conn core.IConnection, hdr core.IHeader, executor, origin uint32, perm string, msg message, localFn func()) (bool, int, string) {
	if srv == nil || conn == nil || hdr == nil || executor == 0 {
		return false, 500, "invalid route"
	}
	local := srv.NodeID()
	cm := srv.ConnManager()
	if cm == nil {
		return false, 500, "no conn manager"
	}
	// 来自父节点：信任父，直接向下转交到 executor（或本地）。
	if isParentConn(conn) {
		if executor == local && localFn != nil {
			localFn()
			return true, 0, ""
		}
		execConn, ok := cm.GetByNode(executor)
		if !ok || execConn == nil || isParentConn(execConn) {
			return false, 404, "not found"
		}
		downHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			h.log.Warn("drop flow frame due to hop_limit", "target", executor, "source", hdr.SourceID())
			return false, 500, "hop limit exceeded"
		}
		downHdr.WithTargetID(executor)
		if err := h.sendToConn(ctx, execConn, downHdr, payloadFrom(msg)); err != nil {
			h.log.Warn("forward flow frame failed", "target", executor, "source", hdr.SourceID(), "err", err)
			return false, 500, "forward failed"
		}
		return true, 0, ""
	}
	if executor == local {
		if strings.TrimSpace(perm) != "" && !h.hasPermission(origin, perm) {
			return false, 403, "permission denied"
		}
		if localFn != nil {
			localFn()
		}
		return true, 0, ""
	}
	execConn, ok := cm.GetByNode(executor)
	if !ok || execConn == nil || isParentConn(execConn) {
		parent := findParentConn(cm)
		if parent == nil {
			return false, 404, "not found"
		}
		parentNode := connNodeID(parent)
		if parentNode == 0 {
			return false, 500, "invalid parent route"
		}
		upHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			h.log.Warn("drop flow frame due to hop_limit", "target", parentNode, "source", hdr.SourceID())
			return false, 500, "hop limit exceeded"
		}
		upHdr.WithTargetID(parentNode)
		if err := h.sendToConn(ctx, parent, upHdr, payloadFrom(msg)); err != nil {
			h.log.Warn("forward flow frame failed", "target", parentNode, "source", hdr.SourceID(), "err", err)
			return false, 500, "forward failed"
		}
		return true, 0, ""
	}
	originConn, ok2 := cm.GetByNode(origin)
	if ok2 && originConn != nil && originConn.ID() == execConn.ID() {
		nextNode := connNodeID(originConn)
		if nextNode == 0 {
			return false, 500, "invalid route"
		}
		childHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			h.log.Warn("drop flow frame due to hop_limit", "target", nextNode, "source", hdr.SourceID())
			return false, 500, "hop limit exceeded"
		}
		childHdr.WithTargetID(nextNode)
		if err := h.sendToConn(ctx, originConn, childHdr, payloadFrom(msg)); err != nil {
			h.log.Warn("forward flow frame failed", "target", nextNode, "source", hdr.SourceID(), "err", err)
			return false, 500, "forward failed"
		}
		return true, 0, ""
	}
	if strings.TrimSpace(perm) != "" && !h.hasPermission(origin, perm) {
		return false, 403, "permission denied"
	}
	downHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		h.log.Warn("drop flow frame due to hop_limit", "target", executor, "source", hdr.SourceID())
		return false, 500, "hop limit exceeded"
	}
	downHdr.WithTargetID(executor)
	if err := h.sendToConn(ctx, execConn, downHdr, payloadFrom(msg)); err != nil {
		h.log.Warn("forward flow frame failed", "target", executor, "source", hdr.SourceID(), "err", err)
		return false, 500, "forward failed"
	}
	return true, 0, ""
}

func (h *Handler) forwardToExecutorNoPerm(ctx context.Context, srv core.IServer, conn core.IConnection, hdr core.IHeader, executor, origin uint32, msg message, localFn func()) (bool, int, string) {
	return h.forwardToExecutorWithPerm(ctx, srv, conn, hdr, executor, origin, "", msg, localFn)
}

func (h *Handler) sendListResp(ctx context.Context, hdr core.IHeader, resp listResp) {
	target := uint32(0)
	if hdr != nil {
		target = hdr.SourceID()
	}
	if target == 0 {
		return
	}
	h.sendCtrlToNodeWithReqHdr(ctx, hdr, target, message{Action: actionListResp, Data: mustJSON(resp)})
}

func (h *Handler) sendGetResp(ctx context.Context, hdr core.IHeader, resp getResp) {
	target := uint32(0)
	if hdr != nil {
		target = hdr.SourceID()
	}
	if target == 0 {
		return
	}
	h.sendCtrlToNodeWithReqHdr(ctx, hdr, target, message{Action: actionGetResp, Data: mustJSON(resp)})
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

func (h *Handler) backgroundRunContext() context.Context {
	h.mu.Lock()
	srv := h.srv
	h.mu.Unlock()
	return backgroundRunContextForServer(srv)
}

func backgroundRunContextForServer(srv core.IServer) context.Context {
	ctx := context.Background()
	if srv != nil {
		return core.WithServerContext(ctx, srv)
	}
	return ctx
}

type flowStackContextKey struct{}

func pushFlowExecutionContext(ctx context.Context, flowID string) context.Context {
	flowID = strings.TrimSpace(flowID)
	if flowID == "" {
		return ctx
	}
	stack := make(map[string]struct{})
	if existing, ok := ctx.Value(flowStackContextKey{}).(map[string]struct{}); ok {
		for id := range existing {
			stack[id] = struct{}{}
		}
	}
	stack[flowID] = struct{}{}
	return context.WithValue(ctx, flowStackContextKey{}, stack)
}

func flowExecutionContains(ctx context.Context, flowID string) bool {
	flowID = strings.TrimSpace(flowID)
	if flowID == "" || ctx == nil {
		return false
	}
	stack, ok := ctx.Value(flowStackContextKey{}).(map[string]struct{})
	if !ok {
		return false
	}
	_, exists := stack[flowID]
	return exists
}

func newNestedRunState(flowID, runID string, executorNode uint32, triggerCtx json.RawMessage) *runState {
	return &runState{
		flowID:  strings.TrimSpace(flowID),
		runID:   strings.TrimSpace(runID),
		status:  "queued",
		start:   time.Now(),
		runtime: newRunContext(flowID, runID, executorNode, triggerCtx),
	}
}

func newLoopRunState(parent *runState, item any, index int) *runState {
	if parent == nil {
		return newNestedRunState("", "", 0, nil)
	}
	parent.mu.Lock()
	flowID := parent.runtime.FlowID
	runID := parent.runtime.RunID
	executorNode := parent.runtime.ExecutorNode
	triggerCtx := cloneRawJSON(parent.runtime.Trigger)
	parent.mu.Unlock()
	child := newNestedRunState(flowID, runID, executorNode, triggerCtx)
	child.runtime.Loop = &loopRuntimeData{
		Item:  mustJSON(item),
		Index: index,
	}
	return child
}

func newSubflowRunState(parent *runState, flowID string, triggerCtx json.RawMessage) *runState {
	executorNode := uint32(0)
	if parent != nil {
		parent.mu.Lock()
		executorNode = parent.runtime.ExecutorNode
		parent.mu.Unlock()
	}
	return newNestedRunState(flowID, newUUID(), executorNode, triggerCtx)
}

func (h *Handler) executeFlow(ctx context.Context, flow setReq, state *runState) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = pushFlowExecutionContext(ctx, flow.FlowID)
	defer h.finalizeRun(flow.FlowID, state)
	h.executeGraph(ctx, flow, flow.Graph, state)
}

func (h *Handler) executeGraph(ctx context.Context, flow setReq, g graph, state *runState) {
	order, err := topoOrder(g)
	idx, idxErr := buildGraphIndex(g)
	state.mu.Lock()
	if ctx.Err() != nil {
		state.status = "cancelled"
		state.end = time.Now()
		if state.cancelReason == "" {
			state.cancelReason = runCancelMsgCancelled
		}
		state.markActiveNodesCancelledLocked(state.cancelReason)
		state.mu.Unlock()
		return
	}
	if err != nil || idxErr != nil {
		state.status = "failed"
		state.end = time.Now()
		state.mu.Unlock()
		return
	}
	state.status = "running"
	state.mu.Unlock()

	for _, n := range order {
		if ctx.Err() != nil {
			markRunCancelled(state, runCancelMsgCancelled)
			return
		}
		if n == nil {
			continue
		}
		id := strings.TrimSpace(n.ID)
		if id == "" {
			continue
		}
		active, activeErr := shouldExecuteNode(idx, id, state)
		if activeErr != nil {
			state.mu.Lock()
			state.setNodeRuntimeLocked(id, nodeRuntimeData{
				Status: "failed",
				Code:   400,
				Msg:    activeErr.Error(),
			})
			state.status = "failed"
			state.end = time.Now()
			state.mu.Unlock()
			return
		}
		if !active {
			state.mu.Lock()
			state.setNodeRuntimeLocked(id, nodeRuntimeData{Status: "skipped"})
			state.mu.Unlock()
			continue
		}
		state.mu.Lock()
		state.setNodeRuntimeLocked(id, nodeRuntimeData{Status: "running"})
		state.mu.Unlock()
		retry := 1
		if n.Retry != nil {
			retry = *n.Retry
		}
		if retry < 0 {
			retry = 0
		}
		retryBackoffMs := 0
		if n.RetryBackoffMs != nil {
			retryBackoffMs = *n.RetryBackoffMs
		}
		if retryBackoffMs < 0 {
			retryBackoffMs = 0
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
		var lastResult json.RawMessage
		for attempt := 0; attempt <= retry; attempt++ {
			nodeCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
			lastCode, lastResult, lastErr = h.executeNode(nodeCtx, flow, state, *n)
			cancel()
			if lastErr == nil && lastCode == 1 {
				break
			}
			if ctx.Err() != nil {
				break
			}
			if attempt < retry && retryBackoffMs > 0 {
				if !waitRetryBackoff(ctx, time.Duration(retryBackoffMs)*time.Millisecond) {
					break
				}
			}
		}
		if ctx.Err() != nil {
			markRunCancelled(state, runCancelMsgCancelled)
			return
		}
		rt := nodeRuntimeData{}
		if lastErr == nil && lastCode == 1 {
			rt.Status = "succeeded"
			rt.Code = 1
			rt.Result = lastResult
		} else {
			rt.Status = "failed"
			if lastCode != 0 {
				rt.Code = lastCode
			} else {
				rt.Code = 500
			}
			if lastErr != nil {
				rt.Msg = lastErr.Error()
			}
		}
		state.mu.Lock()
		if state.status == "cancelled" {
			if state.end.IsZero() {
				state.end = time.Now()
			}
			if state.cancelReason == "" {
				state.cancelReason = runCancelMsgCancelled
			}
			state.markActiveNodesCancelledLocked(state.cancelReason)
			state.mu.Unlock()
			return
		}
		state.setNodeRuntimeLocked(id, rt)
		state.mu.Unlock()

		if rt.Status != "succeeded" && !n.AllowFail {
			state.mu.Lock()
			state.status = "failed"
			state.end = time.Now()
			state.mu.Unlock()
			return
		}
	}
	if ctx.Err() != nil {
		markRunCancelled(state, runCancelMsgCancelled)
		return
	}
	state.mu.Lock()
	if state.status == "cancelled" {
		if state.end.IsZero() {
			state.end = time.Now()
		}
		if state.cancelReason == "" {
			state.cancelReason = runCancelMsgCancelled
		}
		state.markActiveNodesCancelledLocked(state.cancelReason)
		state.mu.Unlock()
		return
	}
	state.status = "succeeded"
	state.end = time.Now()
	state.mu.Unlock()
}

func shouldExecuteNode(idx *graphIndex, nodeID string, state *runState) (bool, error) {
	if idx == nil {
		return true, nil
	}
	incoming := idx.incomingEdges(nodeID)
	if len(incoming) == 0 {
		return true, nil
	}
	activeCount := 0
	state.mu.Lock()
	defer state.mu.Unlock()
	for _, e := range incoming {
		parentID := strings.TrimSpace(e.From)
		parentNode, ok := idx.node(parentID)
		if !ok {
			return false, fmt.Errorf("node %s parent %s missing", nodeID, parentID)
		}
		parentData, ok := state.runtime.Nodes[parentID]
		if !ok {
			return false, fmt.Errorf("node %s parent %s not executed", nodeID, parentID)
		}
		if parentData.Status == "skipped" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(parentNode.Kind), "branch") {
			wantCase := strings.TrimSpace(e.Case)
			if wantCase == "" {
				return false, fmt.Errorf("node %s branch edge case required", nodeID)
			}
			gotCase, found, err := readSelectedBranchCase(parentData.Result)
			if err != nil {
				return false, fmt.Errorf("node %s branch parent %s invalid result: %w", nodeID, parentID, err)
			}
			if !found || gotCase != wantCase {
				continue
			}
		} else if strings.TrimSpace(e.Case) != "" {
			return false, fmt.Errorf("node %s non-branch edge.case unsupported", nodeID)
		}
		activeCount++
	}
	return activeCount > 0, nil
}

func waitRetryBackoff(ctx context.Context, backoff time.Duration) bool {
	if backoff <= 0 {
		return true
	}
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type callSpec struct {
	Target       uint32          `json:"target,omitempty"`
	Method       string          `json:"method"`
	Args         json.RawMessage `json:"args,omitempty"`
	ArgsTemplate json.RawMessage `json:"args_template,omitempty"`
	Inputs       []inputBinding  `json:"inputs,omitempty"`
}

type legacyLocalSpec struct {
	Method string          `json:"method"`
	Args   json.RawMessage `json:"args,omitempty"`
}

type legacyExecSpec struct {
	Target uint32          `json:"target"`
	Method string          `json:"method"`
	Args   json.RawMessage `json:"args,omitempty"`
}

// Runtime compatibility is intentionally broader than set validation:
// new writes are call-only, but historical local/exec payloads can still run.
func decodeNodeCallSpec(n node) (callSpec, error) {
	kind := strings.ToLower(strings.TrimSpace(n.Kind))
	switch kind {
	case "call":
		var spec callSpec
		if err := json.Unmarshal(n.Spec, &spec); err != nil {
			return callSpec{}, errors.New("invalid call spec")
		}
		spec.Method = strings.TrimSpace(spec.Method)
		if spec.Method == "" {
			return callSpec{}, errors.New("call method required")
		}
		return spec, nil
	case "local":
		var spec legacyLocalSpec
		if err := json.Unmarshal(n.Spec, &spec); err != nil {
			return callSpec{}, errors.New("invalid local spec")
		}
		spec.Method = strings.TrimSpace(spec.Method)
		if spec.Method == "" {
			return callSpec{}, errors.New("local method required")
		}
		return callSpec{Method: spec.Method, Args: spec.Args}, nil
	case "exec":
		var spec legacyExecSpec
		if err := json.Unmarshal(n.Spec, &spec); err != nil {
			return callSpec{}, errors.New("invalid exec spec")
		}
		spec.Method = strings.TrimSpace(spec.Method)
		if spec.Target == 0 || spec.Method == "" {
			return callSpec{}, errors.New("exec target/method required")
		}
		return callSpec{Target: spec.Target, Method: spec.Method, Args: spec.Args}, nil
	default:
		return callSpec{}, fmt.Errorf("unknown node kind: %s", kind)
	}
}

func (h *Handler) executeNode(ctx context.Context, _ setReq, state *runState, n node) (code int, result json.RawMessage, err error) {
	nodeID := strings.TrimSpace(n.ID)
	kind := strings.ToLower(strings.TrimSpace(n.Kind))
	if kind == "branch" {
		spec, specErr := decodeNodeBranchSpec(n)
		if specErr != nil {
			return 400, nil, specErr
		}
		selectedCase, matchErr := evaluateBranchCases(spec, state)
		if matchErr != nil {
			return 400, nil, matchErr
		}
		return 1, mustJSON(map[string]any{"case": selectedCase}), nil
	}
	if kind == "foreach" {
		spec, specErr := decodeNodeForeachSpec(n)
		if specErr != nil {
			return 400, nil, specErr
		}
		itemsValue, found, resolveErr := state.resolveBindingSource(spec.Source)
		if resolveErr != nil {
			return 400, nil, resolveErr
		}
		required := true
		if spec.Required != nil {
			required = *spec.Required
		}
		if !found {
			if required {
				return 400, nil, errors.New("foreach source missing")
			}
			return 1, json.RawMessage(`[]`), nil
		}
		items, ok := itemsValue.([]any)
		if !ok {
			return 400, nil, errors.New("foreach source must be array")
		}
		results := make([]any, 0, len(items))
		for i, item := range items {
			if ctx.Err() != nil {
				return 408, nil, errors.New("timeout")
			}
			childState := newLoopRunState(state, item, i)
			h.executeGraph(ctx, setReq{FlowID: childState.flowID, Graph: spec.Body}, spec.Body, childState)
			childState.mu.Lock()
			childStatus := childState.status
			childMsg := childState.cancelReason
			resultNode, ok := childState.runtime.Nodes[spec.ResultNodeID]
			resultRaw := cloneRawJSON(resultNode.Result)
			childState.mu.Unlock()
			if childStatus != "succeeded" {
				if childMsg == "" {
					childMsg = "foreach body " + childStatus
				}
				return 500, nil, errors.New(childMsg)
			}
			if !ok {
				return 500, nil, fmt.Errorf("foreach result_node_id not found: %s", spec.ResultNodeID)
			}
			var value any
			if len(bytes.TrimSpace(resultRaw)) != 0 {
				if err := json.Unmarshal(resultRaw, &value); err != nil {
					return 500, nil, errors.New("invalid foreach result json")
				}
			}
			results = append(results, value)
		}
		return 1, mustJSON(results), nil
	}
	if kind == "subflow" {
		spec, specErr := decodeNodeSubflowSpec(n)
		if specErr != nil {
			return 400, nil, specErr
		}
		if flowExecutionContains(ctx, spec.FlowID) {
			return 400, nil, errors.New("subflow recursion detected")
		}
		input, materializeErr := materializeSubflowInput(nodeID, spec, state)
		if materializeErr != nil {
			return 400, nil, materializeErr
		}
		h.mu.Lock()
		childFlow, ok := h.flows[spec.FlowID]
		h.mu.Unlock()
		if !ok {
			return 404, nil, fmt.Errorf("subflow target not found: %s", spec.FlowID)
		}
		childTrigger := buildSubflowTriggerContext(state, nodeID, input)
		childState := newSubflowRunState(state, childFlow.FlowID, childTrigger)
		childCtx := pushFlowExecutionContext(ctx, childFlow.FlowID)
		h.executeGraph(childCtx, childFlow, childFlow.Graph, childState)
		childState.mu.Lock()
		childStatus := childState.status
		childRunID := childState.runID
		childMsg := childState.cancelReason
		var childResult json.RawMessage
		if spec.ResultNodeID != "" {
			resultNode, ok := childState.runtime.Nodes[spec.ResultNodeID]
			if !ok {
				childState.mu.Unlock()
				return 500, nil, fmt.Errorf("subflow result_node_id not found: %s", spec.ResultNodeID)
			}
			childResult = cloneRawJSON(resultNode.Result)
		}
		childState.mu.Unlock()
		if childStatus != "succeeded" {
			if childMsg == "" {
				childMsg = "subflow " + childStatus
			}
			return 500, nil, errors.New(childMsg)
		}
		payload := map[string]any{
			"flow_id": childFlow.FlowID,
			"run_id":  childRunID,
			"status":  childStatus,
		}
		if len(bytes.TrimSpace(childResult)) != 0 {
			var value any
			if err := json.Unmarshal(childResult, &value); err != nil {
				return 500, nil, errors.New("invalid subflow result json")
			}
			payload["result"] = value
		}
		return 1, mustJSON(payload), nil
	}
	if kind == "compose" {
		spec, specErr := decodeNodeComposeSpec(n)
		if specErr != nil {
			return 400, nil, specErr
		}
		out, materializeErr := materializeComposeResult(nodeID, spec, state)
		if materializeErr != nil {
			return 400, nil, materializeErr
		}
		return 1, out, nil
	}
	if kind == "set_var" {
		spec, specErr := decodeNodeSetVarSpec(n)
		if specErr != nil {
			return 400, nil, specErr
		}
		out, materializeErr := materializeSetVarValue(nodeID, spec, state)
		if materializeErr != nil {
			return 400, nil, materializeErr
		}
		if state != nil {
			state.mu.Lock()
			state.setVarRuntimeLocked(spec.Name, varRuntimeData{
				Value:        out,
				WriterNodeID: nodeID,
			})
			state.mu.Unlock()
		}
		return 1, out, nil
	}
	if kind == "transform" {
		spec, specErr := decodeNodeTransformSpec(n)
		if specErr != nil {
			return 400, nil, specErr
		}
		out, materializeErr := materializeTransformResult(nodeID, spec, state)
		if materializeErr != nil {
			return 400, nil, materializeErr
		}
		return 1, out, nil
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		h.mu.Lock()
		srv = h.srv
		h.mu.Unlock()
	}
	if srv == nil {
		return 500, nil, errors.New("no server")
	}
	local := srv.NodeID()
	spec, specErr := decodeNodeCallSpec(n)
	if specErr != nil {
		return 400, nil, specErr
	}
	args, materializeErr := materializeCallArgs(nodeID, spec, state)
	if materializeErr != nil {
		return 400, nil, materializeErr
	}
	method := spec.Method
	target := spec.Target
	if target == 0 || target == local {
		if fn := h.localMethods[method]; fn != nil {
			res, err := fn(ctx, args)
			if err != nil {
				if ctx.Err() == context.DeadlineExceeded {
					return 408, nil, err
				}
				return 500, nil, err
			}
			normalized, normalizeErr := normalizeNodeResult(res)
			if normalizeErr != nil {
				return 500, nil, normalizeErr
			}
			return 1, normalized, nil
		}
		if h.capRegistry != nil {
			_, invoke, ok := h.capRegistry.Lookup(method, "")
			if ok && invoke != nil {
				res, err := invoke(ctx, args)
				if err != nil {
					if ctx.Err() == context.DeadlineExceeded {
						return 408, nil, err
					}
					return 500, nil, err
				}
				normalized, normalizeErr := normalizeNodeResult(res)
				if normalizeErr != nil {
					return 500, nil, normalizeErr
				}
				return 1, normalized, nil
			}
		}
		return 404, nil, fmt.Errorf("call method not found: %s", method)
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
		Args:         args,
		TimeoutMs:    timeoutMs,
	}
	if err := h.sendExecCall(ctx, srv, call); err != nil {
		return 500, nil, err
	}
	select {
	case resp, ok := <-ch:
		if !ok {
			return 500, nil, errors.New("exec response closed")
		}
		if resp.Code != 1 {
			msg := strings.TrimSpace(resp.Msg)
			if msg == "" {
				msg = "call failed"
			}
			return resp.Code, nil, errors.New(msg)
		}
		normalized, normalizeErr := normalizeNodeResult(resp.Result)
		if normalizeErr != nil {
			return 500, nil, normalizeErr
		}
		return 1, normalized, nil
	case <-ctx.Done():
		return 408, nil, errors.New("timeout")
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
	h.sendSetRespToNode(ctx, hdr, target, setResp{ReqID: "", Code: code, Msg: msg, FlowID: flowID})
}

func (h *Handler) sendSetRespToNode(ctx context.Context, reqHdr core.IHeader, target uint32, resp setResp) {
	h.sendCtrlToNodeWithReqHdr(ctx, reqHdr, target, message{Action: actionSetResp, Data: mustJSON(resp)})
}

func (h *Handler) sendDeleteResp(ctx context.Context, hdr core.IHeader, resp deleteResp) {
	target := uint32(0)
	if hdr != nil {
		target = hdr.SourceID()
	}
	if target == 0 {
		return
	}
	h.sendDeleteRespToNode(ctx, hdr, target, resp)
}

func (h *Handler) sendDeleteRespToNode(ctx context.Context, reqHdr core.IHeader, target uint32, resp deleteResp) {
	h.sendCtrlToNodeWithReqHdr(ctx, reqHdr, target, message{Action: actionDeleteResp, Data: mustJSON(resp)})
}

func (h *Handler) sendRunResp(ctx context.Context, hdr core.IHeader, resp runResp) {
	target := uint32(0)
	if hdr != nil {
		target = hdr.SourceID()
	}
	if target == 0 {
		return
	}
	h.sendCtrlToNodeWithReqHdr(ctx, hdr, target, message{Action: actionRunResp, Data: mustJSON(resp)})
}

func (h *Handler) sendCancelRunResp(ctx context.Context, hdr core.IHeader, resp cancelRunResp) {
	target := uint32(0)
	if hdr != nil {
		target = hdr.SourceID()
	}
	if target == 0 {
		return
	}
	h.sendCtrlToNodeWithReqHdr(ctx, hdr, target, message{Action: actionCancelRunResp, Data: mustJSON(resp)})
}

func (h *Handler) sendStatusResp(ctx context.Context, hdr core.IHeader, resp statusResp) {
	target := uint32(0)
	if hdr != nil {
		target = hdr.SourceID()
	}
	if target == 0 {
		return
	}
	h.sendCtrlToNodeWithReqHdr(ctx, hdr, target, message{Action: actionStatusResp, Data: mustJSON(resp)})
}

func (h *Handler) sendDetailResp(ctx context.Context, hdr core.IHeader, resp detailResp) {
	target := uint32(0)
	if hdr != nil {
		target = hdr.SourceID()
	}
	if target == 0 {
		return
	}
	h.sendCtrlToNodeWithReqHdr(ctx, hdr, target, message{Action: actionDetailResp, Data: mustJSON(resp)})
}

func (h *Handler) sendListRunsResp(ctx context.Context, hdr core.IHeader, resp listRunsResp) {
	target := uint32(0)
	if hdr != nil {
		target = hdr.SourceID()
	}
	if target == 0 {
		return
	}
	h.sendCtrlToNodeWithReqHdr(ctx, hdr, target, message{Action: actionListRunsResp, Data: mustJSON(resp)})
}

func (h *Handler) sendCtrlToNode(ctx context.Context, target uint32, msg message) {
	h.sendCtrlToNodeWithReqHdr(ctx, nil, target, msg)
}

func (h *Handler) sendCtrlToNodeWithReqHdr(ctx context.Context, reqHdr core.IHeader, target uint32, msg message) {
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
	if err := h.sendToConn(ctx, next, fwdHdr, body); err != nil {
		h.log.Warn("forward flow frame failed", "target", target, "source", hdr.SourceID(), "err", err)
		return false
	}
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

func (h *Handler) sendToConn(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) error {
	if conn == nil || hdr == nil || len(payload) == 0 {
		return errors.New("invalid frame")
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return conn.SendWithHeader(hdr, payload, header.HeaderTcpCodec{})
	}
	return srv.Send(ctx, conn.ID(), hdr, payload)
}

func (h *Handler) currentPersistenceLocked() Persistence {
	if h.explicitStore && h.persistence != nil {
		return h.persistence
	}
	return NewJSONPersistence(h.baseDir)
}

func (h *Handler) currentRunArchiveStoreLocked() RunArchiveStore {
	if !h.runArchive {
		return nil
	}
	if h.explicitRunArchiveStore && h.runArchiveStore != nil {
		return h.runArchiveStore
	}
	switch strings.TrimSpace(h.runArchiveBackend) {
	case "", runArchiveBackendOff, runArchiveBackendFile:
		return NewFileRunArchiveStore(h.baseDir)
	case runArchiveBackendPG:
		return nil
	default:
		return nil
	}
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanupTmp := true
	defer func() {
		_ = tmp.Close()
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	_ = tmp.Chmod(perm)
	if err := tmp.Close(); err != nil {
		return err
	}

	backupPath := path + ".bak"
	if _, err := os.Stat(path); err == nil {
		_ = os.Remove(backupPath)
		if err := os.Rename(path, backupPath); err != nil {
			return err
		}
		if err := os.Rename(tmpPath, path); err != nil {
			_ = os.Rename(backupPath, path)
			return err
		}
		cleanupTmp = false
		_ = os.Remove(backupPath)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanupTmp = false
	return nil
}

func isTerminalRunStatus(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (h *Handler) retainedRunLimit() int {
	if h.maxRetainedRuns <= 0 {
		return defaultMaxRetainedRuns
	}
	return h.maxRetainedRuns
}

func (h *Handler) recordRunLocked(state *runState) {
	if state == nil || strings.TrimSpace(state.flowID) == "" || strings.TrimSpace(state.runID) == "" {
		return
	}
	h.runs[state.runID] = state
	h.runOrderByFlow[state.flowID] = append(h.runOrderByFlow[state.flowID], state.runID)
}

func (h *Handler) latestRunStateLocked(flowID string) *runState {
	ids := h.runOrderByFlow[flowID]
	for i := len(ids) - 1; i >= 0; i-- {
		if st := h.runs[ids[i]]; st != nil {
			return st
		}
	}
	return nil
}

func (h *Handler) activeRunCountLocked(flowID string) int {
	ids := h.runOrderByFlow[flowID]
	active := 0
	for i := len(ids) - 1; i >= 0; i-- {
		st := h.runs[ids[i]]
		if st == nil {
			continue
		}
		st.mu.Lock()
		status := st.status
		st.mu.Unlock()
		if status == "queued" || status == "running" {
			active++
		}
	}
	return active
}

func (h *Handler) hasActiveRunLocked(flowID string) bool {
	return h.activeRunCountLocked(flowID) > 0
}

func (h *Handler) pruneRuns(flowID string) {
	h.mu.Lock()
	toDelete := h.pruneRunsLocked(flowID)
	h.mu.Unlock()
	h.removeArchivedRuns(toDelete)
}

func (h *Handler) pruneRunsLocked(flowID string) []archivedRunRef {
	ids := h.runOrderByFlow[flowID]
	if len(ids) == 0 {
		delete(h.runOrderByFlow, flowID)
		return nil
	}

	limit := h.retainedRunLimit()
	terminalKept := 0
	kept := make([]string, 0, len(ids))
	pruned := make([]archivedRunRef, 0)

	for i := len(ids) - 1; i >= 0; i-- {
		runID := ids[i]
		st := h.runs[runID]
		if st == nil {
			continue
		}
		st.mu.Lock()
		status := st.status
		st.mu.Unlock()

		if !isTerminalRunStatus(status) {
			kept = append(kept, runID)
			continue
		}
		if terminalKept < limit {
			kept = append(kept, runID)
			terminalKept++
			continue
		}
		delete(h.runs, runID)
		pruned = append(pruned, archivedRunRef{flowID: flowID, runID: runID})
	}

	if len(kept) == 0 {
		delete(h.runOrderByFlow, flowID)
		return pruned
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	h.runOrderByFlow[flowID] = kept
	return pruned
}

func validateGraphForFlow(flowID string, g graph) error {
	return validateGraphScoped(strings.TrimSpace(flowID), g, bindingValidationOptions{})
}

func validateGraph(g graph) error {
	return validateGraphForFlow("", g)
}

func validateGraphScoped(flowID string, g graph, opts bindingValidationOptions) error {
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
	idx, err := buildGraphIndex(g)
	if err != nil {
		return err
	}
	if err := collectSetVarWriters(g, idx); err != nil {
		return err
	}
	for _, n := range g.Nodes {
		if err := validateSetNodeKindAndSpec(flowID, strings.TrimSpace(n.ID), n, idx, opts); err != nil {
			return err
		}
	}
	if err := validateEdgeCases(g, idx); err != nil {
		return err
	}
	return nil
}

func validateEdgeCases(g graph, idx *graphIndex) error {
	if idx == nil {
		return errors.New("graph index required")
	}
	for _, n := range g.Nodes {
		nodeID := strings.TrimSpace(n.ID)
		kind := strings.ToLower(strings.TrimSpace(n.Kind))
		outgoing := idx.outgoingEdges(nodeID)
		if kind != "branch" {
			for _, e := range outgoing {
				if strings.TrimSpace(e.Case) != "" {
					return fmt.Errorf("node %s edge.case only allowed for branch", nodeID)
				}
			}
			continue
		}
		spec, err := decodeNodeBranchSpec(n)
		if err != nil {
			return fmt.Errorf("node %s %w", nodeID, err)
		}
		allowed := make(map[string]struct{}, len(spec.Cases)+1)
		for _, candidate := range spec.Cases {
			allowed[strings.TrimSpace(candidate.Name)] = struct{}{}
		}
		if spec.DefaultCase != "" {
			allowed[strings.TrimSpace(spec.DefaultCase)] = struct{}{}
		}
		for _, e := range outgoing {
			caseName := strings.TrimSpace(e.Case)
			if caseName == "" {
				return fmt.Errorf("node %s branch edge to %s requires case", nodeID, strings.TrimSpace(e.To))
			}
			if _, ok := allowed[caseName]; !ok {
				return fmt.Errorf("node %s branch edge case %q not declared", nodeID, caseName)
			}
		}
	}
	return nil
}

func validateSetNodeKindAndSpec(flowID, nodeID string, n node, idx *graphIndex, opts bindingValidationOptions) error {
	if n.RetryBackoffMs != nil && *n.RetryBackoffMs < 0 {
		return fmt.Errorf("node %s retry_backoff_ms must be >= 0", nodeID)
	}
	kind := strings.ToLower(strings.TrimSpace(n.Kind))
	switch kind {
	case "call":
		var spec callSpec
		if err := json.Unmarshal(n.Spec, &spec); err != nil {
			return fmt.Errorf("node %s invalid call spec", nodeID)
		}
		return validateCallSpecForSet(nodeID, spec, idx, opts)
	case "compose":
		var spec composeSpec
		if err := json.Unmarshal(n.Spec, &spec); err != nil {
			return fmt.Errorf("node %s invalid compose spec", nodeID)
		}
		return validateComposeSpecForSet(nodeID, spec, idx, opts)
	case "set_var":
		var spec setVarSpec
		if err := json.Unmarshal(n.Spec, &spec); err != nil {
			return fmt.Errorf("node %s invalid set_var spec", nodeID)
		}
		return validateSetVarSpecForSet(nodeID, spec, idx, opts)
	case "transform":
		spec, err := decodeNodeTransformSpec(n)
		if err != nil {
			return err
		}
		return validateTransformSpecForSet(nodeID, spec, idx, opts)
	case "branch":
		spec, err := decodeNodeBranchSpec(n)
		if err != nil {
			return fmt.Errorf("node %s %w", nodeID, err)
		}
		return validateBranchSpecForSet(nodeID, spec, idx, opts)
	case "foreach":
		spec, err := decodeNodeForeachSpec(n)
		if err != nil {
			return fmt.Errorf("node %s %w", nodeID, err)
		}
		return validateForeachSpecForSet(flowID, nodeID, spec, idx, opts)
	case "subflow":
		spec, err := decodeNodeSubflowSpec(n)
		if err != nil {
			return fmt.Errorf("node %s %w", nodeID, err)
		}
		return validateSubflowSpecForSet(flowID, nodeID, spec, idx, opts)
	default:
		return fmt.Errorf("node %s kind must be call, compose, transform, set_var, branch, foreach or subflow", nodeID)
	}
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

func (h *Handler) loadFlowsFromDisk() error {
	h.mu.Lock()
	store := h.currentPersistenceLocked()
	h.mu.Unlock()
	docs, err := store.LoadAll(context.Background())
	if err != nil {
		return err
	}
	loaded := make(map[string]setReq, len(docs))
	for _, doc := range docs {
		req := setReq(doc)
		validFlowID, err := validateFlowID(req.FlowID)
		if err != nil {
			continue
		}
		req.FlowID = validFlowID
		normalizeTrigger(&req.Trigger)
		if validateTrigger(req.Trigger) != nil {
			continue
		}
		if validateFlowRunConfig(req) != nil {
			continue
		}
		if validateGraphForFlow(req.FlowID, req.Graph) != nil {
			continue
		}
		loaded[req.FlowID] = req
	}
	h.mu.Lock()
	h.flows = loaded
	h.mu.Unlock()
	return nil
}

func (h *Handler) ensureTriggerSubscriptions(srv core.IServer) {
	if srv == nil {
		return
	}
	h.eventSubOnce.Do(func() {
		eb := srv.EventBus()
		if eb == nil {
			return
		}
		eb.Subscribe("topicbus.publish", func(_ context.Context, evt eventbus.Event) {
			h.handleTopicPublishEvent(eventModePublish, evt.Data)
		})
		eb.Subscribe("topicbus.received", func(_ context.Context, evt eventbus.Event) {
			h.handleTopicPublishEvent(eventModeReceived, evt.Data)
		})
		eb.Subscribe("varstore.changed", func(_ context.Context, evt eventbus.Event) {
			h.handleVarChangedEvent(varChangeOpChanged, evt.Data)
		})
		eb.Subscribe("varstore.deleted", func(_ context.Context, evt eventbus.Event) {
			h.handleVarChangedEvent(varChangeOpDeleted, evt.Data)
		})
	})
}

func (h *Handler) handleTopicPublishEvent(mode string, data any) {
	mode = normalizeEventMode(mode)
	if mode == "" {
		return
	}
	var ev topicPublishEvent
	if !decodeEventData(data, &ev) {
		return
	}
	ev.Topic = strings.TrimSpace(ev.Topic)
	ev.Name = strings.TrimSpace(ev.Name)
	if ev.Topic == "" && ev.Name == "" {
		return
	}
	ids := h.collectTriggeredFlows(func(tr trigger) bool {
		if triggerType(tr) != triggerTypeEvent {
			return false
		}
		wantMode := normalizeEventMode(tr.EventMode)
		if wantMode == "" {
			return false
		}
		if wantMode != eventModeAny && wantMode != mode {
			return false
		}
		wantTopic := strings.TrimSpace(tr.EventTopic)
		if wantTopic != "" && wantTopic != ev.Topic {
			return false
		}
		wantName := strings.TrimSpace(tr.EventName)
		if wantName != "" && wantName != ev.Name {
			return false
		}
		return true
	})
	triggerCtx := buildTopicTriggerContext(mode, ev)
	for _, id := range ids {
		h.tryStartRunWithTrigger(id, triggerCtx)
	}
}

func (h *Handler) handleVarChangedEvent(op string, data any) {
	var ev varChangedEvent
	if !decodeEventData(data, &ev) {
		return
	}
	ev.Name = strings.TrimSpace(ev.Name)
	if ev.Owner == 0 || ev.Name == "" {
		return
	}
	ids := h.collectTriggeredFlows(func(tr trigger) bool {
		if triggerType(tr) != triggerTypeVarChanged {
			return false
		}
		if tr.VarOwner != 0 && tr.VarOwner != ev.Owner {
			return false
		}
		wantName := strings.TrimSpace(tr.VarName)
		if wantName != "" && wantName != ev.Name {
			return false
		}
		return true
	})
	triggerCtx := buildVarChangedTriggerContext(op, ev)
	for _, id := range ids {
		h.tryStartRunWithTrigger(id, triggerCtx)
	}
}

func (h *Handler) collectTriggeredFlows(match func(trigger) bool) []string {
	if match == nil {
		return nil
	}
	h.mu.Lock()
	ids := make([]string, 0, len(h.flows))
	for flowID, req := range h.flows {
		if match(req.Trigger) {
			ids = append(ids, flowID)
		}
	}
	h.mu.Unlock()
	return ids
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
	triggerKind := triggerType(flow.Trigger)
	cronExpr := strings.TrimSpace(flow.Trigger.Cron)
	every := time.Duration(flow.Trigger.EveryMs) * time.Millisecond
	h.mu.Unlock()
	switch triggerKind {
	case triggerTypeInterval:
		if every <= 0 {
			h.mu.Lock()
			delete(h.schedulers, flowID)
			h.mu.Unlock()
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
					h.tryStartScheduledRun(flowID)
				}
			}
		}()
	case triggerTypeCron:
		schedule, err := parseCronExpr(cronExpr)
		if err != nil {
			h.mu.Lock()
			delete(h.schedulers, flowID)
			h.mu.Unlock()
			return
		}
		go func() {
			for {
				next := schedule.NextAfter(time.Now())
				if next.IsZero() {
					return
				}
				wait := time.Until(next)
				if wait < 0 {
					wait = 0
				}
				timer := time.NewTimer(wait)
				select {
				case <-stop:
					timer.Stop()
					return
				case <-timer.C:
					h.tryStartRunWithTrigger(flowID, buildCronTriggerContext(next, cronExpr))
				}
			}
		}()
	default:
		h.mu.Lock()
		delete(h.schedulers, flowID)
		h.mu.Unlock()
	}
}

func (h *Handler) tryStartScheduledRun(flowID string) {
	h.tryStartRunWithTrigger(flowID, buildIntervalTriggerContext(time.Now()))
}

func (h *Handler) cancelRunsLocked(flowID, reason string) {
	for _, runID := range h.runOrderByFlow[flowID] {
		st := h.runs[runID]
		if st == nil {
			continue
		}
		cancelRunState(st, reason)
	}
}

func markRunCancelled(state *runState, reason string) {
	cancelRunState(state, reason)
}

func (state *runState) markActiveNodesCancelledLocked(reason string) {
	for nodeID, nodeData := range state.runtime.Nodes {
		switch nodeData.Status {
		case "queued", "running":
			nodeData.Status = "cancelled"
			if reason != "" && strings.TrimSpace(nodeData.Msg) == "" {
				nodeData.Msg = reason
			}
			state.runtime.Nodes[nodeID] = nodeData
		}
	}
}

func cancelRunState(state *runState, reason string) (string, bool) {
	if state == nil {
		return "", false
	}
	var cancel context.CancelFunc
	state.mu.Lock()
	status := state.status
	if isTerminalRunStatus(status) {
		state.mu.Unlock()
		return status, false
	}
	state.status = "cancelled"
	if state.end.IsZero() {
		state.end = time.Now()
	}
	if reason != "" && strings.TrimSpace(state.cancelReason) == "" {
		state.cancelReason = reason
	}
	state.markActiveNodesCancelledLocked(state.cancelReason)
	cancel = state.cancel
	status = state.status
	state.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return status, true
}

func (state *runState) snapshotRunSummaryLocked() runSummary {
	sum := runSummary{
		RunID:  state.runID,
		Status: state.status,
	}
	if !state.start.IsZero() {
		sum.StartedAtMs = state.start.UTC().UnixMilli()
	}
	if !state.end.IsZero() {
		sum.EndedAtMs = state.end.UTC().UnixMilli()
	}
	if strings.TrimSpace(state.cancelReason) != "" {
		sum.Msg = strings.TrimSpace(state.cancelReason)
	}
	return sum
}

func (h *Handler) enqueueRun(ctx context.Context, flow setReq) string {
	return h.enqueueRunWithTrigger(ctx, flow, nil)
}

func (h *Handler) enqueueRunWithTrigger(ctx context.Context, flow setReq, triggerCtx json.RawMessage) string {
	if ctx == nil {
		ctx = h.backgroundRunContext()
	}
	runCtx, cancel := context.WithCancel(ctx)
	h.mu.Lock()
	state := h.newQueuedRunStateLocked(flow, triggerCtx, cancel, time.Now())
	h.recordRunLocked(state)
	h.mu.Unlock()
	go h.executeFlow(runCtx, flow, state)
	return state.runID
}

func (h *Handler) tryStartRun(flowID string) {
	h.tryStartRunWithTrigger(flowID, nil)
}

func (h *Handler) tryStartRunWithTrigger(flowID string, triggerCtx json.RawMessage) {
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
	state, runCtx, allowed := h.prepareQueuedRunLocked(flow, triggerCtx, runStartSourceTrigger)
	h.mu.Unlock()
	if !allowed {
		return
	}
	go h.executeFlow(runCtx, flow, state)
}

func effectiveMaxActiveRuns(flow setReq, source runStartSource) int {
	if flow.MaxActiveRuns != nil {
		if *flow.MaxActiveRuns < 0 {
			return 0
		}
		return *flow.MaxActiveRuns
	}
	if source == runStartSourceTrigger {
		return 1
	}
	return 0
}

func triggerDedupWindow(t trigger) time.Duration {
	if t.DedupWindowMs == nil || *t.DedupWindowMs <= 0 {
		return 0
	}
	return time.Duration(*t.DedupWindowMs) * time.Millisecond
}

func triggerDedupKey(triggerCtx json.RawMessage) string {
	raw := bytes.TrimSpace(triggerCtx)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	return string(raw)
}

func (h *Handler) checkAndRecordTriggerDedupLocked(flow setReq, triggerCtx json.RawMessage, now time.Time) bool {
	window := triggerDedupWindow(flow.Trigger)
	if window <= 0 {
		return false
	}
	key := triggerDedupKey(triggerCtx)
	if key == "" {
		return false
	}
	cutoff := now.Add(-window)
	bucket := h.triggerDedup[flow.FlowID]
	if bucket == nil {
		bucket = make(map[string]time.Time)
		h.triggerDedup[flow.FlowID] = bucket
	}
	for existingKey, seenAt := range bucket {
		if !seenAt.After(cutoff) {
			delete(bucket, existingKey)
		}
	}
	if seenAt, ok := bucket[key]; ok && now.Sub(seenAt) < window {
		return true
	}
	bucket[key] = now
	return false
}

func (h *Handler) prepareQueuedRunLocked(flow setReq, triggerCtx json.RawMessage, source runStartSource) (*runState, context.Context, bool) {
	limit := effectiveMaxActiveRuns(flow, source)
	if limit > 0 && h.activeRunCountLocked(flow.FlowID) >= limit {
		return nil, nil, false
	}
	now := time.Now()
	if source == runStartSourceTrigger && h.checkAndRecordTriggerDedupLocked(flow, triggerCtx, now) {
		return nil, nil, false
	}
	runCtx, cancel := context.WithCancel(backgroundRunContextForServer(h.srv))
	state := h.newQueuedRunStateLocked(flow, triggerCtx, cancel, now)
	h.recordRunLocked(state)
	return state, runCtx, true
}

func (h *Handler) newQueuedRunStateLocked(flow setReq, triggerCtx json.RawMessage, cancel context.CancelFunc, start time.Time) *runState {
	runID := newUUID()
	executorNode := uint32(0)
	if h.srv != nil {
		executorNode = h.srv.NodeID()
	}
	return &runState{
		flowID:  flow.FlowID,
		runID:   runID,
		status:  "queued",
		start:   start,
		cancel:  cancel,
		runtime: newRunContext(flow.FlowID, runID, executorNode, triggerCtx),
	}
}

type cronField struct {
	any     bool
	allowed map[int]struct{}
}

type cronSchedule struct {
	minute     cronField
	hour       cronField
	dayOfMonth cronField
	month      cronField
	dayOfWeek  cronField
}

func parseCronExpr(expr string) (cronSchedule, error) {
	parts := strings.Fields(strings.TrimSpace(expr))
	if len(parts) != 5 {
		return cronSchedule{}, errors.New("cron must have 5 fields")
	}
	minute, err := parseCronField(parts[0], 0, 59, false)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("minute: %w", err)
	}
	hour, err := parseCronField(parts[1], 0, 23, false)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("hour: %w", err)
	}
	dayOfMonth, err := parseCronField(parts[2], 1, 31, false)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("day-of-month: %w", err)
	}
	month, err := parseCronField(parts[3], 1, 12, false)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("month: %w", err)
	}
	dayOfWeek, err := parseCronField(parts[4], 0, 7, true)
	if err != nil {
		return cronSchedule{}, fmt.Errorf("day-of-week: %w", err)
	}
	return cronSchedule{
		minute:     minute,
		hour:       hour,
		dayOfMonth: dayOfMonth,
		month:      month,
		dayOfWeek:  dayOfWeek,
	}, nil
}

func parseCronField(raw string, min, max int, sundayAlias bool) (cronField, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return cronField{}, errors.New("field required")
	}
	if raw == "*" {
		return cronField{any: true}, nil
	}
	allowed := make(map[int]struct{})
	for _, token := range strings.Split(raw, ",") {
		values, err := expandCronToken(token, min, max, sundayAlias)
		if err != nil {
			return cronField{}, err
		}
		for _, value := range values {
			allowed[value] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return cronField{}, errors.New("empty field")
	}
	return cronField{allowed: allowed}, nil
}

func expandCronToken(token string, min, max int, sundayAlias bool) ([]int, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("empty token")
	}
	step := 1
	base := token
	if slash := strings.Index(token, "/"); slash >= 0 {
		base = strings.TrimSpace(token[:slash])
		stepValue, err := strconv.Atoi(strings.TrimSpace(token[slash+1:]))
		if err != nil || stepValue <= 0 {
			return nil, errors.New("invalid step")
		}
		step = stepValue
	}
	start := min
	end := max
	if base != "*" {
		if dash := strings.Index(base, "-"); dash >= 0 {
			left, err := parseCronValue(base[:dash], min, max, sundayAlias)
			if err != nil {
				return nil, err
			}
			right, err := parseCronValue(base[dash+1:], min, max, sundayAlias)
			if err != nil {
				return nil, err
			}
			if left > right {
				return nil, errors.New("invalid range")
			}
			start = left
			end = right
		} else {
			value, err := parseCronValue(base, min, max, sundayAlias)
			if err != nil {
				return nil, err
			}
			start = value
			end = value
		}
	}
	values := make([]int, 0, end-start+1)
	for value := start; value <= end; value += step {
		if sundayAlias && value == 7 {
			values = append(values, 0)
			continue
		}
		values = append(values, value)
	}
	return values, nil
}

func parseCronValue(raw string, min, max int, sundayAlias bool) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, errors.New("invalid value")
	}
	if sundayAlias && value == 7 {
		return 7, nil
	}
	if value < min || value > max {
		return 0, errors.New("value out of range")
	}
	return value, nil
}

func (f cronField) matches(value int) bool {
	if f.any {
		return true
	}
	_, ok := f.allowed[value]
	return ok
}

func (s cronSchedule) matches(t time.Time) bool {
	local := t.In(time.Local)
	if !s.month.matches(int(local.Month())) || !s.hour.matches(local.Hour()) || !s.minute.matches(local.Minute()) {
		return false
	}
	dayOfMonthMatch := s.dayOfMonth.matches(local.Day())
	dayOfWeekMatch := s.dayOfWeek.matches(int(local.Weekday()))
	switch {
	case s.dayOfMonth.any && s.dayOfWeek.any:
		return true
	case s.dayOfMonth.any:
		return dayOfWeekMatch
	case s.dayOfWeek.any:
		return dayOfMonthMatch
	default:
		return dayOfMonthMatch || dayOfWeekMatch
	}
}

func (s cronSchedule) NextAfter(now time.Time) time.Time {
	candidate := now.In(time.Local).Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 60*24*366*5; i++ {
		if s.matches(candidate) {
			return candidate
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}
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
