package exec

// 本文件覆盖 SubProto 中 `exec` 模块里与 `cap_registry` 相关的行为。

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/header"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
)

func TestCapSnapshotAndQuery(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	child := &mockConnection{id: "c2"}
	child.SetMeta("nodeID", uint32(2))
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child conn err=%v", err)
	}

	h := NewHandler(nil)
	h.Init()

	snapHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(2).
		WithTargetID(1).
		WithMsgID(101).
		WithTraceID(202)
	snapReq := CapSnapshotReq{
		ReqID:    "snap-1",
		FromNode: 2,
		Epoch:    1,
		LeaseMs:  60000,
		Caps: []CapabilityDescriptor{
			{ProviderNode: 2, Method: "sensor::read", Version: "v1"},
		},
	}
	rawSnap, _ := json.Marshal(snapReq)
	h.handleCapSnapshot(ctx, child, snapHdr, rawSnap)

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 frame for snapshot resp, got %d", len(srv.sends))
	}
	snapAction, snapData := decodeExecEnvelope(t, srv.sends[0].payload)
	if snapAction != actionCapSyncResp {
		t.Fatalf("unexpected action=%s", snapAction)
	}
	var snapResp CapSyncResp
	if err := json.Unmarshal(snapData, &snapResp); err != nil {
		t.Fatalf("unmarshal cap_sync_resp err=%v", err)
	}
	if snapResp.Code != 1 || snapResp.Applied != 1 || snapResp.FromNode != 2 || snapResp.Epoch != 1 {
		t.Fatalf("unexpected cap_sync_resp: %+v", snapResp)
	}
	if srv.sends[0].hdr == nil || srv.sends[0].hdr.GetMsgID() != 101 || srv.sends[0].hdr.GetTraceID() != 202 {
		t.Fatalf("snapshot resp should inherit msg/trace")
	}

	srv.sends = nil
	queryHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(2).
		WithTargetID(1).
		WithMsgID(303).
		WithTraceID(404)
	queryReq := CapQueryReq{
		ReqID:         "query-1",
		RequesterNode: 2,
		Method:        "sensor::read",
	}
	rawQuery, _ := json.Marshal(queryReq)
	h.handleCapQuery(ctx, child, queryHdr, rawQuery)

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 frame for query resp, got %d", len(srv.sends))
	}
	queryAction, queryData := decodeExecEnvelope(t, srv.sends[0].payload)
	if queryAction != actionCapQueryResp {
		t.Fatalf("unexpected action=%s", queryAction)
	}
	var queryResp CapQueryResp
	if err := json.Unmarshal(queryData, &queryResp); err != nil {
		t.Fatalf("unmarshal cap_query_resp err=%v", err)
	}
	if queryResp.Code != 1 || queryResp.Total != 1 || len(queryResp.Routes) != 1 {
		t.Fatalf("unexpected cap_query_resp: %+v", queryResp)
	}
	route := queryResp.Routes[0]
	if route.ProviderNode != 2 || route.ViaNode != 2 || route.Method != "sensor::read" || route.Version != "v1" {
		t.Fatalf("unexpected route: %+v", route)
	}
	if srv.sends[0].hdr == nil || srv.sends[0].hdr.GetMsgID() != 303 || srv.sends[0].hdr.GetTraceID() != 404 {
		t.Fatalf("query resp should inherit msg/trace")
	}
}

func TestCapSnapshotRejectsSourceMismatch(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	child := &mockConnection{id: "c3"}
	child.SetMeta("nodeID", uint32(3))
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child conn err=%v", err)
	}

	h := NewHandler(nil)
	h.Init()

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(3).
		WithTargetID(1)
	req := CapSnapshotReq{
		ReqID:    "snap-mismatch",
		FromNode: 2,
		Epoch:    1,
		Caps: []CapabilityDescriptor{
			{ProviderNode: 2, Method: "sensor::read"},
		},
	}
	raw, _ := json.Marshal(req)
	h.handleCapSnapshot(ctx, child, reqHdr, raw)

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(srv.sends))
	}
	action, data := decodeExecEnvelope(t, srv.sends[0].payload)
	if action != actionCapSyncResp {
		t.Fatalf("unexpected action=%s", action)
	}
	var resp CapSyncResp
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal cap_sync_resp err=%v", err)
	}
	if resp.Code != 403 {
		t.Fatalf("expected code=403, got %+v", resp)
	}
}

func TestCapSnapshotRejectsStaleEpoch(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	child := &mockConnection{id: "c2"}
	child.SetMeta("nodeID", uint32(2))
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child conn err=%v", err)
	}

	h := NewHandler(nil)
	h.Init()

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(2).
		WithTargetID(1)

	first := CapSnapshotReq{
		ReqID:    "snap-new",
		FromNode: 2,
		Epoch:    2,
		Caps: []CapabilityDescriptor{
			{ProviderNode: 2, Method: "sensor::alpha"},
		},
	}
	rawFirst, _ := json.Marshal(first)
	h.handleCapSnapshot(ctx, child, reqHdr, rawFirst)
	srv.sends = nil

	stale := CapSnapshotReq{
		ReqID:    "snap-old",
		FromNode: 2,
		Epoch:    1,
		Caps: []CapabilityDescriptor{
			{ProviderNode: 2, Method: "sensor::beta"},
		},
	}
	rawStale, _ := json.Marshal(stale)
	h.handleCapSnapshot(ctx, child, reqHdr, rawStale)

	if len(srv.sends) != 1 {
		t.Fatalf("expected stale snapshot response, got %d", len(srv.sends))
	}
	action, data := decodeExecEnvelope(t, srv.sends[0].payload)
	if action != actionCapSyncResp {
		t.Fatalf("unexpected action=%s", action)
	}
	var resp CapSyncResp
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal cap_sync_resp err=%v", err)
	}
	if resp.Code != 409 {
		t.Fatalf("expected code=409, got %+v", resp)
	}

	srv.sends = nil
	queryReq := CapQueryReq{
		ReqID:         "query-alpha",
		RequesterNode: 2,
		Method:        "sensor::alpha",
	}
	rawQuery, _ := json.Marshal(queryReq)
	h.handleCapQuery(ctx, child, reqHdr, rawQuery)
	if len(srv.sends) != 1 {
		t.Fatalf("expected query response, got %d", len(srv.sends))
	}
	_, queryData := decodeExecEnvelope(t, srv.sends[0].payload)
	var queryResp CapQueryResp
	if err := json.Unmarshal(queryData, &queryResp); err != nil {
		t.Fatalf("unmarshal cap_query_resp err=%v", err)
	}
	if queryResp.Code != 1 || queryResp.Total != 1 || len(queryResp.Routes) != 1 || queryResp.Routes[0].Method != "sensor::alpha" {
		t.Fatalf("unexpected query response after stale snapshot: %+v", queryResp)
	}
}

func TestCapSnapshotRequiresSyncPermission(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	child := &mockConnection{id: "c2"}
	child.SetMeta("nodeID", uint32(2))
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child conn err=%v", err)
	}

	h := NewHandler(nil)
	h.Init()
	h.permCfg.ApplySnapshot(permission.Snapshot{
		DefaultRole:  "deny",
		DefaultPerms: []string{},
		NodeRoles: map[uint32]string{
			2: "deny",
		},
		RolePerms: map[string][]string{
			"deny": []string{},
		},
	})

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(2).
		WithTargetID(1)
	req := CapSnapshotReq{
		ReqID:    "snap-denied",
		FromNode: 2,
		Epoch:    1,
		Caps: []CapabilityDescriptor{
			{ProviderNode: 2, Method: "sensor::read"},
		},
	}
	raw, _ := json.Marshal(req)
	h.handleCapSnapshot(ctx, child, reqHdr, raw)

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(srv.sends))
	}
	action, data := decodeExecEnvelope(t, srv.sends[0].payload)
	if action != actionCapSyncResp {
		t.Fatalf("unexpected action=%s", action)
	}
	var resp CapSyncResp
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal cap_sync_resp err=%v", err)
	}
	if resp.Code != 403 {
		t.Fatalf("expected code=403, got %+v", resp)
	}
}

func TestCapQueryRequiresQueryPermission(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	child := &mockConnection{id: "c2"}
	child.SetMeta("nodeID", uint32(2))
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child conn err=%v", err)
	}

	h := NewHandler(nil)
	h.Init()
	h.permCfg.ApplySnapshot(permission.Snapshot{
		DefaultRole:  "deny",
		DefaultPerms: []string{},
		NodeRoles: map[uint32]string{
			2: "deny",
		},
		RolePerms: map[string][]string{
			"deny": []string{permExecCapSync},
		},
	})

	queryHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(2).
		WithTargetID(1)
	queryReq := CapQueryReq{
		ReqID:         "query-denied",
		RequesterNode: 2,
		Method:        "debug::echo",
	}
	rawQuery, _ := json.Marshal(queryReq)
	h.handleCapQuery(ctx, child, queryHdr, rawQuery)

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(srv.sends))
	}
	action, data := decodeExecEnvelope(t, srv.sends[0].payload)
	if action != actionCapQueryResp {
		t.Fatalf("unexpected action=%s", action)
	}
	var resp CapQueryResp
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal cap_query_resp err=%v", err)
	}
	if resp.Code != 403 {
		t.Fatalf("expected code=403, got %+v", resp)
	}
}

func TestCapQueryIncludesLocalCapabilitySchemaWhenRequested(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	child := &mockConnection{id: "c2"}
	child.SetMeta("nodeID", uint32(2))
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child conn err=%v", err)
	}

	h := NewHandler(nil)
	h.Init()
	if err := h.capRegistry.Register(execcap.Descriptor{
		Provider: "demo",
		Method:   "demo::schema",
		InputSchema: json.RawMessage(`{
			"title": "Schema Demo",
			"type": "object",
			"required": ["name"],
			"properties": {
				"name": { "type": "string" },
				"max_bytes": { "type": "integer" }
			}
		}`),
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}); err != nil {
		t.Fatalf("register local capability err=%v", err)
	}

	queryHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(2).
		WithTargetID(1)
	queryReq := CapQueryReq{
		ReqID:         "query-schema",
		RequesterNode: 2,
		Method:        "demo::schema",
		IncludeSchema: true,
	}
	rawQuery, _ := json.Marshal(queryReq)
	h.handleCapQuery(ctx, child, queryHdr, rawQuery)

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(srv.sends))
	}
	action, data := decodeExecEnvelope(t, srv.sends[0].payload)
	if action != actionCapQueryResp {
		t.Fatalf("unexpected action=%s", action)
	}
	var resp CapQueryResp
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal cap_query_resp err=%v", err)
	}
	if resp.Code != 1 || resp.Total != 1 || len(resp.Routes) != 1 {
		t.Fatalf("unexpected cap_query_resp: %+v", resp)
	}
	route := resp.Routes[0]
	if route.ProviderNode != 1 || route.Method != "demo::schema" {
		t.Fatalf("unexpected route: %+v", route)
	}
	assertCapabilityRouteSchema(t, route.InputSchema)

	srv.sends = nil
	queryReq.IncludeSchema = false
	rawQuery, _ = json.Marshal(queryReq)
	h.handleCapQuery(ctx, child, queryHdr, rawQuery)

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 frame without schema, got %d", len(srv.sends))
	}
	_, data = decodeExecEnvelope(t, srv.sends[0].payload)
	resp = CapQueryResp{}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal cap_query_resp without schema err=%v", err)
	}
	if len(resp.Routes) != 1 || len(resp.Routes[0].InputSchema) != 0 {
		t.Fatalf("expected query route without schema payload: %+v", resp.Routes)
	}
}

func assertCapabilityRouteSchema(t *testing.T, raw json.RawMessage) {
	t.Helper()
	if len(raw) == 0 {
		t.Fatalf("expected input schema")
	}
	var schema struct {
		Title      string   `json:"title"`
		Type       string   `json:"type"`
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal route schema err=%v", err)
	}
	if schema.Title != "Schema Demo" || schema.Type != "object" {
		t.Fatalf("unexpected schema header: %+v", schema)
	}
	if !reflect.DeepEqual(schema.Required, []string{"name"}) {
		t.Fatalf("unexpected required fields: %v", schema.Required)
	}
	wantTypes := map[string]string{
		"name":      "string",
		"max_bytes": "integer",
	}
	if len(schema.Properties) != len(wantTypes) {
		t.Fatalf("unexpected property count: got=%d want=%d", len(schema.Properties), len(wantTypes))
	}
	for key, wantType := range wantTypes {
		got, ok := schema.Properties[key]
		if !ok {
			t.Fatalf("missing schema property %s", key)
		}
		if got.Type != wantType {
			t.Fatalf("unexpected type for %s: got=%s want=%s", key, got.Type, wantType)
		}
	}
}

func decodeExecEnvelope(t *testing.T, payload []byte) (string, json.RawMessage) {
	t.Helper()
	var msg message
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("unmarshal envelope err=%v", err)
	}
	return msg.Action, msg.Data
}
