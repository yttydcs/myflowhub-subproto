package management

// 本文件覆盖 SubProto 中 `management` 模块里与 `capability_provider` 相关的行为。

import (
	"context"
	"encoding/json"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/eventbus"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
)

type capServer struct {
	nodeID uint32
	cfg    core.IConfig
	cm     core.IConnectionManager
}

func (s *capServer) Start(context.Context) error { return nil }
func (s *capServer) Stop(context.Context) error  { return nil }
func (s *capServer) Config() core.IConfig        { return s.cfg }
func (s *capServer) ConnManager() core.IConnectionManager {
	return s.cm
}
func (s *capServer) Process() core.IProcess         { return nil }
func (s *capServer) HeaderCodec() core.IHeaderCodec { return nil }
func (s *capServer) NodeID() uint32                 { return s.nodeID }
func (s *capServer) UpdateNodeID(id uint32)         { s.nodeID = id }
func (s *capServer) EventBus() eventbus.IBus        { return nil }
func (s *capServer) Send(context.Context, string, core.IHeader, []byte) error {
	return nil
}

func TestManagementCapabilitiesListNodesNodeInfo(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{
		configKeyNodeDisplayName: "Hub Alpha",
	})
	cm := &stubConnManager{}
	child := newStubConn("child")
	child.SetMeta("nodeID", uint32(6))
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child err=%v", err)
	}
	srv := &capServer{nodeID: 1, cfg: cfg, cm: cm}

	h := NewHandler(nil)
	h.BindServer(srv)

	reg := execcap.SharedRegistry(cfg)
	_, listInvoke, ok := reg.Lookup(capabilityMgmtListNodes, "")
	if !ok || listInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityMgmtListNodes)
	}
	_, infoInvoke, ok := reg.Lookup(capabilityMgmtNodeInfo, "")
	if !ok || infoInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityMgmtNodeInfo)
	}

	ctx := core.WithServerContext(context.Background(), srv)
	rawNodes, err := listInvoke(ctx, nil)
	if err != nil {
		t.Fatalf("invoke list_nodes err=%v", err)
	}
	var nodesResp map[string]json.RawMessage
	if err := json.Unmarshal(rawNodes, &nodesResp); err != nil {
		t.Fatalf("unmarshal list_nodes result err=%v", err)
	}
	if len(nodesResp["nodes"]) == 0 {
		t.Fatalf("expected nodes in list_nodes result")
	}

	rawInfo, err := infoInvoke(ctx, nil)
	if err != nil {
		t.Fatalf("invoke node_info err=%v", err)
	}
	var infoResp map[string]map[string]string
	if err := json.Unmarshal(rawInfo, &infoResp); err != nil {
		t.Fatalf("unmarshal node_info result err=%v", err)
	}
	if infoResp["items"]["node_id"] != "1" {
		t.Fatalf("unexpected node_info result=%v", infoResp)
	}
	if infoResp["items"]["display_name"] != "Hub Alpha" {
		t.Fatalf("expected display_name in node_info result, got %v", infoResp)
	}
}
