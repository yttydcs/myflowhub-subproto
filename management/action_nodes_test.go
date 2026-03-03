package management

import (
	"net"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
)

type stubConn struct {
	id   string
	meta map[string]any
}

func newStubConn(id string) *stubConn {
	return &stubConn{id: id, meta: make(map[string]any)}
}

func (c *stubConn) Send([]byte) error { return nil }

func (c *stubConn) SendWithHeader(core.IHeader, []byte, core.IHeaderCodec) error { return nil }

func (c *stubConn) ID() string { return c.id }

func (c *stubConn) Close() error { return nil }

func (c *stubConn) OnReceive(core.ReceiveHandler) {}

func (c *stubConn) SetMeta(key string, val any) {
	c.meta[key] = val
}

func (c *stubConn) GetMeta(key string) (any, bool) {
	v, ok := c.meta[key]
	return v, ok
}

func (c *stubConn) Metadata() map[string]any { return c.meta }

func (c *stubConn) LocalAddr() net.Addr  { return nil }
func (c *stubConn) RemoteAddr() net.Addr { return nil }

func (c *stubConn) Reader() core.IReader { return nil }

func (c *stubConn) SetReader(core.IReader) {}

func (c *stubConn) DispatchReceive(core.IHeader, []byte) {}

func (c *stubConn) RawConn() net.Conn { return nil }

type stubConnManager struct {
	conns []core.IConnection
}

func (m *stubConnManager) Add(conn core.IConnection) error {
	m.conns = append(m.conns, conn)
	return nil
}

func (m *stubConnManager) Remove(string) error { return nil }

func (m *stubConnManager) Get(id string) (core.IConnection, bool) {
	for _, c := range m.conns {
		if c.ID() == id {
			return c, true
		}
	}
	return nil, false
}

func (m *stubConnManager) Range(fn func(core.IConnection) bool) {
	for _, c := range m.conns {
		if !fn(c) {
			return
		}
	}
}

func (m *stubConnManager) Count() int { return len(m.conns) }

func (m *stubConnManager) Broadcast([]byte) error { return nil }

func (m *stubConnManager) CloseAll() error { return nil }

func (m *stubConnManager) SetHooks(core.ConnectionHooks) {}

func (m *stubConnManager) GetByNode(uint32) (core.IConnection, bool) { return nil, false }

func (m *stubConnManager) UpdateNodeIndex(uint32, core.IConnection) {}

func (m *stubConnManager) AddNodeIndex(uint32, core.IConnection) {}

func (m *stubConnManager) RemoveNodeIndex(uint32) {}

func (m *stubConnManager) GetByDevice(string) (core.IConnection, bool) { return nil, false }

func (m *stubConnManager) UpdateDeviceIndex(string, core.IConnection) {}

func TestEnumerateDirectNodes_ChildrenOnlySkipsParent(t *testing.T) {
	parent := newStubConn("parent")
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	parent.SetMeta("nodeID", uint32(1))

	child := newStubConn("child")
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	child.SetMeta("nodeID", uint32(6))

	cm := &stubConnManager{conns: []core.IConnection{parent, child}}
	got := enumerateDirectNodes(cm)

	if len(got) != 1 {
		t.Fatalf("expected 1 node, got %d: %+v", len(got), got)
	}
	if got[0].NodeID != 6 {
		t.Fatalf("expected node_id=6, got %d", got[0].NodeID)
	}
}
