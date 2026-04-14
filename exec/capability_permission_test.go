package exec

// 本文件覆盖 SubProto 中 `exec` 模块里与 `capability_permission` 相关的行为。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/header"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
)

func TestExecCall_EnforcesCapabilityPermission(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	conn := &mockConnection{id: "caller"}
	conn.SetMeta("nodeID", uint32(2))
	if err := cm.Add(conn); err != nil {
		t.Fatalf("add conn err=%v", err)
	}

	h := NewHandler(nil)
	h.Init()
	if err := h.capRegistry.Register(execcap.Descriptor{
		Provider:    "test",
		Method:      "secure::echo",
		Permissions: []string{"cap.secure"},
	}, func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
		return args, nil
	}); err != nil {
		t.Fatalf("register secure capability err=%v", err)
	}

	h.permCfg.ApplySnapshot(permission.Snapshot{
		DefaultRole:  "deny",
		DefaultPerms: []string{},
		NodeRoles: map[uint32]string{
			2: "node",
		},
		RolePerms: map[string][]string{
			"deny": []string{},
			"node": []string{permExecCall},
		},
	})

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(2).
		WithTargetID(1)
	raw, _ := json.Marshal(CallReq{
		ReqID:        "call-secure-deny",
		ExecutorNode: 2,
		TargetNode:   1,
		Method:       "secure::echo",
		Args:         json.RawMessage(`{"x":1}`),
		TimeoutMs:    1000,
	})
	h.handleCall(ctx, conn, reqHdr, raw)

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(srv.sends))
	}
	action, data := decodeExecEnvelope(t, srv.sends[0].payload)
	if action != actionCallResp {
		t.Fatalf("unexpected action=%s", action)
	}
	var resp CallResp
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal call_resp err=%v", err)
	}
	if resp.Code != 403 || !strings.Contains(resp.Msg, "capability permission denied") {
		t.Fatalf("expected capability permission denied, got %+v", resp)
	}
}

func TestExecCall_SameNodeBypassesCapabilityPermission(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	conn := &mockConnection{id: "local"}
	conn.SetMeta("nodeID", uint32(1))
	if err := cm.Add(conn); err != nil {
		t.Fatalf("add conn err=%v", err)
	}

	h := NewHandler(nil)
	h.Init()
	if err := h.capRegistry.Register(execcap.Descriptor{
		Provider:    "test",
		Method:      "secure::echo",
		Permissions: []string{"cap.secure"},
	}, func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
		return args, nil
	}); err != nil {
		t.Fatalf("register secure capability err=%v", err)
	}

	h.permCfg.ApplySnapshot(permission.Snapshot{
		DefaultRole:  "deny",
		DefaultPerms: []string{},
		NodeRoles: map[uint32]string{
			1: "node",
		},
		RolePerms: map[string][]string{
			"deny": []string{},
			"node": []string{permExecCall},
		},
	})

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoExec).
		WithSourceID(1).
		WithTargetID(1)
	raw, _ := json.Marshal(CallReq{
		ReqID:        "call-secure-self",
		ExecutorNode: 1,
		TargetNode:   1,
		Method:       "secure::echo",
		Args:         json.RawMessage(`{"ok":1}`),
		TimeoutMs:    1000,
	})
	h.handleCall(ctx, conn, reqHdr, raw)

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(srv.sends))
	}
	action, data := decodeExecEnvelope(t, srv.sends[0].payload)
	if action != actionCallResp {
		t.Fatalf("unexpected action=%s", action)
	}
	var resp CallResp
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal call_resp err=%v", err)
	}
	if resp.Code != 1 {
		t.Fatalf("expected same-node bypass success, got %+v", resp)
	}
}
