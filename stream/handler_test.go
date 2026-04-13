package stream

// Context: This file belongs to the SubProto implementation layer around handler_test.

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
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

type recordServer struct {
	nodeID uint32
	cm     core.IConnectionManager
	sent   []sentFrame
}

var _ core.IServer = (*recordServer)(nil)

func (s *recordServer) Start(context.Context) error { return nil }
func (s *recordServer) Stop(context.Context) error  { return nil }
func (s *recordServer) Config() core.IConfig        { return nil }
func (s *recordServer) ConnManager() core.IConnectionManager {
	return s.cm
}
func (s *recordServer) Process() core.IProcess         { return nil }
func (s *recordServer) HeaderCodec() core.IHeaderCodec { return nil }
func (s *recordServer) NodeID() uint32                 { return s.nodeID }
func (s *recordServer) UpdateNodeID(id uint32)         { s.nodeID = id }
func (s *recordServer) EventBus() eventbus.IBus        { return nil }
func (s *recordServer) Send(_ context.Context, connID string, hdr core.IHeader, payload []byte) error {
	cloneHdr := hdr
	if hdr != nil {
		cloneHdr = hdr.Clone()
	}
	s.sent = append(s.sent, sentFrame{
		connID:  connID,
		hdr:     cloneHdr,
		payload: append([]byte(nil), payload...),
	})
	return nil
}

func newMockConnection(id string, nodeID uint32) *mockConnection {
	conn := &mockConnection{id: id}
	conn.SetMeta("nodeID", nodeID)
	return conn
}

func newTestContext(t *testing.T) (context.Context, *recordServer) {
	t.Helper()

	cm := connmgr.New()
	requesterConn := newMockConnection("c-requester", 2)
	if err := cm.Add(requesterConn); err != nil {
		t.Fatalf("add requester conn err=%v", err)
	}
	producerConn := newMockConnection("c-producer", 3)
	if err := cm.Add(producerConn); err != nil {
		t.Fatalf("add producer conn err=%v", err)
	}
	srv := &recordServer{nodeID: 1, cm: cm}
	return core.WithServerContext(context.Background(), srv), srv
}

func newRequestHeader(sourceID, targetID uint32) core.IHeader {
	return (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoStream).
		WithSourceID(sourceID).
		WithTargetID(targetID).
		WithMsgID(101).
		WithTraceID(202)
}

func decodeStreamCtrl[T any](t *testing.T, payload []byte) (string, T) {
	t.Helper()
	if len(payload) == 0 || payload[0] != kindCtrl {
		t.Fatalf("expected ctrl payload, got len=%d", len(payload))
	}
	var msg message
	if err := json.Unmarshal(payload[1:], &msg); err != nil {
		t.Fatalf("unmarshal message err=%v", err)
	}
	var out T
	if err := json.Unmarshal(msg.Data, &out); err != nil {
		t.Fatalf("unmarshal action payload err=%v", err)
	}
	return msg.Action, out
}

func TestStreamLocalSourceCatalogActions(t *testing.T) {
	ctx, srv := newTestContext(t)
	h := NewHandler(nil)
	hdr := newRequestHeader(2, 1)

	h.handleAnnounceLocal(ctx, hdr, announceReq{
		ReqID: "announce-1",
		Source: sourceDescriptor{
			SourceID: "source-1",
			Producer: 1,
			Name:     "Music",
			Kind:     streamKindMusic,
			Tags:     []string{"Live", "live", "  "},
		},
	})

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response after announce, got %d", len(srv.sent))
	}
	action, resp := decodeStreamCtrl[announceResp](t, srv.sent[0].payload)
	if action != actionAnnounceResp || resp.Code != 1 || resp.Source == nil {
		t.Fatalf("unexpected announce resp: action=%s code=%d source_nil=%v", action, resp.Code, resp.Source == nil)
	}
	if resp.Source.Mode != modeLive || resp.Source.UnitMode != unitModeChunk {
		t.Fatalf("expected default mode/unit_mode, got mode=%s unit_mode=%s", resp.Source.Mode, resp.Source.UnitMode)
	}
	if len(resp.Source.Tags) != 1 || resp.Source.Tags[0] != "live" {
		t.Fatalf("expected normalized tags, got %#v", resp.Source.Tags)
	}
	if src := h.sources["source-1"]; src == nil || src.desc.Producer != 1 {
		t.Fatalf("expected source stored locally")
	}

	srv.sent = nil
	h.handleGetSourceLocal(ctx, hdr, getSourceReq{ReqID: "get-1", Producer: 1, SourceID: "source-1"})
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response after get, got %d", len(srv.sent))
	}
	action, getResp := decodeStreamCtrl[getSourceResp](t, srv.sent[0].payload)
	if action != actionGetSourceResp || getResp.Code != 1 || getResp.Source == nil || getResp.Source.SourceID != "source-1" {
		t.Fatalf("unexpected get_source resp: action=%s code=%d source=%#v", action, getResp.Code, getResp.Source)
	}

	srv.sent = nil
	h.handleListSourcesLocal(ctx, hdr, listSourcesReq{ReqID: "list-1", Producer: 1, Kind: streamKindMusic})
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response after list, got %d", len(srv.sent))
	}
	action, listResp := decodeStreamCtrl[listSourcesResp](t, srv.sent[0].payload)
	if action != actionListSourcesResp || listResp.Code != 1 || len(listResp.Sources) != 1 || listResp.Sources[0].SourceID != "source-1" {
		t.Fatalf("unexpected list_sources resp: action=%s code=%d sources=%#v", action, listResp.Code, listResp.Sources)
	}
}

func TestStreamLocalConsumerCatalogActions(t *testing.T) {
	ctx, srv := newTestContext(t)
	h := NewHandler(nil)
	hdr := newRequestHeader(2, 1)

	h.handleAnnounceConsumerLocal(ctx, hdr, announceConsumerReq{
		ReqID: "announce-consumer-1",
		ConsumerEndpoint: consumerDescriptor{
			ConsumerID: "consumer-1",
			Consumer:   1,
			Name:       "Display",
			Kind:       streamKindVideo,
			Tags:       []string{"Screen", "screen"},
		},
	})

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response after announce_consumer, got %d", len(srv.sent))
	}
	action, resp := decodeStreamCtrl[announceConsumerResp](t, srv.sent[0].payload)
	if action != actionAnnounceConsumerResp || resp.Code != 1 || resp.ConsumerEndpoint == nil {
		t.Fatalf("unexpected announce_consumer resp: action=%s code=%d consumer_nil=%v", action, resp.Code, resp.ConsumerEndpoint == nil)
	}
	if len(resp.ConsumerEndpoint.Tags) != 1 || resp.ConsumerEndpoint.Tags[0] != "screen" {
		t.Fatalf("expected normalized tags, got %#v", resp.ConsumerEndpoint.Tags)
	}

	srv.sent = nil
	h.handleGetConsumerLocal(ctx, hdr, getConsumerReq{ReqID: "get-consumer-1", Consumer: 1, ConsumerID: "consumer-1"})
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response after get_consumer, got %d", len(srv.sent))
	}
	action, getResp := decodeStreamCtrl[getConsumerResp](t, srv.sent[0].payload)
	if action != actionGetConsumerResp || getResp.Code != 1 || getResp.ConsumerEndpoint == nil || getResp.ConsumerEndpoint.ConsumerID != "consumer-1" {
		t.Fatalf("unexpected get_consumer resp: action=%s code=%d consumer=%#v", action, getResp.Code, getResp.ConsumerEndpoint)
	}

	srv.sent = nil
	h.handleListConsumersLocal(ctx, hdr, listConsumersReq{ReqID: "list-consumer-1", Consumer: 1, Kind: streamKindVideo})
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response after list_consumers, got %d", len(srv.sent))
	}
	action, listResp := decodeStreamCtrl[listConsumersResp](t, srv.sent[0].payload)
	if action != actionListConsumersResp || listResp.Code != 1 || len(listResp.ConsumerEndpoints) != 1 || listResp.ConsumerEndpoints[0].ConsumerID != "consumer-1" {
		t.Fatalf("unexpected list_consumers resp: action=%s code=%d consumers=%#v", action, listResp.Code, listResp.ConsumerEndpoints)
	}
}

func TestStreamConnectSameNodeSuccess(t *testing.T) {
	ctx, srv := newTestContext(t)
	h := NewHandler(nil)
	hdr := newRequestHeader(2, 1)

	h.sources["source-video"] = &sourceEntry{
		desc: sourceDescriptor{
			SourceID: "source-video",
			Producer: 1,
			Kind:     streamKindVideo,
			Mode:     modeLive,
			UnitMode: unitModeFrame,
		},
		deliveries: make(map[string]struct{}),
	}
	h.consumers["consumer-video"] = &consumerEntry{
		desc: consumerDescriptor{
			ConsumerID: "consumer-video",
			Consumer:   1,
			Kind:       streamKindVideo,
		},
		deliveries: make(map[string]struct{}),
	}

	h.handleConnectCoordinatorLocal(ctx, hdr, connectReq{
		ReqID:      "connect-1",
		Producer:   1,
		SourceID:   "source-video",
		Consumer:   1,
		ConsumerID: "consumer-video",
	})

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response after connect, got %d", len(srv.sent))
	}
	action, resp := decodeStreamCtrl[connectResp](t, srv.sent[0].payload)
	if action != actionConnectResp || resp.Code != 1 || !resp.Accept || resp.DeliveryID == "" {
		t.Fatalf("unexpected connect resp: action=%s code=%d accept=%v delivery_id=%q", action, resp.Code, resp.Accept, resp.DeliveryID)
	}
	route, ok := h.getRoute(resp.DeliveryID)
	if !ok || route.State != stateActive {
		t.Fatalf("expected active delivery route, ok=%v state=%q", ok, route.State)
	}
	h.mu.RLock()
	pd := h.producerDeliveries[resp.DeliveryID]
	cd := h.consumerDeliveries[resp.DeliveryID]
	h.mu.RUnlock()
	if pd == nil || pd.State != stateActive || pd.Coordinator != 1 {
		t.Fatalf("unexpected producer delivery: %#v", pd)
	}
	if cd == nil || cd.State != stateActive || cd.Coordinator != 1 || cd.UnitMode != unitModeFrame {
		t.Fatalf("unexpected consumer delivery: %#v", cd)
	}
}

func TestStreamConnectKindMismatch(t *testing.T) {
	ctx, srv := newTestContext(t)
	h := NewHandler(nil)
	hdr := newRequestHeader(2, 1)

	h.sources["source-video"] = &sourceEntry{
		desc: sourceDescriptor{
			SourceID: "source-video",
			Producer: 1,
			Kind:     streamKindVideo,
			Mode:     modeLive,
			UnitMode: unitModeFrame,
		},
		deliveries: make(map[string]struct{}),
	}
	h.consumers["consumer-text"] = &consumerEntry{
		desc: consumerDescriptor{
			ConsumerID: "consumer-text",
			Consumer:   1,
			Kind:       streamKindText,
		},
		deliveries: make(map[string]struct{}),
	}

	h.handleConnectCoordinatorLocal(ctx, hdr, connectReq{
		ReqID:      "connect-mismatch",
		Producer:   1,
		SourceID:   "source-video",
		Consumer:   1,
		ConsumerID: "consumer-text",
	})

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response after connect mismatch, got %d", len(srv.sent))
	}
	action, resp := decodeStreamCtrl[connectResp](t, srv.sent[0].payload)
	if action != actionConnectResp || resp.Code != 406 {
		t.Fatalf("expected kind mismatch resp, action=%s code=%d", action, resp.Code)
	}
	if len(h.deliveryRoutes) != 0 || len(h.producerDeliveries) != 0 || len(h.consumerDeliveries) != 0 {
		t.Fatalf("expected no leaked delivery state, routes=%d producer=%d consumer=%d", len(h.deliveryRoutes), len(h.producerDeliveries), len(h.consumerDeliveries))
	}
}

func TestStreamDataAckStateGuards(t *testing.T) {
	ctx, srv := newTestContext(t)
	h := NewHandler(nil)
	dataUUID, ok := parseUUID("123e4567-e89b-12d3-a456-426614174000")
	if !ok {
		t.Fatal("parse data uuid failed")
	}
	dataDeliveryID := uuidToString(dataUUID)

	h.consumerDeliveries[dataDeliveryID] = &consumerDelivery{
		DeliveryID:       dataDeliveryID,
		TxnID:            "txn-data",
		Producer:         3,
		Consumer:         1,
		ConsumerID:       "consumer-video",
		Kind:             streamKindVideo,
		UnitMode:         unitModeChunk,
		State:            statePending,
		ExpectedPosition: 10,
	}

	dataHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorMsg).
		WithSubProto(SubProtoStream).
		WithSourceID(3).
		WithTargetID(1)
	dataPayload := encodeDataHeaderV1(dataUUID, 10, 0, 0, []byte("hello"))

	h.handleData(ctx, dataHdr, dataPayload)
	if got := h.consumerDeliveries[dataDeliveryID].ExpectedPosition; got != 10 {
		t.Fatalf("expected pending delivery to ignore data, got position=%d", got)
	}
	if len(srv.sent) != 0 {
		t.Fatalf("expected no ack for pending delivery, got %d sends", len(srv.sent))
	}

	h.consumerDeliveries[dataDeliveryID].State = stateActive
	h.handleData(ctx, dataHdr, dataPayload)
	if got := h.consumerDeliveries[dataDeliveryID].ExpectedPosition; got != 15 {
		t.Fatalf("expected active delivery to advance to 15, got %d", got)
	}
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 ack send, got %d", len(srv.sent))
	}
	if srv.sent[0].hdr.SourceID() != 1 || srv.sent[0].hdr.TargetID() != 3 || srv.sent[0].hdr.Major() != header.MajorMsg {
		t.Fatalf("unexpected ack hdr src=%d tgt=%d major=%d", srv.sent[0].hdr.SourceID(), srv.sent[0].hdr.TargetID(), srv.sent[0].hdr.Major())
	}
	ackHdr, ok := decodeAckHeaderV1(srv.sent[0].payload)
	if !ok || ackHdr.Position != 15 {
		t.Fatalf("unexpected ack payload: ok=%v pos=%d", ok, ackHdr.Position)
	}

	ackUUID, ok := parseUUID("123e4567-e89b-12d3-a456-426614174001")
	if !ok {
		t.Fatal("parse ack uuid failed")
	}
	ackDeliveryID := uuidToString(ackUUID)
	h.producerDeliveries[ackDeliveryID] = &producerDelivery{
		DeliveryID:    ackDeliveryID,
		TxnID:         "txn-ack",
		Producer:      1,
		Consumer:      3,
		ConsumerID:    "consumer-video",
		State:         stateActive,
		AckedPosition: 7,
	}

	wrongAckHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorMsg).
		WithSubProto(SubProtoStream).
		WithSourceID(4).
		WithTargetID(1)
	h.handleAck(ctx, wrongAckHdr, encodeAckHeaderV1(ackUUID, 12, 0, 0))
	if got := h.producerDeliveries[ackDeliveryID].AckedPosition; got != 7 {
		t.Fatalf("expected wrong-direction ack to be ignored, got %d", got)
	}

	validAckHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorMsg).
		WithSubProto(SubProtoStream).
		WithSourceID(3).
		WithTargetID(1)
	h.handleAck(ctx, validAckHdr, encodeAckHeaderV1(ackUUID, 12, 0, 0))
	if got := h.producerDeliveries[ackDeliveryID].AckedPosition; got != 12 {
		t.Fatalf("expected valid ack to advance to 12, got %d", got)
	}
}

func TestRouteCoordinatorRequestKeepsSameChildEndpointsLocal(t *testing.T) {
	ctx, srv := newTestContext(t)
	cfg := coreconfig.NewMap(map[string]string{
		coreconfig.KeyAuthNodeRoles: "2:superadmin",
		coreconfig.KeyAuthRolePerms: "superadmin:*",
	})
	h := NewHandlerWithConfig(cfg, nil)
	hdr := newRequestHeader(2, 1)

	calledLocal := false
	h.routeCoordinatorRequest(ctx, nil, hdr, []byte{kindCtrl, 0x01}, 2, permConnect, 3, 3,
		func(code int, msg string) {
			t.Fatalf("unexpected sendErr: code=%d msg=%s", code, msg)
		},
		func() {
			calledLocal = true
		},
	)

	if !calledLocal {
		t.Fatal("expected same-child endpoints to stay on local coordinator")
	}
	if len(srv.sent) != 0 {
		t.Fatalf("expected no forwarded public request, got %d sends", len(srv.sent))
	}
}

func TestRouteCoordinatorRequestSameChildStillChecksPermission(t *testing.T) {
	ctx, srv := newTestContext(t)
	h := NewHandlerWithConfig(coreconfig.NewMap(nil), nil)
	hdr := newRequestHeader(2, 1)

	gotCode := 0
	gotMsg := ""
	h.routeCoordinatorRequest(ctx, nil, hdr, []byte{kindCtrl, 0x01}, 2, permConnect, 3, 3,
		func(code int, msg string) {
			gotCode = code
			gotMsg = msg
		},
		func() {
			t.Fatal("expected permission denial before local coordination")
		},
	)

	if gotCode != 403 || gotMsg != "permission denied" {
		t.Fatalf("expected permission denied, got code=%d msg=%q", gotCode, gotMsg)
	}
	if len(srv.sent) != 0 {
		t.Fatalf("expected no forwarded request on permission denial, got %d sends", len(srv.sent))
	}
}

func TestRouteCoordinatorRequestForwardsUpWhenEndpointUnreachable(t *testing.T) {
	cm := connmgr.New()
	requesterConn := newMockConnection("c-requester", 2)
	if err := cm.Add(requesterConn); err != nil {
		t.Fatalf("add requester conn err=%v", err)
	}
	parentConn := newMockConnection("c-parent", 9)
	parentConn.SetMeta(core.MetaRoleKey, core.RoleParent)
	if err := cm.Add(parentConn); err != nil {
		t.Fatalf("add parent conn err=%v", err)
	}
	srv := &recordServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	cfg := coreconfig.NewMap(map[string]string{
		coreconfig.KeyAuthNodeRoles: "2:superadmin",
		coreconfig.KeyAuthRolePerms: "superadmin:*",
	})
	h := NewHandlerWithConfig(cfg, nil)
	hdr := newRequestHeader(2, 1)

	h.routeCoordinatorRequest(ctx, nil, hdr, []byte{kindCtrl, 0x01}, 2, permConnect, 3, 3,
		func(code int, msg string) {
			t.Fatalf("unexpected sendErr: code=%d msg=%s", code, msg)
		},
		func() {
			t.Fatal("expected unreachable endpoints to forward upward")
		},
	)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 upward forward, got %d", len(srv.sent))
	}
	if srv.sent[0].connID != parentConn.ID() {
		t.Fatalf("expected forward to parent conn, got %s", srv.sent[0].connID)
	}
	if srv.sent[0].hdr.TargetID() != 9 {
		t.Fatalf("expected forwarded target 9, got %d", srv.sent[0].hdr.TargetID())
	}
}
