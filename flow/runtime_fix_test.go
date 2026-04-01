package flow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
)

func TestFlowSetPersistenceFailureKeepsPreviousState(t *testing.T) {
	h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	flowID := "123e4567-e89b-12d3-a456-426614174010"
	oldScheduler := &flowScheduler{stop: make(chan struct{})}
	h.flows[flowID] = setReq{
		FlowID: flowID,
		Name:   "old-name",
		Trigger: trigger{
			Type:    "interval",
			EveryMs: 1000,
		},
		Graph: simpleGraph("debug::echo"),
	}
	h.schedulers[flowID] = oldScheduler

	basePath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(basePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("prepare base path err=%v", err)
	}
	h.baseDir = basePath

	req := setReq{
		ReqID:  "req-set-persist-fail",
		FlowID: flowID,
		Name:   "new-name",
		Trigger: trigger{
			Type:    "interval",
			EveryMs: 2000,
		},
		Graph: simpleGraph("debug::fail"),
	}
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleSet(ctx, childConn, reqHdr, mustJSON(req))

	assertRespCode(t, srv, actionSetResp, 500)

	h.mu.Lock()
	got := h.flows[flowID]
	gotScheduler := h.schedulers[flowID]
	h.mu.Unlock()

	if got.Name != "old-name" || got.Trigger.EveryMs != 1000 {
		t.Fatalf("flow should remain unchanged after persist failure, got=%#v", got)
	}
	if gotScheduler != oldScheduler {
		t.Fatalf("scheduler should remain unchanged")
	}
	select {
	case <-oldScheduler.stop:
		t.Fatalf("scheduler should not be closed on set failure")
	default:
	}
}

func TestFlowDeleteFileFailureKeepsState(t *testing.T) {
	h, srv, childConn, ctx, baseDir := newDeleteTestEnv(t, nil)
	flowID := "123e4567-e89b-12d3-a456-426614174011"
	oldScheduler := &flowScheduler{stop: make(chan struct{})}
	cancelled := false
	runID := "run-active"
	run := &runState{
		flowID:  flowID,
		runID:   runID,
		status:  "running",
		start:   time.Now(),
		cancel:  func() { cancelled = true },
		runtime: newRunContext(flowID, runID, 1, nil),
	}
	h.flows[flowID] = setReq{FlowID: flowID, Graph: simpleGraph("debug::echo")}
	h.schedulers[flowID] = oldScheduler
	h.runs[runID] = run
	h.runOrderByFlow[flowID] = []string{runID}

	flowPath := filepath.Join(baseDir, flowID+".json")
	if err := os.Mkdir(flowPath, 0o755); err != nil {
		t.Fatalf("mkdir flow path err=%v", err)
	}
	if err := os.WriteFile(filepath.Join(flowPath, "child.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write nested file err=%v", err)
	}

	req := deleteReq{ReqID: "req-del-file-fail", FlowID: flowID}
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleDelete(ctx, childConn, reqHdr, mustJSON(req))

	assertRespCode(t, srv, actionDeleteResp, 500)

	h.mu.Lock()
	_, flowExists := h.flows[flowID]
	gotScheduler := h.schedulers[flowID]
	gotRun := h.runs[runID]
	gotOrder := append([]string(nil), h.runOrderByFlow[flowID]...)
	h.mu.Unlock()

	if !flowExists {
		t.Fatalf("flow should remain when delete file fails")
	}
	if gotScheduler != oldScheduler {
		t.Fatalf("scheduler should remain when delete file fails")
	}
	if gotRun != run {
		t.Fatalf("run state should remain registered when delete file fails")
	}
	if len(gotOrder) != 1 || gotOrder[0] != runID {
		t.Fatalf("unexpected run order after delete failure: %v", gotOrder)
	}
	if cancelled {
		t.Fatalf("run cancel should not happen before delete commit")
	}
	run.mu.Lock()
	status := run.status
	run.mu.Unlock()
	if status != "running" {
		t.Fatalf("run status should remain running, got=%s", status)
	}
	select {
	case <-oldScheduler.stop:
		t.Fatalf("scheduler should not be closed on delete failure")
	default:
	}
	if _, err := os.Stat(flowPath); err != nil {
		t.Fatalf("flow path should remain after delete failure, err=%v", err)
	}
}

func TestFlowRemoteForwardFailureReturnsResp(t *testing.T) {
	type testCase struct {
		name       string
		wantAction string
		invoke     func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader)
	}

	cases := []testCase{
		{
			name:       "run",
			wantAction: actionRunResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleRun(ctx, conn, hdr, mustJSON(runReq{
					ReqID:        "req-run-remote-fail",
					FlowID:       "123e4567-e89b-12d3-a456-426614174012",
					ExecutorNode: 99,
				}))
			},
		},
		{
			name:       "status",
			wantAction: actionStatusResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleStatus(ctx, conn, hdr, mustJSON(statusReq{
					ReqID:        "req-status-remote-fail",
					FlowID:       "123e4567-e89b-12d3-a456-426614174013",
					ExecutorNode: 99,
				}))
			},
		},
		{
			name:       "detail",
			wantAction: actionDetailResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleDetail(ctx, conn, hdr, mustJSON(detailReq{
					ReqID:        "req-detail-remote-fail",
					FlowID:       "123e4567-e89b-12d3-a456-426614174016",
					ExecutorNode: 99,
					NodeID:       "n1",
				}))
			},
		},
		{
			name:       "list",
			wantAction: actionListResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleList(ctx, conn, hdr, mustJSON(listReq{
					ReqID:        "req-list-remote-fail",
					ExecutorNode: 99,
				}))
			},
		},
		{
			name:       "get",
			wantAction: actionGetResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleGet(ctx, conn, hdr, mustJSON(getReq{
					ReqID:        "req-get-remote-fail",
					FlowID:       "123e4567-e89b-12d3-a456-426614174014",
					ExecutorNode: 99,
				}))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
			reqHdr := (&header.HeaderTcp{}).
				WithMajor(header.MajorCmd).
				WithSubProto(SubProtoFlow).
				WithSourceID(2).
				WithTargetID(1)

			tc.invoke(h, ctx, childConn, reqHdr)

			assertRespCode(t, srv, tc.wantAction, 404)
		})
	}
}

func TestFlowRunRetentionPrunesCompletedRunsAndKeepsLatestLookup(t *testing.T) {
	h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	h.maxRetainedRuns = 2

	flowID := "123e4567-e89b-12d3-a456-426614174015"
	flowDef := setReq{
		FlowID: flowID,
		Name:   "retention-flow",
		Graph:  simpleGraph("debug::echo"),
	}
	h.flows[flowID] = flowDef

	runIDs := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		runID := h.enqueueRun(context.Background(), flowDef)
		runIDs = append(runIDs, runID)
		waitRunTerminal(t, h, runID)
	}
	waitRunPruned(t, h, flowID, 2)

	h.mu.Lock()
	if _, ok := h.runs[runIDs[0]]; ok {
		h.mu.Unlock()
		t.Fatalf("oldest completed run should be pruned")
	}
	gotOrder := append([]string(nil), h.runOrderByFlow[flowID]...)
	h.mu.Unlock()
	if len(gotOrder) != 2 || gotOrder[0] != runIDs[1] || gotOrder[1] != runIDs[2] {
		t.Fatalf("unexpected retained run order: %v", gotOrder)
	}

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleStatus(ctx, childConn, reqHdr, mustJSON(statusReq{
		ReqID:  "req-status-latest",
		FlowID: flowID,
	}))
	statusResp := mustDecodeStatusResp(t, srv.sends[len(srv.sends)-1].payload)
	if statusResp.Code != 1 || statusResp.RunID != runIDs[2] {
		t.Fatalf("unexpected latest status resp: %#v", statusResp)
	}

	srv.sends = nil
	h.handleList(ctx, childConn, reqHdr, mustJSON(listReq{ReqID: "req-list-latest"}))
	listResp := mustDecodeListResp(t, srv.sends[len(srv.sends)-1].payload)
	if listResp.Code != 1 || len(listResp.Flows) != 1 || listResp.Flows[0].LastRunID != runIDs[2] {
		t.Fatalf("unexpected list resp: %#v", listResp)
	}

	srv.sends = nil
	h.handleStatus(ctx, childConn, reqHdr, mustJSON(statusReq{
		ReqID:  "req-status-pruned",
		FlowID: flowID,
		RunID:  runIDs[0],
	}))
	assertRespCode(t, srv, actionStatusResp, 404)
}

func simpleGraph(method string) graph {
	return graph{
		Nodes: []node{
			{
				ID:   "n1",
				Kind: "call",
				Spec: mustJSON(map[string]any{"method": method}),
			},
		},
	}
}

func waitRunTerminal(t *testing.T, h *Handler, runID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		st := h.runs[runID]
		h.mu.Unlock()
		if st != nil {
			st.mu.Lock()
			status := st.status
			st.mu.Unlock()
			if isTerminalRunStatus(status) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run %s did not reach terminal status", runID)
}

func waitRunPruned(t *testing.T, h *Handler, flowID string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		got := len(h.runOrderByFlow[flowID])
		h.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run order was not pruned to %d", want)
}

func mustDecodeStatusResp(t *testing.T, payload []byte) statusResp {
	t.Helper()
	var env message
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode status envelope err=%v", err)
	}
	if env.Action != actionStatusResp {
		t.Fatalf("unexpected status action=%s", env.Action)
	}
	var resp statusResp
	if err := json.Unmarshal(env.Data, &resp); err != nil {
		t.Fatalf("decode status response err=%v", err)
	}
	return resp
}

func mustDecodeListResp(t *testing.T, payload []byte) listResp {
	t.Helper()
	var env message
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode list envelope err=%v", err)
	}
	if env.Action != actionListResp {
		t.Fatalf("unexpected list action=%s", env.Action)
	}
	var resp listResp
	if err := json.Unmarshal(env.Data, &resp); err != nil {
		t.Fatalf("decode list response err=%v", err)
	}
	return resp
}
