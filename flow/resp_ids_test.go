package flow

// Context: This file belongs to the SubProto implementation layer around resp_ids_test.

import (
	"context"
	"io"
	"net"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/eventbus"
	"github.com/yttydcs/myflowhub-core/header"
)

type mockAddr struct{}

func (mockAddr) Network() string { return "tcp" }
func (mockAddr) String() string  { return "127.0.0.1:0" }

type nopPipe struct{}

func (nopPipe) Read([]byte) (int, error)    { return 0, io.EOF }
func (nopPipe) Write(p []byte) (int, error) { return len(p), nil }
func (nopPipe) Close() error                { return nil }

type mockConnection struct {
	id   string
	meta map[string]any
}

var _ core.IConnection = (*mockConnection)(nil)

func (m *mockConnection) ID() string                    { return m.id }
func (m *mockConnection) Pipe() core.IPipe              { return nopPipe{} }
func (m *mockConnection) Close() error                  { return nil }
func (m *mockConnection) OnReceive(core.ReceiveHandler) {}
func (m *mockConnection) SetMeta(k string, v any) {
	if m.meta == nil {
		m.meta = make(map[string]any)
	}
	m.meta[k] = v
}
func (m *mockConnection) GetMeta(k string) (any, bool) {
	if m.meta == nil {
		return nil, false
	}
	v, ok := m.meta[k]
	return v, ok
}
func (m *mockConnection) Metadata() map[string]any                                     { return m.meta }
func (m *mockConnection) LocalAddr() net.Addr                                          { return mockAddr{} }
func (m *mockConnection) RemoteAddr() net.Addr                                         { return mockAddr{} }
func (m *mockConnection) Reader() core.IReader                                         { return nil }
func (m *mockConnection) SetReader(core.IReader)                                       {}
func (m *mockConnection) DispatchReceive(core.IHeader, []byte)                         {}
func (m *mockConnection) Send([]byte) error                                            { return nil }
func (m *mockConnection) SendWithHeader(core.IHeader, []byte, core.IHeaderCodec) error { return nil }

type sentFrame struct {
	connID  string
	hdr     core.IHeader
	payload []byte
}

type testServer struct {
	nodeID uint32
	cm     core.IConnectionManager
	sends  []sentFrame
}

var _ core.IServer = (*testServer)(nil)

func (s *testServer) Start(context.Context) error { return nil }
func (s *testServer) Stop(context.Context) error  { return nil }

func (s *testServer) Config() core.IConfig                 { return nil }
func (s *testServer) ConnManager() core.IConnectionManager { return s.cm }
func (s *testServer) Process() core.IProcess               { return nil }
func (s *testServer) HeaderCodec() core.IHeaderCodec       { return nil }
func (s *testServer) NodeID() uint32                       { return s.nodeID }
func (s *testServer) UpdateNodeID(id uint32)               { s.nodeID = id }
func (s *testServer) EventBus() eventbus.IBus              { return nil }
func (s *testServer) Send(_ context.Context, connID string, hdr core.IHeader, payload []byte) error {
	cloneHdr := hdr
	if hdr != nil {
		cloneHdr = hdr.Clone()
	}
	cp := append([]byte(nil), payload...)
	s.sends = append(s.sends, sentFrame{connID: connID, hdr: cloneHdr, payload: cp})
	return nil
}

func TestFlowResp_InheritsMsgIDTraceID_ListResp(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	targetConn := &mockConnection{id: "c1"}
	targetConn.SetMeta("nodeID", uint32(2))
	if err := cm.Add(targetConn); err != nil {
		t.Fatalf("add conn err=%v", err)
	}

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1).
		WithMsgID(111).
		WithTraceID(222)

	h := NewHandler(nil)
	h.sendListResp(ctx, reqHdr, listResp{ReqID: "r1", Code: 1, Msg: "ok", ExecutorNode: 1})

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(srv.sends))
	}
	got := srv.sends[0].hdr
	if got.GetMsgID() != 111 || got.GetTraceID() != 222 {
		t.Fatalf("expected msg_id=111 trace_id=222, got msg_id=%d trace_id=%d", got.GetMsgID(), got.GetTraceID())
	}
	if got.Major() != header.MajorOKResp || got.SubProto() != SubProtoFlow || got.SourceID() != 1 || got.TargetID() != 2 {
		t.Fatalf("unexpected hdr: major=%d sub=%d src=%d tgt=%d", got.Major(), got.SubProto(), got.SourceID(), got.TargetID())
	}
}

func TestFlowResp_InheritsMsgIDTraceID_SetResp(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	targetConn := &mockConnection{id: "c1"}
	targetConn.SetMeta("nodeID", uint32(2))
	if err := cm.Add(targetConn); err != nil {
		t.Fatalf("add conn err=%v", err)
	}

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1).
		WithMsgID(333).
		WithTraceID(444)

	h := NewHandler(nil)
	h.sendSetResp(ctx, reqHdr, 400, "invalid set", "123e4567-e89b-12d3-a456-426614174004")

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 send, got %d", len(srv.sends))
	}
	got := srv.sends[0].hdr
	if got.GetMsgID() != 333 || got.GetTraceID() != 444 {
		t.Fatalf("expected msg_id=333 trace_id=444, got msg_id=%d trace_id=%d", got.GetMsgID(), got.GetTraceID())
	}
	if got.Major() != header.MajorOKResp || got.SubProto() != SubProtoFlow || got.SourceID() != 1 || got.TargetID() != 2 {
		t.Fatalf("unexpected hdr: major=%d sub=%d src=%d tgt=%d", got.Major(), got.SubProto(), got.SourceID(), got.TargetID())
	}
}
