package flow

// 本文件覆盖 SubProto 中 `flow` 模块里与 `delete` 相关的行为。

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

func TestFlowCancelRunSuccess(t *testing.T) {
	h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
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

	flowID := "123e4567-e89b-12d3-a456-426614174003"
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

	runID := h.enqueueRun(context.Background(), flowDef)

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("run did not enter blocking node")
	}

	req := cancelRunReq{ReqID: "req-cancel-ok", FlowID: flowID, RunID: runID}
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)
	h.handleCancelRun(ctx, childConn, reqHdr, mustJSON(req))

	if len(srv.sends) == 0 {
		t.Fatalf("expected cancel_run response frame")
	}
	resp := mustDecodeCancelRunResp(t, srv.sends[len(srv.sends)-1].payload)
	if resp.Code != 1 || resp.FlowID != flowID || resp.RunID != runID || resp.Status != "cancelled" {
		t.Fatalf("unexpected cancel_run resp: %#v", resp)
	}

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatalf("run was not interrupted by cancel_run")
	}

	deadline := time.Now().Add(2 * time.Second)
	cancelled := false
	for time.Now().Before(deadline) {
		h.mu.Lock()
		st := h.runs[runID]
		_, flowExists := h.flows[flowID]
		h.mu.Unlock()
		if st == nil {
			t.Fatalf("run state missing: %s", runID)
		}
		st.mu.Lock()
		status := st.status
		reason := st.cancelReason
		st.mu.Unlock()
		if status == "cancelled" {
			if reason != runCancelMsgManual {
				t.Fatalf("unexpected cancel reason: %q", reason)
			}
			if !flowExists {
				t.Fatalf("flow definition should remain after cancel_run")
			}
			cancelled = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !cancelled {
		t.Fatalf("run status did not become cancelled after cancel_run")
	}

	srv.sends = nil
	h.handleStatus(ctx, childConn, reqHdr, mustJSON(statusReq{
		ReqID:  "req-status-after-cancel",
		FlowID: flowID,
		RunID:  runID,
	}))
	statusResp := mustDecodeStatusResp(t, srv.sends[len(srv.sends)-1].payload)
	if statusResp.Code != 1 || statusResp.Status != "cancelled" || statusResp.Msg != runCancelMsgManual {
		t.Fatalf("unexpected status after cancel_run: %#v", statusResp)
	}

	srv.sends = nil
	h.handleDetail(ctx, childConn, reqHdr, mustJSON(detailReq{
		ReqID:  "req-detail-after-cancel",
		FlowID: flowID,
		RunID:  runID,
		NodeID: "n1",
	}))
	detailResp := mustDecodeDetailResp(t, srv.sends[len(srv.sends)-1].payload)
	if detailResp.Code != 1 || detailResp.Msg != runCancelMsgManual {
		t.Fatalf("unexpected detail after cancel_run: %#v", detailResp)
	}
	if detailResp.Node == nil || detailResp.Node.Status != "cancelled" || detailResp.Node.Msg != runCancelMsgManual {
		t.Fatalf("unexpected detail node after cancel_run: %#v", detailResp.Node)
	}
}

func TestFlowCancelRunNotFoundOrTerminal(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
		reqHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(SubProtoFlow).
			WithSourceID(2).
			WithTargetID(1)

		h.handleCancelRun(ctx, childConn, reqHdr, mustJSON(cancelRunReq{
			ReqID:  "req-cancel-missing",
			FlowID: "123e4567-e89b-12d3-a456-426614174004",
			RunID:  "123e4567-e89b-12d3-a456-426614174104",
		}))

		assertRespCode(t, srv, actionCancelRunResp, 404)
	})

	t.Run("flow mismatch", func(t *testing.T) {
		h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
		runID := "123e4567-e89b-12d3-a456-426614174105"
		state := &runState{
			flowID:  "123e4567-e89b-12d3-a456-426614174005",
			runID:   runID,
			status:  "running",
			cancel:  func() {},
			runtime: newRunContext("123e4567-e89b-12d3-a456-426614174005", runID, 1, nil),
		}
		h.runs[runID] = state
		h.runOrderByFlow[state.flowID] = []string{runID}

		reqHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(SubProtoFlow).
			WithSourceID(2).
			WithTargetID(1)
		h.handleCancelRun(ctx, childConn, reqHdr, mustJSON(cancelRunReq{
			ReqID:  "req-cancel-flow-mismatch",
			FlowID: "123e4567-e89b-12d3-a456-426614174006",
			RunID:  runID,
		}))

		assertRespCode(t, srv, actionCancelRunResp, 404)
	})

	t.Run("terminal", func(t *testing.T) {
		h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
		flowID := "123e4567-e89b-12d3-a456-426614174007"
		runID := "123e4567-e89b-12d3-a456-426614174107"
		state := &runState{
			flowID:  flowID,
			runID:   runID,
			status:  "succeeded",
			runtime: newRunContext(flowID, runID, 1, nil),
		}
		h.runs[runID] = state
		h.runOrderByFlow[flowID] = []string{runID}

		reqHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(SubProtoFlow).
			WithSourceID(2).
			WithTargetID(1)
		h.handleCancelRun(ctx, childConn, reqHdr, mustJSON(cancelRunReq{
			ReqID:  "req-cancel-terminal",
			FlowID: flowID,
			RunID:  runID,
		}))

		resp := mustDecodeCancelRunResp(t, srv.sends[len(srv.sends)-1].payload)
		if resp.Code != 409 || resp.Status != "succeeded" {
			t.Fatalf("unexpected terminal cancel_run resp: %#v", resp)
		}
	})
}

func TestFlowRunReadPermissionDenied(t *testing.T) {
	type testCase struct {
		name       string
		wantAction string
		invoke     func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader)
	}

	cfgData := map[string]string{
		coreconfig.KeyAuthDefaultRole: "node",
		coreconfig.KeyAuthRolePerms:   "node:flow.set",
	}
	cases := []testCase{
		{
			name:       "run",
			wantAction: actionRunResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleRun(ctx, conn, hdr, mustJSON(runReq{
					ReqID:  "req-run-deny",
					FlowID: "123e4567-e89b-12d3-a456-426614174108",
				}))
			},
		},
		{
			name:       "cancel_run",
			wantAction: actionCancelRunResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleCancelRun(ctx, conn, hdr, mustJSON(cancelRunReq{
					ReqID:  "req-cancel-deny",
					FlowID: "123e4567-e89b-12d3-a456-426614174109",
					RunID:  "123e4567-e89b-12d3-a456-426614174209",
				}))
			},
		},
		{
			name:       "status",
			wantAction: actionStatusResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleStatus(ctx, conn, hdr, mustJSON(statusReq{
					ReqID:  "req-status-deny",
					FlowID: "123e4567-e89b-12d3-a456-426614174110",
				}))
			},
		},
		{
			name:       "detail",
			wantAction: actionDetailResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleDetail(ctx, conn, hdr, mustJSON(detailReq{
					ReqID:  "req-detail-deny",
					FlowID: "123e4567-e89b-12d3-a456-426614174111",
					NodeID: "n1",
				}))
			},
		},
		{
			name:       "list_runs",
			wantAction: actionListRunsResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleListRuns(ctx, conn, hdr, mustJSON(listRunsReq{
					ReqID:  "req-list-runs-deny",
					FlowID: "123e4567-e89b-12d3-a456-426614174112",
				}))
			},
		},
		{
			name:       "list",
			wantAction: actionListResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleList(ctx, conn, hdr, mustJSON(listReq{
					ReqID: "req-list-deny",
				}))
			},
		},
		{
			name:       "get",
			wantAction: actionGetResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleGet(ctx, conn, hdr, mustJSON(getReq{
					ReqID:  "req-get-deny",
					FlowID: "123e4567-e89b-12d3-a456-426614174113",
				}))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, srv, childConn, ctx, _ := newDeleteTestEnv(t, cfgData)
			reqHdr := (&header.HeaderTcp{}).
				WithMajor(header.MajorCmd).
				WithSubProto(SubProtoFlow).
				WithSourceID(2).
				WithTargetID(1)

			tc.invoke(h, ctx, childConn, reqHdr)

			assertRespCode(t, srv, tc.wantAction, 403)
		})
	}
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

func mustDecodeCancelRunResp(t *testing.T, payload []byte) cancelRunResp {
	t.Helper()
	var env message
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode response envelope err=%v", err)
	}
	if env.Action != actionCancelRunResp {
		t.Fatalf("unexpected action=%s", env.Action)
	}
	var resp cancelRunResp
	if err := json.Unmarshal(env.Data, &resp); err != nil {
		t.Fatalf("decode cancel_run response err=%v", err)
	}
	return resp
}
