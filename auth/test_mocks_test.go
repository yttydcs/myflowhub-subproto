package auth

// 本文件覆盖 SubProto 中 `auth` 模块里与 `test_mocks` 相关的行为。

import (
	"context"
	"io"
	"net"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/eventbus"
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

type testServer struct {
	nodeID uint32
	cm     core.IConnectionManager
	cfg    core.IConfig
}

var _ core.IServer = (*testServer)(nil)

func (s *testServer) Start(context.Context) error { return nil }
func (s *testServer) Stop(context.Context) error  { return nil }

func (s *testServer) Config() core.IConfig                                     { return s.cfg }
func (s *testServer) ConnManager() core.IConnectionManager                     { return s.cm }
func (s *testServer) Process() core.IProcess                                   { return nil }
func (s *testServer) HeaderCodec() core.IHeaderCodec                           { return nil }
func (s *testServer) NodeID() uint32                                           { return s.nodeID }
func (s *testServer) UpdateNodeID(id uint32)                                   { s.nodeID = id }
func (s *testServer) EventBus() eventbus.IBus                                  { return nil }
func (s *testServer) Send(context.Context, string, core.IHeader, []byte) error { return nil }
