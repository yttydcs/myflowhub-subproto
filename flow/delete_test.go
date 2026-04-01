package flow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/header"
)

func TestFlowDeleteSuccess(t *testing.T) {
	h, srv, childConn, ctx, baseDir := newDeleteTestEnv(t, nil)
	flowID := "123e4567-e89b-12d3-a456-426614174000"
	h.flows[flowID] = setReq{FlowID: flowID}
	h.schedulers[flowID] = &flowScheduler{stop: make(chan struct{})}
	if err := os.WriteFile(filepath.Join(baseDir, flowID+".json"), []byte(`{"flow_id":"123e4567-e89b-12d3-a456-426614174000"}`), 0o644); err != nil {
		t.Fatalf("write flow file err=%v", err)
	}

	req := deleteReq{ReqID: "req-del-ok", FlowID: flowID}
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1).
		WithMsgID(1001).
		WithTraceID(2002)

	h.handleDelete(ctx, childConn, reqHdr, mustJSON(req))

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 response frame, got=%d", len(srv.sends))
	}
	resp := mustDecodeDeleteResp(t, srv.sends[0].payload)
	if resp.Code != 1 || resp.FlowID != flowID {
		t.Fatalf("unexpected delete resp: %#v", resp)
	}
	if got := srv.sends[0].hdr; got.GetMsgID() != 1001 || got.GetTraceID() != 2002 {
		t.Fatalf("expected msg/trace inherit 1001/2002, got %d/%d", got.GetMsgID(), got.GetTraceID())
	}

	h.mu.Lock()
	_, flowExists := h.flows[flowID]
	_, schedExists := h.schedulers[flowID]
	h.mu.Unlock()
	if flowExists || schedExists {
		t.Fatalf("expected flow/scheduler removed, flow=%v scheduler=%v", flowExists, schedExists)
	}
	if _, err := os.Stat(filepath.Join(baseDir, flowID+".json")); !os.IsNotExist(err) {
		t.Fatalf("expected persisted file removed, err=%v", err)
	}
}

func TestFlowDeleteNotFound(t *testing.T) {
	h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	req := deleteReq{ReqID: "req-del-404", FlowID: "123e4567-e89b-12d3-a456-426614174099"}
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleDelete(ctx, childConn, reqHdr, mustJSON(req))

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 response frame, got=%d", len(srv.sends))
	}
	resp := mustDecodeDeleteResp(t, srv.sends[0].payload)
	if resp.Code != 404 || resp.FlowID != "123e4567-e89b-12d3-a456-426614174099" {
		t.Fatalf("unexpected delete not-found resp: %#v", resp)
	}
}

func TestFlowDeletePermissionDenied(t *testing.T) {
	h, srv, childConn, ctx, baseDir := newDeleteTestEnv(t, map[string]string{
		coreconfig.KeyAuthDefaultRole:  "node",
		coreconfig.KeyAuthDefaultPerms: permFlowSet,
	})
	flowID := "123e4567-e89b-12d3-a456-426614174001"
	h.flows[flowID] = setReq{FlowID: flowID}
	if err := os.WriteFile(filepath.Join(baseDir, flowID+".json"), []byte(`{"flow_id":"123e4567-e89b-12d3-a456-426614174001"}`), 0o644); err != nil {
		t.Fatalf("write flow file err=%v", err)
	}

	req := deleteReq{ReqID: "req-del-deny", FlowID: flowID}
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleDelete(ctx, childConn, reqHdr, mustJSON(req))

	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 response frame, got=%d", len(srv.sends))
	}
	resp := mustDecodeDeleteResp(t, srv.sends[0].payload)
	if resp.Code != 403 {
		t.Fatalf("expected permission denied, got %#v", resp)
	}

	h.mu.Lock()
	_, exists := h.flows[flowID]
	h.mu.Unlock()
	if !exists {
		t.Fatalf("flow should remain when permission denied")
	}
	if _, err := os.Stat(filepath.Join(baseDir, flowID+".json")); err != nil {
		t.Fatalf("persisted file should remain, err=%v", err)
	}
}

func TestFlowDeleteInterruptsActiveRun(t *testing.T) {
	h, srv, childConn, ctx, baseDir := newDeleteTestEnv(t, nil)
	entered := make(chan struct{}, 1)
	stopped := make(chan struct{}, 1)
	h.RegisterLocalMethod("test::wait", func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		select {
		case stopped <- struct{}{}:
		default:
		}
		return nil, ctx.Err()
	})

	flowID := "123e4567-e89b-12d3-a456-426614174002"
	timeoutMs := 10_000
	retry := 0
	flowDef := setReq{
		FlowID: flowID,
		Graph: graph{
			Nodes: []node{
				{
					ID:        "n1",
					Kind:      "call",
					Retry:     &retry,
					TimeoutMs: &timeoutMs,
					Spec:      json.RawMessage(`{"method":"test::wait"}`),
				},
			},
		},
	}
	h.flows[flowID] = flowDef
	if err := os.WriteFile(filepath.Join(baseDir, flowID+".json"), []byte(`{"flow_id":"123e4567-e89b-12d3-a456-426614174002"}`), 0o644); err != nil {
		t.Fatalf("write flow file err=%v", err)
	}

	runID := h.enqueueRun(context.Background(), flowDef)

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("run did not enter blocking node")
	}

	req := deleteReq{ReqID: "req-del-cancel", FlowID: flowID}
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)
	h.handleDelete(ctx, childConn, reqHdr, mustJSON(req))

	if len(srv.sends) == 0 {
		t.Fatalf("expected delete response frame")
	}
	resp := mustDecodeDeleteResp(t, srv.sends[len(srv.sends)-1].payload)
	if resp.Code != 1 {
		t.Fatalf("expected delete success resp, got %#v", resp)
	}

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatalf("run was not interrupted by delete")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		st := h.runs[runID]
		h.mu.Unlock()
		if st == nil {
			t.Fatalf("run state missing: %s", runID)
		}
		st.mu.Lock()
		status := st.status
		reason := st.cancelReason
		st.mu.Unlock()
		if status == "cancelled" {
			if reason != runCancelMsgFlowDeleted {
				t.Fatalf("unexpected cancel reason: %q", reason)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run status did not become cancelled")
}

func newDeleteTestEnv(t *testing.T, cfgData map[string]string) (*Handler, *testServer, *mockConnection, context.Context, string) {
	t.Helper()
	if cfgData == nil {
		cfgData = map[string]string{
			// 默认 happy-path 环境显式使用具备 flow.delete 的角色。
			// 2026-03-26 之后 node 默认角色不再拥有 delete 权限，
			// 仅设置 auth.default_perms="*" 不会覆盖 role_perms[node]。
			coreconfig.KeyAuthDefaultRole: "admin",
		}
	}
	cfg := coreconfig.NewMap(cfgData)
	h := NewHandlerWithConfig(cfg, nil)
	baseDir := t.TempDir()
	h.baseDir = baseDir

	cm := connmgr.New()
	childConn := &mockConnection{id: "c-child"}
	childConn.SetMeta("nodeID", uint32(2))
	if err := cm.Add(childConn); err != nil {
		t.Fatalf("add child conn err=%v", err)
	}

	srv := &testServer{nodeID: 1, cm: cm}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)
	return h, srv, childConn, ctx, baseDir
}

func mustDecodeDeleteResp(t *testing.T, payload []byte) deleteResp {
	t.Helper()
	var env message
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode response envelope err=%v", err)
	}
	if env.Action != actionDeleteResp {
		t.Fatalf("unexpected action=%s", env.Action)
	}
	var resp deleteResp
	if err := json.Unmarshal(env.Data, &resp); err != nil {
		t.Fatalf("decode delete response err=%v", err)
	}
	return resp
}
