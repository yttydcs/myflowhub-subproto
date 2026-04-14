package auth

// 本文件覆盖 SubProto 中 `auth` 模块里与 `remote_admin` 相关的行为。

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/header"
)

func TestOnReceive_SemiCentralForwardsRemoteAdminActionsByHeaderTarget(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		configKeyAuthAuthorityMode:      authorityModeConfigSemiCentral,
		configKeyAuthAuthorityPolicyTTL: "30",
		coreconfig.KeyParentEnable:      "true",
		coreconfig.KeyParentAddr:        "tcp://root.example:9000",
	})
	cm := connmgr.New()
	leaf := &mockConnection{id: "leaf-admin-forward"}
	leaf.SetMeta(core.MetaRoleKey, core.RoleChild)
	leaf.SetMeta("nodeID", uint32(11))
	_ = cm.Add(leaf)
	parent := &mockConnection{id: "parent-admin-forward"}
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	parent.SetMeta("nodeID", uint32(1))
	_ = cm.Add(parent)

	srv := newRecordingAuthServerWithConfig(9, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)
	h := newSemiCentralTestHandler(cfg)
	h.log = slog.Default()
	h.initActions()
	if !h.applyRuntimeAuthorityPolicy(time.Now().UTC(), authorityPolicySyncData{
		Mode:                 authorityModeConfigSemiCentral,
		EffectiveAuthorityID: 1,
		Epoch:                5,
		TTLSec:               30,
	}) {
		t.Fatalf("expected runtime authority policy apply")
	}

	cases := []struct {
		name   string
		action string
		data   any
	}{
		{name: "list pending", action: actionListPendingRegisters, data: listPendingRegistersReq{Limit: 10}},
		{name: "approve", action: actionApproveRegister, data: approveRegisterReq{RequestID: "req-1"}},
		{name: "reject", action: actionRejectRegister, data: rejectRegisterReq{RequestID: "req-1"}},
		{name: "list permits", action: actionListRegisterPermits, data: listRegisterPermitsReq{Limit: 10}},
		{name: "issue permit", action: actionIssueRegisterPermit, data: issueRegisterPermitReq{DeviceID: "dev-a", Role: "admin"}},
		{name: "revoke permit", action: actionRevokeRegisterPermit, data: revokeRegisterPermitReq{Permit: "permit-1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv.sent = nil
			payload := mustAuthMessage(t, tc.action, tc.data)
			hdr := (&header.HeaderTcp{}).
				WithMajor(header.MajorCmd).
				WithSubProto(2).
				WithSourceID(11).
				WithTargetID(1)

			h.OnReceive(ctx, leaf, hdr, payload)

			if len(srv.sent) != 1 {
				t.Fatalf("expected 1 forwarded frame, got %d", len(srv.sent))
			}
			if srv.sent[0].connID != parent.ID() {
				t.Fatalf("expected forward to parent, got %q", srv.sent[0].connID)
			}
			if srv.sent[0].header.SourceID() != 11 || srv.sent[0].header.TargetID() != 1 {
				t.Fatalf("unexpected forwarded header source/target: source=%d target=%d", srv.sent[0].header.SourceID(), srv.sent[0].header.TargetID())
			}
			frame, _ := decodeAuthFrame[map[string]any](t, srv.sent[0].payload)
			if frame.Action != tc.action {
				t.Fatalf("unexpected forwarded action: got %q want %q", frame.Action, tc.action)
			}
		})
	}
}

func TestHandleListRegisterPermits_RemoteAuthorityForwardPreservesInheritedSource(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		configKeyAuthAuthorityMode: authorityModeConfigSemiCentral,
		coreconfig.KeyParentEnable: "true",
		coreconfig.KeyParentAddr:   "tcp://root.example:9000",
	})
	cm := connmgr.New()
	leaf := &mockConnection{id: "leaf-admin-handler-forward"}
	leaf.SetMeta(core.MetaRoleKey, core.RoleChild)
	leaf.SetMeta("nodeID", uint32(11))
	_ = cm.Add(leaf)
	parent := &mockConnection{id: "parent-admin-handler-forward"}
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	parent.SetMeta("nodeID", uint32(1))
	_ = cm.Add(parent)

	srv := newRecordingAuthServerWithConfig(9, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)
	h := newSemiCentralTestHandler(cfg)
	reqRaw, err := json.Marshal(listRegisterPermitsReq{Limit: 10})
	if err != nil {
		t.Fatalf("marshal list permit req: %v", err)
	}
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(11)

	h.handleListRegisterPermits(ctx, leaf, hdr, reqRaw)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 forwarded frame, got %d", len(srv.sent))
	}
	if srv.sent[0].connID != parent.ID() {
		t.Fatalf("expected forward to parent, got %q", srv.sent[0].connID)
	}
	if srv.sent[0].header.SourceID() != 11 || srv.sent[0].header.TargetID() != 1 {
		t.Fatalf("unexpected forwarded source/target: source=%d target=%d", srv.sent[0].header.SourceID(), srv.sent[0].header.TargetID())
	}
	frame, _ := decodeAuthFrame[listRegisterPermitsReq](t, srv.sent[0].payload)
	if frame.Action != actionListRegisterPermits {
		t.Fatalf("unexpected forwarded action: got %q want %q", frame.Action, actionListRegisterPermits)
	}
}

func TestOnReceive_RemoteAdminActionAllowsRoutedSourceAndTargetsResponse(t *testing.T) {
	cm := connmgr.New()
	child := &mockConnection{id: "child-admin-root"}
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	child.SetMeta("nodeID", uint32(9))
	_ = cm.Add(child)
	cm.AddNodeIndex(11, child)

	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)
	h := newAdmissionTestHandler(false, "admin:auth.pending.list")
	h.log = slog.Default()
	h.initActions()
	h.whitelist["actor-11"] = bindingRecord{NodeID: 11, Role: "admin"}
	now := h.now()
	h.pendingRegisters["req-1"] = pendingRegisterRecord{
		RequestID:     "req-1",
		DeviceID:      "dev-pending",
		RequestedRole: "admin",
		CreatedAt:     now.Unix(),
		ExpiresAt:     now.Add(time.Hour).Unix(),
	}
	h.pendingByDevice["dev-pending"] = "req-1"

	payload := mustAuthMessage(t, actionListPendingRegisters, listPendingRegistersReq{Limit: 10})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(11).
		WithTargetID(1).
		WithMsgID(17).
		WithTraceID(23)

	h.OnReceive(ctx, child, hdr, payload)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 targeted response, got %d", len(srv.sent))
	}
	if srv.sent[0].header == nil {
		t.Fatalf("expected response header")
	}
	if srv.sent[0].header.Major() != header.MajorOKResp {
		t.Fatalf("unexpected response major: got %d want %d", srv.sent[0].header.Major(), header.MajorOKResp)
	}
	if srv.sent[0].header.SourceID() != 1 || srv.sent[0].header.TargetID() != 11 {
		t.Fatalf("unexpected response source/target: source=%d target=%d", srv.sent[0].header.SourceID(), srv.sent[0].header.TargetID())
	}
	if srv.sent[0].header.GetMsgID() != 17 || srv.sent[0].header.GetTraceID() != 23 {
		t.Fatalf("expected msg/trace preserved, got msg=%d trace=%d", srv.sent[0].header.GetMsgID(), srv.sent[0].header.GetTraceID())
	}
	frame, resp := decodeAuthFrame[listPendingRegistersResp](t, srv.sent[0].payload)
	if frame.Action != actionListPendingRegistersResp {
		t.Fatalf("unexpected response action: got %q want %q", frame.Action, actionListPendingRegistersResp)
	}
	if resp.Code != 1 || resp.Total != 1 || len(resp.Items) != 1 || resp.Items[0].RequestID != "req-1" {
		t.Fatalf("unexpected remote admin list response: %+v", resp)
	}
}

func TestOnReceive_RemoteAdminActionRejectsRouteOwnershipMismatch(t *testing.T) {
	cm := connmgr.New()
	child := &mockConnection{id: "child-admin-mismatch"}
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	child.SetMeta("nodeID", uint32(9))
	_ = cm.Add(child)
	other := &mockConnection{id: "other-admin-mismatch"}
	other.SetMeta(core.MetaRoleKey, core.RoleChild)
	other.SetMeta("nodeID", uint32(12))
	_ = cm.Add(other)
	cm.AddNodeIndex(11, other)

	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)
	h := newAdmissionTestHandler(false, "admin:auth.pending.list")
	h.log = slog.Default()
	h.initActions()
	h.whitelist["actor-11"] = bindingRecord{NodeID: 11, Role: "admin"}

	payload := mustAuthMessage(t, actionListPendingRegisters, listPendingRegistersReq{Limit: 10})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(11).
		WithTargetID(1)

	h.OnReceive(ctx, child, hdr, payload)

	if len(srv.sent) != 0 {
		t.Fatalf("route ownership mismatch should be dropped, got %d frames", len(srv.sent))
	}
}

func TestOnReceive_RemoteAdminActionPermissionDeniedUsesOriginalActor(t *testing.T) {
	cm := connmgr.New()
	child := &mockConnection{id: "child-admin-denied"}
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	child.SetMeta("nodeID", uint32(9))
	_ = cm.Add(child)
	cm.AddNodeIndex(11, child)

	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)
	h := newAdmissionTestHandler(false, "")
	h.log = slog.Default()
	h.initActions()
	h.whitelist["actor-11"] = bindingRecord{NodeID: 11, Role: "node"}

	payload := mustAuthMessage(t, actionIssueRegisterPermit, issueRegisterPermitReq{DeviceID: "dev-denied", Role: "admin"})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(11).
		WithTargetID(1)

	h.OnReceive(ctx, child, hdr, payload)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 permission denied response, got %d", len(srv.sent))
	}
	if srv.sent[0].header.SourceID() != 1 || srv.sent[0].header.TargetID() != 11 {
		t.Fatalf("unexpected response source/target: source=%d target=%d", srv.sent[0].header.SourceID(), srv.sent[0].header.TargetID())
	}
	frame, resp := decodeAuthFrame[issueRegisterPermitResp](t, srv.sent[0].payload)
	if frame.Action != actionIssueRegisterPermitResp {
		t.Fatalf("unexpected response action: got %q want %q", frame.Action, actionIssueRegisterPermitResp)
	}
	if resp.Code != 4403 {
		t.Fatalf("unexpected response code: got %d want 4403", resp.Code)
	}
}

func TestOnReceive_RemoteAdminForwardErrorReturnsExplicitUnavailable(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		configKeyAuthAuthorityMode:      authorityModeConfigSemiCentral,
		configKeyAuthAuthorityPolicyTTL: "30",
		coreconfig.KeyParentEnable:      "true",
		coreconfig.KeyParentAddr:        "tcp://root.example:9000",
	})
	cm := connmgr.New()
	leaf := &mockConnection{id: "leaf-admin-unavailable"}
	leaf.SetMeta(core.MetaRoleKey, core.RoleChild)
	leaf.SetMeta("nodeID", uint32(11))
	_ = cm.Add(leaf)

	srv := newRecordingAuthServerWithConfig(9, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)
	h := newSemiCentralTestHandler(cfg)
	h.log = slog.Default()
	h.initActions()
	if !h.applyRuntimeAuthorityPolicy(time.Now().UTC(), authorityPolicySyncData{
		Mode:                 authorityModeConfigSemiCentral,
		EffectiveAuthorityID: 1,
		Epoch:                5,
		TTLSec:               30,
	}) {
		t.Fatalf("expected runtime authority policy apply")
	}

	payload := mustAuthMessage(t, actionListRegisterPermits, listRegisterPermitsReq{Limit: 10})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(11).
		WithTargetID(1)

	h.OnReceive(ctx, leaf, hdr, payload)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 unavailable response, got %d", len(srv.sent))
	}
	frame, resp := decodeAuthFrame[listRegisterPermitsResp](t, srv.sent[0].payload)
	if frame.Action != actionListRegisterPermitsResp {
		t.Fatalf("unexpected response action: got %q want %q", frame.Action, actionListRegisterPermitsResp)
	}
	if resp.Code != 4500 || resp.Msg != "authority unavailable" {
		t.Fatalf("unexpected unavailable response: %+v", resp)
	}
}
