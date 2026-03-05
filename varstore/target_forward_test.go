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

func mustJSON(v any) []byte {
	raw, _ := json.Marshal(v)
	return raw
}
