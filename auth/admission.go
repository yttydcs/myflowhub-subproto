package auth

// Context: This file belongs to the SubProto implementation layer around admission.

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
)

const (
	admissionStatusApproved = "approved"
	admissionStatusPending  = "pending"
	admissionStatusRejected = "rejected"

	defaultPendingTTL = 24 * time.Hour
	defaultPermitTTL  = time.Hour
)

type pendingRegisterRecord struct {
	RequestID     string `json:"request_id,omitempty"`
	DeviceID      string `json:"device_id,omitempty"`
	RequestedRole string `json:"requested_role,omitempty"`
	DisplayName   string `json:"display_name,omitempty"`
	PubKey        string `json:"pubkey,omitempty"`
	CreatedAt     int64  `json:"created_at,omitempty"`
	ExpiresAt     int64  `json:"expires_at,omitempty"`
}

type approvedRegisterRecord struct {
	RequestID  string `json:"request_id,omitempty"`
	DeviceID   string `json:"device_id,omitempty"`
	NodeID     uint32 `json:"node_id,omitempty"`
	Role       string `json:"role,omitempty"`
	ApprovedAt int64  `json:"approved_at,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
}

type registerPermitRecord struct {
	Permit    string `json:"permit,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
	Role      string `json:"role,omitempty"`
	IssuedBy  uint32 `json:"issued_by,omitempty"`
	IssuedAt  int64  `json:"issued_at,omitempty"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
}

func (h *LoginHandler) loadAdmissionConfig(cfg core.IConfig) {
	if h == nil {
		return
	}
	h.requireApproval = false
	h.pendingTTL = defaultPendingTTL
	h.permitTTL = defaultPermitTTL
	if h.now == nil {
		h.now = time.Now
	}
	if cfg == nil {
		return
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthRegisterRequireApproval); ok {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "1", "true", "yes", "y", "on":
			h.requireApproval = true
		case "0", "false", "no", "n", "off":
			h.requireApproval = false
		}
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthRegisterPendingTTLSec); ok {
		if ttl := parseDurationSeconds(raw); ttl > 0 {
			h.pendingTTL = ttl
		}
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthRegisterPermitTTLSec); ok {
		if ttl := parseDurationSeconds(raw); ttl > 0 {
			h.permitTTL = ttl
		}
	}
}

func parseDurationSeconds(raw string) time.Duration {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

func (h *LoginHandler) admissionNow() time.Time {
	if h != nil && h.now != nil {
		return h.now().UTC()
	}
	return time.Now().UTC()
}

func (h *LoginHandler) cleanupExpiredAdmission() {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.cleanupExpiredAdmissionLocked(h.admissionNow())
	h.mu.Unlock()
}

func (h *LoginHandler) cleanupExpiredAdmissionLocked(now time.Time) {
	if h == nil {
		return
	}
	nowUnix := now.Unix()
	for requestID, rec := range h.pendingRegisters {
		if rec.ExpiresAt == 0 || rec.ExpiresAt > nowUnix {
			continue
		}
		delete(h.pendingRegisters, requestID)
		if currentID, ok := h.pendingByDevice[rec.DeviceID]; ok && currentID == requestID {
			delete(h.pendingByDevice, rec.DeviceID)
		}
	}
	for deviceID, rec := range h.approvedRegisters {
		if rec.ExpiresAt != 0 && rec.ExpiresAt <= nowUnix {
			delete(h.approvedRegisters, deviceID)
		}
	}
	for permit, rec := range h.registerPermits {
		if rec.ExpiresAt != 0 && rec.ExpiresAt <= nowUnix {
			delete(h.registerPermits, permit)
		}
	}
}

func (h *LoginHandler) ensureAdmissionMapsLocked() {
	if h.pendingRegisters == nil {
		h.pendingRegisters = make(map[string]pendingRegisterRecord)
	}
	if h.pendingByDevice == nil {
		h.pendingByDevice = make(map[string]string)
	}
	if h.approvedRegisters == nil {
		h.approvedRegisters = make(map[string]approvedRegisterRecord)
	}
	if h.registerPermits == nil {
		h.registerPermits = make(map[string]registerPermitRecord)
	}
}

func (h *LoginHandler) ensureNodeIDLocked(deviceID string) uint32 {
	if rec, ok := h.whitelist[deviceID]; ok && rec.NodeID != 0 {
		return rec.NodeID
	}
	if rec, ok := h.approvedRegisters[deviceID]; ok && rec.NodeID != 0 {
		return rec.NodeID
	}
	next := h.nextID.Add(1) - 1
	return next
}

func (h *LoginHandler) savePendingRegister(req registerData) (pendingRegisterRecord, error) {
	now := h.admissionNow()
	record := pendingRegisterRecord{
		DeviceID:      strings.TrimSpace(req.DeviceID),
		RequestedRole: strings.TrimSpace(req.RequestedRole),
		DisplayName:   normalizeDisplayName(req.DisplayName),
		PubKey:        strings.TrimSpace(req.PubKey),
		CreatedAt:     now.Unix(),
		ExpiresAt:     now.Add(h.effectivePendingTTL()).Unix(),
	}
	h.mu.Lock()
	h.cleanupExpiredAdmissionLocked(now)
	h.ensureAdmissionMapsLocked()
	if requestID, ok := h.pendingByDevice[record.DeviceID]; ok {
		if existing, ok2 := h.pendingRegisters[requestID]; ok2 {
			record.RequestID = existing.RequestID
		}
	}
	if record.RequestID == "" {
		requestID, err := newAdmissionToken("req")
		if err != nil {
			h.mu.Unlock()
			return pendingRegisterRecord{}, err
		}
		record.RequestID = requestID
	}
	h.pendingRegisters[record.RequestID] = record
	h.pendingByDevice[record.DeviceID] = record.RequestID
	h.mu.Unlock()
	h.persistState()
	return record, nil
}

func (h *LoginHandler) consumeApprovedRegister(deviceID string) (approvedRegisterRecord, bool) {
	deviceID = strings.TrimSpace(deviceID)
	if h == nil || deviceID == "" {
		return approvedRegisterRecord{}, false
	}
	now := h.admissionNow()
	h.mu.Lock()
	h.cleanupExpiredAdmissionLocked(now)
	rec, ok := h.approvedRegisters[deviceID]
	if ok {
		delete(h.approvedRegisters, deviceID)
	}
	h.mu.Unlock()
	if ok {
		h.persistState()
	}
	return rec, ok
}

func (h *LoginHandler) approvePendingRegister(requestID, role string) (approvedRegisterRecord, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return approvedRegisterRecord{}, fmt.Errorf("request_id required")
	}
	now := h.admissionNow()

	h.mu.Lock()
	h.cleanupExpiredAdmissionLocked(now)
	pending, ok := h.pendingRegisters[requestID]
	if !ok {
		h.mu.Unlock()
		return approvedRegisterRecord{}, fmt.Errorf("pending request not found")
	}
	finalRole, err := h.resolveAdmissionRole(role, pending.RequestedRole)
	if err != nil {
		h.mu.Unlock()
		return approvedRegisterRecord{}, err
	}
	h.ensureAdmissionMapsLocked()
	approved := approvedRegisterRecord{
		RequestID:  pending.RequestID,
		DeviceID:   pending.DeviceID,
		NodeID:     h.ensureNodeIDLocked(pending.DeviceID),
		Role:       finalRole,
		ApprovedAt: now.Unix(),
		ExpiresAt:  now.Add(h.effectivePendingTTL()).Unix(),
	}
	delete(h.pendingRegisters, requestID)
	delete(h.pendingByDevice, pending.DeviceID)
	h.approvedRegisters[pending.DeviceID] = approved
	h.mu.Unlock()
	h.persistState()
	return approved, nil
}

func (h *LoginHandler) rejectPendingRegister(requestID string) (pendingRegisterRecord, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return pendingRegisterRecord{}, fmt.Errorf("request_id required")
	}
	now := h.admissionNow()
	h.mu.Lock()
	h.cleanupExpiredAdmissionLocked(now)
	rec, ok := h.pendingRegisters[requestID]
	if ok {
		delete(h.pendingRegisters, requestID)
		delete(h.pendingByDevice, rec.DeviceID)
	}
	h.mu.Unlock()
	if !ok {
		return pendingRegisterRecord{}, fmt.Errorf("pending request not found")
	}
	h.persistState()
	return rec, nil
}

func (h *LoginHandler) issueRegisterPermit(deviceID, role string, expiresAt int64, actorID uint32) (registerPermitRecord, error) {
	deviceID = strings.TrimSpace(deviceID)
	role = strings.TrimSpace(role)
	if deviceID == "" {
		return registerPermitRecord{}, fmt.Errorf("device_id required")
	}
	if role == "" {
		return registerPermitRecord{}, fmt.Errorf("role required")
	}
	if !h.roleKnown(role) {
		return registerPermitRecord{}, fmt.Errorf("unknown role: %s", role)
	}
	now := h.admissionNow()
	if expiresAt == 0 {
		expiresAt = now.Add(h.effectivePermitTTL()).Unix()
	}
	if expiresAt <= now.Unix() {
		return registerPermitRecord{}, fmt.Errorf("expires_at must be in the future")
	}
	permit, err := newAdmissionToken("permit")
	if err != nil {
		return registerPermitRecord{}, err
	}
	record := registerPermitRecord{
		Permit:    permit,
		DeviceID:  deviceID,
		Role:      role,
		IssuedBy:  actorID,
		IssuedAt:  now.Unix(),
		ExpiresAt: expiresAt,
	}
	h.mu.Lock()
	h.cleanupExpiredAdmissionLocked(now)
	h.ensureAdmissionMapsLocked()
	h.registerPermits[permit] = record
	h.mu.Unlock()
	h.persistState()
	return record, nil
}

func (h *LoginHandler) revokeRegisterPermit(permit string) (registerPermitRecord, bool) {
	permit = strings.TrimSpace(permit)
	if h == nil || permit == "" {
		return registerPermitRecord{}, false
	}
	now := h.admissionNow()
	h.mu.Lock()
	h.cleanupExpiredAdmissionLocked(now)
	rec, ok := h.registerPermits[permit]
	if ok {
		delete(h.registerPermits, permit)
	}
	h.mu.Unlock()
	if ok {
		h.persistState()
	}
	return rec, ok
}

func (h *LoginHandler) consumeRegisterPermit(permit, deviceID string) (registerPermitRecord, error) {
	permit = strings.TrimSpace(permit)
	deviceID = strings.TrimSpace(deviceID)
	if permit == "" {
		return registerPermitRecord{}, fmt.Errorf("join_permit required")
	}
	now := h.admissionNow()
	h.mu.Lock()
	h.cleanupExpiredAdmissionLocked(now)
	rec, ok := h.registerPermits[permit]
	if !ok {
		h.mu.Unlock()
		return registerPermitRecord{}, fmt.Errorf("join permit not found or expired")
	}
	if rec.DeviceID != deviceID {
		h.mu.Unlock()
		return registerPermitRecord{}, fmt.Errorf("join permit device_id mismatch")
	}
	delete(h.registerPermits, permit)
	h.mu.Unlock()
	h.persistState()
	return rec, nil
}

func (h *LoginHandler) listPendingRegisters(req listPendingRegistersReq) listPendingRegistersResp {
	now := h.admissionNow()
	filterDeviceID := strings.TrimSpace(req.DeviceID)
	h.mu.Lock()
	h.cleanupExpiredAdmissionLocked(now)
	items := make([]pendingRegisterInfo, 0, len(h.pendingRegisters))
	for _, rec := range h.pendingRegisters {
		if filterDeviceID != "" && rec.DeviceID != filterDeviceID {
			continue
		}
		items = append(items, pendingRegisterInfo{
			RequestID:     rec.RequestID,
			DeviceID:      rec.DeviceID,
			RequestedRole: rec.RequestedRole,
			DisplayName:   rec.DisplayName,
			CreatedAt:     rec.CreatedAt,
			ExpiresAt:     rec.ExpiresAt,
		})
	}
	h.mu.Unlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt == items[j].CreatedAt {
			return items[i].RequestID < items[j].RequestID
		}
		return items[i].CreatedAt < items[j].CreatedAt
	})
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	total := len(items)
	if offset >= total {
		return listPendingRegistersResp{Code: 1, Msg: "ok", Total: total, Items: []pendingRegisterInfo{}}
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return listPendingRegistersResp{
		Code:  1,
		Msg:   "ok",
		Total: total,
		Items: items[offset:end],
	}
}

func (h *LoginHandler) listRegisterPermits(req listRegisterPermitsReq) listRegisterPermitsResp {
	now := h.admissionNow()
	filterDeviceID := strings.TrimSpace(req.DeviceID)
	h.mu.Lock()
	h.cleanupExpiredAdmissionLocked(now)
	items := make([]registerPermitInfo, 0, len(h.registerPermits))
	for _, rec := range h.registerPermits {
		if filterDeviceID != "" && rec.DeviceID != filterDeviceID {
			continue
		}
		items = append(items, registerPermitInfo{
			Permit:    rec.Permit,
			DeviceID:  rec.DeviceID,
			Role:      rec.Role,
			IssuedBy:  rec.IssuedBy,
			IssuedAt:  rec.IssuedAt,
			ExpiresAt: rec.ExpiresAt,
		})
	}
	h.mu.Unlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].IssuedAt == items[j].IssuedAt {
			return items[i].Permit < items[j].Permit
		}
		return items[i].IssuedAt > items[j].IssuedAt
	})
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	total := len(items)
	if offset >= total {
		return listRegisterPermitsResp{Code: 1, Msg: "ok", Total: total, Items: []registerPermitInfo{}}
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return listRegisterPermitsResp{
		Code:  1,
		Msg:   "ok",
		Total: total,
		Items: items[offset:end],
	}
}

func (h *LoginHandler) resolveAdmissionRole(values ...string) (string, error) {
	for _, value := range values {
		role := strings.TrimSpace(value)
		if role == "" {
			continue
		}
		if !h.roleKnown(role) {
			return "", fmt.Errorf("unknown role: %s", role)
		}
		return role, nil
	}
	return "", nil
}

func (h *LoginHandler) roleKnown(role string) bool {
	role = strings.TrimSpace(role)
	if role == "" {
		return false
	}
	if h.permCfg == nil {
		return false
	}
	return h.permCfg.HasRole(role)
}

func (h *LoginHandler) effectivePendingTTL() time.Duration {
	if h == nil || h.pendingTTL <= 0 {
		return defaultPendingTTL
	}
	return h.pendingTTL
}

func (h *LoginHandler) effectivePermitTTL() time.Duration {
	if h == nil || h.permitTTL <= 0 {
		return defaultPermitTTL
	}
	return h.permitTTL
}

func newAdmissionToken(prefix string) (string, error) {
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return token, nil
	}
	return prefix + "_" + token, nil
}
