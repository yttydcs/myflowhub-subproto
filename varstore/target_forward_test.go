package varstore

import (
	"context"
	"encoding/json"
	"net"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/eventbus"
	"github.com/yttydcs/myflowhub-core/header"
)

type testAddr struct{}

func (testAddr) Network() string { return "test" }
func (testAddr) String() string  { return "test" }

type testConn struct {
	id   string
	meta map[string]any
	sent []testFrame
}

type testFrame struct {
	hdr     core.IHeader
	payload []byte
}

func newTestConn(id string) *testConn {
	return &testConn{
		id:   id,
		meta: make(map[string]any),
	}
}

func (c *testConn) ID() string                           { return c.id }
func (c *testConn) Close() error                         { return nil }
func (c *testConn) OnReceive(core.ReceiveHandler)        {}
func (c *testConn) SetMeta(key string, val any)          { c.meta[key] = val }
func (c *testConn) GetMeta(key string) (any, bool)       { v, ok := c.meta[key]; return v, ok }
func (c *testConn) Metadata() map[string]any             { return c.meta }
func (c *testConn) LocalAddr() net.Addr                  { return testAddr{} }
func (c *testConn) RemoteAddr() net.Addr                 { return testAddr{} }
func (c *testConn) Reader() core.IReader                 { return nil }
func (c *testConn) SetReader(core.IReader)               {}
func (c *testConn) DispatchReceive(core.IHeader, []byte) {}
func (c *testConn) RawConn() net.Conn                    { return nil }
func (c *testConn) Send(data []byte) error {
	c.sent = append(c.sent, testFrame{payload: data})
	return nil
}
func (c *testConn) SendWithHeader(h core.IHeader, payload []byte, _ core.IHeaderCodec) error {
	c.sent = append(c.sent, testFrame{hdr: h, payload: payload})
	return nil
}

type testServer struct {
	nodeID uint32
	cm     core.IConnectionManager
	cfg    core.IConfig
	bus    eventbus.IBus
}

func newTestServer(nodeID uint32, cm core.IConnectionManager) *testServer {
	return &testServer{
		nodeID: nodeID,
		cm:     cm,
		cfg:    config.NewMap(nil),
		bus:    eventbus.New(eventbus.Options{}),
	}
}

func (s *testServer) Start(context.Context) error          { return nil }
func (s *testServer) Stop(context.Context) error           { return nil }
func (s *testServer) Config() core.IConfig                 { return s.cfg }
func (s *testServer) ConnManager() core.IConnectionManager { return s.cm }
func (s *testServer) Process() core.IProcess               { return nil }
func (s *testServer) HeaderCodec() core.IHeaderCodec       { return header.HeaderTcpCodec{} }
func (s *testServer) NodeID() uint32                       { return s.nodeID }
func (s *testServer) UpdateNodeID(id uint32)               { s.nodeID = id }
func (s *testServer) EventBus() eventbus.IBus              { return s.bus }
func (s *testServer) Send(_ context.Context, connID string, hdr core.IHeader, payload []byte) error {
	if c, ok := s.cm.Get(connID); ok {
		return c.SendWithHeader(hdr, payload, header.HeaderTcpCodec{})
	}
	return nil
}

func TestForwardCmdByHeaderTarget(t *testing.T) {
	h := NewVarStoreHandlerWithConfig(nil, nil)
	h.Init()

	cm := connmgr.New()
	src := newTestConn("src")
	src.SetMeta("nodeID", uint32(2))
	_ = cm.Add(src)

	next := newTestConn("next")
	next.SetMeta("nodeID", uint32(9))
	_ = cm.Add(next)
	cm.AddNodeIndex(10, next)

	srv := newTestServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	payload := mustJSON(map[string]any{
		"action": "set",
		"data": map[string]any{
			"name":       "sys_flashlight_enabled",
			"value":      "1",
			"visibility": "public",
			"owner":      10,
		},
	})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(2).
		WithTargetID(10)

	h.OnReceive(ctx, src, hdr, payload)

	if len(src.sent) != 0 {
		t.Fatalf("source should not receive local response when frame is forwarded")
	}
	if len(next.sent) != 1 {
		t.Fatalf("expected forwarded frame to next hop, got %d", len(next.sent))
	}
	if next.sent[0].hdr == nil {
		t.Fatalf("expected forwarded header")
	}
	if next.sent[0].hdr.TargetID() != 10 {
		t.Fatalf("forwarded target mismatch: got=%d want=10", next.sent[0].hdr.TargetID())
	}
	if next.sent[0].hdr.SourceID() != 2 {
		t.Fatalf("forwarded source mismatch: got=%d want=2", next.sent[0].hdr.SourceID())
	}
}

func TestForwardCmdByHeaderTargetNotFoundReturnsResp(t *testing.T) {
	h := NewVarStoreHandlerWithConfig(nil, nil)
	h.Init()

	cm := connmgr.New()
	src := newTestConn("src")
	src.SetMeta("nodeID", uint32(2))
	_ = cm.Add(src)

	srv := newTestServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	payload := mustJSON(map[string]any{
		"action": "set",
		"data": map[string]any{
			"name":       "sys_flashlight_enabled",
			"value":      "1",
			"visibility": "public",
			"owner":      10,
		},
	})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(2).
		WithTargetID(10)

	h.OnReceive(ctx, src, hdr, payload)

	if len(src.sent) != 1 {
		t.Fatalf("expected error response on source, got %d", len(src.sent))
	}
	var msg struct {
		Action string          `json:"action"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(src.sent[0].payload, &msg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if msg.Action != "set_resp" {
		t.Fatalf("unexpected response action: %s", msg.Action)
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		t.Fatalf("decode response data: %v", err)
	}
	if resp.Code != 4 {
		t.Fatalf("expected code=4 not found, got %d msg=%q", resp.Code, resp.Msg)
	}
}

func TestUpSetIndexesOwnerRoute(t *testing.T) {
	h := NewVarStoreHandlerWithConfig(nil, nil)
	h.Init()

	cm := connmgr.New()
	child := newTestConn("child")
	child.SetMeta("nodeID", uint32(9))
	_ = cm.Add(child)

	srv := newTestServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	payload := mustJSON(map[string]any{
		"action": "up_set",
		"data": map[string]any{
			"name":       "sys_flashlight_enabled",
			"value":      "0",
			"visibility": "public",
			"type":       "string",
			"owner":      10,
		},
	})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(9).
		WithTargetID(1)

	h.OnReceive(ctx, child, hdr, payload)

	got, ok := cm.GetByNode(10)
	if !ok || got == nil {
		t.Fatalf("expected owner route indexed from up_set")
	}
	if got.ID() != child.ID() {
		t.Fatalf("route owner=10 mapped to unexpected conn: got=%s want=%s", got.ID(), child.ID())
	}
}

func TestListOwnerEmptyReturnsNamesArray(t *testing.T) {
	h := NewVarStoreHandlerWithConfig(nil, nil)
	h.Init()

	cm := connmgr.New()
	src := newTestConn("src")
	src.SetMeta("nodeID", uint32(2))
	_ = cm.Add(src)

	srv := newTestServer(5, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	payload := mustJSON(map[string]any{
		"action": "list",
		"data": map[string]any{
			"owner": 5,
		},
	})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(5).
		WithTargetID(0).
		WithMsgID(123).
		WithTraceID(456)

	h.OnReceive(ctx, src, hdr, payload)

	if len(src.sent) != 1 {
		t.Fatalf("expected list response on source, got %d", len(src.sent))
	}
	var msg struct {
		Action string          `json:"action"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(src.sent[0].payload, &msg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if msg.Action != "list_resp" {
		t.Fatalf("unexpected response action: %s", msg.Action)
	}
	var data map[string]any
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		t.Fatalf("decode response data: %v", err)
	}
	if code, _ := data["code"].(float64); int(code) != 1 {
		t.Fatalf("expected code=1, got=%v", data["code"])
	}
	namesAny, ok := data["names"]
	if !ok {
		t.Fatalf("expected names present for empty list")
	}
	names, ok := namesAny.([]any)
	if !ok {
		t.Fatalf("expected names array, got %T", namesAny)
	}
	if len(names) != 0 {
		t.Fatalf("expected empty names array, got %v", names)
	}
}

func TestSubscribeInvalidSubscriberReturnsCode2(t *testing.T) {
	h := NewVarStoreHandlerWithConfig(nil, nil)
	h.Init()

	cm := connmgr.New()
	src := newTestConn("src")
	src.SetMeta("nodeID", uint32(2))
	_ = cm.Add(src)

	srv := newTestServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	payload := mustJSON(map[string]any{
		"action": "subscribe",
		"data": map[string]any{
			"name":       "sys_volume_percent",
			"owner":      10,
			"subscriber": 9,
		},
	})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(2).
		WithTargetID(0).
		WithMsgID(10)

	h.OnReceive(ctx, src, hdr, payload)

	if len(src.sent) != 1 {
		t.Fatalf("expected subscribe response on source, got %d", len(src.sent))
	}
	var msg struct {
		Action string          `json:"action"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(src.sent[0].payload, &msg); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if msg.Action != "subscribe_resp" {
		t.Fatalf("unexpected response action: %s", msg.Action)
	}
	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		t.Fatalf("decode response data: %v", err)
	}
	if resp.Code != 2 {
		t.Fatalf("expected code=2 invalid subscriber, got %d", resp.Code)
	}
}

func TestNotifySetForwardedAndCachedLocally(t *testing.T) {
	h := NewVarStoreHandlerWithConfig(nil, nil)
	h.Init()

	cm := connmgr.New()
	parent := newTestConn("parent")
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	parent.SetMeta("nodeID", uint32(1))
	_ = cm.Add(parent)

	child := newTestConn("child")
	child.SetMeta("nodeID", uint32(10))
	_ = cm.Add(child)
	cm.AddNodeIndex(10, child)

	srv := newTestServer(2, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	payload := mustJSON(map[string]any{
		"action": "notify_set",
		"data": map[string]any{
			"code":       1,
			"msg":        "ok",
			"name":       "sys_flashlight_enabled",
			"value":      "1",
			"owner":      10,
			"visibility": "public",
			"type":       "string",
		},
	})
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(7).
		WithTargetID(10)

	h.OnReceive(ctx, parent, hdr, payload)

	if len(child.sent) != 1 {
		t.Fatalf("expected forwarded notify to child, got %d", len(child.sent))
	}
	if child.sent[0].hdr == nil {
		t.Fatalf("expected forwarded header")
	}
	if child.sent[0].hdr.TargetID() != 10 {
		t.Fatalf("forwarded target mismatch: got=%d want=10", child.sent[0].hdr.TargetID())
	}
	if child.sent[0].hdr.SourceID() != 7 {
		t.Fatalf("forwarded source mismatch: got=%d want=7", child.sent[0].hdr.SourceID())
	}

	rec, ok := h.lookupOwned(10, "sys_flashlight_enabled")
	if !ok {
		t.Fatalf("expected notify_set to update local cache")
	}
	if rec.Value != "1" || rec.Owner != 10 || !rec.IsPublic {
		t.Fatalf("unexpected cached record: %+v", rec)
	}
}

func TestSetRespRestoresDownstreamMsgIDPerWrite(t *testing.T) {
	h := NewVarStoreHandlerWithConfig(nil, nil)
	h.Init()

	cm := connmgr.New()
	parent := newTestConn("parent")
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	parent.SetMeta("nodeID", uint32(1))
	_ = cm.Add(parent)

	a := newTestConn("a")
	a.SetMeta("nodeID", uint32(21))
	_ = cm.Add(a)

	b := newTestConn("b")
	b.SetMeta("nodeID", uint32(22))
	_ = cm.Add(b)

	srv := newTestServer(2, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	reqPayloadA := mustJSON(map[string]any{
		"action": "set",
		"data": map[string]any{
			"name":       "sys_flashlight_enabled",
			"value":      "1",
			"visibility": "public",
			"owner":      99,
		},
	})
	reqHdrA := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(21).
		WithTargetID(2).
		WithMsgID(111).
		WithTraceID(1001)
	h.OnReceive(ctx, a, reqHdrA, reqPayloadA)

	reqPayloadB := mustJSON(map[string]any{
		"action": "set",
		"data": map[string]any{
			"name":       "sys_flashlight_enabled",
			"value":      "2",
			"visibility": "public",
			"owner":      99,
		},
	})
	reqHdrB := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(22).
		WithTargetID(2).
		WithMsgID(222).
		WithTraceID(1002)
	h.OnReceive(ctx, b, reqHdrB, reqPayloadB)

	if len(parent.sent) != 2 {
		t.Fatalf("expected 2 forwarded writes to parent, got %d", len(parent.sent))
	}

	upA := uint32(0)
	upB := uint32(0)
	for _, f := range parent.sent {
		if f.hdr == nil {
			continue
		}
		switch f.hdr.GetTraceID() {
		case 1001:
			upA = f.hdr.GetMsgID()
		case 1002:
			upB = f.hdr.GetMsgID()
		}
	}
	if upA == 0 || upB == 0 || upA == upB {
		t.Fatalf("expected distinct upstream msg_ids, got upA=%d upB=%d", upA, upB)
	}

	respPayloadB := mustJSON(map[string]any{
		"action": "assist_set_resp",
		"data": map[string]any{
			"code":       1,
			"msg":        "ok",
			"name":       "sys_flashlight_enabled",
			"value":      "2",
			"owner":      99,
			"visibility": "public",
			"type":       "string",
		},
	})
	respHdrB := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(22).
		WithTargetID(2).
		WithMsgID(upB).
		WithTraceID(1002)
	h.OnReceive(ctx, parent, respHdrB, respPayloadB)

	respPayloadA := mustJSON(map[string]any{
		"action": "assist_set_resp",
		"data": map[string]any{
			"code":       1,
			"msg":        "ok",
			"name":       "sys_flashlight_enabled",
			"value":      "1",
			"owner":      99,
			"visibility": "public",
			"type":       "string",
		},
	})
	respHdrA := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(21).
		WithTargetID(2).
		WithMsgID(upA).
		WithTraceID(1001)
	h.OnReceive(ctx, parent, respHdrA, respPayloadA)

	if len(a.sent) != 1 {
		t.Fatalf("expected 1 downstream resp to a, got %d", len(a.sent))
	}
	if len(b.sent) != 1 {
		t.Fatalf("expected 1 downstream resp to b, got %d", len(b.sent))
	}

	if a.sent[0].hdr == nil {
		t.Fatalf("expected response header for a")
	}
	if b.sent[0].hdr == nil {
		t.Fatalf("expected response header for b")
	}
	if a.sent[0].hdr.GetMsgID() != 111 || a.sent[0].hdr.GetTraceID() != 1001 {
		t.Fatalf("a msg_id/trace_id not restored: msg_id=%d trace_id=%d", a.sent[0].hdr.GetMsgID(), a.sent[0].hdr.GetTraceID())
	}
	if b.sent[0].hdr.GetMsgID() != 222 || b.sent[0].hdr.GetTraceID() != 1002 {
		t.Fatalf("b msg_id/trace_id not restored: msg_id=%d trace_id=%d", b.sent[0].hdr.GetMsgID(), b.sent[0].hdr.GetTraceID())
	}

	var msgA struct {
		Action string          `json:"action"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(a.sent[0].payload, &msgA); err != nil {
		t.Fatalf("decode a response: %v", err)
	}
	if msgA.Action != "set_resp" {
		t.Fatalf("unexpected action for a: %s", msgA.Action)
	}
	var dataA map[string]any
	_ = json.Unmarshal(msgA.Data, &dataA)
	if dataA["value"] != "1" {
		t.Fatalf("unexpected value for a: %v", dataA["value"])
	}

	var msgB struct {
		Action string          `json:"action"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b.sent[0].payload, &msgB); err != nil {
		t.Fatalf("decode b response: %v", err)
	}
	if msgB.Action != "set_resp" {
		t.Fatalf("unexpected action for b: %s", msgB.Action)
	}
	var dataB map[string]any
	_ = json.Unmarshal(msgB.Data, &dataB)
	if dataB["value"] != "2" {
		t.Fatalf("unexpected value for b: %v", dataB["value"])
	}
}

func mustJSON(v any) []byte {
	raw, _ := json.Marshal(v)
	return raw
}
