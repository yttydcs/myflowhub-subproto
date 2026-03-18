package exec

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/eventbus"
	"github.com/yttydcs/myflowhub-core/header"
	"github.com/yttydcs/myflowhub-subproto/broker"
)

func TestCapSnapshotSyncsUpstreamToParent(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	child := &mockConnection{id: "child"}
	child.SetMeta("nodeID", uint32(2))
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child err=%v", err)
	}

	parent := &mockConnection{id: "parent"}
	parent.SetMeta("nodeID", uint32(9))
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	if err := cm.Add(parent); err != nil {
		t.Fatalf("add parent err=%v", err)
	}

	h := NewHandler(nil)
	h.Init()

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(2).
		WithTargetID(1)
	req := CapSnapshotReq{
		ReqID:    "child-snap",
		FromNode: 2,
		Epoch:    1,
		Caps: []CapabilityDescriptor{
			{ProviderNode: 2, Method: "sensor::read", Version: "v1"},
		},
	}
	raw, _ := json.Marshal(req)
	h.handleCapSnapshot(ctx, child, reqHdr, raw)

	if len(srv.sends) < 2 {
		t.Fatalf("expected at least 2 frames (resp + upstream sync), got %d", len(srv.sends))
	}

	var syncPayload []byte
	for _, frame := range srv.sends {
		if frame.connID == parent.ID() {
			action, data := decodeExecEnvelope(t, frame.payload)
			if action == actionCapSnapshot {
				syncPayload = data
				break
			}
		}
	}
	if len(syncPayload) == 0 {
		t.Fatalf("expected cap_snapshot forwarded to parent")
	}

	var syncReq CapSnapshotReq
	if err := json.Unmarshal(syncPayload, &syncReq); err != nil {
		t.Fatalf("unmarshal upstream cap_snapshot err=%v", err)
	}
	if syncReq.FromNode != 1 {
		t.Fatalf("upstream from_node mismatch, got=%d", syncReq.FromNode)
	}
	if syncReq.Epoch == 0 {
		t.Fatalf("upstream epoch should be > 0")
	}

	foundLocal := false
	foundChild := false
	for _, capDesc := range syncReq.Caps {
		if capDesc.ProviderNode == 1 && capDesc.Method == "debug::echo" {
			foundLocal = true
		}
		if capDesc.ProviderNode == 2 && capDesc.Method == "sensor::read" {
			foundChild = true
		}
	}
	if !foundLocal || !foundChild {
		t.Fatalf("upstream snapshot missing aggregated capabilities: local=%v child=%v caps=%+v", foundLocal, foundChild, syncReq.Caps)
	}
}

func TestCapQueryFallsBackToParent(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	child := &mockConnection{id: "child"}
	child.SetMeta("nodeID", uint32(2))
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child err=%v", err)
	}

	parent := &mockConnection{id: "parent"}
	parent.SetMeta("nodeID", uint32(9))
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	if err := cm.Add(parent); err != nil {
		t.Fatalf("add parent err=%v", err)
	}

	h := NewHandler(nil)
	h.Init()

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(2).
		WithTargetID(1).
		WithMsgID(777).
		WithTraceID(888)
	req := CapQueryReq{
		ReqID:         "query-parent",
		RequesterNode: 2,
		Method:        "remote::scan",
	}
	raw, _ := json.Marshal(req)

	go func() {
		time.Sleep(20 * time.Millisecond)
		broker.SharedExecCapQueryBroker().Deliver(req.ReqID, CapQueryResp{
			ReqID:         req.ReqID,
			Code:          1,
			Msg:           "ok",
			ResponderNode: 9,
			Total:         1,
			Routes: []CapabilityRoute{
				{ProviderNode: 10, ViaNode: 9, Method: "remote::scan", Version: "v2"},
			},
		})
	}()

	h.handleCapQuery(ctx, child, reqHdr, raw)

	if len(srv.sends) < 2 {
		t.Fatalf("expected at least 2 frames (upstream query + downstream resp), got %d", len(srv.sends))
	}

	hasUpstreamQuery := false
	var respData json.RawMessage
	for _, frame := range srv.sends {
		action, data := decodeExecEnvelope(t, frame.payload)
		if frame.connID == parent.ID() && action == actionCapQuery {
			hasUpstreamQuery = true
		}
		if frame.connID == child.ID() && action == actionCapQueryResp {
			respData = data
		}
	}
	if !hasUpstreamQuery {
		t.Fatalf("expected cap_query forwarded to parent")
	}
	if len(respData) == 0 {
		t.Fatalf("expected cap_query_resp sent to child")
	}

	var resp CapQueryResp
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("unmarshal cap_query_resp err=%v", err)
	}
	if resp.Code != 1 || resp.Total != 1 || len(resp.Routes) != 1 {
		t.Fatalf("unexpected query response: %+v", resp)
	}
	if resp.Routes[0].Method != "remote::scan" || resp.Routes[0].ViaNode != 9 {
		t.Fatalf("unexpected route in query response: %+v", resp.Routes[0])
	}
}

func TestCapUpsertSyncsIncrementallyToParent(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	child := &mockConnection{id: "child"}
	child.SetMeta("nodeID", uint32(2))
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child err=%v", err)
	}

	parent := &mockConnection{id: "parent"}
	parent.SetMeta("nodeID", uint32(9))
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	if err := cm.Add(parent); err != nil {
		t.Fatalf("add parent err=%v", err)
	}

	h := NewHandler(nil)
	h.Init()

	baseHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(2).
		WithTargetID(1)
	snap := CapSnapshotReq{
		ReqID:    "snap-base",
		FromNode: 2,
		Epoch:    1,
		Caps: []CapabilityDescriptor{
			{ProviderNode: 2, Method: "sensor::a", Version: "v1"},
		},
	}
	rawSnap, _ := json.Marshal(snap)
	h.handleCapSnapshot(ctx, child, baseHdr, rawSnap)

	srv.sends = nil
	upsert := CapUpsertReq{
		ReqID:    "upsert-1",
		FromNode: 2,
		Epoch:    1,
		Caps: []CapabilityDescriptor{
			{ProviderNode: 2, Method: "sensor::b", Version: "v1"},
		},
	}
	rawUpsert, _ := json.Marshal(upsert)
	h.handleCapUpsert(ctx, child, baseHdr, rawUpsert)

	hasParentUpsert := false
	hasParentSnapshot := false
	for _, frame := range srv.sends {
		if frame.connID != parent.ID() {
			continue
		}
		action, data := decodeExecEnvelope(t, frame.payload)
		switch action {
		case actionCapUpsert:
			hasParentUpsert = true
			var req CapUpsertReq
			if err := json.Unmarshal(data, &req); err != nil {
				t.Fatalf("unmarshal parent upsert err=%v", err)
			}
			if req.FromNode != 1 {
				t.Fatalf("parent upsert from_node should be local node, got=%d", req.FromNode)
			}
			if req.Epoch == 0 || len(req.Caps) != 1 || req.Caps[0].Method != "sensor::b" {
				t.Fatalf("unexpected parent upsert payload: %+v", req)
			}
		case actionCapSnapshot:
			hasParentSnapshot = true
		}
	}
	if !hasParentUpsert {
		t.Fatalf("expected cap_upsert sent to parent")
	}
	if hasParentSnapshot {
		t.Fatalf("did not expect full cap_snapshot for incremental change")
	}
}

func TestCapSyncRespStaleEpochForcesResnapshot(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	child := &mockConnection{id: "child"}
	child.SetMeta("nodeID", uint32(2))
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child err=%v", err)
	}

	parent := &mockConnection{id: "parent"}
	parent.SetMeta("nodeID", uint32(9))
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	if err := cm.Add(parent); err != nil {
		t.Fatalf("add parent err=%v", err)
	}

	h := NewHandler(nil)
	h.Init()

	baseHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(2).
		WithTargetID(1)
	snap := CapSnapshotReq{
		ReqID:    "snap-base",
		FromNode: 2,
		Epoch:    1,
		Caps: []CapabilityDescriptor{
			{ProviderNode: 2, Method: "sensor::a", Version: "v1"},
		},
	}
	rawSnap, _ := json.Marshal(snap)
	h.handleCapSnapshot(ctx, child, baseHdr, rawSnap)
	srv.sends = nil

	rawResp, _ := json.Marshal(CapSyncResp{
		ReqID: "capupsert-1-42",
		Code:  409,
		Msg:   "stale epoch",
		Epoch: 5,
	})
	h.handleCapSyncResp(ctx, parent, nil, rawResp)

	var (
		foundSnapshot bool
		syncReq       CapSnapshotReq
	)
	for _, frame := range srv.sends {
		if frame.connID != parent.ID() {
			continue
		}
		action, data := decodeExecEnvelope(t, frame.payload)
		if action != actionCapSnapshot {
			continue
		}
		foundSnapshot = true
		if err := json.Unmarshal(data, &syncReq); err != nil {
			t.Fatalf("unmarshal cap_snapshot err=%v", err)
		}
		break
	}
	if !foundSnapshot {
		t.Fatalf("expected cap_snapshot resend to parent")
	}
	if syncReq.Epoch != 6 {
		t.Fatalf("expected resnapshot epoch=6, got=%d", syncReq.Epoch)
	}
}

func TestConnClosedRemovesChildCapsAndSyncsUpstream(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{
		nodeID: 1,
		cm:     cm,
		bus:    eventbus.New(eventbus.Options{}),
	}
	ctx := core.WithServerContext(context.Background(), srv)

	child := &mockConnection{id: "child"}
	child.SetMeta("nodeID", uint32(2))
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child err=%v", err)
	}

	parent := &mockConnection{id: "parent"}
	parent.SetMeta("nodeID", uint32(9))
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	if err := cm.Add(parent); err != nil {
		t.Fatalf("add parent err=%v", err)
	}

	h := NewHandler(nil)
	h.Init()
	h.ensureConnCloseSubscription(srv)

	baseHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(2).
		WithTargetID(1)
	snap := CapSnapshotReq{
		ReqID:    "snap-base",
		FromNode: 2,
		Epoch:    1,
		Caps: []CapabilityDescriptor{
			{ProviderNode: 2, Method: "sensor::gone", Version: "v1"},
		},
	}
	rawSnap, _ := json.Marshal(snap)
	h.handleCapSnapshot(ctx, child, baseHdr, rawSnap)
	srv.sends = nil

	srv.bus.PublishSync(ctx, "conn.closed", map[string]any{
		"conn_id": child.ID(),
		"node_id": uint32(2),
	}, nil)

	withdrawFound := false
	for _, frame := range srv.sends {
		if frame.connID != parent.ID() {
			continue
		}
		action, data := decodeExecEnvelope(t, frame.payload)
		if action != actionCapWithdraw {
			continue
		}
		var req CapWithdrawReq
		if err := json.Unmarshal(data, &req); err != nil {
			t.Fatalf("unmarshal cap_withdraw err=%v", err)
		}
		if len(req.Keys) == 1 && req.Keys[0].ProviderNode == 2 && req.Keys[0].Method == "sensor::gone" {
			withdrawFound = true
			break
		}
	}
	if !withdrawFound {
		t.Fatalf("expected cap_withdraw for closed child capability")
	}

	total, _ := h.queryCapabilityRoutes(CapQueryReq{
		ReqID:  "query-local",
		Method: "sensor::gone",
	}, 1)
	if total != 0 {
		t.Fatalf("expected child capability removed after conn.closed, total=%d", total)
	}
}
