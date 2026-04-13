package stream

// Context: This file belongs to the SubProto implementation layer around handler.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	"github.com/yttydcs/myflowhub-core/subproto"
)

const (
	defaultWindowBytes   = 256 * 1024
	defaultAckIntervalMs = 500
	privateCtrlTimeout   = 2 * time.Second
)

type deliveryState string

const (
	statePending deliveryState = "pending"
	stateActive  deliveryState = "active"
	stateClosing deliveryState = "closing"
)

type sourceEntry struct {
	desc       sourceDescriptor
	deliveries map[string]struct{}
}

type consumerEntry struct {
	desc       consumerDescriptor
	deliveries map[string]struct{}
}

type producerDelivery struct {
	DeliveryID    string
	TxnID         string
	SourceID      string
	Producer      uint32
	Consumer      uint32
	ConsumerID    string
	Kind          string
	UnitMode      string
	Coordinator   uint32
	State         deliveryState
	WindowBytes   uint32
	AckIntervalMs uint32
	Position      uint64
	AckedPosition uint64
	LastActive    time.Time
}

type consumerDelivery struct {
	DeliveryID       string
	TxnID            string
	SourceID         string
	Producer         uint32
	Consumer         uint32
	ConsumerID       string
	Kind             string
	UnitMode         string
	Coordinator      uint32
	State            deliveryState
	WindowBytes      uint32
	AckIntervalMs    uint32
	ExpectedPosition uint64
	LastAckPosition  uint64
	LastActive       time.Time
}

type deliveryRoute struct {
	DeliveryID    string
	TxnID         string
	Requester     uint32
	Producer      uint32
	SourceID      string
	Consumer      uint32
	ConsumerID    string
	Kind          string
	State         deliveryState
	CreatedAt     time.Time
	LastControlAt time.Time
}

type privateCtrlResp struct {
	Action string
	Data   json.RawMessage
}

type deliveryPrepareReq struct {
	ReqID         string `json:"req_id"`
	TxnID         string `json:"txn_id"`
	DeliveryID    string `json:"delivery_id"`
	Role          string `json:"role"`
	Coordinator   uint32 `json:"coordinator,omitempty"`
	Requester     uint32 `json:"requester,omitempty"`
	Producer      uint32 `json:"producer"`
	SourceID      string `json:"source_id"`
	Consumer      uint32 `json:"consumer"`
	ConsumerID    string `json:"consumer_id"`
	Kind          string `json:"kind,omitempty"`
	UnitMode      string `json:"unit_mode,omitempty"`
	ResumeFrom    uint64 `json:"resume_from,omitempty"`
	WindowBytes   uint32 `json:"window_bytes,omitempty"`
	AckIntervalMs uint32 `json:"ack_interval_ms,omitempty"`
}

type deliveryPrepareResp struct {
	ReqID            string              `json:"req_id"`
	Code             int                 `json:"code"`
	Msg              string              `json:"msg,omitempty"`
	Role             string              `json:"role,omitempty"`
	Source           *sourceDescriptor   `json:"source,omitempty"`
	ConsumerEndpoint *consumerDescriptor `json:"consumer_endpoint,omitempty"`
	StartPosition    uint64              `json:"start_position,omitempty"`
	WindowBytes      uint32              `json:"window_bytes,omitempty"`
	AckIntervalMs    uint32              `json:"ack_interval_ms,omitempty"`
}

type deliveryActivateReq struct {
	ReqID      string `json:"req_id"`
	TxnID      string `json:"txn_id"`
	DeliveryID string `json:"delivery_id"`
	Role       string `json:"role"`
}

type deliveryActivateResp struct {
	ReqID string `json:"req_id"`
	Code  int    `json:"code"`
	Msg   string `json:"msg,omitempty"`
	Role  string `json:"role,omitempty"`
}

type deliveryAbortReq struct {
	ReqID      string `json:"req_id"`
	TxnID      string `json:"txn_id"`
	DeliveryID string `json:"delivery_id"`
	Role       string `json:"role,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type deliveryAbortResp struct {
	ReqID string `json:"req_id"`
	Code  int    `json:"code"`
	Msg   string `json:"msg,omitempty"`
	Role  string `json:"role,omitempty"`
}

type deliveryCloseReq struct {
	ReqID      string `json:"req_id"`
	TxnID      string `json:"txn_id,omitempty"`
	DeliveryID string `json:"delivery_id"`
	Role       string `json:"role,omitempty"`
	Reason     string `json:"reason,omitempty"`
	CloseRoute bool   `json:"close_route,omitempty"`
}

type deliveryCloseResp struct {
	ReqID string `json:"req_id"`
	Code  int    `json:"code"`
	Msg   string `json:"msg,omitempty"`
	Role  string `json:"role,omitempty"`
}

type Handler struct {
	subproto.BaseSubProcess
	log     *slog.Logger
	cfg     core.IConfig
	permCfg *permission.Config

	mu sync.RWMutex

	sources            map[string]*sourceEntry
	consumers          map[string]*consumerEntry
	producerDeliveries map[string]*producerDelivery
	consumerDeliveries map[string]*consumerDelivery
	deliveryRoutes     map[string]*deliveryRoute

	pendingMu   sync.Mutex
	pendingCtrl map[string]chan privateCtrlResp
}

func NewHandler(log *slog.Logger) *Handler {
	return NewHandlerWithConfig(nil, log)
}

func NewHandlerWithConfig(cfg core.IConfig, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{
		log:                log,
		cfg:                cfg,
		permCfg:            permission.NewConfig(cfg),
		sources:            make(map[string]*sourceEntry),
		consumers:          make(map[string]*consumerEntry),
		producerDeliveries: make(map[string]*producerDelivery),
		consumerDeliveries: make(map[string]*consumerDelivery),
		deliveryRoutes:     make(map[string]*deliveryRoute),
		pendingCtrl:        make(map[string]chan privateCtrlResp),
	}
}

func (h *Handler) SubProto() uint8 { return SubProtoStream }

func (h *Handler) Init() bool { return true }

func (h *Handler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	if hdr == nil || len(payload) == 0 {
		return
	}
	switch payload[0] {
	case kindCtrl:
		h.handleCtrl(ctx, conn, hdr, payload)
	case kindData:
		h.handleData(ctx, hdr, payload)
	case kindAck:
		h.handleAck(ctx, hdr, payload)
	}
}

func (h *Handler) handleCtrl(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	if len(payload) < 2 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}

	var msg message
	if err := json.Unmarshal(payload[1:], &msg); err != nil {
		return
	}
	action := strings.ToLower(strings.TrimSpace(msg.Action))
	if h.tryDeliverPendingCtrl(action, msg.Data) {
		return
	}

	switch action {
	case actionAnnounce:
		var req announceReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendAnnounceResp(ctx, hdr, hdr.SourceID(), announceResp{ReqID: req.ReqID, Code: 400, Msg: "invalid announce"})
			return
		}
		h.handleAnnounceRequest(ctx, conn, hdr, payload, req)
	case actionWithdraw:
		var req withdrawReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendWithdrawResp(ctx, hdr, hdr.SourceID(), withdrawResp{ReqID: req.ReqID, Code: 400, Msg: "invalid withdraw"})
			return
		}
		h.handleWithdrawRequest(ctx, conn, hdr, payload, req)
	case actionListSources:
		var req listSourcesReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendListSourcesResp(ctx, hdr, hdr.SourceID(), listSourcesResp{ReqID: req.ReqID, Code: 400, Msg: "invalid list_sources"})
			return
		}
		h.handleListSourcesRequest(ctx, conn, hdr, payload, req)
	case actionGetSource:
		var req getSourceReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendGetSourceResp(ctx, hdr, hdr.SourceID(), getSourceResp{ReqID: req.ReqID, Code: 400, Msg: "invalid get_source"})
			return
		}
		h.handleGetSourceRequest(ctx, conn, hdr, payload, req)
	case actionAnnounceConsumer:
		var req announceConsumerReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendAnnounceConsumerResp(ctx, hdr, hdr.SourceID(), announceConsumerResp{ReqID: req.ReqID, Code: 400, Msg: "invalid announce_consumer"})
			return
		}
		h.handleAnnounceConsumerRequest(ctx, conn, hdr, payload, req)
	case actionWithdrawConsumer:
		var req withdrawConsumerReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendWithdrawConsumerResp(ctx, hdr, hdr.SourceID(), withdrawConsumerResp{ReqID: req.ReqID, Code: 400, Msg: "invalid withdraw_consumer"})
			return
		}
		h.handleWithdrawConsumerRequest(ctx, conn, hdr, payload, req)
	case actionListConsumers:
		var req listConsumersReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendListConsumersResp(ctx, hdr, hdr.SourceID(), listConsumersResp{ReqID: req.ReqID, Code: 400, Msg: "invalid list_consumers"})
			return
		}
		h.handleListConsumersRequest(ctx, conn, hdr, payload, req)
	case actionGetConsumer:
		var req getConsumerReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendGetConsumerResp(ctx, hdr, hdr.SourceID(), getConsumerResp{ReqID: req.ReqID, Code: 400, Msg: "invalid get_consumer"})
			return
		}
		h.handleGetConsumerRequest(ctx, conn, hdr, payload, req)
	case actionSubscribe:
		var req subscribeReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendSubscribeResp(ctx, hdr, hdr.SourceID(), subscribeResp{ReqID: req.ReqID, Code: 400, Msg: "invalid subscribe"})
			return
		}
		h.handleSubscribeRequest(ctx, conn, hdr, payload, req)
	case actionUnsubscribe:
		var req unsubscribeReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendUnsubscribeResp(ctx, hdr, hdr.SourceID(), unsubscribeResp{ReqID: req.ReqID, Code: 400, Msg: "invalid unsubscribe"})
			return
		}
		h.handleUnsubscribeRequest(ctx, conn, hdr, payload, req)
	case actionConnect:
		var req connectReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendConnectResp(ctx, hdr, hdr.SourceID(), connectResp{ReqID: req.ReqID, Code: 400, Msg: "invalid connect"})
			return
		}
		h.handleConnectRequest(ctx, conn, hdr, payload, req)
	case actionDisconnect:
		var req disconnectReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendDisconnectResp(ctx, hdr, hdr.SourceID(), disconnectResp{ReqID: req.ReqID, Code: 400, Msg: "invalid disconnect"})
			return
		}
		h.handleDisconnectRequest(ctx, conn, hdr, payload, req)
	case actionSignal:
		var req signalReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendSignalResp(ctx, hdr, hdr.SourceID(), signalResp{ReqID: req.ReqID, Code: 400, Msg: "invalid signal"})
			return
		}
		h.handleSignalRequest(ctx, conn, hdr, payload, req)
	case actionDeliveryPrepare:
		var req deliveryPrepareReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendPrivateResp(ctx, hdr, hdr.SourceID(), actionDeliveryPrepareResp, deliveryPrepareResp{ReqID: req.ReqID, Code: 400, Msg: "invalid delivery_prepare"})
			return
		}
		h.handleDeliveryPrepareRequest(ctx, hdr, req)
	case actionDeliveryActivate:
		var req deliveryActivateReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendPrivateResp(ctx, hdr, hdr.SourceID(), actionDeliveryActivateResp, deliveryActivateResp{ReqID: req.ReqID, Code: 400, Msg: "invalid delivery_activate"})
			return
		}
		h.handleDeliveryActivateRequest(ctx, hdr, req)
	case actionDeliveryAbort:
		var req deliveryAbortReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendPrivateResp(ctx, hdr, hdr.SourceID(), actionDeliveryAbortResp, deliveryAbortResp{ReqID: req.ReqID, Code: 400, Msg: "invalid delivery_abort"})
			return
		}
		h.handleDeliveryAbortRequest(ctx, hdr, req)
	case actionDeliveryClose:
		var req deliveryCloseReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendPrivateResp(ctx, hdr, hdr.SourceID(), actionDeliveryCloseResp, deliveryCloseResp{ReqID: req.ReqID, Code: 400, Msg: "invalid delivery_close"})
			return
		}
		h.handleDeliveryCloseRequest(ctx, hdr, req)
	case actionAnnounceResp, actionWithdrawResp, actionListSourcesResp, actionGetSourceResp,
		actionAnnounceConsumerResp, actionWithdrawConsumerResp, actionListConsumersResp, actionGetConsumerResp,
		actionSubscribeResp, actionUnsubscribeResp, actionConnectResp, actionDisconnectResp, actionSignalResp,
		actionDeliveryPrepareResp, actionDeliveryActivateResp, actionDeliveryAbortResp, actionDeliveryCloseResp:
		if hdr.TargetID() != srv.NodeID() {
			h.forwardCtrlByHeaderTarget(ctx, hdr, payload)
		}
	default:
		if hdr.TargetID() != srv.NodeID() {
			h.forwardCtrlByHeaderTarget(ctx, hdr, payload)
		}
	}
}

func (h *Handler) handleAnnounceRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, req announceReq) {
	requester := hdr.SourceID()
	target := requester
	if req.Source.Producer != 0 {
		target = req.Source.Producer
	}
	h.routeOwnerRequest(ctx, conn, hdr, payload, requester, permPublish, target,
		func(code int, msg string) {
			h.sendAnnounceResp(ctx, hdr, requester, announceResp{ReqID: req.ReqID, Code: code, Msg: msg})
		},
		func() {
			h.handleAnnounceLocal(ctx, hdr, req)
		},
	)
}

func (h *Handler) handleWithdrawRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, req withdrawReq) {
	requester := hdr.SourceID()
	h.routeOwnerRequest(ctx, conn, hdr, payload, requester, permPublish, requester,
		func(code int, msg string) {
			h.sendWithdrawResp(ctx, hdr, requester, withdrawResp{ReqID: req.ReqID, Code: code, Msg: msg, SourceID: strings.TrimSpace(req.SourceID)})
		},
		func() {
			h.handleWithdrawLocal(ctx, hdr, req)
		},
	)
}

func (h *Handler) handleListSourcesRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, req listSourcesReq) {
	requester := hdr.SourceID()
	h.routeOwnerRequest(ctx, conn, hdr, payload, requester, permSubscribe, req.Producer,
		func(code int, msg string) {
			h.sendListSourcesResp(ctx, hdr, requester, listSourcesResp{ReqID: req.ReqID, Code: code, Msg: msg, Producer: req.Producer})
		},
		func() {
			h.handleListSourcesLocal(ctx, hdr, req)
		},
	)
}

func (h *Handler) handleGetSourceRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, req getSourceReq) {
	requester := hdr.SourceID()
	h.routeOwnerRequest(ctx, conn, hdr, payload, requester, permSubscribe, req.Producer,
		func(code int, msg string) {
			h.sendGetSourceResp(ctx, hdr, requester, getSourceResp{ReqID: req.ReqID, Code: code, Msg: msg})
		},
		func() {
			h.handleGetSourceLocal(ctx, hdr, req)
		},
	)
}

func (h *Handler) handleAnnounceConsumerRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, req announceConsumerReq) {
	requester := hdr.SourceID()
	target := requester
	if req.ConsumerEndpoint.Consumer != 0 {
		target = req.ConsumerEndpoint.Consumer
	}
	h.routeOwnerRequest(ctx, conn, hdr, payload, requester, permConsume, target,
		func(code int, msg string) {
			h.sendAnnounceConsumerResp(ctx, hdr, requester, announceConsumerResp{ReqID: req.ReqID, Code: code, Msg: msg})
		},
		func() {
			h.handleAnnounceConsumerLocal(ctx, hdr, req)
		},
	)
}

func (h *Handler) handleWithdrawConsumerRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, req withdrawConsumerReq) {
	requester := hdr.SourceID()
	h.routeOwnerRequest(ctx, conn, hdr, payload, requester, permConsume, requester,
		func(code int, msg string) {
			h.sendWithdrawConsumerResp(ctx, hdr, requester, withdrawConsumerResp{ReqID: req.ReqID, Code: code, Msg: msg, ConsumerID: strings.TrimSpace(req.ConsumerID)})
		},
		func() {
			h.handleWithdrawConsumerLocal(ctx, hdr, req)
		},
	)
}

func (h *Handler) handleListConsumersRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, req listConsumersReq) {
	requester := hdr.SourceID()
	h.routeOwnerRequest(ctx, conn, hdr, payload, requester, permConnect, req.Consumer,
		func(code int, msg string) {
			h.sendListConsumersResp(ctx, hdr, requester, listConsumersResp{ReqID: req.ReqID, Code: code, Msg: msg, Consumer: req.Consumer})
		},
		func() {
			h.handleListConsumersLocal(ctx, hdr, req)
		},
	)
}

func (h *Handler) handleGetConsumerRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, req getConsumerReq) {
	requester := hdr.SourceID()
	h.routeOwnerRequest(ctx, conn, hdr, payload, requester, permConnect, req.Consumer,
		func(code int, msg string) {
			h.sendGetConsumerResp(ctx, hdr, requester, getConsumerResp{ReqID: req.ReqID, Code: code, Msg: msg})
		},
		func() {
			h.handleGetConsumerLocal(ctx, hdr, req)
		},
	)
}

func (h *Handler) handleSubscribeRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, req subscribeReq) {
	requester := hdr.SourceID()
	h.routeCoordinatorRequest(ctx, conn, hdr, payload, requester, permSubscribe, req.Producer, requester,
		func(code int, msg string) {
			h.sendSubscribeResp(ctx, hdr, requester, subscribeResp{ReqID: req.ReqID, Code: code, Msg: msg})
		},
		func() {
			h.handleSubscribeCoordinatorLocal(ctx, hdr, req)
		},
	)
}

func (h *Handler) handleUnsubscribeRequest(ctx context.Context, _ core.IConnection, hdr core.IHeader, payload []byte, req unsubscribeReq) {
	requester := hdr.SourceID()
	h.routeDeliveryRequest(ctx, hdr, payload, requester, permSubscribe, strings.TrimSpace(req.DeliveryID),
		func(code int, msg string) {
			h.sendUnsubscribeResp(ctx, hdr, requester, unsubscribeResp{ReqID: req.ReqID, Code: code, Msg: msg, DeliveryID: strings.TrimSpace(req.DeliveryID), Reason: strings.TrimSpace(req.Reason)})
		},
		func() {
			h.handleUnsubscribeCoordinatorLocal(ctx, hdr, req)
		},
	)
}

func (h *Handler) handleConnectRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, req connectReq) {
	requester := hdr.SourceID()
	h.routeCoordinatorRequest(ctx, conn, hdr, payload, requester, permConnect, req.Producer, req.Consumer,
		func(code int, msg string) {
			h.sendConnectResp(ctx, hdr, requester, connectResp{ReqID: req.ReqID, Code: code, Msg: msg})
		},
		func() {
			h.handleConnectCoordinatorLocal(ctx, hdr, req)
		},
	)
}

func (h *Handler) handleDisconnectRequest(ctx context.Context, _ core.IConnection, hdr core.IHeader, payload []byte, req disconnectReq) {
	requester := hdr.SourceID()
	h.routeDeliveryRequest(ctx, hdr, payload, requester, permConnect, strings.TrimSpace(req.DeliveryID),
		func(code int, msg string) {
			h.sendDisconnectResp(ctx, hdr, requester, disconnectResp{ReqID: req.ReqID, Code: code, Msg: msg, DeliveryID: strings.TrimSpace(req.DeliveryID), Reason: strings.TrimSpace(req.Reason)})
		},
		func() {
			h.handleDisconnectCoordinatorLocal(ctx, hdr, req)
		},
	)
}

func (h *Handler) handleSignalRequest(ctx context.Context, _ core.IConnection, hdr core.IHeader, payload []byte, req signalReq) {
	requester := hdr.SourceID()
	h.routeDeliveryRequest(ctx, hdr, payload, requester, "", strings.TrimSpace(req.DeliveryID),
		func(code int, msg string) {
			h.sendSignalResp(ctx, hdr, requester, signalResp{ReqID: req.ReqID, Code: code, Msg: msg, DeliveryID: strings.TrimSpace(req.DeliveryID), Op: strings.TrimSpace(req.Op)})
		},
		func() {
			h.handleSignalLocal(ctx, hdr, req)
		},
	)
}

func (h *Handler) handleDeliveryPrepareRequest(ctx context.Context, hdr core.IHeader, req deliveryPrepareReq) {
	resp := h.handleDeliveryPrepareLocal(hdr, req)
	h.sendPrivateResp(ctx, hdr, hdr.SourceID(), actionDeliveryPrepareResp, resp)
}

func (h *Handler) handleDeliveryActivateRequest(ctx context.Context, hdr core.IHeader, req deliveryActivateReq) {
	resp := h.handleDeliveryActivateLocal(req)
	h.sendPrivateResp(ctx, hdr, hdr.SourceID(), actionDeliveryActivateResp, resp)
}

func (h *Handler) handleDeliveryAbortRequest(ctx context.Context, hdr core.IHeader, req deliveryAbortReq) {
	resp := h.handleDeliveryAbortLocal(req)
	h.sendPrivateResp(ctx, hdr, hdr.SourceID(), actionDeliveryAbortResp, resp)
}

func (h *Handler) handleDeliveryCloseRequest(ctx context.Context, hdr core.IHeader, req deliveryCloseReq) {
	resp := h.handleDeliveryCloseLocal(req)
	h.sendPrivateResp(ctx, hdr, hdr.SourceID(), actionDeliveryCloseResp, resp)
}

func (h *Handler) handleAnnounceLocal(ctx context.Context, hdr core.IHeader, req announceReq) {
	requester := hdr.SourceID()
	desc, code, msg := normalizeSourceDescriptor(req.Source, requester)
	if code != 0 {
		h.sendAnnounceResp(ctx, hdr, requester, announceResp{ReqID: req.ReqID, Code: code, Msg: msg})
		return
	}

	h.mu.Lock()
	existing, ok := h.sources[desc.SourceID]
	if ok {
		if !sameSourceDescriptor(existing.desc, desc) {
			h.mu.Unlock()
			h.sendAnnounceResp(ctx, hdr, requester, announceResp{ReqID: req.ReqID, Code: 409, Msg: "source conflict"})
			return
		}
		existing.desc = desc
	} else {
		h.sources[desc.SourceID] = &sourceEntry{desc: desc, deliveries: make(map[string]struct{})}
	}
	h.mu.Unlock()

	out := cloneSourceDescriptor(desc)
	h.sendAnnounceResp(ctx, hdr, requester, announceResp{ReqID: req.ReqID, Code: 1, Msg: "ok", Source: &out})
}

func (h *Handler) handleWithdrawLocal(ctx context.Context, hdr core.IHeader, req withdrawReq) {
	requester := hdr.SourceID()
	sourceID := strings.TrimSpace(req.SourceID)
	if sourceID == "" {
		h.sendWithdrawResp(ctx, hdr, requester, withdrawResp{ReqID: req.ReqID, Code: 400, Msg: "source_id required"})
		return
	}

	var deliveries []producerDelivery
	h.mu.Lock()
	entry, ok := h.sources[sourceID]
	if !ok || entry == nil {
		h.mu.Unlock()
		h.sendWithdrawResp(ctx, hdr, requester, withdrawResp{ReqID: req.ReqID, Code: 404, Msg: "source not found", SourceID: sourceID})
		return
	}
	for deliveryID := range entry.deliveries {
		if pd, ok := h.producerDeliveries[deliveryID]; ok && pd != nil {
			deliveries = append(deliveries, *pd)
		}
	}
	delete(h.sources, sourceID)
	h.mu.Unlock()

	for _, delivery := range deliveries {
		h.bestEffortCloseLocalProducer(ctx, delivery, "source withdrawn")
	}

	h.sendWithdrawResp(ctx, hdr, requester, withdrawResp{ReqID: req.ReqID, Code: 1, Msg: "ok", SourceID: sourceID})
}

func (h *Handler) handleListSourcesLocal(ctx context.Context, hdr core.IHeader, req listSourcesReq) {
	requester := hdr.SourceID()
	kindFilter := strings.TrimSpace(req.Kind)
	tagFilter := strings.TrimSpace(req.Tag)

	var items []sourceDescriptor
	h.mu.RLock()
	for _, entry := range h.sources {
		if entry == nil {
			continue
		}
		if req.Producer != 0 && entry.desc.Producer != req.Producer {
			continue
		}
		if kindFilter != "" && entry.desc.Kind != kindFilter {
			continue
		}
		if tagFilter != "" && !containsString(entry.desc.Tags, tagFilter) {
			continue
		}
		items = append(items, cloneSourceDescriptor(entry.desc))
	}
	h.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].SourceID < items[j].SourceID })

	h.sendListSourcesResp(ctx, hdr, requester, listSourcesResp{
		ReqID:    req.ReqID,
		Code:     1,
		Msg:      "ok",
		Producer: req.Producer,
		Sources:  items,
	})
}

func (h *Handler) handleGetSourceLocal(ctx context.Context, hdr core.IHeader, req getSourceReq) {
	requester := hdr.SourceID()
	sourceID := strings.TrimSpace(req.SourceID)
	if sourceID == "" {
		h.sendGetSourceResp(ctx, hdr, requester, getSourceResp{ReqID: req.ReqID, Code: 400, Msg: "source_id required"})
		return
	}

	h.mu.RLock()
	entry := h.sources[sourceID]
	h.mu.RUnlock()
	if entry == nil || (req.Producer != 0 && entry.desc.Producer != req.Producer) {
		h.sendGetSourceResp(ctx, hdr, requester, getSourceResp{ReqID: req.ReqID, Code: 404, Msg: "source not found"})
		return
	}
	out := cloneSourceDescriptor(entry.desc)
	h.sendGetSourceResp(ctx, hdr, requester, getSourceResp{ReqID: req.ReqID, Code: 1, Msg: "ok", Source: &out})
}

func (h *Handler) handleAnnounceConsumerLocal(ctx context.Context, hdr core.IHeader, req announceConsumerReq) {
	requester := hdr.SourceID()
	desc, code, msg := normalizeConsumerDescriptor(req.ConsumerEndpoint, requester)
	if code != 0 {
		h.sendAnnounceConsumerResp(ctx, hdr, requester, announceConsumerResp{ReqID: req.ReqID, Code: code, Msg: msg})
		return
	}

	h.mu.Lock()
	existing, ok := h.consumers[desc.ConsumerID]
	if ok {
		if !sameConsumerDescriptor(existing.desc, desc) {
			h.mu.Unlock()
			h.sendAnnounceConsumerResp(ctx, hdr, requester, announceConsumerResp{ReqID: req.ReqID, Code: 409, Msg: "consumer conflict"})
			return
		}
		existing.desc = desc
	} else {
		h.consumers[desc.ConsumerID] = &consumerEntry{desc: desc, deliveries: make(map[string]struct{})}
	}
	h.mu.Unlock()

	out := cloneConsumerDescriptor(desc)
	h.sendAnnounceConsumerResp(ctx, hdr, requester, announceConsumerResp{ReqID: req.ReqID, Code: 1, Msg: "ok", ConsumerEndpoint: &out})
}

func (h *Handler) handleWithdrawConsumerLocal(ctx context.Context, hdr core.IHeader, req withdrawConsumerReq) {
	requester := hdr.SourceID()
	consumerID := strings.TrimSpace(req.ConsumerID)
	if consumerID == "" {
		h.sendWithdrawConsumerResp(ctx, hdr, requester, withdrawConsumerResp{ReqID: req.ReqID, Code: 400, Msg: "consumer_id required"})
		return
	}

	var deliveries []consumerDelivery
	h.mu.Lock()
	entry, ok := h.consumers[consumerID]
	if !ok || entry == nil {
		h.mu.Unlock()
		h.sendWithdrawConsumerResp(ctx, hdr, requester, withdrawConsumerResp{ReqID: req.ReqID, Code: 404, Msg: "consumer not found", ConsumerID: consumerID})
		return
	}
	for deliveryID := range entry.deliveries {
		if cd, ok := h.consumerDeliveries[deliveryID]; ok && cd != nil {
			deliveries = append(deliveries, *cd)
		}
	}
	delete(h.consumers, consumerID)
	h.mu.Unlock()

	for _, delivery := range deliveries {
		h.bestEffortCloseLocalConsumer(ctx, delivery, "consumer withdrawn")
	}

	h.sendWithdrawConsumerResp(ctx, hdr, requester, withdrawConsumerResp{ReqID: req.ReqID, Code: 1, Msg: "ok", ConsumerID: consumerID})
}

func (h *Handler) handleListConsumersLocal(ctx context.Context, hdr core.IHeader, req listConsumersReq) {
	requester := hdr.SourceID()
	kindFilter := strings.TrimSpace(req.Kind)
	tagFilter := strings.TrimSpace(req.Tag)

	var items []consumerDescriptor
	h.mu.RLock()
	for _, entry := range h.consumers {
		if entry == nil {
			continue
		}
		if req.Consumer != 0 && entry.desc.Consumer != req.Consumer {
			continue
		}
		if kindFilter != "" && entry.desc.Kind != kindFilter {
			continue
		}
		if tagFilter != "" && !containsString(entry.desc.Tags, tagFilter) {
			continue
		}
		items = append(items, cloneConsumerDescriptor(entry.desc))
	}
	h.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].ConsumerID < items[j].ConsumerID })

	h.sendListConsumersResp(ctx, hdr, requester, listConsumersResp{
		ReqID:             req.ReqID,
		Code:              1,
		Msg:               "ok",
		Consumer:          req.Consumer,
		ConsumerEndpoints: items,
	})
}

func (h *Handler) handleGetConsumerLocal(ctx context.Context, hdr core.IHeader, req getConsumerReq) {
	requester := hdr.SourceID()
	consumerID := strings.TrimSpace(req.ConsumerID)
	if consumerID == "" {
		h.sendGetConsumerResp(ctx, hdr, requester, getConsumerResp{ReqID: req.ReqID, Code: 400, Msg: "consumer_id required"})
		return
	}

	h.mu.RLock()
	entry := h.consumers[consumerID]
	h.mu.RUnlock()
	if entry == nil || (req.Consumer != 0 && entry.desc.Consumer != req.Consumer) {
		h.sendGetConsumerResp(ctx, hdr, requester, getConsumerResp{ReqID: req.ReqID, Code: 404, Msg: "consumer not found"})
		return
	}
	out := cloneConsumerDescriptor(entry.desc)
	h.sendGetConsumerResp(ctx, hdr, requester, getConsumerResp{ReqID: req.ReqID, Code: 1, Msg: "ok", ConsumerEndpoint: &out})
}

func (h *Handler) handleSubscribeCoordinatorLocal(ctx context.Context, hdr core.IHeader, req subscribeReq) {
	requester := hdr.SourceID()
	if strings.TrimSpace(req.ReqID) == "" {
		h.sendSubscribeResp(ctx, hdr, requester, subscribeResp{ReqID: req.ReqID, Code: 400, Msg: "req_id required"})
		return
	}
	if req.Producer == 0 || strings.TrimSpace(req.SourceID) == "" || strings.TrimSpace(req.ConsumerID) == "" {
		h.sendSubscribeResp(ctx, hdr, requester, subscribeResp{ReqID: req.ReqID, Code: 400, Msg: "invalid subscribe"})
		return
	}

	resp, code, msg := h.establishDelivery(ctx, requester, req.Producer, strings.TrimSpace(req.SourceID), requester, strings.TrimSpace(req.ConsumerID), req.ResumeFrom, req.WindowBytes, req.AckIntervalMs)
	if code != 1 {
		h.sendSubscribeResp(ctx, hdr, requester, subscribeResp{ReqID: req.ReqID, Code: code, Msg: msg})
		return
	}
	h.sendSubscribeResp(ctx, hdr, requester, subscribeResp{
		ReqID:            req.ReqID,
		Code:             resp.Code,
		Msg:              resp.Msg,
		Accept:           resp.Accept,
		Source:           resp.Source,
		ConsumerEndpoint: resp.ConsumerEndpoint,
		DeliveryID:       resp.DeliveryID,
		Producer:         resp.Producer,
		Consumer:         resp.Consumer,
		ConsumerID:       resp.ConsumerID,
		StartPosition:    resp.StartPosition,
		WindowBytes:      resp.WindowBytes,
		AckIntervalMs:    resp.AckIntervalMs,
	})
}

func (h *Handler) handleConnectCoordinatorLocal(ctx context.Context, hdr core.IHeader, req connectReq) {
	requester := hdr.SourceID()
	if strings.TrimSpace(req.ReqID) == "" {
		h.sendConnectResp(ctx, hdr, requester, connectResp{ReqID: req.ReqID, Code: 400, Msg: "req_id required"})
		return
	}
	if req.Producer == 0 || req.Consumer == 0 || strings.TrimSpace(req.SourceID) == "" || strings.TrimSpace(req.ConsumerID) == "" {
		h.sendConnectResp(ctx, hdr, requester, connectResp{ReqID: req.ReqID, Code: 400, Msg: "invalid connect"})
		return
	}

	resp, code, msg := h.establishDelivery(ctx, requester, req.Producer, strings.TrimSpace(req.SourceID), req.Consumer, strings.TrimSpace(req.ConsumerID), req.ResumeFrom, req.WindowBytes, req.AckIntervalMs)
	if code != 1 {
		h.sendConnectResp(ctx, hdr, requester, connectResp{ReqID: req.ReqID, Code: code, Msg: msg})
		return
	}
	resp.ReqID = req.ReqID
	h.sendConnectResp(ctx, hdr, requester, resp)
}

func (h *Handler) establishDelivery(ctx context.Context, requester, producer uint32, sourceID string, consumer uint32, consumerID string, resumeFrom uint64, windowBytes, ackIntervalMs uint32) (connectResp, int, string) {
	deliveryUUID, err := newUUID()
	if err != nil {
		return connectResp{}, 500, "uuid failed"
	}
	txnUUID, err := newUUID()
	if err != nil {
		return connectResp{}, 500, "uuid failed"
	}
	deliveryID := uuidToString(deliveryUUID)
	txnID := uuidToString(txnUUID)
	coordinator := requester
	if srv := core.ServerFromContext(ctx); srv != nil && srv.NodeID() != 0 {
		coordinator = srv.NodeID()
	}

	prepareProducerReq := deliveryPrepareReq{
		ReqID:         uuidToString(txnUUID),
		TxnID:         txnID,
		DeliveryID:    deliveryID,
		Role:          "producer",
		Coordinator:   coordinator,
		Requester:     requester,
		Producer:      producer,
		SourceID:      sourceID,
		Consumer:      consumer,
		ConsumerID:    consumerID,
		ResumeFrom:    resumeFrom,
		WindowBytes:   coalesceWindowBytes(windowBytes),
		AckIntervalMs: coalesceAckIntervalMs(ackIntervalMs),
	}
	producerResp, code, msg := h.prepareDelivery(ctx, producer, prepareProducerReq)
	if code != 1 {
		return connectResp{}, code, msg
	}

	prepareConsumerReq := prepareProducerReq
	prepareConsumerReq.ReqID = uuidToString(txnUUID)
	prepareConsumerReq.Role = "consumer"
	if producerResp.Source != nil {
		prepareConsumerReq.Kind = producerResp.Source.Kind
		prepareConsumerReq.UnitMode = producerResp.Source.UnitMode
	}
	consumerResp, code, msg := h.prepareDelivery(ctx, consumer, prepareConsumerReq)
	if code != 1 {
		h.abortDelivery(ctx, producer, deliveryAbortReq{
			ReqID:      prepareConsumerReq.ReqID,
			TxnID:      txnID,
			DeliveryID: deliveryID,
			Role:       "producer",
			Reason:     msg,
		})
		return connectResp{}, code, msg
	}

	if producerResp.Source == nil || consumerResp.ConsumerEndpoint == nil {
		h.abortDelivery(ctx, producer, deliveryAbortReq{ReqID: prepareConsumerReq.ReqID, TxnID: txnID, DeliveryID: deliveryID, Role: "producer", Reason: "prepare incomplete"})
		h.abortDelivery(ctx, consumer, deliveryAbortReq{ReqID: prepareConsumerReq.ReqID, TxnID: txnID, DeliveryID: deliveryID, Role: "consumer", Reason: "prepare incomplete"})
		return connectResp{}, 500, "prepare incomplete"
	}
	if producerResp.Source.Kind != consumerResp.ConsumerEndpoint.Kind {
		h.abortDelivery(ctx, producer, deliveryAbortReq{ReqID: prepareConsumerReq.ReqID, TxnID: txnID, DeliveryID: deliveryID, Role: "producer", Reason: "kind mismatch"})
		h.abortDelivery(ctx, consumer, deliveryAbortReq{ReqID: prepareConsumerReq.ReqID, TxnID: txnID, DeliveryID: deliveryID, Role: "consumer", Reason: "kind mismatch"})
		return connectResp{}, 406, "kind mismatch"
	}

	h.mu.Lock()
	h.deliveryRoutes[deliveryID] = &deliveryRoute{
		DeliveryID:    deliveryID,
		TxnID:         txnID,
		Requester:     requester,
		Producer:      producer,
		SourceID:      sourceID,
		Consumer:      consumer,
		ConsumerID:    consumerID,
		Kind:          producerResp.Source.Kind,
		State:         statePending,
		CreatedAt:     time.Now(),
		LastControlAt: time.Now(),
	}
	h.mu.Unlock()

	activateReq := deliveryActivateReq{
		ReqID:      uuidToString(txnUUID),
		TxnID:      txnID,
		DeliveryID: deliveryID,
		Role:       "producer",
	}
	if code, msg = h.activateDelivery(ctx, producer, activateReq); code != 1 {
		h.abortDelivery(ctx, producer, deliveryAbortReq{ReqID: activateReq.ReqID, TxnID: txnID, DeliveryID: deliveryID, Role: "producer", Reason: msg})
		h.abortDelivery(ctx, consumer, deliveryAbortReq{ReqID: activateReq.ReqID, TxnID: txnID, DeliveryID: deliveryID, Role: "consumer", Reason: msg})
		h.removeRoute(deliveryID)
		return connectResp{}, code, msg
	}
	activateReq.ReqID = uuidToString(txnUUID)
	activateReq.Role = "consumer"
	if code, msg = h.activateDelivery(ctx, consumer, activateReq); code != 1 {
		h.closeDelivery(ctx, producer, deliveryCloseReq{ReqID: activateReq.ReqID, TxnID: txnID, DeliveryID: deliveryID, Role: "producer", Reason: msg})
		h.abortDelivery(ctx, consumer, deliveryAbortReq{ReqID: activateReq.ReqID, TxnID: txnID, DeliveryID: deliveryID, Role: "consumer", Reason: msg})
		h.removeRoute(deliveryID)
		return connectResp{}, code, msg
	}

	h.mu.Lock()
	if route := h.deliveryRoutes[deliveryID]; route != nil {
		route.State = stateActive
		route.LastControlAt = time.Now()
	}
	h.mu.Unlock()

	sourceOut := cloneSourceDescriptor(*producerResp.Source)
	consumerOut := cloneConsumerDescriptor(*consumerResp.ConsumerEndpoint)
	return connectResp{
		Code:             1,
		Msg:              "ok",
		Accept:           true,
		Source:           &sourceOut,
		ConsumerEndpoint: &consumerOut,
		DeliveryID:       deliveryID,
		Producer:         producer,
		Consumer:         consumer,
		ConsumerID:       consumerID,
		StartPosition:    producerResp.StartPosition,
		WindowBytes:      producerResp.WindowBytes,
		AckIntervalMs:    producerResp.AckIntervalMs,
	}, 1, "ok"
}

func (h *Handler) handleUnsubscribeCoordinatorLocal(ctx context.Context, hdr core.IHeader, req unsubscribeReq) {
	requester := hdr.SourceID()
	route, ok := h.getRoute(strings.TrimSpace(req.DeliveryID))
	if !ok {
		h.sendUnsubscribeResp(ctx, hdr, requester, unsubscribeResp{ReqID: req.ReqID, Code: 404, Msg: "delivery not found", DeliveryID: strings.TrimSpace(req.DeliveryID), Reason: strings.TrimSpace(req.Reason)})
		return
	}
	if requester != route.Consumer && requester != route.Requester {
		h.sendUnsubscribeResp(ctx, hdr, requester, unsubscribeResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", DeliveryID: route.DeliveryID})
		return
	}
	code, msg := h.closeDeliveryRoute(ctx, route, strings.TrimSpace(req.Reason))
	h.sendUnsubscribeResp(ctx, hdr, requester, unsubscribeResp{ReqID: req.ReqID, Code: code, Msg: msg, DeliveryID: route.DeliveryID, Reason: strings.TrimSpace(req.Reason)})
}

func (h *Handler) handleDisconnectCoordinatorLocal(ctx context.Context, hdr core.IHeader, req disconnectReq) {
	requester := hdr.SourceID()
	route, ok := h.getRoute(strings.TrimSpace(req.DeliveryID))
	if !ok {
		h.sendDisconnectResp(ctx, hdr, requester, disconnectResp{ReqID: req.ReqID, Code: 404, Msg: "delivery not found", DeliveryID: strings.TrimSpace(req.DeliveryID), Reason: strings.TrimSpace(req.Reason)})
		return
	}
	code, msg := h.closeDeliveryRoute(ctx, route, strings.TrimSpace(req.Reason))
	h.sendDisconnectResp(ctx, hdr, requester, disconnectResp{ReqID: req.ReqID, Code: code, Msg: msg, DeliveryID: route.DeliveryID, Reason: strings.TrimSpace(req.Reason)})
}

func (h *Handler) closeDeliveryRoute(ctx context.Context, route deliveryRoute, reason string) (int, string) {
	h.mu.Lock()
	if current := h.deliveryRoutes[route.DeliveryID]; current != nil {
		current.State = stateClosing
		current.LastControlAt = time.Now()
	}
	h.mu.Unlock()

	reqID := route.TxnID
	if reqID == "" {
		reqID = route.DeliveryID
	}
	if code, msg := h.closeDelivery(ctx, route.Producer, deliveryCloseReq{
		ReqID:      reqID,
		TxnID:      route.TxnID,
		DeliveryID: route.DeliveryID,
		Role:       "producer",
		Reason:     reason,
	}); code != 1 {
		h.removeRoute(route.DeliveryID)
		return code, msg
	}
	if code, msg := h.closeDelivery(ctx, route.Consumer, deliveryCloseReq{
		ReqID:      reqID,
		TxnID:      route.TxnID,
		DeliveryID: route.DeliveryID,
		Role:       "consumer",
		Reason:     reason,
	}); code != 1 {
		h.removeRoute(route.DeliveryID)
		return code, msg
	}

	h.removeRoute(route.DeliveryID)
	return 1, "ok"
}

func (h *Handler) handleSignalLocal(ctx context.Context, hdr core.IHeader, req signalReq) {
	requester := hdr.SourceID()
	deliveryID := strings.TrimSpace(req.DeliveryID)
	op := strings.TrimSpace(req.Op)
	if deliveryID == "" || op == "" {
		h.sendSignalResp(ctx, hdr, requester, signalResp{ReqID: req.ReqID, Code: 400, Msg: "invalid signal", DeliveryID: deliveryID, Op: op})
		return
	}
	if !isValidSignalOp(op) {
		h.sendSignalResp(ctx, hdr, requester, signalResp{ReqID: req.ReqID, Code: 406, Msg: "unsupported signal", DeliveryID: deliveryID, Op: op})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if pd := h.producerDeliveries[deliveryID]; pd != nil {
		if requester != pd.Producer && requester != pd.Consumer {
			h.sendSignalResp(ctx, hdr, requester, signalResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", DeliveryID: deliveryID, Op: op})
			return
		}
		pd.LastActive = time.Now()
		h.sendSignalResp(ctx, hdr, requester, signalResp{ReqID: req.ReqID, Code: 1, Msg: "ok", DeliveryID: deliveryID, Op: op})
		return
	}
	if cd := h.consumerDeliveries[deliveryID]; cd != nil {
		if requester != cd.Producer && requester != cd.Consumer {
			h.sendSignalResp(ctx, hdr, requester, signalResp{ReqID: req.ReqID, Code: 403, Msg: "permission denied", DeliveryID: deliveryID, Op: op})
			return
		}
		cd.LastActive = time.Now()
		h.sendSignalResp(ctx, hdr, requester, signalResp{ReqID: req.ReqID, Code: 1, Msg: "ok", DeliveryID: deliveryID, Op: op})
		return
	}
	if route := h.deliveryRoutes[deliveryID]; route != nil {
		route.LastControlAt = time.Now()
		h.sendSignalResp(ctx, hdr, requester, signalResp{ReqID: req.ReqID, Code: 1, Msg: "ok", DeliveryID: deliveryID, Op: op})
		return
	}
	h.sendSignalResp(ctx, hdr, requester, signalResp{ReqID: req.ReqID, Code: 404, Msg: "delivery not found", DeliveryID: deliveryID, Op: op})
}

func (h *Handler) handleDeliveryPrepareLocal(hdr core.IHeader, req deliveryPrepareReq) deliveryPrepareResp {
	if strings.TrimSpace(req.ReqID) == "" || strings.TrimSpace(req.TxnID) == "" {
		return deliveryPrepareResp{ReqID: req.ReqID, Code: 400, Msg: "req_id and txn_id required", Role: req.Role}
	}
	localNode := uint32(0)
	if hdr != nil {
		localNode = hdr.TargetID()
	}
	if localNode == 0 {
		localNode = req.Producer
		if req.Role == "consumer" {
			localNode = req.Consumer
		}
	}
	if req.Coordinator == 0 && hdr != nil {
		req.Coordinator = hdr.SourceID()
	}

	switch strings.TrimSpace(req.Role) {
	case "producer":
		return h.prepareProducerLocal(localNode, req)
	case "consumer":
		return h.prepareConsumerLocal(localNode, req)
	default:
		return deliveryPrepareResp{ReqID: req.ReqID, Code: 400, Msg: "invalid role", Role: req.Role}
	}
}

func (h *Handler) prepareProducerLocal(localNode uint32, req deliveryPrepareReq) deliveryPrepareResp {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry := h.sources[strings.TrimSpace(req.SourceID)]
	if entry == nil || entry.desc.Producer != localNode {
		return deliveryPrepareResp{ReqID: req.ReqID, Code: 404, Msg: "source not found", Role: req.Role}
	}
	if existing := h.producerDeliveries[req.DeliveryID]; existing != nil {
		if existing.TxnID != req.TxnID || existing.SourceID != strings.TrimSpace(req.SourceID) || existing.Consumer != req.Consumer || existing.ConsumerID != strings.TrimSpace(req.ConsumerID) {
			return deliveryPrepareResp{ReqID: req.ReqID, Code: 409, Msg: "delivery conflict", Role: req.Role}
		}
		sourceOut := cloneSourceDescriptor(entry.desc)
		return deliveryPrepareResp{
			ReqID:         req.ReqID,
			Code:          1,
			Msg:           "ok",
			Role:          req.Role,
			Source:        &sourceOut,
			StartPosition: existing.Position,
			WindowBytes:   existing.WindowBytes,
			AckIntervalMs: existing.AckIntervalMs,
		}
	}

	windowBytes := coalesceWindowBytes(req.WindowBytes)
	ackIntervalMs := coalesceAckIntervalMs(req.AckIntervalMs)
	h.producerDeliveries[req.DeliveryID] = &producerDelivery{
		DeliveryID:    req.DeliveryID,
		TxnID:         req.TxnID,
		SourceID:      strings.TrimSpace(req.SourceID),
		Producer:      entry.desc.Producer,
		Consumer:      req.Consumer,
		ConsumerID:    strings.TrimSpace(req.ConsumerID),
		Kind:          entry.desc.Kind,
		UnitMode:      entry.desc.UnitMode,
		Coordinator:   req.Coordinator,
		State:         statePending,
		WindowBytes:   windowBytes,
		AckIntervalMs: ackIntervalMs,
		Position:      req.ResumeFrom,
		AckedPosition: req.ResumeFrom,
		LastActive:    time.Now(),
	}
	entry.deliveries[req.DeliveryID] = struct{}{}

	sourceOut := cloneSourceDescriptor(entry.desc)
	return deliveryPrepareResp{
		ReqID:         req.ReqID,
		Code:          1,
		Msg:           "ok",
		Role:          req.Role,
		Source:        &sourceOut,
		StartPosition: req.ResumeFrom,
		WindowBytes:   windowBytes,
		AckIntervalMs: ackIntervalMs,
	}
}

func (h *Handler) prepareConsumerLocal(localNode uint32, req deliveryPrepareReq) deliveryPrepareResp {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry := h.consumers[strings.TrimSpace(req.ConsumerID)]
	if entry == nil || entry.desc.Consumer != localNode {
		return deliveryPrepareResp{ReqID: req.ReqID, Code: 404, Msg: "consumer not found", Role: req.Role}
	}
	if kind := strings.TrimSpace(req.Kind); kind != "" && entry.desc.Kind != kind {
		return deliveryPrepareResp{ReqID: req.ReqID, Code: 406, Msg: "kind mismatch", Role: req.Role}
	}
	if existing := h.consumerDeliveries[req.DeliveryID]; existing != nil {
		if existing.TxnID != req.TxnID || existing.Producer != req.Producer || existing.ConsumerID != strings.TrimSpace(req.ConsumerID) {
			return deliveryPrepareResp{ReqID: req.ReqID, Code: 409, Msg: "delivery conflict", Role: req.Role}
		}
		consumerOut := cloneConsumerDescriptor(entry.desc)
		return deliveryPrepareResp{
			ReqID:            req.ReqID,
			Code:             1,
			Msg:              "ok",
			Role:             req.Role,
			ConsumerEndpoint: &consumerOut,
			StartPosition:    existing.ExpectedPosition,
			WindowBytes:      existing.WindowBytes,
			AckIntervalMs:    existing.AckIntervalMs,
		}
	}

	windowBytes := coalesceWindowBytes(req.WindowBytes)
	ackIntervalMs := coalesceAckIntervalMs(req.AckIntervalMs)
	unitMode := strings.TrimSpace(req.UnitMode)
	if unitMode == "" {
		unitMode = unitModeChunk
	}
	h.consumerDeliveries[req.DeliveryID] = &consumerDelivery{
		DeliveryID:       req.DeliveryID,
		TxnID:            req.TxnID,
		SourceID:         strings.TrimSpace(req.SourceID),
		Producer:         req.Producer,
		Consumer:         entry.desc.Consumer,
		ConsumerID:       strings.TrimSpace(req.ConsumerID),
		Kind:             entry.desc.Kind,
		UnitMode:         unitMode,
		Coordinator:      req.Coordinator,
		State:            statePending,
		WindowBytes:      windowBytes,
		AckIntervalMs:    ackIntervalMs,
		ExpectedPosition: req.ResumeFrom,
		LastActive:       time.Now(),
	}
	entry.deliveries[req.DeliveryID] = struct{}{}

	consumerOut := cloneConsumerDescriptor(entry.desc)
	return deliveryPrepareResp{
		ReqID:            req.ReqID,
		Code:             1,
		Msg:              "ok",
		Role:             req.Role,
		ConsumerEndpoint: &consumerOut,
		StartPosition:    req.ResumeFrom,
		WindowBytes:      windowBytes,
		AckIntervalMs:    ackIntervalMs,
	}
}

func (h *Handler) handleDeliveryActivateLocal(req deliveryActivateReq) deliveryActivateResp {
	deliveryID := strings.TrimSpace(req.DeliveryID)
	if deliveryID == "" || strings.TrimSpace(req.TxnID) == "" {
		return deliveryActivateResp{ReqID: req.ReqID, Code: 400, Msg: "invalid activate", Role: req.Role}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	switch strings.TrimSpace(req.Role) {
	case "producer":
		pd := h.producerDeliveries[deliveryID]
		if pd == nil || pd.TxnID != req.TxnID {
			return deliveryActivateResp{ReqID: req.ReqID, Code: 404, Msg: "delivery not found", Role: req.Role}
		}
		pd.State = stateActive
		pd.LastActive = time.Now()
	case "consumer":
		cd := h.consumerDeliveries[deliveryID]
		if cd == nil || cd.TxnID != req.TxnID {
			return deliveryActivateResp{ReqID: req.ReqID, Code: 404, Msg: "delivery not found", Role: req.Role}
		}
		cd.State = stateActive
		cd.LastActive = time.Now()
	default:
		return deliveryActivateResp{ReqID: req.ReqID, Code: 400, Msg: "invalid role", Role: req.Role}
	}
	return deliveryActivateResp{ReqID: req.ReqID, Code: 1, Msg: "ok", Role: req.Role}
}

func (h *Handler) handleDeliveryAbortLocal(req deliveryAbortReq) deliveryAbortResp {
	deliveryID := strings.TrimSpace(req.DeliveryID)
	if deliveryID == "" {
		return deliveryAbortResp{ReqID: req.ReqID, Code: 400, Msg: "delivery_id required", Role: req.Role}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	switch strings.TrimSpace(req.Role) {
	case "producer":
		h.removeProducerDeliveryLocked(deliveryID)
	case "consumer":
		h.removeConsumerDeliveryLocked(deliveryID)
	default:
		h.removeProducerDeliveryLocked(deliveryID)
		h.removeConsumerDeliveryLocked(deliveryID)
	}
	return deliveryAbortResp{ReqID: req.ReqID, Code: 1, Msg: "ok", Role: req.Role}
}

func (h *Handler) handleDeliveryCloseLocal(req deliveryCloseReq) deliveryCloseResp {
	deliveryID := strings.TrimSpace(req.DeliveryID)
	if deliveryID == "" {
		return deliveryCloseResp{ReqID: req.ReqID, Code: 400, Msg: "delivery_id required", Role: req.Role}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	switch strings.TrimSpace(req.Role) {
	case "producer":
		h.removeProducerDeliveryLocked(deliveryID)
	case "consumer":
		h.removeConsumerDeliveryLocked(deliveryID)
	default:
		h.removeProducerDeliveryLocked(deliveryID)
		h.removeConsumerDeliveryLocked(deliveryID)
	}
	if req.CloseRoute {
		delete(h.deliveryRoutes, deliveryID)
	}
	return deliveryCloseResp{ReqID: req.ReqID, Code: 1, Msg: "ok", Role: req.Role}
}

func (h *Handler) handleData(ctx context.Context, hdr core.IHeader, payload []byte) {
	if hdr == nil {
		return
	}
	dataHdr, body, ok := decodeDataHeaderV1(payload)
	if !ok || dataHdr.Ver != headerVersionV1 {
		return
	}
	deliveryID := uuidToString(dataHdr.DeliveryID)

	h.mu.Lock()
	cd := h.consumerDeliveries[deliveryID]
	if cd == nil || cd.State != stateActive {
		h.mu.Unlock()
		return
	}
	if hdr.SourceID() != cd.Producer || hdr.TargetID() != cd.Consumer {
		h.mu.Unlock()
		return
	}
	if dataHdr.Position < cd.ExpectedPosition {
		h.mu.Unlock()
		return
	}
	nextPosition := nextExpectedPosition(cd.UnitMode, dataHdr.Position, len(body))
	if nextPosition < cd.ExpectedPosition {
		h.mu.Unlock()
		return
	}
	cd.ExpectedPosition = nextPosition
	cd.LastActive = time.Now()
	cd.LastAckPosition = cd.ExpectedPosition
	producer := cd.Producer
	consumer := cd.Consumer
	h.mu.Unlock()

	ackPayload := encodeAckHeaderV1(dataHdr.DeliveryID, nextPosition, 0, 0)
	ackHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorMsg).
		WithSubProto(SubProtoStream).
		WithSourceID(consumer).
		WithTargetID(producer)
	_ = h.sendToNode(ctx, producer, ackHdr, ackPayload)
}

func (h *Handler) handleAck(_ context.Context, hdr core.IHeader, payload []byte) {
	if hdr == nil {
		return
	}
	ackHdr, ok := decodeAckHeaderV1(payload)
	if !ok || ackHdr.Ver != headerVersionV1 {
		return
	}
	deliveryID := uuidToString(ackHdr.DeliveryID)

	h.mu.Lock()
	defer h.mu.Unlock()
	pd := h.producerDeliveries[deliveryID]
	if pd == nil || pd.State != stateActive {
		return
	}
	if hdr.SourceID() != pd.Consumer || hdr.TargetID() != pd.Producer {
		return
	}
	if ackHdr.Position > pd.AckedPosition {
		pd.AckedPosition = ackHdr.Position
	}
	pd.LastActive = time.Now()
}

func (h *Handler) routeOwnerRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, requester uint32, perm string, target uint32, sendErr func(int, string), handleLocal func()) {
	srv := core.ServerFromContext(ctx)
	if srv == nil || srv.ConnManager() == nil {
		return
	}
	local := srv.NodeID()
	cm := srv.ConnManager()

	if isParentConn(conn) {
		if target == local {
			handleLocal()
			return
		}
		h.forwardCtrlEnd(ctx, hdr, payload, target)
		return
	}
	if target == 0 {
		sendErr(400, "target required")
		return
	}
	if target == local {
		if !h.hasPermission(requester, perm) {
			sendErr(403, "permission denied")
			return
		}
		handleLocal()
		return
	}

	targetConn, ok := cm.GetByNode(target)
	if !ok || targetConn == nil {
		parent, parentNode := findParentConn(cm)
		if parent == nil {
			sendErr(404, "not found")
			return
		}
		fwdHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			sendErr(500, "hop limit exceeded")
			return
		}
		fwdHdr.WithTargetID(parentNode)
		h.sendToConn(ctx, parent, fwdHdr, payload)
		return
	}

	if requesterConn, ok := cm.GetByNode(requester); ok && requesterConn != nil && requesterConn.ID() == targetConn.ID() {
		nextNode := connNodeID(requesterConn)
		if nextNode == 0 {
			sendErr(500, "invalid route")
			return
		}
		fwdHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			sendErr(500, "hop limit exceeded")
			return
		}
		fwdHdr.WithTargetID(nextNode)
		h.sendToConn(ctx, requesterConn, fwdHdr, payload)
		return
	}

	if !h.hasPermission(requester, perm) {
		sendErr(403, "permission denied")
		return
	}
	fwdHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		sendErr(500, "hop limit exceeded")
		return
	}
	fwdHdr.WithTargetID(target)
	h.sendToConn(ctx, targetConn, fwdHdr, payload)
}

func (h *Handler) routeCoordinatorRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, requester uint32, perm string, producer, consumer uint32, sendErr func(int, string), handleLocal func()) {
	srv := core.ServerFromContext(ctx)
	if srv == nil || srv.ConnManager() == nil {
		return
	}
	if producer == 0 || consumer == 0 {
		sendErr(400, "producer and consumer required")
		return
	}

	local := srv.NodeID()
	cm := srv.ConnManager()
	_, _, okProducer := resolveRoute(cm, local, producer)
	_, _, okConsumer := resolveRoute(cm, local, consumer)
	if !okProducer || !okConsumer {
		parent, parentNode := findParentConn(cm)
		if parent == nil {
			sendErr(404, "not found")
			return
		}
		fwdHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			sendErr(500, "hop limit exceeded")
			return
		}
		fwdHdr.WithTargetID(parentNode)
		h.sendToConn(ctx, parent, fwdHdr, payload)
		return
	}

	if !h.hasPermission(requester, perm) {
		sendErr(403, "permission denied")
		return
	}
	handleLocal()
}

func (h *Handler) routeDeliveryRequest(ctx context.Context, hdr core.IHeader, payload []byte, requester uint32, perm string, deliveryID string, sendErr func(int, string), handleLocal func()) {
	if deliveryID == "" {
		sendErr(400, "delivery_id required")
		return
	}
	if h.hasLocalDelivery(deliveryID) || h.hasRoute(deliveryID) {
		if perm != "" && !h.hasPermission(requester, perm) {
			sendErr(403, "permission denied")
			return
		}
		handleLocal()
		return
	}

	srv := core.ServerFromContext(ctx)
	if srv == nil || srv.ConnManager() == nil {
		return
	}
	parent, parentNode := findParentConn(srv.ConnManager())
	if parent == nil {
		sendErr(404, "delivery not found")
		return
	}
	fwdHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		sendErr(500, "hop limit exceeded")
		return
	}
	fwdHdr.WithTargetID(parentNode)
	h.sendToConn(ctx, parent, fwdHdr, payload)
}

func (h *Handler) prepareDelivery(ctx context.Context, target uint32, req deliveryPrepareReq) (deliveryPrepareResp, int, string) {
	if target == 0 {
		return deliveryPrepareResp{}, 400, "target required"
	}
	if srv := core.ServerFromContext(ctx); srv != nil && srv.NodeID() == target {
		resp := h.handleDeliveryPrepareLocal((&header.HeaderTcp{}).WithSourceID(srv.NodeID()).WithTargetID(target), req)
		return resp, resp.Code, resp.Msg
	}
	raw, err := h.callPrivate(ctx, target, actionDeliveryPrepare, actionDeliveryPrepareResp, req.ReqID, req)
	if err != nil {
		return deliveryPrepareResp{}, 408, err.Error()
	}
	var resp deliveryPrepareResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return deliveryPrepareResp{}, 500, "invalid prepare response"
	}
	return resp, resp.Code, resp.Msg
}

func (h *Handler) activateDelivery(ctx context.Context, target uint32, req deliveryActivateReq) (int, string) {
	if target == 0 {
		return 400, "target required"
	}
	if srv := core.ServerFromContext(ctx); srv != nil && srv.NodeID() == target {
		resp := h.handleDeliveryActivateLocal(req)
		return resp.Code, resp.Msg
	}
	raw, err := h.callPrivate(ctx, target, actionDeliveryActivate, actionDeliveryActivateResp, req.ReqID, req)
	if err != nil {
		return 408, err.Error()
	}
	var resp deliveryActivateResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 500, "invalid activate response"
	}
	return resp.Code, resp.Msg
}

func (h *Handler) abortDelivery(ctx context.Context, target uint32, req deliveryAbortReq) (int, string) {
	if target == 0 {
		return 400, "target required"
	}
	if srv := core.ServerFromContext(ctx); srv != nil && srv.NodeID() == target {
		resp := h.handleDeliveryAbortLocal(req)
		return resp.Code, resp.Msg
	}
	raw, err := h.callPrivate(ctx, target, actionDeliveryAbort, actionDeliveryAbortResp, req.ReqID, req)
	if err != nil {
		return 408, err.Error()
	}
	var resp deliveryAbortResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 500, "invalid abort response"
	}
	return resp.Code, resp.Msg
}

func (h *Handler) closeDelivery(ctx context.Context, target uint32, req deliveryCloseReq) (int, string) {
	if target == 0 {
		return 400, "target required"
	}
	if srv := core.ServerFromContext(ctx); srv != nil && srv.NodeID() == target {
		resp := h.handleDeliveryCloseLocal(req)
		return resp.Code, resp.Msg
	}
	raw, err := h.callPrivate(ctx, target, actionDeliveryClose, actionDeliveryCloseResp, req.ReqID, req)
	if err != nil {
		return 408, err.Error()
	}
	var resp deliveryCloseResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 500, "invalid close response"
	}
	return resp.Code, resp.Msg
}

func (h *Handler) callPrivate(ctx context.Context, target uint32, action, respAction, reqID string, data any) (json.RawMessage, error) {
	ch := h.registerPendingCtrl(reqID)
	defer h.unregisterPendingCtrl(reqID)

	if err := h.sendCtrlRequestToNode(ctx, target, message{Action: action, Data: mustJSON(data)}); err != nil {
		return nil, err
	}

	timer := time.NewTimer(privateCtrlTimeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, context.DeadlineExceeded
	case resp := <-ch:
		if resp.Action != respAction {
			return nil, errors.New("unexpected private response action")
		}
		return resp.Data, nil
	}
}

func (h *Handler) registerPendingCtrl(reqID string) chan privateCtrlResp {
	ch := make(chan privateCtrlResp, 1)
	h.pendingMu.Lock()
	h.pendingCtrl[reqID] = ch
	h.pendingMu.Unlock()
	return ch
}

func (h *Handler) unregisterPendingCtrl(reqID string) {
	h.pendingMu.Lock()
	delete(h.pendingCtrl, reqID)
	h.pendingMu.Unlock()
}

func (h *Handler) tryDeliverPendingCtrl(action string, data json.RawMessage) bool {
	switch action {
	case actionDeliveryPrepareResp, actionDeliveryActivateResp, actionDeliveryAbortResp, actionDeliveryCloseResp:
	default:
		return false
	}
	var base struct {
		ReqID string `json:"req_id"`
	}
	if err := json.Unmarshal(data, &base); err != nil || strings.TrimSpace(base.ReqID) == "" {
		return false
	}
	h.pendingMu.Lock()
	ch, ok := h.pendingCtrl[base.ReqID]
	h.pendingMu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- privateCtrlResp{Action: action, Data: data}:
	default:
	}
	return true
}

func (h *Handler) sendAnnounceResp(ctx context.Context, reqHdr core.IHeader, target uint32, data announceResp) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, actionAnnounceResp, data)
}

func (h *Handler) sendWithdrawResp(ctx context.Context, reqHdr core.IHeader, target uint32, data withdrawResp) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, actionWithdrawResp, data)
}

func (h *Handler) sendListSourcesResp(ctx context.Context, reqHdr core.IHeader, target uint32, data listSourcesResp) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, actionListSourcesResp, data)
}

func (h *Handler) sendGetSourceResp(ctx context.Context, reqHdr core.IHeader, target uint32, data getSourceResp) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, actionGetSourceResp, data)
}

func (h *Handler) sendAnnounceConsumerResp(ctx context.Context, reqHdr core.IHeader, target uint32, data announceConsumerResp) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, actionAnnounceConsumerResp, data)
}

func (h *Handler) sendWithdrawConsumerResp(ctx context.Context, reqHdr core.IHeader, target uint32, data withdrawConsumerResp) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, actionWithdrawConsumerResp, data)
}

func (h *Handler) sendListConsumersResp(ctx context.Context, reqHdr core.IHeader, target uint32, data listConsumersResp) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, actionListConsumersResp, data)
}

func (h *Handler) sendGetConsumerResp(ctx context.Context, reqHdr core.IHeader, target uint32, data getConsumerResp) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, actionGetConsumerResp, data)
}

func (h *Handler) sendSubscribeResp(ctx context.Context, reqHdr core.IHeader, target uint32, data subscribeResp) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, actionSubscribeResp, data)
}

func (h *Handler) sendUnsubscribeResp(ctx context.Context, reqHdr core.IHeader, target uint32, data unsubscribeResp) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, actionUnsubscribeResp, data)
}

func (h *Handler) sendConnectResp(ctx context.Context, reqHdr core.IHeader, target uint32, data connectResp) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, actionConnectResp, data)
}

func (h *Handler) sendDisconnectResp(ctx context.Context, reqHdr core.IHeader, target uint32, data disconnectResp) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, actionDisconnectResp, data)
}

func (h *Handler) sendSignalResp(ctx context.Context, reqHdr core.IHeader, target uint32, data signalResp) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, actionSignalResp, data)
}

func (h *Handler) sendPrivateResp(ctx context.Context, reqHdr core.IHeader, target uint32, action string, data any) {
	h.sendCtrlRespToNode(ctx, reqHdr, target, action, data)
}

func (h *Handler) sendCtrlRequestToNode(ctx context.Context, target uint32, msg message) error {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return errors.New("server missing")
	}
	payload, err := encodeCtrlPayload(msg)
	if err != nil {
		return err
	}
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoStream).
		WithSourceID(srv.NodeID()).
		WithTargetID(target)
	return h.sendToNode(ctx, target, hdr, payload)
}

func (h *Handler) sendCtrlRespToNode(ctx context.Context, reqHdr core.IHeader, target uint32, action string, data any) {
	if target == 0 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	payload, err := encodeCtrlPayload(message{Action: action, Data: mustJSON(data)})
	if err != nil {
		return
	}
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorOKResp).
		WithSubProto(SubProtoStream).
		WithSourceID(srv.NodeID()).
		WithTargetID(target)
	if reqHdr != nil {
		hdr = hdr.WithMsgID(reqHdr.GetMsgID()).WithTraceID(reqHdr.GetTraceID())
	}
	_ = h.sendToNode(ctx, target, hdr, payload)
}

func (h *Handler) forwardCtrlByHeaderTarget(ctx context.Context, hdr core.IHeader, payload []byte) {
	if hdr == nil {
		return
	}
	target := hdr.TargetID()
	if target == 0 {
		return
	}
	h.forwardCtrlEnd(ctx, hdr, payload, target)
}

func (h *Handler) forwardCtrlEnd(ctx context.Context, hdr core.IHeader, payload []byte, target uint32) {
	srv := core.ServerFromContext(ctx)
	if srv == nil || hdr == nil || len(payload) == 0 {
		return
	}
	next, err := h.resolveNextHop(srv, target)
	if err != nil || next == nil {
		return
	}
	fwdHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		h.log.Warn("drop ctrl frame due to hop_limit", "target", target, "source", hdr.SourceID())
		return
	}
	fwdHdr.WithTargetID(target)
	_ = h.sendToConn(ctx, next, fwdHdr, payload)
}

func (h *Handler) sendToNode(ctx context.Context, target uint32, hdr core.IHeader, payload []byte) error {
	if target == 0 {
		return errors.New("target required")
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return errors.New("server missing")
	}
	if target == srv.NodeID() {
		return nil
	}
	next, err := h.resolveNextHop(srv, target)
	if err != nil {
		return err
	}
	return h.sendToConn(ctx, next, hdr, payload)
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

func (h *Handler) resolveNextHop(srv core.IServer, target uint32) (core.IConnection, error) {
	if srv == nil || srv.ConnManager() == nil {
		return nil, errors.New("conn manager missing")
	}
	if c, ok := srv.ConnManager().GetByNode(target); ok && c != nil {
		return c, nil
	}
	parent, _ := findParentConn(srv.ConnManager())
	if parent == nil {
		return nil, errors.New("route not found")
	}
	return parent, nil
}

func encodeCtrlPayload(msg message) ([]byte, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, 1+len(body))
	payload[0] = kindCtrl
	copy(payload[1:], body)
	return payload, nil
}

func (h *Handler) hasPermission(nodeID uint32, perm string) bool {
	if perm == "" {
		return true
	}
	if h.permCfg == nil {
		return false
	}
	return h.permCfg.Has(nodeID, perm)
}

func (h *Handler) hasLocalDelivery(deliveryID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.producerDeliveries[deliveryID] != nil || h.consumerDeliveries[deliveryID] != nil
}

func (h *Handler) hasRoute(deliveryID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.deliveryRoutes[deliveryID] != nil
}

func (h *Handler) getRoute(deliveryID string) (deliveryRoute, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	route := h.deliveryRoutes[deliveryID]
	if route == nil {
		return deliveryRoute{}, false
	}
	return *route, true
}

func (h *Handler) removeRoute(deliveryID string) {
	h.mu.Lock()
	delete(h.deliveryRoutes, deliveryID)
	h.mu.Unlock()
}

func (h *Handler) removeProducerDeliveryLocked(deliveryID string) {
	pd := h.producerDeliveries[deliveryID]
	if pd == nil {
		return
	}
	delete(h.producerDeliveries, deliveryID)
	if entry := h.sources[pd.SourceID]; entry != nil {
		delete(entry.deliveries, deliveryID)
	}
}

func (h *Handler) removeConsumerDeliveryLocked(deliveryID string) {
	cd := h.consumerDeliveries[deliveryID]
	if cd == nil {
		return
	}
	delete(h.consumerDeliveries, deliveryID)
	if entry := h.consumers[cd.ConsumerID]; entry != nil {
		delete(entry.deliveries, deliveryID)
	}
}

func (h *Handler) bestEffortCloseLocalProducer(ctx context.Context, delivery producerDelivery, reason string) {
	reqID := strings.TrimSpace(delivery.TxnID)
	if reqID == "" {
		reqID = delivery.DeliveryID
	}
	_ = h.handleDeliveryCloseLocal(deliveryCloseReq{
		ReqID:      reqID,
		TxnID:      delivery.TxnID,
		DeliveryID: delivery.DeliveryID,
		Role:       "producer",
		Reason:     reason,
		CloseRoute: delivery.Coordinator == delivery.Producer,
	})
	if delivery.Consumer != 0 && delivery.Consumer != delivery.Producer {
		_, _ = h.closeDelivery(ctx, delivery.Consumer, deliveryCloseReq{
			ReqID:      reqID,
			TxnID:      delivery.TxnID,
			DeliveryID: delivery.DeliveryID,
			Role:       "consumer",
			Reason:     reason,
			CloseRoute: delivery.Coordinator == delivery.Consumer,
		})
	}
	if delivery.Coordinator != 0 && delivery.Coordinator != delivery.Producer && delivery.Coordinator != delivery.Consumer {
		_, _ = h.closeDelivery(ctx, delivery.Coordinator, deliveryCloseReq{
			ReqID:      reqID,
			TxnID:      delivery.TxnID,
			DeliveryID: delivery.DeliveryID,
			Reason:     reason,
			CloseRoute: true,
		})
	}
}

func (h *Handler) bestEffortCloseLocalConsumer(ctx context.Context, delivery consumerDelivery, reason string) {
	reqID := strings.TrimSpace(delivery.TxnID)
	if reqID == "" {
		reqID = delivery.DeliveryID
	}
	_ = h.handleDeliveryCloseLocal(deliveryCloseReq{
		ReqID:      reqID,
		TxnID:      delivery.TxnID,
		DeliveryID: delivery.DeliveryID,
		Role:       "consumer",
		Reason:     reason,
		CloseRoute: delivery.Coordinator == delivery.Consumer,
	})
	if delivery.Producer != 0 && delivery.Producer != delivery.Consumer {
		_, _ = h.closeDelivery(ctx, delivery.Producer, deliveryCloseReq{
			ReqID:      reqID,
			TxnID:      delivery.TxnID,
			DeliveryID: delivery.DeliveryID,
			Role:       "producer",
			Reason:     reason,
			CloseRoute: delivery.Coordinator == delivery.Producer,
		})
	}
	if delivery.Coordinator != 0 && delivery.Coordinator != delivery.Consumer && delivery.Coordinator != delivery.Producer {
		_, _ = h.closeDelivery(ctx, delivery.Coordinator, deliveryCloseReq{
			ReqID:      reqID,
			TxnID:      delivery.TxnID,
			DeliveryID: delivery.DeliveryID,
			Reason:     reason,
			CloseRoute: true,
		})
	}
}

func normalizeSourceDescriptor(desc sourceDescriptor, requester uint32) (sourceDescriptor, int, string) {
	desc.SourceID = strings.TrimSpace(desc.SourceID)
	desc.Name = strings.TrimSpace(desc.Name)
	desc.Kind = strings.ToLower(strings.TrimSpace(desc.Kind))
	desc.ContentType = strings.TrimSpace(desc.ContentType)
	desc.Mode = strings.ToLower(strings.TrimSpace(desc.Mode))
	desc.UnitMode = strings.ToLower(strings.TrimSpace(desc.UnitMode))
	desc.Tags = normalizeTags(desc.Tags)
	desc.Metadata = cloneRaw(desc.Metadata)
	if desc.Producer == 0 {
		desc.Producer = requester
	}
	if desc.SourceID == "" {
		return sourceDescriptor{}, 400, "source_id required"
	}
	if !isValidKind(desc.Kind) {
		return sourceDescriptor{}, 406, "invalid kind"
	}
	if desc.Mode == "" {
		desc.Mode = modeLive
	} else if !isValidMode(desc.Mode) {
		return sourceDescriptor{}, 406, "invalid mode"
	}
	if desc.UnitMode == "" {
		desc.UnitMode = unitModeChunk
	} else if !isValidUnitMode(desc.UnitMode) {
		return sourceDescriptor{}, 406, "invalid unit_mode"
	}
	return desc, 0, ""
}

func normalizeConsumerDescriptor(desc consumerDescriptor, requester uint32) (consumerDescriptor, int, string) {
	desc.ConsumerID = strings.TrimSpace(desc.ConsumerID)
	desc.Name = strings.TrimSpace(desc.Name)
	desc.Kind = strings.ToLower(strings.TrimSpace(desc.Kind))
	desc.ContentType = strings.TrimSpace(desc.ContentType)
	desc.Tags = normalizeTags(desc.Tags)
	desc.Metadata = cloneRaw(desc.Metadata)
	if desc.Consumer == 0 {
		desc.Consumer = requester
	}
	if desc.ConsumerID == "" {
		return consumerDescriptor{}, 400, "consumer_id required"
	}
	if !isValidKind(desc.Kind) {
		return consumerDescriptor{}, 406, "invalid kind"
	}
	return desc, 0, ""
}

func cloneSourceDescriptor(desc sourceDescriptor) sourceDescriptor {
	desc.Tags = append([]string(nil), desc.Tags...)
	desc.Metadata = cloneRaw(desc.Metadata)
	return desc
}

func cloneConsumerDescriptor(desc consumerDescriptor) consumerDescriptor {
	desc.Tags = append([]string(nil), desc.Tags...)
	desc.Metadata = cloneRaw(desc.Metadata)
	return desc
}

func sameSourceDescriptor(a, b sourceDescriptor) bool {
	return a.SourceID == b.SourceID &&
		a.Producer == b.Producer &&
		a.Name == b.Name &&
		a.Kind == b.Kind &&
		a.ContentType == b.ContentType &&
		a.Mode == b.Mode &&
		a.UnitMode == b.UnitMode &&
		strings.Join(a.Tags, "\x00") == strings.Join(b.Tags, "\x00") &&
		bytes.Equal(a.Metadata, b.Metadata)
}

func sameConsumerDescriptor(a, b consumerDescriptor) bool {
	return a.ConsumerID == b.ConsumerID &&
		a.Consumer == b.Consumer &&
		a.Name == b.Name &&
		a.Kind == b.Kind &&
		a.ContentType == b.ContentType &&
		strings.Join(a.Tags, "\x00") == strings.Join(b.Tags, "\x00") &&
		bytes.Equal(a.Metadata, b.Metadata)
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out
}

func containsString(items []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return false
	}
	for _, item := range items {
		if strings.ToLower(strings.TrimSpace(item)) == target {
			return true
		}
	}
	return false
}

func isValidKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case streamKindMusic, streamKindVideo, streamKindText, streamKindCustom:
		return true
	default:
		return false
	}
}

func isValidMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case modeLive, modeBounded:
		return true
	default:
		return false
	}
}

func isValidUnitMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case unitModeFrame, unitModeChunk:
		return true
	default:
		return false
	}
}

func isValidSignalOp(op string) bool {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case signalOpPause, signalOpResume, signalOpMetadataUpdate, signalOpKeyframeRequest, signalOpCustom:
		return true
	default:
		return false
	}
}

func coalesceWindowBytes(v uint32) uint32 {
	if v == 0 {
		return uint32(defaultWindowBytes)
	}
	return v
}

func coalesceAckIntervalMs(v uint32) uint32 {
	if v == 0 {
		return uint32(defaultAckIntervalMs)
	}
	return v
}

func nextExpectedPosition(unitMode string, position uint64, bodyLen int) uint64 {
	if strings.TrimSpace(unitMode) == unitModeFrame {
		return position + 1
	}
	if bodyLen <= 0 {
		return position
	}
	return position + uint64(bodyLen)
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
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

func connNodeID(conn core.IConnection) uint32 {
	if conn == nil {
		return 0
	}
	if v, ok := conn.GetMeta("nodeID"); ok {
		switch vv := v.(type) {
		case uint32:
			return vv
		case uint64:
			return uint32(vv)
		case int:
			if vv >= 0 {
				return uint32(vv)
			}
		case int64:
			if vv >= 0 {
				return uint32(vv)
			}
		case float64:
			if vv >= 0 {
				return uint32(vv)
			}
		}
	}
	return 0
}

func findParentConn(cm core.IConnectionManager) (core.IConnection, uint32) {
	if cm == nil {
		return nil, 0
	}
	var parent core.IConnection
	var parentNode uint32
	cm.Range(func(c core.IConnection) bool {
		if isParentConn(c) {
			parent = c
			parentNode = connNodeID(c)
			return false
		}
		return true
	})
	return parent, parentNode
}

func resolveRoute(cm core.IConnectionManager, local, target uint32) (bool, core.IConnection, bool) {
	if target == 0 {
		return false, nil, false
	}
	if target == local {
		return true, nil, true
	}
	if cm == nil {
		return false, nil, false
	}
	if c, ok := cm.GetByNode(target); ok && c != nil {
		return false, c, true
	}
	parent, parentNode := findParentConn(cm)
	if parent != nil && parentNode == target {
		return false, parent, true
	}
	return false, nil, false
}
