package auth

// Context: This file belongs to the SubProto implementation layer around authority_policy_test.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/header"
)

func TestBindServer_SemiCentralRootBroadcastsAuthorityPolicy(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		configKeyAuthAuthorityMode:      authorityModeConfigSemiCentral,
		configKeyAuthAuthorityPolicyTTL: "30",
	})
	cm := connmgr.New()
	child := &mockConnection{id: "child-root-policy"}
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	_ = cm.Add(child)

	srv := newRecordingAuthServerWithConfig(1, cm, cfg)
	h := newSemiCentralTestHandler(cfg)

	h.BindServer(srv)

	if len(srv.sent) == 0 {
		t.Fatalf("expected authority policy broadcast")
	}
	if srv.sent[0].connID != child.ID() {
		t.Fatalf("expected broadcast to child conn, got %q", srv.sent[0].connID)
	}
	if srv.sent[0].header == nil {
		t.Fatalf("expected authority policy header")
	}
	if srv.sent[0].header.Major() != header.MajorCmd {
		t.Fatalf("unexpected major: got %d want %d", srv.sent[0].header.Major(), header.MajorCmd)
	}
	if srv.sent[0].header.SourceID() != 1 {
		t.Fatalf("unexpected source: got %d want 1", srv.sent[0].header.SourceID())
	}
	frame, policy := decodeAuthFrame[authorityPolicySyncData](t, srv.sent[0].payload)
	if frame.Action != actionAuthorityPolicySync {
		t.Fatalf("unexpected action: got %q want %q", frame.Action, actionAuthorityPolicySync)
	}
	if policy.EffectiveAuthorityID != 1 {
		t.Fatalf("unexpected authority id: got %d want 1", policy.EffectiveAuthorityID)
	}
	if policy.Mode != authorityModeConfigSemiCentral {
		t.Fatalf("unexpected mode: got %q want %q", policy.Mode, authorityModeConfigSemiCentral)
	}
}

func TestHandleAuthorityPolicySync_AppliesAndForwardsNewestOnly(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		configKeyAuthAuthorityMode:      authorityModeConfigSemiCentral,
		configKeyAuthAuthorityPolicyTTL: "45",
	})
	cm := connmgr.New()
	parent := &mockConnection{id: "parent-policy"}
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	parent.SetMeta("nodeID", uint32(9))
	_ = cm.Add(parent)
	child := &mockConnection{id: "child-policy"}
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	child.SetMeta("nodeID", uint32(20))
	_ = cm.Add(child)

	srv := newRecordingAuthServerWithConfig(5, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)
	h := newSemiCentralTestHandler(cfg)

	raw, err := json.Marshal(authorityPolicySyncData{
		Mode:                 authorityModeConfigSemiCentral,
		EffectiveAuthorityID: 1,
		Epoch:                7,
		TTLSec:               60,
	})
	if err != nil {
		t.Fatalf("marshal authority policy: %v", err)
	}
	h.handleAuthorityPolicySync(ctx, parent, raw)

	policy, ok := h.currentRuntimeAuthorityPolicy(time.Now().UTC())
	if !ok {
		t.Fatalf("expected runtime policy applied")
	}
	if policy.effectiveAuthorityID != 1 || policy.epoch != 7 {
		t.Fatalf("unexpected runtime policy: %+v", policy)
	}
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 forwarded policy frame, got %d", len(srv.sent))
	}
	if srv.sent[0].connID != child.ID() {
		t.Fatalf("expected policy forwarded to child, got %q", srv.sent[0].connID)
	}
	frame, forwarded := decodeAuthFrame[authorityPolicySyncData](t, srv.sent[0].payload)
	if frame.Action != actionAuthorityPolicySync {
		t.Fatalf("unexpected action: got %q want %q", frame.Action, actionAuthorityPolicySync)
	}
	if forwarded.Epoch != 7 || forwarded.EffectiveAuthorityID != 1 {
		t.Fatalf("unexpected forwarded policy: %+v", forwarded)
	}

	srv.sent = nil
	staleRaw, err := json.Marshal(authorityPolicySyncData{
		Mode:                 authorityModeConfigSemiCentral,
		EffectiveAuthorityID: 2,
		Epoch:                6,
		TTLSec:               60,
	})
	if err != nil {
		t.Fatalf("marshal stale authority policy: %v", err)
	}
	h.handleAuthorityPolicySync(ctx, parent, staleRaw)

	policy, ok = h.currentRuntimeAuthorityPolicy(time.Now().UTC())
	if !ok || policy.effectiveAuthorityID != 1 || policy.epoch != 7 {
		t.Fatalf("stale policy should be ignored, got %+v", policy)
	}
	if len(srv.sent) != 0 {
		t.Fatalf("stale policy must not be forwarded, got %d frames", len(srv.sent))
	}
}

func TestOnReceive_SemiCentralForwardsAssistRegisterByHeaderTarget(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		configKeyAuthAuthorityMode:      authorityModeConfigSemiCentral,
		configKeyAuthAuthorityPolicyTTL: "30",
		coreconfig.KeyParentEnable:      "true",
		coreconfig.KeyParentAddr:        "tcp://root.example:9000",
	})
	cm := connmgr.New()
	leaf := &mockConnection{id: "leaf-header-forward"}
	leaf.SetMeta(core.MetaRoleKey, core.RoleChild)
	leaf.SetMeta("nodeID", uint32(11))
	_ = cm.Add(leaf)
	parent := &mockConnection{id: "parent-header-forward"}
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	parent.SetMeta("nodeID", uint32(1))
	_ = cm.Add(parent)

	srv := newRecordingAuthServerWithConfig(9, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)
	h := newSemiCentralTestHandler(cfg)
	if !h.applyRuntimeAuthorityPolicy(time.Now().UTC(), authorityPolicySyncData{
		Mode:                 authorityModeConfigSemiCentral,
		EffectiveAuthorityID: 1,
		Epoch:                3,
		TTLSec:               30,
	}) {
		t.Fatalf("expected runtime policy apply")
	}

	payload := mustAuthMessage(t, actionAssistRegister, registerData{DeviceID: "dev-header-forward"})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(11).
		WithTargetID(1)

	h.OnReceive(ctx, leaf, hdr, payload)

	if len(srv.sent) != 1 {
		t.Fatalf("expected forwarded frame, got %d", len(srv.sent))
	}
	if srv.sent[0].connID != parent.ID() {
		t.Fatalf("expected forward to parent, got %q", srv.sent[0].connID)
	}
	if len(h.pendingConn) != 0 || len(h.whitelist) != 0 {
		t.Fatalf("header-target forward must not mutate local pending/binding state")
	}
	if srv.sent[0].header.SourceID() != 11 || srv.sent[0].header.TargetID() != 1 {
		t.Fatalf("unexpected forwarded header source/target: source=%d target=%d", srv.sent[0].header.SourceID(), srv.sent[0].header.TargetID())
	}
}

func TestHandleRegister_SemiCentralAssistPathForwardsUpstreamWithoutLocalState(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		configKeyAuthAuthorityMode: authorityModeConfigSemiCentral,
		coreconfig.KeyParentEnable: "true",
		coreconfig.KeyParentAddr:   "tcp://root.example:9000",
	})
	cm := connmgr.New()
	leaf := &mockConnection{id: "leaf-assist-forward"}
	leaf.SetMeta(core.MetaRoleKey, core.RoleChild)
	leaf.SetMeta("nodeID", uint32(11))
	_ = cm.Add(leaf)
	parent := &mockConnection{id: "parent-assist-forward"}
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	parent.SetMeta("nodeID", uint32(1))
	_ = cm.Add(parent)

	srv := newRecordingAuthServerWithConfig(9, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)
	h := newSemiCentralTestHandler(cfg)

	payload, err := json.Marshal(registerData{DeviceID: "dev-assist-forward"})
	if err != nil {
		t.Fatalf("marshal register: %v", err)
	}
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(11).
		WithTargetID(9)

	h.handleRegister(ctx, leaf, hdr, payload, true)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 upstream forwarded frame, got %d", len(srv.sent))
	}
	if srv.sent[0].connID != parent.ID() {
		t.Fatalf("expected forward to parent, got %q", srv.sent[0].connID)
	}
	if len(h.pendingConn) != 0 || len(h.whitelist) != 0 {
		t.Fatalf("assist forward must not create local pending/binding state")
	}
	frame, forwarded := decodeAuthFrame[registerData](t, srv.sent[0].payload)
	if frame.Action != actionAssistRegister {
		t.Fatalf("unexpected forwarded action: got %q want %q", frame.Action, actionAssistRegister)
	}
	if forwarded.DeviceID != "dev-assist-forward" {
		t.Fatalf("unexpected forwarded device: %+v", forwarded)
	}
	if srv.sent[0].header.SourceID() != 11 || srv.sent[0].header.TargetID() != 1 {
		t.Fatalf("unexpected forwarded header source/target: source=%d target=%d", srv.sent[0].header.SourceID(), srv.sent[0].header.TargetID())
	}
}

func TestHandleRegister_SemiCentralAssistResponseTargetsOriginEdgeHub(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		configKeyAuthAuthorityMode: authorityModeConfigSemiCentral,
	})
	cm := connmgr.New()
	child := &mockConnection{id: "child-assist-root"}
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	child.SetMeta("nodeID", uint32(9))
	_ = cm.Add(child)

	srv := newRecordingAuthServerWithConfig(1, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)
	h := newSemiCentralTestHandler(cfg)

	payload, err := json.Marshal(registerData{DeviceID: "leaf-root-register"})
	if err != nil {
		t.Fatalf("marshal register: %v", err)
	}
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(9).
		WithTargetID(1).
		WithMsgID(17).
		WithTraceID(23)

	h.handleRegister(ctx, child, hdr, payload, true)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 targeted assist response, got %d", len(srv.sent))
	}
	if srv.sent[0].header == nil {
		t.Fatalf("expected response header")
	}
	if srv.sent[0].header.Major() != header.MajorOKResp {
		t.Fatalf("unexpected response major: got %d want %d", srv.sent[0].header.Major(), header.MajorOKResp)
	}
	if srv.sent[0].header.SourceID() != 1 || srv.sent[0].header.TargetID() != 9 {
		t.Fatalf("unexpected response source/target: source=%d target=%d", srv.sent[0].header.SourceID(), srv.sent[0].header.TargetID())
	}
	if srv.sent[0].header.GetMsgID() != 17 || srv.sent[0].header.GetTraceID() != 23 {
		t.Fatalf("expected msg/trace preserved, got msg=%d trace=%d", srv.sent[0].header.GetMsgID(), srv.sent[0].header.GetTraceID())
	}
	frame, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if frame.Action != actionAssistRegisterResp {
		t.Fatalf("unexpected action: got %q want %q", frame.Action, actionAssistRegisterResp)
	}
	if resp.Code != 1 || resp.NodeID == 0 {
		t.Fatalf("unexpected assist register resp: %+v", resp)
	}
}

func TestHandleLogin_SemiCentralDegradedKnownDeviceStillSucceeds(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		configKeyAuthAuthorityMode: authorityModeConfigSemiCentral,
		coreconfig.KeyParentEnable: "true",
		coreconfig.KeyParentAddr:   "tcp://root.example:9000",
	})
	priv, pubRaw, _ := mustKeyPair(t)

	cm := connmgr.New()
	device := &mockConnection{id: "device-degraded-known"}
	_ = cm.Add(device)
	srv := newRecordingAuthServerWithConfig(9, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)
	h := newSemiCentralTestHandler(cfg)
	h.whitelist["dev-known"] = bindingRecord{NodeID: 21, PubKey: cloneSlice(pubRaw)}

	req := loginData{
		DeviceID: "dev-known",
		NodeID:   21,
		TS:       123,
		Nonce:    "nonce-known",
		Alg:      defaultAlgES256,
	}
	req.Sig = signWithNodeKey(priv, loginSignBytes(req))
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal login: %v", err)
	}

	h.handleLogin(ctx, device, nil, raw, false)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 login response, got %d", len(srv.sent))
	}
	_, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if resp.Code != 1 || resp.NodeID != 21 {
		t.Fatalf("unexpected degraded known-device login resp: %+v", resp)
	}
}

func TestHandleLogin_SemiCentralDegradedUnknownDeviceFails(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		configKeyAuthAuthorityMode: authorityModeConfigSemiCentral,
		coreconfig.KeyParentEnable: "true",
		coreconfig.KeyParentAddr:   "tcp://root.example:9000",
	})
	cm := connmgr.New()
	device := &mockConnection{id: "device-degraded-unknown"}
	_ = cm.Add(device)
	srv := newRecordingAuthServerWithConfig(9, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)
	h := newSemiCentralTestHandler(cfg)

	raw, err := json.Marshal(loginData{
		DeviceID: "dev-unknown",
		TS:       1,
		Nonce:    "nonce-unknown",
		Sig:      "sig",
		Alg:      defaultAlgES256,
	})
	if err != nil {
		t.Fatalf("marshal login: %v", err)
	}

	h.handleLogin(ctx, device, nil, raw, false)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 login response, got %d", len(srv.sent))
	}
	_, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if resp.Code != 4500 || resp.Msg != "authority unavailable" {
		t.Fatalf("unexpected degraded unknown-device login resp: %+v", resp)
	}
}

func newSemiCentralTestHandler(cfg core.IConfig) *LoginHandler {
	h := newAdmissionTestHandler(false, "")
	h.loadAuthorityPolicyConfig(cfg)
	return h
}

func mustAuthMessage(t *testing.T, action string, data any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal auth data: %v", err)
	}
	payload, err := json.Marshal(message{Action: action, Data: raw})
	if err != nil {
		t.Fatalf("marshal auth message: %v", err)
	}
	return payload
}
