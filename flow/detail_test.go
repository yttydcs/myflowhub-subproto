package flow

import (
	"encoding/json"
	"testing"

	"github.com/yttydcs/myflowhub-core/header"
)

func TestFlowDetailReturnsLatestRunNodeResult(t *testing.T) {
	h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	flowID := "123e4567-e89b-12d3-a456-426614174108"
	runID := "123e4567-e89b-12d3-a456-426614174109"
	state := &runState{
		flowID:  flowID,
		runID:   runID,
		status:  "succeeded",
		runtime: newRunContext(flowID, runID, 1, nil),
	}
	state.runtime.Nodes["compose"] = nodeRuntimeData{
		Status: "succeeded",
		Code:   1,
		Result: json.RawMessage(`{"payload":{"user":{"id":"u-1"},"roles":["admin","ops"]}}`),
	}
	h.runs[runID] = state
	h.runOrderByFlow[flowID] = []string{runID}

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleDetail(ctx, childConn, reqHdr, mustJSON(detailReq{
		ReqID:  "req-detail-root",
		FlowID: flowID,
		NodeID: "compose",
	}))

	resp := mustDecodeDetailResp(t, srv.sends[len(srv.sends)-1].payload)
	if resp.Code != 1 || resp.FlowID != flowID || resp.RunID != runID {
		t.Fatalf("unexpected detail resp: %#v", resp)
	}
	if resp.Node == nil || resp.Node.ID != "compose" || resp.Node.Status != "succeeded" {
		t.Fatalf("unexpected detail node: %#v", resp.Node)
	}
	assertJSONEq(t, resp.Result, `{"payload":{"user":{"id":"u-1"},"roles":["admin","ops"]}}`)
}

func TestFlowDetailReturnsResultPath(t *testing.T) {
	h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	flowID := "123e4567-e89b-12d3-a456-426614174110"
	runID := "123e4567-e89b-12d3-a456-426614174111"
	state := &runState{
		flowID:  flowID,
		runID:   runID,
		status:  "succeeded",
		runtime: newRunContext(flowID, runID, 1, nil),
	}
	state.runtime.Nodes["compose"] = nodeRuntimeData{
		Status: "succeeded",
		Code:   1,
		Result: json.RawMessage(`{"payload":{"items":[{"id":"item-1","meta":{"label":"alpha"}},{"id":"item-2","meta":{"label":"beta"}}]}}`),
	}
	h.runs[runID] = state
	h.runOrderByFlow[flowID] = []string{runID}

	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleDetail(ctx, childConn, reqHdr, mustJSON(detailReq{
		ReqID:  "req-detail-path",
		FlowID: flowID,
		RunID:  runID,
		NodeID: "compose",
		Path:   "/payload/items/1/meta/label",
	}))

	resp := mustDecodeDetailResp(t, srv.sends[len(srv.sends)-1].payload)
	if resp.Code != 1 || resp.Path != "/payload/items/1/meta/label" {
		t.Fatalf("unexpected detail path resp: %#v", resp)
	}
	assertJSONEq(t, resp.Result, `"beta"`)
}

func TestFlowDetailNotFound(t *testing.T) {
	t.Run("node missing", func(t *testing.T) {
		h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
		flowID := "123e4567-e89b-12d3-a456-426614174112"
		runID := "123e4567-e89b-12d3-a456-426614174113"
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

		h.handleDetail(ctx, childConn, reqHdr, mustJSON(detailReq{
			ReqID:  "req-detail-node-missing",
			FlowID: flowID,
			RunID:  runID,
			NodeID: "missing",
		}))

		assertRespCode(t, srv, actionDetailResp, 404)
	})

	t.Run("path missing", func(t *testing.T) {
		h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
		flowID := "123e4567-e89b-12d3-a456-426614174114"
		runID := "123e4567-e89b-12d3-a456-426614174115"
		state := &runState{
			flowID:  flowID,
			runID:   runID,
			status:  "succeeded",
			runtime: newRunContext(flowID, runID, 1, nil),
		}
		state.runtime.Nodes["compose"] = nodeRuntimeData{
			Status: "succeeded",
			Code:   1,
			Result: json.RawMessage(`{"payload":{"user":{"id":"u-1"}}}`),
		}
		h.runs[runID] = state
		h.runOrderByFlow[flowID] = []string{runID}

		reqHdr := (&header.HeaderTcp{}).
			WithMajor(header.MajorCmd).
			WithSubProto(SubProtoFlow).
			WithSourceID(2).
			WithTargetID(1)

		h.handleDetail(ctx, childConn, reqHdr, mustJSON(detailReq{
			ReqID:  "req-detail-path-missing",
			FlowID: flowID,
			RunID:  runID,
			NodeID: "compose",
			Path:   "/payload/user/name",
		}))

		assertRespCode(t, srv, actionDetailResp, 404)
	})
}

func TestFlowDetailRejectsInvalidPath(t *testing.T) {
	h, srv, childConn, ctx, _ := newDeleteTestEnv(t, nil)
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(SubProtoFlow).
		WithSourceID(2).
		WithTargetID(1)

	h.handleDetail(ctx, childConn, reqHdr, mustJSON(detailReq{
		ReqID:  "req-detail-invalid-path",
		FlowID: "123e4567-e89b-12d3-a456-426614174116",
		NodeID: "compose",
		Path:   "payload/user/id",
	}))

	assertRespCode(t, srv, actionDetailResp, 400)
}

func mustDecodeDetailResp(t *testing.T, payload []byte) detailResp {
	t.Helper()
	var env message
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode detail envelope err=%v", err)
	}
	if env.Action != actionDetailResp {
		t.Fatalf("unexpected detail action=%s", env.Action)
	}
	var resp detailResp
	if err := json.Unmarshal(env.Data, &resp); err != nil {
		t.Fatalf("decode detail response err=%v", err)
	}
	return resp
}
