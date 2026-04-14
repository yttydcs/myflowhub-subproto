package management

// 本文件覆盖 SubProto 中 `management` 模块里与 `management_response_path` 相关的行为。

import (
	"context"
	"encoding/json"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
)

func encodeMgmtFrame(t *testing.T, action string, data any) []byte {
	t.Helper()

	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal management payload err=%v", err)
	}
	body, err := json.Marshal(mgmtMessage{Action: action, Data: raw})
	if err != nil {
		t.Fatalf("marshal management frame err=%v", err)
	}
	return body
}

func TestOnReceive_ConfigSetRespRefreshesDirectChildDisplayName(t *testing.T) {
	child := newStubConn("child")
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	child.SetMeta("nodeID", uint32(6))

	parent := newStubConn("parent")
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	parent.SetMeta("nodeID", uint32(1))

	cm := &stubConnManager{conns: []core.IConnection{child, parent}}
	srv := &recordServer{nodeID: 5, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	h := NewHandler(nil)
	payload := encodeMgmtFrame(t, actionConfigSetResp, configResp{
		Code:  1,
		Msg:   "ok",
		Key:   configKeyNodeDisplayName,
		Value: "  Child Renamed  ",
	})

	h.OnReceive(ctx, child, newRequestHeader(6, 1), payload)

	if got, _ := child.GetMeta("display_name"); got != "Child Renamed" {
		t.Fatalf("expected trimmed display_name metadata, got %v", got)
	}
	if got, _ := child.GetMeta(configKeyNodeDisplayName); got != "Child Renamed" {
		t.Fatalf("expected config key metadata to refresh, got %v", got)
	}
	nodes := enumerateDirectNodes(cm)
	if len(nodes) != 1 || nodes[0].DisplayName != "Child Renamed" {
		t.Fatalf("expected list_nodes to expose refreshed display_name, got %+v", nodes)
	}
	if len(srv.sent) != 1 || srv.sent[0].connID != parent.ID() {
		t.Fatalf("expected response to keep forwarding to parent, got %+v", srv.sent)
	}
}

func TestOnReceive_ConfigSetRespDoesNotPolluteIntermediateConnForDescendant(t *testing.T) {
	child := newStubConn("child")
	child.SetMeta(core.MetaRoleKey, core.RoleChild)
	child.SetMeta("nodeID", uint32(6))
	child.SetMeta("display_name", "Hub Six")
	child.SetMeta(configKeyNodeDisplayName, "Hub Six")

	parent := newStubConn("parent")
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	parent.SetMeta("nodeID", uint32(1))

	cm := &stubConnManager{conns: []core.IConnection{child, parent}}
	srv := &recordServer{nodeID: 5, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	h := NewHandler(nil)
	payload := encodeMgmtFrame(t, actionConfigSetResp, configResp{
		Code:  1,
		Msg:   "ok",
		Key:   configKeyNodeDisplayName,
		Value: "Leaf Eleven",
	})

	h.OnReceive(ctx, child, newRequestHeader(11, 1), payload)

	if got, _ := child.GetMeta("display_name"); got != "Hub Six" {
		t.Fatalf("expected intermediate conn display_name to stay unchanged, got %v", got)
	}
	if got, _ := child.GetMeta(configKeyNodeDisplayName); got != "Hub Six" {
		t.Fatalf("expected intermediate conn config metadata to stay unchanged, got %v", got)
	}
	nodes := enumerateDirectNodes(cm)
	if len(nodes) != 1 || nodes[0].DisplayName != "Hub Six" {
		t.Fatalf("expected list_nodes to keep original display_name, got %+v", nodes)
	}
	if len(srv.sent) != 1 || srv.sent[0].connID != parent.ID() {
		t.Fatalf("expected descendant response to continue forwarding to parent, got %+v", srv.sent)
	}
}
