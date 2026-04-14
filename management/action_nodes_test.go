package management

// 本文件覆盖 SubProto 中 `management` 模块里与 `action_nodes` 相关的行为。

import (
	"context"
	"io"
	"net"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
)

type nopPipe struct{}

func (nopPipe) Read([]byte) (int, error)    { return 0, io.EOF }
func (nopPipe) Write(p []byte) (int, error) { return len(p), nil }
func (nopPipe) Close() error                { return nil }

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

func (c *stubConn) Pipe() core.IPipe { return nopPipe{} }

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

func TestEnumerateDirectNodes_DisplayNameOmitsBlankMeta(t *testing.T) {
	child := newStubConn("child")
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	child.SetMeta("nodeID", uint32(6))
	child.SetMeta("display_name", "   ")

	got := enumerateDirectNodes(&stubConnManager{conns: []core.IConnection{child}})
	if len(got) != 1 {
		t.Fatalf("expected 1 node, got %d", len(got))
	}
	if got[0].DisplayName != "" {
		t.Fatalf("expected blank display_name to be omitted, got %q", got[0].DisplayName)
	}
}

func TestListSubtreeResponse_IncludesLocalDisplayName(t *testing.T) {
	child := newStubConn("child")
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	child.SetMeta("nodeID", uint32(6))

	srv := &recordServer{
		nodeID: 1,
		cfg: coreconfig.NewMap(map[string]string{
			configKeyNodeDisplayName: "  Hub Alpha  ",
		}),
		cm: &stubConnManager{conns: []core.IConnection{child}},
	}
	act := registerListSubtreeActions(NewHandler(nil))
	ctx := core.WithServerContext(context.Background(), srv)

	act.Handle(ctx, newStubConn("caller"), newRequestHeader(9, 1), nil)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response frame, got %d", len(srv.sent))
	}
	resp := decodeMgmtResponse[listSubtreeResp](t, srv.sent[0].payload)
	if len(resp.Nodes) != 2 {
		t.Fatalf("expected 2 nodes (child+self), got %+v", resp.Nodes)
	}
	self := resp.Nodes[1]
	if self.NodeID != 1 {
		t.Fatalf("expected self node_id=1, got %+v", self)
	}
	if self.DisplayName != "Hub Alpha" {
		t.Fatalf("expected trimmed display_name, got %+v", self)
	}
}
