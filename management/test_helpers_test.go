package management

// 本文件覆盖 SubProto 中 `management` 模块里与 `test_helpers` 相关的行为。

import (
	"context"
	"encoding/json"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/eventbus"
	"github.com/yttydcs/myflowhub-core/header"
)

type sentFrame struct {
	connID  string
	header  core.IHeader
	payload []byte
}

type recordServer struct {
	nodeID uint32
	cfg    core.IConfig
	cm     core.IConnectionManager
	sent   []sentFrame
}

func (s *recordServer) Start(context.Context) error { return nil }
func (s *recordServer) Stop(context.Context) error  { return nil }
func (s *recordServer) Config() core.IConfig        { return s.cfg }
func (s *recordServer) ConnManager() core.IConnectionManager {
	return s.cm
}
func (s *recordServer) Process() core.IProcess         { return nil }
func (s *recordServer) HeaderCodec() core.IHeaderCodec { return nil }
func (s *recordServer) NodeID() uint32                 { return s.nodeID }
func (s *recordServer) UpdateNodeID(id uint32)         { s.nodeID = id }
func (s *recordServer) EventBus() eventbus.IBus        { return nil }
func (s *recordServer) Send(_ context.Context, connID string, hdr core.IHeader, payload []byte) error {
	frame := sentFrame{
		connID:  connID,
		header:  hdr,
		payload: append([]byte(nil), payload...),
	}
	s.sent = append(s.sent, frame)
	return nil
}

func newRequestHeader(sourceID, targetID uint32) core.IHeader {
	hdr := &header.HeaderTcp{}
	hdr.WithMajor(header.MajorCmd).
		WithSubProto(SubProtoManagement).
		WithSourceID(sourceID).
		WithTargetID(targetID).
		WithMsgID(11).
		WithTraceID(22)
	return hdr
}

func decodeMgmtResponse[T any](t *testing.T, payload []byte) T {
	t.Helper()

	var frame mgmtMessage
	if err := json.Unmarshal(payload, &frame); err != nil {
		t.Fatalf("unmarshal management frame err=%v", err)
	}
	var resp T
	if err := json.Unmarshal(frame.Data, &resp); err != nil {
		t.Fatalf("unmarshal action payload err=%v", err)
	}
	return resp
}
