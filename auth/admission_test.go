package auth

// 本文件覆盖 SubProto 中 `auth` 模块里与 `admission` 相关的行为。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/header"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
)

func TestHandleRegister_RequireApprovalReturnsPendingWithoutSideEffects(t *testing.T) {
	cm := connmgr.New()
	conn := &mockConnection{id: "child-pending"}
	_ = cm.Add(conn)
	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h := newAdmissionTestHandler(true, "")

	raw, err := json.Marshal(registerData{DeviceID: "dev-pending", RequestedRole: "admin"})
	if err != nil {
		t.Fatalf("marshal register data: %v", err)
	}
	h.handleRegister(ctx, conn, nil, raw, false)

	if len(h.whitelist) != 0 {
		t.Fatalf("whitelist should stay empty while pending")
	}
	if len(h.pendingRegisters) != 1 {
		t.Fatalf("expected 1 pending register, got %d", len(h.pendingRegisters))
	}
	if _, ok := conn.GetMeta("nodeID"); ok {
		t.Fatalf("pending register should not bind nodeID to connection")
	}
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response, got %d", len(srv.sent))
	}
	frame, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if frame.Action != actionRegisterResp {
		t.Fatalf("unexpected action: got %q want %q", frame.Action, actionRegisterResp)
	}
	if resp.Status != admissionStatusPending {
		t.Fatalf("unexpected status: got %q want %q", resp.Status, admissionStatusPending)
	}
	if resp.RequestID == "" {
		t.Fatalf("pending response should include request_id")
	}
	if resp.NodeID != 0 {
		t.Fatalf("pending response should not include node_id, got %d", resp.NodeID)
	}
}

func TestApprovePendingRegisterThenRetryRegisterSucceeds(t *testing.T) {
	cm := connmgr.New()
	conn := &mockConnection{id: "child-approved"}
	_ = cm.Add(conn)
	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h := newAdmissionTestHandler(true, "admin:auth.pending.list")

	initialRaw, err := json.Marshal(registerData{DeviceID: "dev-approved", RequestedRole: "admin"})
	if err != nil {
		t.Fatalf("marshal register data: %v", err)
	}
	h.handleRegister(ctx, conn, nil, initialRaw, false)
	requestID := h.pendingByDevice["dev-approved"]
	if requestID == "" {
		t.Fatalf("expected pending request id")
	}

	approved, err := h.approvePendingRegister(requestID, "")
	if err != nil {
		t.Fatalf("approvePendingRegister: %v", err)
	}
	if len(h.whitelist) != 0 {
		t.Fatalf("approval should not finalize binding before retry register")
	}

	srv.sent = nil
	retryRaw, err := json.Marshal(registerData{DeviceID: "dev-approved"})
	if err != nil {
		t.Fatalf("marshal retry data: %v", err)
	}
	h.handleRegister(ctx, conn, nil, retryRaw, false)

	rec, ok := h.whitelist["dev-approved"]
	if !ok {
		t.Fatalf("expected whitelist entry after approved retry")
	}
	if rec.NodeID != approved.NodeID {
		t.Fatalf("unexpected node id: got %d want %d", rec.NodeID, approved.NodeID)
	}
	if rec.Role != "admin" {
		t.Fatalf("unexpected role: got %q want %q", rec.Role, "admin")
	}
	if _, ok := h.approvedRegisters["dev-approved"]; ok {
		t.Fatalf("approved register should be consumed after successful retry")
	}
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 success response, got %d", len(srv.sent))
	}
	_, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if resp.Status != admissionStatusApproved {
		t.Fatalf("unexpected status: got %q want %q", resp.Status, admissionStatusApproved)
	}
	if resp.NodeID != approved.NodeID {
		t.Fatalf("unexpected node id in response: got %d want %d", resp.NodeID, approved.NodeID)
	}
	if resp.Role != "admin" {
		t.Fatalf("unexpected role in response: got %q want %q", resp.Role, "admin")
	}
}

func TestHandleRegister_PermitConsumedOnlyOnce(t *testing.T) {
	cm := connmgr.New()
	conn := &mockConnection{id: "child-permit"}
	_ = cm.Add(conn)
	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h := newAdmissionTestHandler(false, "admin:auth.permit.issue")
	permit, err := h.issueRegisterPermit("dev-permit", "admin", 0, 9)
	if err != nil {
		t.Fatalf("issueRegisterPermit: %v", err)
	}

	raw, err := json.Marshal(registerData{DeviceID: "dev-permit", JoinPermit: permit.Permit})
	if err != nil {
		t.Fatalf("marshal register data: %v", err)
	}
	h.handleRegister(ctx, conn, nil, raw, false)
	if _, ok := h.whitelist["dev-permit"]; !ok {
		t.Fatalf("permit register should create whitelist entry")
	}

	if !h.removeBinding("dev-permit") {
		t.Fatalf("expected binding removal to succeed")
	}
	srv.sent = nil
	h.handleRegister(ctx, conn, nil, raw, false)
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response after reused permit, got %d", len(srv.sent))
	}
	_, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if resp.Status != admissionStatusRejected {
		t.Fatalf("unexpected status: got %q want %q", resp.Status, admissionStatusRejected)
	}
}

func TestHandleRegister_PermitDeviceMismatchDoesNotConsume(t *testing.T) {
	cm := connmgr.New()
	conn := &mockConnection{id: "child-mismatch"}
	_ = cm.Add(conn)
	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h := newAdmissionTestHandler(false, "admin:auth.permit.issue")
	permit, err := h.issueRegisterPermit("dev-a", "admin", 0, 9)
	if err != nil {
		t.Fatalf("issueRegisterPermit: %v", err)
	}

	badRaw, err := json.Marshal(registerData{DeviceID: "dev-b", JoinPermit: permit.Permit})
	if err != nil {
		t.Fatalf("marshal bad register data: %v", err)
	}
	h.handleRegister(ctx, conn, nil, badRaw, false)
	if _, ok := h.registerPermits[permit.Permit]; !ok {
		t.Fatalf("device mismatch should not consume permit")
	}

	srv.sent = nil
	goodRaw, err := json.Marshal(registerData{DeviceID: "dev-a", JoinPermit: permit.Permit})
	if err != nil {
		t.Fatalf("marshal good register data: %v", err)
	}
	h.handleRegister(ctx, conn, nil, goodRaw, false)
	if _, ok := h.whitelist["dev-a"]; !ok {
		t.Fatalf("expected successful permit register after mismatch attempt")
	}
}

func TestHandleIssueRegisterPermit_PermissionDenied(t *testing.T) {
	cm := connmgr.New()
	conn := &mockConnection{id: "child-action"}
	conn.SetMeta("nodeID", uint32(5))
	_ = cm.Add(conn)
	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h := newAdmissionTestHandler(false, "")
	h.whitelist["actor"] = bindingRecord{NodeID: 5, Role: "node"}
	reqRaw, err := json.Marshal(issueRegisterPermitReq{DeviceID: "dev-action", Role: "node"})
	if err != nil {
		t.Fatalf("marshal issue permit req: %v", err)
	}

	hdr := (&header.HeaderTcp{}).WithSourceID(5)
	h.handleIssueRegisterPermit(ctx, conn, hdr, reqRaw)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response, got %d", len(srv.sent))
	}
	frame, resp := decodeAuthFrame[issueRegisterPermitResp](t, srv.sent[0].payload)
	if frame.Action != actionIssueRegisterPermitResp {
		t.Fatalf("unexpected action: got %q want %q", frame.Action, actionIssueRegisterPermitResp)
	}
	if resp.Code != 4403 {
		t.Fatalf("unexpected code: got %d want 4403", resp.Code)
	}
}

func TestHandleListRegisterPermits_PermissionDenied(t *testing.T) {
	cm := connmgr.New()
	conn := &mockConnection{id: "child-list-action"}
	conn.SetMeta("nodeID", uint32(5))
	_ = cm.Add(conn)
	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h := newAdmissionTestHandler(false, "")
	h.whitelist["actor"] = bindingRecord{NodeID: 5, Role: "node"}
	reqRaw, err := json.Marshal(listRegisterPermitsReq{Limit: 10})
	if err != nil {
		t.Fatalf("marshal list permit req: %v", err)
	}

	hdr := (&header.HeaderTcp{}).WithSourceID(5)
	h.handleListRegisterPermits(ctx, conn, hdr, reqRaw)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response, got %d", len(srv.sent))
	}
	frame, resp := decodeAuthFrame[listRegisterPermitsResp](t, srv.sent[0].payload)
	if frame.Action != actionListRegisterPermitsResp {
		t.Fatalf("unexpected action: got %q want %q", frame.Action, actionListRegisterPermitsResp)
	}
	if resp.Code != 4403 {
		t.Fatalf("unexpected code: got %d want 4403", resp.Code)
	}
}

func TestHandleListRegisterPermits_AllowsPermitRevokeRoleAndCleansExpired(t *testing.T) {
	cm := connmgr.New()
	conn := &mockConnection{id: "child-list"}
	conn.SetMeta("nodeID", uint32(5))
	_ = cm.Add(conn)
	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h := newAdmissionTestHandler(false, "admin:auth.permit.revoke")
	h.whitelist["actor"] = bindingRecord{NodeID: 5, Role: "admin"}
	now := h.now()
	h.registerPermits["permit-old"] = registerPermitRecord{
		Permit:    "permit-old",
		DeviceID:  "dev-a",
		Role:      "admin",
		IssuedBy:  9,
		IssuedAt:  now.Add(-10 * time.Minute).Unix(),
		ExpiresAt: now.Add(30 * time.Minute).Unix(),
	}
	h.registerPermits["permit-new"] = registerPermitRecord{
		Permit:    "permit-new",
		DeviceID:  "dev-b",
		Role:      "admin",
		IssuedBy:  9,
		IssuedAt:  now.Add(-5 * time.Minute).Unix(),
		ExpiresAt: now.Add(40 * time.Minute).Unix(),
	}
	h.registerPermits["permit-expired"] = registerPermitRecord{
		Permit:    "permit-expired",
		DeviceID:  "dev-c",
		Role:      "admin",
		IssuedBy:  9,
		IssuedAt:  now.Add(-20 * time.Minute).Unix(),
		ExpiresAt: now.Add(-time.Minute).Unix(),
	}
	reqRaw, err := json.Marshal(listRegisterPermitsReq{Offset: 0, Limit: 10})
	if err != nil {
		t.Fatalf("marshal list permit req: %v", err)
	}

	hdr := (&header.HeaderTcp{}).WithSourceID(5)
	h.handleListRegisterPermits(ctx, conn, hdr, reqRaw)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response, got %d", len(srv.sent))
	}
	frame, resp := decodeAuthFrame[listRegisterPermitsResp](t, srv.sent[0].payload)
	if frame.Action != actionListRegisterPermitsResp {
		t.Fatalf("unexpected action: got %q want %q", frame.Action, actionListRegisterPermitsResp)
	}
	if resp.Code != 1 {
		t.Fatalf("unexpected code: got %d want 1", resp.Code)
	}
	if resp.Total != 2 {
		t.Fatalf("unexpected total: got %d want 2", resp.Total)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("unexpected item count: got %d want 2", len(resp.Items))
	}
	if resp.Items[0].Permit != "permit-new" || resp.Items[1].Permit != "permit-old" {
		t.Fatalf("unexpected order: %+v", resp.Items)
	}
	if _, ok := h.registerPermits["permit-expired"]; ok {
		t.Fatalf("expired permit should be cleaned before list")
	}

	filtered := h.listRegisterPermits(listRegisterPermitsReq{DeviceID: "dev-a"})
	if filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].Permit != "permit-old" {
		t.Fatalf("unexpected filtered result: %+v", filtered)
	}
}

func TestListRegisterPermits_ReflectsRevokeAndConsume(t *testing.T) {
	h := newAdmissionTestHandler(false, "admin:auth.permit.issue")

	permitA, err := h.issueRegisterPermit("dev-a", "admin", 0, 9)
	if err != nil {
		t.Fatalf("issueRegisterPermit A: %v", err)
	}
	permitB, err := h.issueRegisterPermit("dev-b", "admin", 0, 9)
	if err != nil {
		t.Fatalf("issueRegisterPermit B: %v", err)
	}
	listed := h.listRegisterPermits(listRegisterPermitsReq{Offset: 0, Limit: 10})
	if listed.Total != 2 {
		t.Fatalf("unexpected total before revoke/consume: got %d want 2", listed.Total)
	}

	if _, ok := h.revokeRegisterPermit(permitA.Permit); !ok {
		t.Fatalf("expected revoke to succeed")
	}
	if _, err := h.consumeRegisterPermit(permitB.Permit, "dev-b"); err != nil {
		t.Fatalf("consumeRegisterPermit: %v", err)
	}

	listed = h.listRegisterPermits(listRegisterPermitsReq{Offset: 0, Limit: 10})
	if listed.Total != 0 || len(listed.Items) != 0 {
		t.Fatalf("expected empty permit list after revoke and consume, got %+v", listed)
	}
}

func TestPersistAndReloadAdmissionState(t *testing.T) {
	tempDir := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})

	now := time.Now().UTC()
	h := &LoginHandler{
		whitelist:                   map[string]bindingRecord{"dev-a": {NodeID: 7, Role: "node", Perms: []string{"exec.call"}}},
		pendingRegisters:            map[string]pendingRegisterRecord{"req-1": {RequestID: "req-1", DeviceID: "dev-pending", RequestedRole: "admin", CreatedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix()}},
		pendingByDevice:             map[string]string{"dev-pending": "req-1"},
		approvedRegisters:           map[string]approvedRegisterRecord{"dev-approved": {RequestID: "req-2", DeviceID: "dev-approved", NodeID: 8, Role: "admin", ApprovedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix()}},
		registerPermits:             map[string]registerPermitRecord{"permit-1": {Permit: "permit-1", DeviceID: "dev-permit", Role: "admin", IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix()}},
		firstRegisterBootstrapState: firstRegisterBootstrapState{ConsumedEpoch: 3, ConsumedAt: now.Unix(), DeviceID: "dev-bootstrap", NodeID: 9, Role: "admin"},
		now:                         func() time.Time { return now },
	}
	h.persistState()

	if _, err := os.Stat(filepath.Join(tempDir, trustedNodesFile)); err != nil {
		t.Fatalf("trusted state file not written: %v", err)
	}

	wl, _, pending, approved, permits, bootstrapState, maxNode, err := loadTrustedBindings(nil)
	if err != nil {
		t.Fatalf("loadTrustedBindings: %v", err)
	}
	if len(wl) != 1 {
		t.Fatalf("unexpected whitelist size: got %d want 1", len(wl))
	}
	if len(pending) != 1 {
		t.Fatalf("unexpected pending size: got %d want 1", len(pending))
	}
	if len(approved) != 1 {
		t.Fatalf("unexpected approved size: got %d want 1", len(approved))
	}
	if len(permits) != 1 {
		t.Fatalf("unexpected permits size: got %d want 1", len(permits))
	}
	if bootstrapState.ConsumedEpoch != 3 || bootstrapState.NodeID != 9 {
		t.Fatalf("unexpected bootstrap state: %+v", bootstrapState)
	}
	if maxNode != 9 {
		t.Fatalf("unexpected max node: got %d want 9", maxNode)
	}
}

func newAdmissionTestHandler(requireApproval bool, rolePerms string) *LoginHandler {
	cfg := coreconfig.NewMap(map[string]string{
		coreconfig.KeyAuthDefaultRole: "node",
		coreconfig.KeyAuthRolePerms:   rolePerms,
	})
	now := time.Unix(1_700_000_000, 0).UTC()
	h := &LoginHandler{
		whitelist:         make(map[string]bindingRecord),
		pendingConn:       make(map[string]pendingInfo),
		pendingRegisters:  make(map[string]pendingRegisterRecord),
		pendingByDevice:   make(map[string]string),
		approvedRegisters: make(map[string]approvedRegisterRecord),
		registerPermits:   make(map[string]registerPermitRecord),
		disablePersist:    true,
		requireApproval:   requireApproval,
		pendingTTL:        time.Hour,
		permitTTL:         time.Hour,
		permCfg:           permission.NewConfig(cfg),
		now:               func() time.Time { return now },
	}
	h.nextID.Store(2)
	return h
}
