package flow

// 本文件覆盖 SubProto 中 `flow` 模块里与 `runtime_fix` 相关的行为。

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
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
			name:       "cancel_run",
			wantAction: actionCancelRunResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleCancelRun(ctx, conn, hdr, mustJSON(cancelRunReq{
					ReqID:        "req-cancel-remote-fail",
					FlowID:       "123e4567-e89b-12d3-a456-426614174017",
					RunID:        "123e4567-e89b-12d3-a456-426614174117",
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
			name:       "list_runs",
			wantAction: actionListRunsResp,
			invoke: func(h *Handler, ctx context.Context, conn *mockConnection, hdr core.IHeader) {
				h.handleListRuns(ctx, conn, hdr, mustJSON(listRunsReq{
					ReqID:        "req-list-runs-remote-fail",
					FlowID:       "123e4567-e89b-12d3-a456-426614174018",
					ExecutorNode: 99,
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

func TestFlowListRunsReturnsRetainedHistory(t *testing.T) {
	h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	flowID := "123e4567-e89b-12d3-a456-426614174019"
	h.flows[flowID] = setReq{FlowID: flowID, Name: "history-flow", Graph: simpleGraph("debug::echo")}

	now := time.Now().UTC()
	run1 := &runState{
		flowID: flowID,
		runID:  "123e4567-e89b-12d3-a456-426614174119",
		status: "succeeded",
		start:  now.Add(-3 * time.Minute),
		end:    now.Add(-2 * time.Minute),
	}
	run2 := &runState{
		flowID:       flowID,
		runID:        "123e4567-e89b-12d3-a456-426614174120",
		status:       "cancelled",
		start:        now.Add(-2 * time.Minute),
		end:          now.Add(-90 * time.Second),
		cancelReason: runCancelMsgManual,
	}
	run3 := &runState{
		flowID: flowID,
		runID:  "123e4567-e89b-12d3-a456-426614174121",
		status: "running",
		start:  now.Add(-30 * time.Second),
	}
	h.runs[run1.runID] = run1
	h.runs[run2.runID] = run2
	h.runs[run3.runID] = run3
	h.runOrderByFlow[flowID] = []string{run1.runID, run2.runID, run3.runID}

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleListRuns(ctx, childConn, reqHdr, mustJSON(listRunsReq{
		ReqID:  "req-list-runs",
		FlowID: flowID,
		Limit:  2,
	}))

	resp := mustDecodeListRunsResp(t, srv.sends[len(srv.sends)-1].payload)
	if resp.Code != 1 || resp.FlowID != flowID || len(resp.Runs) != 2 {
		t.Fatalf("unexpected list_runs resp: %#v", resp)
	}
	if resp.Runs[0].RunID != run3.runID || resp.Runs[0].Status != "running" {
		t.Fatalf("unexpected latest run summary: %#v", resp.Runs[0])
	}
	if resp.Runs[1].RunID != run2.runID || resp.Runs[1].Status != "cancelled" || resp.Runs[1].Msg != runCancelMsgManual {
		t.Fatalf("unexpected cancelled run summary: %#v", resp.Runs[1])
	}
	if resp.Runs[0].EndedAtMs != 0 {
		t.Fatalf("running run should not have ended_at_ms: %#v", resp.Runs[0])
	}
	if resp.Runs[1].StartedAtMs == 0 || resp.Runs[1].EndedAtMs == 0 {
		t.Fatalf("cancelled run should carry started/ended timestamps: %#v", resp.Runs[1])
	}
}

func TestFlowListRunsNotFound(t *testing.T) {
	h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleListRuns(ctx, childConn, reqHdr, mustJSON(listRunsReq{
		ReqID:  "req-list-runs-404",
		FlowID: "123e4567-e89b-12d3-a456-426614174020",
	}))

	assertRespCode(t, srv, actionListRunsResp, 404)
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

func TestRunLocalRejectsWhenActiveRunLimitReached(t *testing.T) {
	h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	release := make(chan struct{})
	method := "test::manual-run-limit"
	h.RegisterLocalMethod(method, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		<-release
		return json.RawMessage(`{"ok":true}`), nil
	})

	limit := 1
	flowID := "123e4567-e89b-12d3-a456-426614174128"
	h.flows[flowID] = setReq{
		FlowID:        flowID,
		MaxActiveRuns: &limit,
		Graph:         simpleGraph(method),
	}

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleRun(ctx, childConn, reqHdr, mustJSON(runReq{
		ReqID:  "req-run-limit-first",
		FlowID: flowID,
	}))
	firstResp := mustDecodeRunResp(t, srv.sends[len(srv.sends)-1].payload)
	if firstResp.Code != 1 || firstResp.RunID == "" {
		t.Fatalf("expected first run accepted, resp=%#v", firstResp)
	}

	h.handleRun(ctx, childConn, reqHdr, mustJSON(runReq{
		ReqID:  "req-run-limit-second",
		FlowID: flowID,
	}))
	secondResp := mustDecodeRunResp(t, srv.sends[len(srv.sends)-1].payload)
	if secondResp.Code != 409 || secondResp.Msg != "active run limit reached" {
		t.Fatalf("expected second run rejected by active-run limit, resp=%#v", secondResp)
	}

	h.mu.Lock()
	gotRuns := len(h.runOrderByFlow[flowID])
	h.mu.Unlock()
	if gotRuns != 1 {
		t.Fatalf("expected only one registered run, got=%d", gotRuns)
	}

	close(release)
	waitRunTerminal(t, h, firstResp.RunID)
}

func TestGetEchoesMaxActiveRuns(t *testing.T) {
	h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	limit := 2
	flowID := "123e4567-e89b-12d3-a456-426614174129"
	h.flows[flowID] = setReq{
		FlowID:        flowID,
		Name:          "active-run-limit",
		MaxActiveRuns: &limit,
		Trigger: trigger{
			Type:    "interval",
			EveryMs: 1000,
		},
		Graph: simpleGraph("debug::echo"),
	}

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleGet(ctx, childConn, reqHdr, mustJSON(getReq{
		ReqID:  "req-get-max-active-runs",
		FlowID: flowID,
	}))

	resp := mustDecodeGetResp(t, srv.sends[len(srv.sends)-1].payload)
	if resp.Code != 1 {
		t.Fatalf("expected get ok, resp=%#v", resp)
	}
	if resp.MaxActiveRuns == nil || *resp.MaxActiveRuns != limit {
		t.Fatalf("expected get to echo max_active_runs=%d, resp=%#v", limit, resp)
	}
}

func TestFlowRunArchiveReloadsRetainedRunsFromDisk(t *testing.T) {
	h, _, _, _, baseDir := newDeleteTestEnv(t, nil)
	h.runArchive = true
	h.maxRetainedRuns = 2

	flowID := "123e4567-e89b-12d3-a456-426614174131"
	flowDef := setReq{
		FlowID: flowID,
		Name:   "archive-flow",
		Graph:  simpleGraph("debug::echo"),
	}
	h.flows[flowID] = flowDef

	runID := h.enqueueRun(context.Background(), flowDef)
	waitRunTerminal(t, h, runID)
	waitArchivedRunFile(t, baseDir, flowID, runID, true)

	reloaded, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	reloaded.baseDir = baseDir
	reloaded.runArchive = true
	reloaded.maxRetainedRuns = 2
	if err := reloaded.loadArchivedRuns(); err != nil {
		t.Fatalf("loadArchivedRuns err=%v", err)
	}

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	reloaded.handleListRuns(ctx, childConn, reqHdr, mustJSON(listRunsReq{
		ReqID:  "req-list-runs-archive-reload",
		FlowID: flowID,
	}))
	listRunsResp := mustDecodeListRunsResp(t, srv.sends[len(srv.sends)-1].payload)
	if listRunsResp.Code != 1 || len(listRunsResp.Runs) != 1 || listRunsResp.Runs[0].RunID != runID {
		t.Fatalf("unexpected archived list_runs resp: %#v", listRunsResp)
	}

	reloaded.handleStatus(ctx, childConn, reqHdr, mustJSON(statusReq{
		ReqID:  "req-status-archive-reload",
		FlowID: flowID,
	}))
	statusResp := mustDecodeStatusResp(t, srv.sends[len(srv.sends)-1].payload)
	if statusResp.Code != 1 || statusResp.RunID != runID || statusResp.Status != "succeeded" {
		t.Fatalf("unexpected archived status resp: %#v", statusResp)
	}

	reloaded.handleDetail(ctx, childConn, reqHdr, mustJSON(detailReq{
		ReqID:  "req-detail-archive-reload",
		FlowID: flowID,
		NodeID: "n1",
	}))
	detailResp := mustDecodeDetailResp(t, srv.sends[len(srv.sends)-1].payload)
	if detailResp.Code != 1 || detailResp.RunID != runID {
		t.Fatalf("unexpected archived detail resp: %#v", detailResp)
	}
	assertJSONEq(t, detailResp.Result, `{}`)
}

func TestFlowRunArchivePrunesOldArchiveFiles(t *testing.T) {
	h, _, _, _, baseDir := newDeleteTestEnv(t, nil)
	h.runArchive = true
	h.maxRetainedRuns = 2

	flowID := "123e4567-e89b-12d3-a456-426614174132"
	flowDef := setReq{
		FlowID: flowID,
		Name:   "archive-prune",
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
	waitArchivedRunFile(t, baseDir, flowID, runIDs[0], false)
	waitArchivedRunFile(t, baseDir, flowID, runIDs[1], true)
	waitArchivedRunFile(t, baseDir, flowID, runIDs[2], true)

	reloaded, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	reloaded.baseDir = baseDir
	reloaded.runArchive = true
	reloaded.maxRetainedRuns = 2
	if err := reloaded.loadArchivedRuns(); err != nil {
		t.Fatalf("loadArchivedRuns err=%v", err)
	}

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	reloaded.handleListRuns(ctx, childConn, reqHdr, mustJSON(listRunsReq{
		ReqID:  "req-list-runs-archive-prune",
		FlowID: flowID,
	}))
	resp := mustDecodeListRunsResp(t, srv.sends[len(srv.sends)-1].payload)
	if resp.Code != 1 || len(resp.Runs) != 2 || resp.Runs[0].RunID != runIDs[2] || resp.Runs[1].RunID != runIDs[1] {
		t.Fatalf("unexpected archived prune resp: %#v", resp)
	}
}

func TestFlowDeleteKeepsArchivedRunsAfterReload(t *testing.T) {
	h, srv, childConn, ctx, baseDir := newDeleteTestEnv(t, nil)
	h.runArchive = true
	h.maxRetainedRuns = 2

	flowID := "123e4567-e89b-12d3-a456-426614174133"
	flowDef := setReq{
		FlowID: flowID,
		Name:   "archive-after-delete",
		Graph:  simpleGraph("debug::echo"),
	}
	h.flows[flowID] = flowDef
	runID := h.enqueueRun(context.Background(), flowDef)
	waitRunTerminal(t, h, runID)
	waitArchivedRunFile(t, baseDir, flowID, runID, true)

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)
	h.handleDelete(ctx, childConn, reqHdr, mustJSON(deleteReq{
		ReqID:  "req-delete-keep-archive",
		FlowID: flowID,
	}))
	deleteResp := mustDecodeDeleteResp(t, srv.sends[len(srv.sends)-1].payload)
	if deleteResp.Code != 1 {
		t.Fatalf("unexpected delete resp: %#v", deleteResp)
	}

	reloaded, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	reloaded.baseDir = baseDir
	reloaded.runArchive = true
	reloaded.maxRetainedRuns = 2
	if err := reloaded.loadArchivedRuns(); err != nil {
		t.Fatalf("loadArchivedRuns err=%v", err)
	}

	reloaded.handleListRuns(ctx, childConn, reqHdr, mustJSON(listRunsReq{
		ReqID:  "req-list-runs-after-delete",
		FlowID: flowID,
	}))
	listRunsResp := mustDecodeListRunsResp(t, srv.sends[len(srv.sends)-1].payload)
	if listRunsResp.Code != 1 || len(listRunsResp.Runs) != 1 || listRunsResp.Runs[0].RunID != runID {
		t.Fatalf("unexpected archived runs after delete: %#v", listRunsResp)
	}

	reloaded.handleStatus(ctx, childConn, reqHdr, mustJSON(statusReq{
		ReqID:  "req-status-after-delete",
		FlowID: flowID,
		RunID:  runID,
	}))
	statusResp := mustDecodeStatusResp(t, srv.sends[len(srv.sends)-1].payload)
	if statusResp.Code != 1 || statusResp.RunID != runID {
		t.Fatalf("unexpected archived status after delete: %#v", statusResp)
	}
}

func TestExecuteFlowAppliesRetryBackoff(t *testing.T) {
	h, _, _, _, _ := newDeleteTestEnv(t, nil)

	var mu sync.Mutex
	attemptTimes := make([]time.Time, 0, 2)
	attempts := 0
	h.RegisterLocalMethod("test::retry-once", func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		mu.Lock()
		attempts++
		attemptTimes = append(attemptTimes, time.Now())
		currentAttempt := attempts
		mu.Unlock()
		if currentAttempt == 1 {
			return nil, errors.New("transient failure")
		}
		return json.RawMessage(`{"ok":true}`), nil
	})

	retry := 1
	retryBackoffMs := 70
	timeoutMs := 1000
	flowID := "123e4567-e89b-12d3-a456-426614174122"
	flowDef := setReq{
		FlowID: flowID,
		Graph: graph{
			Nodes: []node{
				{
					ID:             "n1",
					Kind:           "call",
					Retry:          &retry,
					RetryBackoffMs: &retryBackoffMs,
					TimeoutMs:      &timeoutMs,
					Spec:           mustJSON(map[string]any{"method": "test::retry-once"}),
				},
			},
		},
	}
	h.flows[flowID] = flowDef

	runID := h.enqueueRun(context.Background(), flowDef)
	waitRunTerminal(t, h, runID)

	mu.Lock()
	gotAttempts := attempts
	gotTimes := append([]time.Time(nil), attemptTimes...)
	mu.Unlock()
	if gotAttempts != 2 || len(gotTimes) != 2 {
		t.Fatalf("expected 2 attempts, got attempts=%d times=%d", gotAttempts, len(gotTimes))
	}
	if delta := gotTimes[1].Sub(gotTimes[0]); delta < 50*time.Millisecond {
		t.Fatalf("retry backoff too short: got=%s want>=50ms", delta)
	}

	h.mu.Lock()
	state := h.runs[runID]
	h.mu.Unlock()
	if state == nil {
		t.Fatalf("run state missing: %s", runID)
	}
	state.mu.Lock()
	status := state.status
	nodeStatus := state.runtime.Nodes["n1"].Status
	state.mu.Unlock()
	if status != "succeeded" || nodeStatus != "succeeded" {
		t.Fatalf("unexpected run/node status: run=%s node=%s", status, nodeStatus)
	}
}

func TestExecuteFlowRetryBackoffHonorsCancel(t *testing.T) {
	h, _, _, _, _ := newDeleteTestEnv(t, nil)

	firstAttempt := make(chan struct{}, 1)
	var mu sync.Mutex
	attempts := 0
	h.RegisterLocalMethod("test::always-fail", func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		mu.Lock()
		attempts++
		currentAttempt := attempts
		mu.Unlock()
		if currentAttempt == 1 {
			select {
			case firstAttempt <- struct{}{}:
			default:
			}
		}
		return nil, errors.New("transient failure")
	})

	retry := 5
	retryBackoffMs := 150
	timeoutMs := 1000
	flowID := "123e4567-e89b-12d3-a456-426614174123"
	flowDef := setReq{
		FlowID: flowID,
		Graph: graph{
			Nodes: []node{
				{
					ID:             "n1",
					Kind:           "call",
					Retry:          &retry,
					RetryBackoffMs: &retryBackoffMs,
					TimeoutMs:      &timeoutMs,
					Spec:           mustJSON(map[string]any{"method": "test::always-fail"}),
				},
			},
		},
	}
	h.flows[flowID] = flowDef

	runID := h.enqueueRun(context.Background(), flowDef)
	select {
	case <-firstAttempt:
	case <-time.After(2 * time.Second):
		t.Fatalf("first attempt did not start")
	}

	h.mu.Lock()
	state := h.runs[runID]
	h.mu.Unlock()
	if state == nil {
		t.Fatalf("run state missing: %s", runID)
	}
	if _, ok := cancelRunState(state, runCancelMsgManual); !ok {
		t.Fatalf("expected cancelRunState to succeed")
	}

	time.Sleep(250 * time.Millisecond)

	mu.Lock()
	gotAttempts := attempts
	mu.Unlock()
	if gotAttempts != 1 {
		t.Fatalf("expected no retry after cancel during backoff, got attempts=%d", gotAttempts)
	}

	state.mu.Lock()
	status := state.status
	nodeStatus := state.runtime.Nodes["n1"].Status
	nodeMsg := state.runtime.Nodes["n1"].Msg
	state.mu.Unlock()
	if status != "cancelled" {
		t.Fatalf("unexpected run status=%s", status)
	}
	if nodeStatus != "cancelled" || nodeMsg != runCancelMsgManual {
		t.Fatalf("unexpected node status after cancel during backoff: status=%s msg=%q", nodeStatus, nodeMsg)
	}
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

func mustDecodeListRunsResp(t *testing.T, payload []byte) listRunsResp {
	t.Helper()
	var env message
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode list_runs envelope err=%v", err)
	}
	if env.Action != actionListRunsResp {
		t.Fatalf("unexpected list_runs action=%s", env.Action)
	}
	var resp listRunsResp
	if err := json.Unmarshal(env.Data, &resp); err != nil {
		t.Fatalf("decode list_runs response err=%v", err)
	}
	return resp
}

func mustDecodeRunResp(t *testing.T, payload []byte) runResp {
	t.Helper()
	var env message
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode run envelope err=%v", err)
	}
	if env.Action != actionRunResp {
		t.Fatalf("unexpected run action=%s", env.Action)
	}
	var resp runResp
	if err := json.Unmarshal(env.Data, &resp); err != nil {
		t.Fatalf("decode run response err=%v", err)
	}
	return resp
}

func mustDecodeGetResp(t *testing.T, payload []byte) getResp {
	t.Helper()
	var env message
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode get envelope err=%v", err)
	}
	if env.Action != actionGetResp {
		t.Fatalf("unexpected get action=%s", env.Action)
	}
	var resp getResp
	if err := json.Unmarshal(env.Data, &resp); err != nil {
		t.Fatalf("decode get response err=%v", err)
	}
	return resp
}

func waitArchivedRunFile(t *testing.T, baseDir, flowID, runID string, want bool) {
	t.Helper()
	path, err := archivedRunFilePath(baseDir, flowID, runID)
	if err != nil {
		t.Fatalf("archivedRunFilePath err=%v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, statErr := os.Stat(path)
		exists := statErr == nil
		if exists == want {
			return
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("archive stat err=%v", statErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if want {
		t.Fatalf("expected archive file to exist: %s", path)
	}
	t.Fatalf("expected archive file to be removed: %s", path)
}
