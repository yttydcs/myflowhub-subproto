package auth

// Context: This file belongs to the SubProto implementation layer around authority_test.

import (
	"context"
	"encoding/json"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
)

func TestHandleRegister_ExplicitAuthorityUnavailableDoesNotFallback(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		"authority.node_id":        "99",
		coreconfig.KeyParentEnable: "true",
		coreconfig.KeyParentAddr:   "tcp://parent.example:9000",
	})
	cm := connmgr.New()
	device := &mockConnection{id: "device"}
	parent := &mockConnection{id: "parent"}
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	parent.SetMeta("nodeID", uint32(77))
	_ = cm.Add(device)
	_ = cm.Add(parent)
	srv := newRecordingAuthServerWithConfig(1, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)

	h := newAdmissionTestHandler(false, "")
	raw, err := json.Marshal(registerData{DeviceID: "dev-explicit"})
	if err != nil {
		t.Fatalf("marshal register data: %v", err)
	}

	h.handleRegister(ctx, device, nil, raw, false)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response, got %d", len(srv.sent))
	}
	if srv.sent[0].connID != device.ID() {
		t.Fatalf("register should reply to device, got conn_id=%q", srv.sent[0].connID)
	}
	_, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if resp.Code != 4500 || resp.Msg != "authority unavailable" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(h.whitelist) != 0 {
		t.Fatalf("explicit authority unavailable should not create whitelist entry")
	}
	if len(h.pendingRegisters) != 0 {
		t.Fatalf("explicit authority unavailable should not create pending register")
	}
}

func TestHandleRegister_ParentConfiguredButUnavailableDoesNotFallback(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		coreconfig.KeyParentEnable: "true",
		coreconfig.KeyParentAddr:   "tcp://parent.example:9000",
	})
	cm := connmgr.New()
	device := &mockConnection{id: "device"}
	_ = cm.Add(device)
	srv := newRecordingAuthServerWithConfig(1, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)

	h := newAdmissionTestHandler(false, "")
	raw, err := json.Marshal(registerData{DeviceID: "dev-parent"})
	if err != nil {
		t.Fatalf("marshal register data: %v", err)
	}

	h.handleRegister(ctx, device, nil, raw, false)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response, got %d", len(srv.sent))
	}
	if srv.sent[0].connID != device.ID() {
		t.Fatalf("register should reply to device, got conn_id=%q", srv.sent[0].connID)
	}
	_, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if resp.Code != 4500 || resp.Msg != "authority unavailable" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if len(h.whitelist) != 0 {
		t.Fatalf("parent unavailable should not create whitelist entry")
	}
}

func TestHandleRegister_NoAuthorityOrParentConfiguredUsesLocalAuthority(t *testing.T) {
	cm := connmgr.New()
	device := &mockConnection{id: "device"}
	_ = cm.Add(device)
	srv := newRecordingAuthServerWithConfig(1, cm, coreconfig.NewMap(nil))
	ctx := core.WithServerContext(context.Background(), srv)

	h := newAdmissionTestHandler(false, "")
	raw, err := json.Marshal(registerData{DeviceID: "dev-local"})
	if err != nil {
		t.Fatalf("marshal register data: %v", err)
	}

	h.handleRegister(ctx, device, nil, raw, false)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response, got %d", len(srv.sent))
	}
	_, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if resp.Code != 1 || resp.Status != admissionStatusApproved || resp.NodeID == 0 {
		t.Fatalf("unexpected local authority response: %+v", resp)
	}
	if len(h.whitelist) != 1 {
		t.Fatalf("local authority should create whitelist entry")
	}
}

func TestHandleLogin_ParentConfiguredButUnavailableReturnsAuthorityUnavailable(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		coreconfig.KeyParentEnable: "true",
		coreconfig.KeyParentAddr:   "tcp://parent.example:9000",
	})
	cm := connmgr.New()
	device := &mockConnection{id: "device"}
	_ = cm.Add(device)
	srv := newRecordingAuthServerWithConfig(1, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)

	h := newAdmissionTestHandler(false, "")
	raw, err := json.Marshal(loginData{DeviceID: "dev-login", TS: 1, Nonce: "n1", Sig: "sig", Alg: defaultAlgES256})
	if err != nil {
		t.Fatalf("marshal login data: %v", err)
	}

	h.handleLogin(ctx, device, nil, raw, false)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response, got %d", len(srv.sent))
	}
	_, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if resp.Code != 4500 || resp.Msg != "authority unavailable" {
		t.Fatalf("unexpected login response: %+v", resp)
	}
}
