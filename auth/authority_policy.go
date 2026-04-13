package auth

// Context: This file belongs to the SubProto implementation layer around authority_policy.

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
)

const (
	configKeyAuthAuthorityMode      = "auth.authority_mode"
	configKeyAuthAuthorityPolicyTTL = "auth.authority_policy_ttl_sec"

	authorityModeConfigSemiCentral = "semi-central"

	defaultAuthorityPolicyTTL = 90 * time.Second
	minAuthorityPolicyTTL     = 5 * time.Second
	maxAuthorityPolicyTTL     = 10 * time.Minute
)

type runtimeAuthorityPolicy struct {
	mode                 string
	effectiveAuthorityID uint32
	epoch                uint64
	ttl                  time.Duration
	expiresAt            time.Time
}

func (h *LoginHandler) loadAuthorityPolicyConfig(cfg core.IConfig) {
	if h == nil {
		return
	}
	h.authorityMode = ""
	h.authorityPolicyTTL = defaultAuthorityPolicyTTL
	if cfg == nil {
		return
	}
	if raw, ok := cfg.Get(configKeyAuthAuthorityMode); ok {
		h.authorityMode = normalizeAuthorityMode(raw)
	}
	if raw, ok := cfg.Get(configKeyAuthAuthorityPolicyTTL); ok {
		if ttl := parseDurationSeconds(raw); ttl > 0 {
			h.authorityPolicyTTL = clampAuthorityPolicyTTL(ttl)
		}
	}
}

func normalizeAuthorityMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "semi-central", "semi_central", "semi":
		return authorityModeConfigSemiCentral
	default:
		return ""
	}
}

func clampAuthorityPolicyTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return defaultAuthorityPolicyTTL
	}
	if ttl < minAuthorityPolicyTTL {
		return minAuthorityPolicyTTL
	}
	if ttl > maxAuthorityPolicyTTL {
		return maxAuthorityPolicyTTL
	}
	return ttl
}

func (h *LoginHandler) isSemiCentralMode() bool {
	return h != nil && h.authorityMode == authorityModeConfigSemiCentral
}

func (h *LoginHandler) authorityPolicyRefreshInterval() time.Duration {
	ttl := clampAuthorityPolicyTTL(h.authorityPolicyTTL)
	interval := ttl / 2
	if interval < minAuthorityPolicyTTL {
		interval = minAuthorityPolicyTTL
	}
	return interval
}

func (h *LoginHandler) currentRuntimeAuthorityPolicy(now time.Time) (runtimeAuthorityPolicy, bool) {
	if h == nil {
		return runtimeAuthorityPolicy{}, false
	}
	h.mu.RLock()
	policy := h.authorityPolicy
	h.mu.RUnlock()
	if policy.effectiveAuthorityID == 0 || policy.epoch == 0 {
		return runtimeAuthorityPolicy{}, false
	}
	if !policy.expiresAt.IsZero() && !policy.expiresAt.After(now) {
		return runtimeAuthorityPolicy{}, false
	}
	return policy, true
}

func (h *LoginHandler) applyRuntimeAuthorityPolicy(now time.Time, data authorityPolicySyncData) bool {
	if h == nil {
		return false
	}
	mode := normalizeAuthorityMode(data.Mode)
	if mode != authorityModeConfigSemiCentral || data.EffectiveAuthorityID == 0 {
		return false
	}
	ttl := clampAuthorityPolicyTTL(h.authorityPolicyTTL)
	if data.TTLSec > 0 {
		ttl = clampAuthorityPolicyTTL(time.Duration(data.TTLSec) * time.Second)
	}
	next := runtimeAuthorityPolicy{
		mode:                 mode,
		effectiveAuthorityID: data.EffectiveAuthorityID,
		epoch:                data.Epoch,
		ttl:                  ttl,
		expiresAt:            now.Add(ttl),
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	current := h.authorityPolicy
	if current.epoch > next.epoch {
		return false
	}
	if current.epoch == next.epoch {
		if current.mode != next.mode || current.effectiveAuthorityID != next.effectiveAuthorityID {
			return false
		}
		current.ttl = next.ttl
		current.expiresAt = next.expiresAt
		h.authorityPolicy = current
		return true
	}
	h.authorityPolicy = next
	return true
}

func (h *LoginHandler) nextLocalAuthorityPolicy(localNodeID uint32) authorityPolicySyncData {
	ttl := clampAuthorityPolicyTTL(h.authorityPolicyTTL)
	epoch := h.authorityPolicyEpoch.Add(1)
	return authorityPolicySyncData{
		Mode:                 authorityModeConfigSemiCentral,
		EffectiveAuthorityID: localNodeID,
		Epoch:                epoch,
		TTLSec:               uint32(ttl / time.Second),
	}
}

func (h *LoginHandler) broadcastLocalAuthorityPolicy(ctx context.Context) {
	if h == nil {
		return
	}
	local := localNodeID(ctx)
	if local == 0 {
		return
	}
	policy := h.nextLocalAuthorityPolicy(local)
	_ = h.applyRuntimeAuthorityPolicy(time.Now().UTC(), policy)
	h.broadcastAuthorityPolicy(ctx, nil, policy)
}

func (h *LoginHandler) broadcastAuthorityPolicy(ctx context.Context, src core.IConnection, policy authorityPolicySyncData) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	payloadData, _ := json.Marshal(policy)
	msg := message{Action: actionAuthorityPolicySync, Data: payloadData}
	payload, _ := json.Marshal(msg)
	baseHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(srv.NodeID()).
		WithTargetID(0)

	srv.ConnManager().Range(func(c core.IConnection) bool {
		if src != nil && c.ID() == src.ID() {
			return true
		}
		if isParentConnLogin(c) {
			return true
		}
		if err := srv.Send(ctx, c.ID(), baseHdr.Clone(), payload); err != nil && h.log != nil {
			h.log.Warn("broadcast authority policy failed", "conn", c.ID(), "err", err)
		}
		return true
	})
}
