package flow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yttydcs/myflowhub-core/header"
)

func TestValidateFlowID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "valid uuid",
			input:   "123e4567-e89b-12d3-a456-426614174000",
			want:    "123e4567-e89b-12d3-a456-426614174000",
			wantErr: false,
		},
		{
			name:    "trim spaces",
			input:   " 123e4567-e89b-12d3-a456-426614174000 ",
			want:    "123e4567-e89b-12d3-a456-426614174000",
			wantErr: false,
		},
		{
			name:    "reject traversal",
			input:   ".." + string(filepath.Separator) + "escape",
			wantErr: true,
		},
		{
			name:    "reject non uuid",
			input:   "not-a-uuid",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateFlowID(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("validateFlowID err=%v", err)
			}
			if got != tc.want {
				t.Fatalf("validateFlowID got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestFlowHandlersRejectInvalidFlowID(t *testing.T) {
	invalidFlowID := ".." + string(filepath.Separator) + "escape"

	t.Run("set", func(t *testing.T) {
		h, srv, childConn, ctx, baseDir := newDeleteTestEnv(t, nil)
		req := setReq{
			ReqID:  "req-set-invalid-id",
			FlowID: invalidFlowID,
			Trigger: trigger{
				Type:    "interval",
				EveryMs: 1000,
			},
			Graph: graph{
				Nodes: []node{
					{ID: "n1", Kind: "call", Spec: json.RawMessage(`{"method":"debug::echo"}`)},
				},
			},
		}
		reqHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(SubProtoFlow).
			WithSourceID(2).
			WithTargetID(1)

		h.handleSet(ctx, childConn, reqHdr, mustJSON(req))

		assertRespCode(t, srv, actionSetResp, 400)
		assertDirEmpty(t, baseDir)
	})

	t.Run("delete", func(t *testing.T) {
		h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
		req := deleteReq{ReqID: "req-del-invalid-id", FlowID: invalidFlowID}
		reqHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(SubProtoFlow).
			WithSourceID(2).
			WithTargetID(1)

		h.handleDelete(ctx, childConn, reqHdr, mustJSON(req))

		assertRespCode(t, srv, actionDeleteResp, 400)
	})

	t.Run("run", func(t *testing.T) {
		h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
		req := runReq{ReqID: "req-run-invalid-id", FlowID: invalidFlowID}
		reqHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(SubProtoFlow).
			WithSourceID(2).
			WithTargetID(1)

		h.handleRun(ctx, childConn, reqHdr, mustJSON(req))

		assertRespCode(t, srv, actionRunResp, 400)
	})

	t.Run("cancel_run", func(t *testing.T) {
		h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
		req := cancelRunReq{
			ReqID:  "req-cancel-invalid-flow-id",
			FlowID: invalidFlowID,
			RunID:  "123e4567-e89b-12d3-a456-426614174118",
		}
		reqHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(SubProtoFlow).
			WithSourceID(2).
			WithTargetID(1)

		h.handleCancelRun(ctx, childConn, reqHdr, mustJSON(req))

		assertRespCode(t, srv, actionCancelRunResp, 400)
	})

	t.Run("status", func(t *testing.T) {
		h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
		req := statusReq{ReqID: "req-status-invalid-id", FlowID: invalidFlowID}
		reqHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(SubProtoFlow).
			WithSourceID(2).
			WithTargetID(1)

		h.handleStatus(ctx, childConn, reqHdr, mustJSON(req))

		assertRespCode(t, srv, actionStatusResp, 400)
	})

	t.Run("get", func(t *testing.T) {
		h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
		req := getReq{ReqID: "req-get-invalid-id", FlowID: invalidFlowID}
		reqHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(SubProtoFlow).
			WithSourceID(2).
			WithTargetID(1)

		h.handleGet(ctx, childConn, reqHdr, mustJSON(req))

		assertRespCode(t, srv, actionGetResp, 400)
	})

	t.Run("detail", func(t *testing.T) {
		h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
		req := detailReq{ReqID: "req-detail-invalid-id", FlowID: invalidFlowID, NodeID: "n1"}
		reqHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(SubProtoFlow).
			WithSourceID(2).
			WithTargetID(1)

		h.handleDetail(ctx, childConn, reqHdr, mustJSON(req))

		assertRespCode(t, srv, actionDetailResp, 400)
	})

	t.Run("list_runs", func(t *testing.T) {
		h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
		req := listRunsReq{ReqID: "req-list-runs-invalid-id", FlowID: invalidFlowID}
		reqHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(SubProtoFlow).
			WithSourceID(2).
			WithTargetID(1)

		h.handleListRuns(ctx, childConn, reqHdr, mustJSON(req))

		assertRespCode(t, srv, actionListRunsResp, 400)
	})
}

func TestFlowCancelRunRejectsInvalidRunID(t *testing.T) {
	h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	req := cancelRunReq{
		ReqID:  "req-cancel-invalid-run-id",
		FlowID: "123e4567-e89b-12d3-a456-426614174008",
		RunID:  "not-a-uuid",
	}
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleCancelRun(ctx, childConn, reqHdr, mustJSON(req))

	assertRespCode(t, srv, actionCancelRunResp, 400)
}

func TestFlowSetRejectsNegativeMaxActiveRuns(t *testing.T) {
	h, srv, childConn, ctx, baseDir := newDeleteTestEnv(t, nil)
	negativeOne := -1
	req := setReq{
		ReqID:         "req-set-negative-max-active-runs",
		FlowID:        "123e4567-e89b-12d3-a456-426614174124",
		MaxActiveRuns: &negativeOne,
		Trigger: trigger{
			Type:    "interval",
			EveryMs: 1000,
		},
		Graph: graph{
			Nodes: []node{
				{ID: "n1", Kind: "call", Spec: json.RawMessage(`{"method":"debug::echo"}`)},
			},
		},
	}
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleSet(ctx, childConn, reqHdr, mustJSON(req))

	assertRespCode(t, srv, actionSetResp, 400)
	assertDirEmpty(t, baseDir)
}

func TestFlowSetRejectsNegativeTriggerDedupWindowMs(t *testing.T) {
	h, srv, childConn, ctx, baseDir := newDeleteTestEnv(t, nil)
	negativeOne := -1
	req := setReq{
		ReqID:  "req-set-negative-trigger-dedup-window",
		FlowID: "123e4567-e89b-12d3-a456-426614174130",
		Trigger: trigger{
			Type:          "event",
			EventName:     "alarm",
			DedupWindowMs: &negativeOne,
		},
		Graph: graph{
			Nodes: []node{
				{ID: "n1", Kind: "call", Spec: json.RawMessage(`{"method":"debug::echo"}`)},
			},
		},
	}
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleSet(ctx, childConn, reqHdr, mustJSON(req))

	assertRespCode(t, srv, actionSetResp, 400)
	assertDirEmpty(t, baseDir)
}

func TestLoadFlowsFromDiskSkipsInvalidFlowID(t *testing.T) {
	h := NewHandler(nil)
	h.baseDir = t.TempDir()

	raw, err := json.Marshal(setReq{
		FlowID: ".." + string(filepath.Separator) + "escape",
		Trigger: trigger{
			Type:    "interval",
			EveryMs: 1000,
		},
		Graph: graph{
			Nodes: []node{
				{ID: "n1", Kind: "call", Spec: json.RawMessage(`{"method":"debug::echo"}`)},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal invalid flow err=%v", err)
	}
	if err := os.WriteFile(filepath.Join(h.baseDir, "bad.json"), raw, 0o644); err != nil {
		t.Fatalf("write invalid flow file err=%v", err)
	}

	h.loadFlowsFromDisk()

	if len(h.flows) != 0 {
		t.Fatalf("expected invalid flow_id to be skipped, got=%d", len(h.flows))
	}
}

func TestLoadFlowsFromDiskKeepsLegacyKindsForCompatibility(t *testing.T) {
	h := NewHandler(nil)
	h.baseDir = t.TempDir()
	flowID := "123e4567-e89b-12d3-a456-426614174000"

	raw, err := json.Marshal(setReq{
		FlowID: flowID,
		Trigger: trigger{
			Type:    "interval",
			EveryMs: 1000,
		},
		Graph: graph{
			Nodes: []node{
				{ID: "n1", Kind: "local", Spec: json.RawMessage(`{"method":"debug::echo"}`)},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal legacy flow err=%v", err)
	}
	if err := os.WriteFile(filepath.Join(h.baseDir, flowID+".json"), raw, 0o644); err != nil {
		t.Fatalf("write legacy flow file err=%v", err)
	}

	h.loadFlowsFromDisk()

	h.mu.Lock()
	loaded, ok := h.flows[flowID]
	h.mu.Unlock()
	if !ok {
		t.Fatalf("expected legacy flow to be loaded for runtime compatibility")
	}
	if !strings.EqualFold(loaded.Graph.Nodes[0].Kind, "local") {
		t.Fatalf("expected legacy node kind preserved, got=%q", loaded.Graph.Nodes[0].Kind)
	}
}

func TestLoadFlowsFromDiskSkipsNegativeMaxActiveRuns(t *testing.T) {
	h := NewHandler(nil)
	h.baseDir = t.TempDir()
	flowID := "123e4567-e89b-12d3-a456-426614174125"
	negativeOne := -1

	raw, err := json.Marshal(setReq{
		FlowID:        flowID,
		MaxActiveRuns: &negativeOne,
		Trigger: trigger{
			Type:    "interval",
			EveryMs: 1000,
		},
		Graph: graph{
			Nodes: []node{
				{ID: "n1", Kind: "call", Spec: json.RawMessage(`{"method":"debug::echo"}`)},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal invalid flow err=%v", err)
	}
	if err := os.WriteFile(filepath.Join(h.baseDir, flowID+".json"), raw, 0o644); err != nil {
		t.Fatalf("write invalid flow file err=%v", err)
	}

	h.loadFlowsFromDisk()

	if len(h.flows) != 0 {
		t.Fatalf("expected flow with negative max_active_runs to be skipped, got=%d", len(h.flows))
	}
}

func TestLoadFlowsFromDiskSkipsNegativeTriggerDedupWindowMs(t *testing.T) {
	h := NewHandler(nil)
	h.baseDir = t.TempDir()
	flowID := "123e4567-e89b-12d3-a456-426614174131"
	negativeOne := -1

	raw, err := json.Marshal(setReq{
		FlowID: flowID,
		Trigger: trigger{
			Type:          "event",
			EventName:     "alarm",
			DedupWindowMs: &negativeOne,
		},
		Graph: graph{
			Nodes: []node{
				{ID: "n1", Kind: "call", Spec: json.RawMessage(`{"method":"debug::echo"}`)},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal invalid flow err=%v", err)
	}
	if err := os.WriteFile(filepath.Join(h.baseDir, flowID+".json"), raw, 0o644); err != nil {
		t.Fatalf("write invalid flow file err=%v", err)
	}

	h.loadFlowsFromDisk()

	if len(h.flows) != 0 {
		t.Fatalf("expected flow with negative trigger dedup window to be skipped, got=%d", len(h.flows))
	}
}

func assertRespCode(t *testing.T, srv *testServer, wantAction string, wantCode int) {
	t.Helper()
	if len(srv.sends) != 1 {
		t.Fatalf("expected 1 response frame, got=%d", len(srv.sends))
	}
	var env message
	if err := json.Unmarshal(srv.sends[0].payload, &env); err != nil {
		t.Fatalf("decode envelope err=%v", err)
	}
	if env.Action != wantAction {
		t.Fatalf("unexpected action=%s want=%s", env.Action, wantAction)
	}
	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(env.Data, &resp); err != nil {
		t.Fatalf("decode response err=%v", err)
	}
	if resp.Code != wantCode {
		t.Fatalf("unexpected code=%d want=%d", resp.Code, wantCode)
	}
}

func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir err=%v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected directory %s to remain empty, got %d entries", dir, len(entries))
	}
}
