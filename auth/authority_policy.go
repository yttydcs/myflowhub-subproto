package auth

// 本文件承载 SubProto 中 `auth` 模块里与 `authority_policy` 相关的逻辑。

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

// loadAuthorityPolicyConfig 从配置读取半中心 authority 模式和 TTL。
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

// normalizeAuthorityMode 收敛配置别名，避免多种写法导致运行期分支漂移。
func normalizeAuthorityMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "semi-central", "semi_central", "semi":
		return authorityModeConfigSemiCentral
	default:
		return ""
	}
}

// clampAuthorityPolicyTTL 为运行期 lease 施加上下界，避免配置把广播频率推向极端。
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

// isSemiCentralMode 表示 authority 选择依赖运行期下发的 policy，而不是静态节点号。
func (h *LoginHandler) isSemiCentralMode() bool {
	return h != nil && h.authorityMode == authorityModeConfigSemiCentral
}

// authorityPolicyRefreshInterval 让本地广播频率始终快于 lease 过期。
func (h *LoginHandler) authorityPolicyRefreshInterval() time.Duration {
	ttl := clampAuthorityPolicyTTL(h.authorityPolicyTTL)
	interval := ttl / 2
	if interval < minAuthorityPolicyTTL {
		interval = minAuthorityPolicyTTL
	}
	return interval
}

// currentRuntimeAuthorityPolicy 读取当前仍未过期的 authority 选择结果。
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

// applyRuntimeAuthorityPolicy 只接受更“新”的 epoch，避免旧 policy 回滚 authority 选择。
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

// nextLocalAuthorityPolicy 生成本节点作为 authority 时对外广播的下一份 policy。
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

// broadcastLocalAuthorityPolicy 把当前节点声明成 authority，并先同步到本地 runtime state。
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

// broadcastAuthorityPolicy 向所有非父连接广播 authority policy，同步下游选择。
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
