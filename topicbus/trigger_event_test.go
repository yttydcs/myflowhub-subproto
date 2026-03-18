package topicbus

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/eventbus"
	"github.com/yttydcs/myflowhub-core/header"
)

type tbTestAddr struct{}

func (tbTestAddr) Network() string { return "test" }
func (tbTestAddr) String() string  { return "test" }

type tbNopPipe struct{}

func (tbNopPipe) Read([]byte) (int, error)    { return 0, io.EOF }
func (tbNopPipe) Write(p []byte) (int, error) { return len(p), nil }
func (tbNopPipe) Close() error                { return nil }

type tbConn struct {
	id   string
	meta map[string]any
}

func newTBConn(id string) *tbConn {
	return &tbConn{id: id, meta: map[string]any{}}
}

func (c *tbConn) ID() string                           { return c.id }
func (c *tbConn) Pipe() core.IPipe                     { return tbNopPipe{} }
func (c *tbConn) Close() error                         { return nil }
func (c *tbConn) OnReceive(core.ReceiveHandler)        {}
func (c *tbConn) SetMeta(key string, val any)          { c.meta[key] = val }
func (c *tbConn) GetMeta(key string) (any, bool)       { v, ok := c.meta[key]; return v, ok }
func (c *tbConn) Metadata() map[string]any             { return c.meta }
func (c *tbConn) LocalAddr() net.Addr                  { return tbTestAddr{} }
func (c *tbConn) RemoteAddr() net.Addr                 { return tbTestAddr{} }
func (c *tbConn) Reader() core.IReader                 { return nil }
func (c *tbConn) SetReader(core.IReader)               {}
func (c *tbConn) DispatchReceive(core.IHeader, []byte) {}
func (c *tbConn) Send([]byte) error                    { return nil }
func (c *tbConn) SendWithHeader(core.IHeader, []byte, core.IHeaderCodec) error {
	return nil
}

type tbServer struct {
	nodeID uint32
	cm     core.IConnectionManager
	bus    eventbus.IBus
}

func newTBServer(nodeID uint32, cm core.IConnectionManager) *tbServer {
	return &tbServer{
		nodeID: nodeID,
		cm:     cm,
		bus:    eventbus.New(eventbus.Options{}),
	}
}

func (s *tbServer) Start(context.Context) error          { return nil }
func (s *tbServer) Stop(context.Context) error           { return nil }
func (s *tbServer) Config() core.IConfig                 { return nil }
func (s *tbServer) ConnManager() core.IConnectionManager { return s.cm }
func (s *tbServer) Process() core.IProcess               { return nil }
func (s *tbServer) HeaderCodec() core.IHeaderCodec       { return header.HeaderTcpCodec{} }
func (s *tbServer) NodeID() uint32                       { return s.nodeID }
func (s *tbServer) UpdateNodeID(id uint32)               { s.nodeID = id }
func (s *tbServer) EventBus() eventbus.IBus              { return s.bus }
func (s *tbServer) Send(_ context.Context, _ string, _ core.IHeader, _ []byte) error {
	return nil
}

func TestHandlePublishEmitsFlowTriggerEvent(t *testing.T) {
	h := NewTopicBusHandlerWithConfig(nil, nil)
	h.Init()

	cm := connmgr.New()
	src := newTBConn("src")
	src.SetMeta("nodeID", uint32(2))
	_ = cm.Add(src)

	srv := newTBServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	recv := make(chan publishReq, 1)
	token := srv.bus.Subscribe("topicbus.publish", func(_ context.Context, evt eventbus.Event) {
		req := parsePublishReqAny(evt.Data)
		if req.Name == "" {
			return
		}
		select {
		case recv <- req:
		default:
		}
	})
	defer srv.bus.Unsubscribe("topicbus.publish", token)

	req := publishReq{
		Topic:   "sensor/temp",
		Name:    "alarm",
		TS:      123,
		Payload: json.RawMessage(`{"k":"v"}`),
	}
	raw, _ := json.Marshal(req)
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoTopicBus).
		WithSourceID(2).
		WithTargetID(1)
	h.handlePublish(ctx, src, hdr, raw)

	select {
	case got := <-recv:
		if got.Topic != req.Topic || got.Name != req.Name || got.TS != req.TS {
			t.Fatalf("event payload mismatch, got=%+v want=%+v", got, req)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected topicbus.publish event")
	}
}

func TestHandlePublishFromParentEmitsFlowReceivedEvent(t *testing.T) {
	h := NewTopicBusHandlerWithConfig(nil, nil)
	h.Init()

	cm := connmgr.New()
	parent := newTBConn("parent")
	parent.SetMeta("nodeID", uint32(9))
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	_ = cm.Add(parent)

	srv := newTBServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	recv := make(chan publishReq, 1)
	token := srv.bus.Subscribe("topicbus.received", func(_ context.Context, evt eventbus.Event) {
		req := parsePublishReqAny(evt.Data)
		if req.Name == "" {
			return
		}
		select {
		case recv <- req:
		default:
		}
	})
	defer srv.bus.Unsubscribe("topicbus.received", token)

	req := publishReq{
		Topic:   "sensor/temp",
		Name:    "alarm",
		TS:      321,
		Payload: json.RawMessage(`{"k":"v2"}`),
	}
	raw, _ := json.Marshal(req)
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoTopicBus).
		WithSourceID(9).
		WithTargetID(1)
	h.handlePublish(ctx, parent, hdr, raw)

	select {
	case got := <-recv:
		if got.Topic != req.Topic || got.Name != req.Name || got.TS != req.TS {
			t.Fatalf("received event payload mismatch, got=%+v want=%+v", got, req)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected topicbus.received event")
	}
}

func TestHandlePublishFromChildDoesNotEmitFlowReceivedEvent(t *testing.T) {
	h := NewTopicBusHandlerWithConfig(nil, nil)
	h.Init()

	cm := connmgr.New()
	child := newTBConn("child")
	child.SetMeta("nodeID", uint32(2))
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	_ = cm.Add(child)

	srv := newTBServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	recv := make(chan publishReq, 1)
	token := srv.bus.Subscribe("topicbus.received", func(_ context.Context, evt eventbus.Event) {
		req := parsePublishReqAny(evt.Data)
		select {
		case recv <- req:
		default:
		}
	})
	defer srv.bus.Unsubscribe("topicbus.received", token)

	req := publishReq{
		Topic: "sensor/temp",
		Name:  "alarm",
		TS:    777,
	}
	raw, _ := json.Marshal(req)
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoTopicBus).
		WithSourceID(2).
		WithTargetID(1)
	h.handlePublish(ctx, child, hdr, raw)

	select {
	case <-recv:
		t.Fatalf("did not expect topicbus.received for child-origin publish")
	case <-time.After(200 * time.Millisecond):
	}
}

func parsePublishReqAny(data any) publishReq {
	switch v := data.(type) {
	case publishReq:
		return v
	case *publishReq:
		if v != nil {
			return *v
		}
	}
	raw, _ := json.Marshal(data)
	var out publishReq
	_ = json.Unmarshal(raw, &out)
	return out
}
