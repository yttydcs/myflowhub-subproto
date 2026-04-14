package flow

// 本文件覆盖 SubProto 中 `flow` 模块里与 `orchestrator` 相关的行为。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
)

func TestValidateGraphRejectsBranchEdgeWithoutCase(t *testing.T) {
	g := graph{
		Nodes: []node{
			{
				ID:   "branch",
				Kind: "branch",
				Spec: json.RawMessage(`{"cases":[{"name":"yes","match":{"source":{"kind":"trigger","path":"/ok"},"op":"eq","value":true}}],"default_case":"no"}`),
			},
			{ID: "next", Kind: "call", Spec: json.RawMessage(`{"method":"debug::echo"}`)},
		},
		Edges: []edge{{From: "branch", To: "next"}},
	}
	err := validateGraph(g)
	if err == nil {
		t.Fatalf("expected err, got nil")
	}
	if !strings.Contains(err.Error(), "branch edge") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestValidateGraphRejectsLoopItemOutsideForeach(t *testing.T) {
	g := graph{
		Nodes: []node{
			{
				ID:   "A",
				Kind: "compose",
				Spec: json.RawMessage(`{"template":{"item":null},"inputs":[{"to":"/item","source":{"kind":"loop_item"},"required":true}]}`),
			},
		},
	}
	err := validateGraph(g)
	if err == nil {
		t.Fatalf("expected err, got nil")
	}
	if !strings.Contains(err.Error(), "loop_item only allowed in foreach body") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestExecuteFlow_BranchSkipsUnselectedPath(t *testing.T) {
	h := NewHandler(nil)
	srv := &testServer{nodeID: 1, cm: connmgr.New()}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)

	flow := setReq{
		FlowID: "123e4567-e89b-12d3-a456-426614174201",
		Graph: graph{
			Nodes: []node{
				{
					ID:   "route",
					Kind: "branch",
					Spec: json.RawMessage(`{"cases":[{"name":"approved","match":{"source":{"kind":"trigger","path":"/approved"},"op":"eq","value":true}}],"default_case":"rejected"}`),
				},
				{
					ID:   "approved",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"path":"approved"}}`),
				},
				{
					ID:   "rejected",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"path":"rejected"}}`),
				},
				{
					ID:   "final",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"done":true}}`),
				},
			},
			Edges: []edge{
				{From: "route", To: "approved", Case: "approved"},
				{From: "route", To: "rejected", Case: "rejected"},
				{From: "approved", To: "final"},
				{From: "rejected", To: "final"},
			},
		},
	}
	state := &runState{
		flowID: flow.FlowID,
		runID:  "123e4567-e89b-12d3-a456-426614174202",
		status: "queued",
		runtime: newRunContext(
			flow.FlowID,
			"123e4567-e89b-12d3-a456-426614174202",
			1,
			json.RawMessage(`{"approved":true}`),
		),
	}

	h.executeFlow(ctx, flow, state)

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.status != "succeeded" {
		t.Fatalf("expected succeeded, got %s", state.status)
	}
	if state.runtime.Nodes["approved"].Status != "succeeded" {
		t.Fatalf("expected approved path to run, got %#v", state.runtime.Nodes["approved"])
	}
	if state.runtime.Nodes["rejected"].Status != "skipped" {
		t.Fatalf("expected rejected path skipped, got %#v", state.runtime.Nodes["rejected"])
	}
	if state.runtime.Nodes["final"].Status != "succeeded" {
		t.Fatalf("expected final to run, got %#v", state.runtime.Nodes["final"])
	}
	assertJSONEq(t, state.runtime.Nodes["route"].Result, `{"case":"approved"}`)
}

func TestExecuteFlow_ForeachAggregatesResults(t *testing.T) {
	h := NewHandler(nil)
	srv := &testServer{nodeID: 1, cm: connmgr.New()}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)

	flow := setReq{
		FlowID: "123e4567-e89b-12d3-a456-426614174203",
		Graph: graph{
			Nodes: []node{
				{
					ID:   "loop",
					Kind: "foreach",
					Spec: json.RawMessage(`{"source":{"kind":"trigger","path":"/items"},"body":{"nodes":[{"id":"emit","kind":"call","spec":{"method":"debug::echo","args_template":{"item":null,"index":0},"inputs":[{"to":"/item","source":{"kind":"loop_item"},"required":true},{"to":"/index","source":{"kind":"loop_index"},"required":true}]}}],"edges":[]},"result_node_id":"emit"}`),
				},
				{
					ID:   "send",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"payload":null},"inputs":[{"to":"/payload","source":{"kind":"node_result","node_id":"loop"},"required":true}]}`),
				},
			},
			Edges: []edge{{From: "loop", To: "send"}},
		},
	}
	state := &runState{
		flowID: flow.FlowID,
		runID:  "123e4567-e89b-12d3-a456-426614174204",
		status: "queued",
		runtime: newRunContext(
			flow.FlowID,
			"123e4567-e89b-12d3-a456-426614174204",
			1,
			json.RawMessage(`{"items":[{"id":"a"},{"id":"b"}]}`),
		),
	}

	h.executeFlow(ctx, flow, state)

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.status != "succeeded" {
		t.Fatalf("expected succeeded, got %s", state.status)
	}
	assertJSONEq(t, state.runtime.Nodes["loop"].Result, `[{"item":{"id":"a"},"index":0},{"item":{"id":"b"},"index":1}]`)
	assertJSONEq(t, state.runtime.Nodes["send"].Result, `{"payload":[{"item":{"id":"a"},"index":0},{"item":{"id":"b"},"index":1}]}`)
}

func TestExecuteFlow_SubflowReturnsResult(t *testing.T) {
	h := NewHandler(nil)
	srv := &testServer{nodeID: 1, cm: connmgr.New()}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)

	childFlowID := "123e4567-e89b-12d3-a456-426614174205"
	h.flows[childFlowID] = setReq{
		FlowID: childFlowID,
		Graph: graph{
			Nodes: []node{
				{
					ID:   "emit",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"message":"child-ok"}}`),
				},
			},
		},
	}

	parentFlow := setReq{
		FlowID: "123e4567-e89b-12d3-a456-426614174206",
		Graph: graph{
			Nodes: []node{
				{
					ID:   "invoke",
					Kind: "subflow",
					Spec: json.RawMessage(`{"flow_id":"123e4567-e89b-12d3-a456-426614174205","result_node_id":"emit"}`),
				},
			},
		},
	}
	state := &runState{
		flowID: parentFlow.FlowID,
		runID:  "123e4567-e89b-12d3-a456-426614174207",
		status: "queued",
		runtime: newRunContext(
			parentFlow.FlowID,
			"123e4567-e89b-12d3-a456-426614174207",
			1,
			nil,
		),
	}

	h.executeFlow(ctx, parentFlow, state)

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.status != "succeeded" {
		t.Fatalf("expected succeeded, got %s", state.status)
	}
	result := state.runtime.Nodes["invoke"].Result
	var payload struct {
		FlowID string          `json:"flow_id"`
		RunID  string          `json:"run_id"`
		Status string          `json:"status"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatalf("unmarshal result err=%v", err)
	}
	if payload.FlowID != childFlowID || payload.RunID == "" || payload.Status != "succeeded" {
		t.Fatalf("unexpected subflow payload: %+v", payload)
	}
	assertJSONEq(t, payload.Result, `{"message":"child-ok"}`)
}
