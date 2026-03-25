package auth

import (
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	"github.com/yttydcs/myflowhub-core/subproto"
)

// LoginHandler implements register/login/revoke/offline flows with action+data payload.
type LoginHandler struct {
	subproto.ActionBaseSubProcess
	log *slog.Logger

	nextID atomic.Uint32

	mu                sync.RWMutex
	whitelist         map[string]bindingRecord          // deviceID -> record
	pendingConn       map[string]pendingInfo            // deviceID -> pending downstream info (in-flight assist)
	pendingRegisters  map[string]pendingRegisterRecord  // requestID -> pending register
	pendingByDevice   map[string]string                 // deviceID -> requestID
	approvedRegisters map[string]approvedRegisterRecord // deviceID -> approved-but-not-finalized
	registerPermits   map[string]registerPermitRecord   // permit -> record

	authNode uint32

	permCfg     *permission.Config
	nodePriv    *ecdsa.PrivateKey
	nodePubB64  string
	trustedNode map[uint32][]byte

	disablePersist  bool
	requireApproval bool
	pendingTTL      time.Duration
	permitTTL       time.Duration
	initErr         error
	now             func() time.Time

	firstRegisterBootstrap      firstRegisterBootstrapConfig
	firstRegisterBootstrapState firstRegisterBootstrapState
}

type pendingInfo struct {
	connID  string
	msgID   uint32
	traceID uint32
}

func NewLoginHandler(log *slog.Logger) *LoginHandler {
	return NewLoginHandlerWithConfig(nil, log)
}

func NewLoginHandlerWithConfig(cfg core.IConfig, log *slog.Logger) *LoginHandler {
	if log == nil {
		log = slog.Default()
	}
	h := &LoginHandler{
		log:               log,
		whitelist:         make(map[string]bindingRecord),
		pendingConn:       make(map[string]pendingInfo),
		pendingRegisters:  make(map[string]pendingRegisterRecord),
		pendingByDevice:   make(map[string]string),
		approvedRegisters: make(map[string]approvedRegisterRecord),
		registerPermits:   make(map[string]registerPermitRecord),
		now:               time.Now,
	}
	if cfg != nil {
		if v, _ := cfg.Get("auth.disable_persist"); strings.EqualFold(strings.TrimSpace(v), "true") {
			h.disablePersist = true
		}
	}
	// load node keys & trusted/bindings
	if cfg != nil {
		if priv, pub, err := loadOrCreateNodeKeys(cfg); err == nil {
			h.nodePriv = priv
			h.nodePubB64 = pub
		}
		if !h.disablePersist {
			wl, trusted, pending, approved, permits, bootstrapState, maxNode, err := loadTrustedBindings(cfg)
			if err != nil {
				h.log.Warn("load auth persisted state failed", "err", err)
			} else {
				if len(wl) > 0 {
					h.whitelist = wl
				}
				if len(trusted) > 0 {
					h.trustedNode = trusted
				}
				if len(pending) > 0 {
					h.pendingRegisters = pending
					h.pendingByDevice = make(map[string]string, len(pending))
					for requestID, rec := range pending {
						if strings.TrimSpace(rec.DeviceID) != "" {
							h.pendingByDevice[rec.DeviceID] = requestID
						}
					}
				}
				if len(approved) > 0 {
					h.approvedRegisters = approved
				}
				if len(permits) > 0 {
					h.registerPermits = permits
				}
				h.firstRegisterBootstrapState = bootstrapState
				if maxNode >= 2 {
					h.nextID.Store(maxNode + 1)
				}
			}
		}
	}
	h.loadAuthConfig(cfg)
	h.loadAdmissionConfig(cfg)
	h.loadFirstRegisterBootstrapConfig(cfg)
	if h.nextID.Load() < 2 {
		h.nextID.Store(2)
	}
	return h
}

func (h *LoginHandler) addTrustedNode(nodeID uint32, pubB64 string) {
	if nodeID == 0 || strings.TrimSpace(pubB64) == "" {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubB64))
	if err != nil || len(raw) == 0 {
		return
	}
	h.mu.Lock()
	if h.trustedNode == nil {
		h.trustedNode = make(map[uint32][]byte)
	}
	existing, ok := h.trustedNode[nodeID]
	same := ok && len(existing) == len(raw)
	if same {
		for i := range existing {
			if existing[i] != raw[i] {
				same = false
				break
			}
		}
	}
	if same {
		h.mu.Unlock()
		return
	}
	h.trustedNode[nodeID] = raw
	h.mu.Unlock()
	h.persistState()
}

func (h *LoginHandler) SubProto() uint8 { return 2 }

func (h *LoginHandler) Init() bool {
	if h.initErr != nil {
		if h.log != nil {
			h.log.Error("init auth handler failed", "err", h.initErr)
		}
		return false
	}
	h.initActions()
	return true
}

// AllowSourceMismatch 登录阶段允许 SourceID 与连接元数据不一致（尚未绑定 nodeID）。
func (h *LoginHandler) AllowSourceMismatch() bool { return true }
func (h *LoginHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	var msg message
	if err := json.Unmarshal(payload, &msg); err != nil {
		h.log.Warn("invalid login payload", "err", err)
		return
	}
	entry, ok := h.LookupAction(msg.Action)
	if !ok {
		h.log.Debug("unknown login action", "action", msg.Action)
		return
	}
	if entry.RequireAuth() && !h.sourceMatches(conn, hdr) {
		h.log.Warn("drop login action due to source mismatch", "action", msg.Action, "hdr_source", hdr.SourceID())
		return
	}
	entry.Handle(ctx, conn, hdr, msg.Data)
}

func (h *LoginHandler) initActions() {
	h.ResetActions()
	for _, act := range registerRegisterActions(h) {
		h.RegisterAction(act)
	}
	for _, act := range registerLoginActions(h) {
		h.RegisterAction(act)
	}
	for _, act := range registerRevokeActions(h) {
		h.RegisterAction(act)
	}
	for _, act := range registerAssistQueryActions(h) {
		h.RegisterAction(act)
	}
	for _, act := range registerOfflineActions(h) {
		h.RegisterAction(act)
	}
	for _, act := range registerPermActions(h) {
		h.RegisterAction(act)
	}
	for _, act := range registerAdmissionActions(h) {
		h.RegisterAction(act)
	}
	for _, act := range registerUpLoginActions(h) {
		h.RegisterAction(act)
	}
}
