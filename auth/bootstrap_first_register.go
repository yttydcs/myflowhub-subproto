package auth

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
)

const defaultFirstRegisterBootstrapRole = coreconfig.DefaultAuthBootstrapFirstRegisterRole

type firstRegisterBootstrapConfig struct {
	Enabled   bool
	Role      string
	DeviceID  string
	PubKey    string
	PubKeyRaw []byte
	Epoch     int64
}

type firstRegisterBootstrapState struct {
	ConsumedEpoch int64  `json:"consumed_epoch,omitempty"`
	ConsumedAt    int64  `json:"consumed_at,omitempty"`
	DeviceID      string `json:"device_id,omitempty"`
	NodeID        uint32 `json:"node_id,omitempty"`
	Role          string `json:"role,omitempty"`
}

type firstRegisterBootstrapGrant struct {
	NodeID uint32
	Role   string
}

func (h *LoginHandler) loadFirstRegisterBootstrapConfig(cfg core.IConfig) {
	if h == nil {
		return
	}
	h.firstRegisterBootstrap = firstRegisterBootstrapConfig{
		Role: defaultFirstRegisterBootstrapRole,
	}
	if cfg == nil {
		return
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthBootstrapFirstRegisterEnable); ok {
		h.firstRegisterBootstrap.Enabled = parseBootstrapBool(raw)
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthBootstrapFirstRegisterRole); ok {
		if role := strings.TrimSpace(raw); role != "" {
			h.firstRegisterBootstrap.Role = role
		}
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthBootstrapFirstRegisterDeviceID); ok {
		h.firstRegisterBootstrap.DeviceID = strings.TrimSpace(raw)
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthBootstrapFirstRegisterPubKey); ok {
		h.firstRegisterBootstrap.PubKey = strings.TrimSpace(raw)
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthBootstrapFirstRegisterEpoch); ok {
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			h.initErr = fmt.Errorf("auth bootstrap first register epoch invalid: %w", err)
			return
		}
		h.firstRegisterBootstrap.Epoch = n
	}
	if err := h.validateFirstRegisterBootstrapConfig(cfg); err != nil {
		h.initErr = err
	}
}

func parseBootstrapBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func (h *LoginHandler) validateFirstRegisterBootstrapConfig(cfg core.IConfig) error {
	if h == nil || !h.firstRegisterBootstrap.Enabled {
		return nil
	}
	if h.disablePersist {
		return fmt.Errorf("auth bootstrap first register requires persist enabled")
	}
	if h.firstRegisterBootstrap.Epoch <= 0 {
		return fmt.Errorf("auth bootstrap first register epoch must be positive")
	}
	if h.firstRegisterBootstrap.DeviceID == "" {
		return fmt.Errorf("auth bootstrap first register device_id required")
	}
	if !h.roleKnown(h.firstRegisterBootstrap.Role) {
		return fmt.Errorf("auth bootstrap first register unknown role: %s", h.firstRegisterBootstrap.Role)
	}
	if cfg != nil && (h.explicitAuthorityNodeID(cfg) != 0 || h.parentConfigured(cfg)) {
		return fmt.Errorf("auth bootstrap first register requires local authority")
	}
	if h.firstRegisterBootstrap.PubKey == "" {
		return nil
	}
	_, raw, err := parseECPubKey(h.firstRegisterBootstrap.PubKey)
	if err != nil {
		return fmt.Errorf("auth bootstrap first register invalid pubkey: %w", err)
	}
	h.firstRegisterBootstrap.PubKeyRaw = raw
	return nil
}

func (h *LoginHandler) tryConsumeFirstRegisterBootstrap(ctx context.Context, req registerData, pubRaw []byte) (firstRegisterBootstrapGrant, *respData, bool) {
	if h == nil || !h.firstRegisterBootstrap.Enabled {
		return firstRegisterBootstrapGrant{}, nil, false
	}
	if !h.resolveAuthority(ctx).local() {
		return firstRegisterBootstrapGrant{}, nil, false
	}
	if strings.TrimSpace(req.DeviceID) != h.firstRegisterBootstrap.DeviceID {
		return firstRegisterBootstrapGrant{}, nil, false
	}
	h.mu.RLock()
	consumed := h.firstRegisterBootstrapState.ConsumedEpoch >= h.firstRegisterBootstrap.Epoch
	h.mu.RUnlock()
	if consumed {
		return firstRegisterBootstrapGrant{}, nil, false
	}
	if len(h.firstRegisterBootstrap.PubKeyRaw) > 0 {
		if len(pubRaw) == 0 {
			resp := rejectedBootstrapFirstRegisterResp(req.DeviceID, "bootstrap first register pubkey required")
			return firstRegisterBootstrapGrant{}, &resp, true
		}
		if !bytes.Equal(pubRaw, h.firstRegisterBootstrap.PubKeyRaw) {
			resp := rejectedBootstrapFirstRegisterResp(req.DeviceID, "bootstrap first register pubkey mismatch")
			return firstRegisterBootstrapGrant{}, &resp, true
		}
	}

	now := h.bootstrapNow()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.firstRegisterBootstrapState.ConsumedEpoch >= h.firstRegisterBootstrap.Epoch {
		return firstRegisterBootstrapGrant{}, nil, false
	}
	nodeID := h.ensureNodeIDLocked(req.DeviceID)
	h.firstRegisterBootstrapState = firstRegisterBootstrapState{
		ConsumedEpoch: h.firstRegisterBootstrap.Epoch,
		ConsumedAt:    now.Unix(),
		DeviceID:      strings.TrimSpace(req.DeviceID),
		NodeID:        nodeID,
		Role:          h.firstRegisterBootstrap.Role,
	}
	return firstRegisterBootstrapGrant{
		NodeID: nodeID,
		Role:   h.firstRegisterBootstrap.Role,
	}, nil, true
}

func (h *LoginHandler) bootstrapNow() time.Time {
	if h != nil && h.now != nil {
		return h.now().UTC()
	}
	return time.Now().UTC()
}

func rejectedBootstrapFirstRegisterResp(deviceID, reason string) respData {
	return respData{
		Code:     4001,
		Msg:      "register rejected",
		DeviceID: strings.TrimSpace(deviceID),
		Status:   admissionStatusRejected,
		Reason:   strings.TrimSpace(reason),
	}
}
