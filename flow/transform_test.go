package flow

// 本文件覆盖 SubProto 中 `flow` 模块里与 `transform` 相关的行为。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
)

func TestValidateGraphAllowsTransformNode(t *testing.T) {
	g := graph{
		Nodes: []node{
			{ID: "fetch", Kind: "call", Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"value":41}}`)},
			{ID: "calc", Kind: "transform", Spec: json.RawMessage(`{"expr":{"op":"add","args":[{"source":{"kind":"node_result","node_id":"fetch","path":"/value"}},{"literal":1}]}}`)},
		},
		Edges: []edge{{From: "fetch", To: "calc"}},
	}
	if err := validateGraph(g); err != nil {
		t.Fatalf("expected ok, got err=%v", err)
	}
}

func TestValidateGraphRejectsTransformUnknownOp(t *testing.T) {
	g := graph{
		Nodes: []node{
			{ID: "calc", Kind: "transform", Spec: json.RawMessage(`{"expr":{"op":"pow","args":[{"literal":2},{"literal":3}]}}`)},
		},
	}
	err := validateGraph(g)
	if err == nil {
		t.Fatalf("expected err, got nil")
	}
	if !strings.Contains(err.Error(), "op unsupported") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestValidateGraphRejectsTransformInvalidArity(t *testing.T) {
	g := graph{
		Nodes: []node{
			{ID: "calc", Kind: "transform", Spec: json.RawMessage(`{"expr":{"op":"if","args":[{"literal":true},{"literal":1}]}}`)},
		},
	}
	err := validateGraph(g)
	if err == nil {
		t.Fatalf("expected err, got nil")
	}
	if !strings.Contains(err.Error(), "if requires exactly 3 args") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestValidateGraphRejectsTransformLoopSourceOutsideForeach(t *testing.T) {
	g := graph{
		Nodes: []node{
			{ID: "calc", Kind: "transform", Spec: json.RawMessage(`{"expr":{"source":{"kind":"loop_item"}}}`)},
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

func TestExecuteFlow_TransformAddsNodeResultNumber(t *testing.T) {
	h := NewHandler(nil)
	srv := &testServer{nodeID: 1, cm: connmgr.New()}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)

	flow := setReq{
		FlowID: "123e4567-e89b-12d3-a456-426614174301",
		Graph: graph{
			Nodes: []node{
				{
					ID:   "fetch",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"value":41}}`),
				},
				{
					ID:   "calc",
					Kind: "transform",
					Spec: json.RawMessage(`{"expr":{"op":"add","args":[{"source":{"kind":"node_result","node_id":"fetch","path":"/value"}},{"literal":1}]}}`),
				},
				{
					ID:   "send",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"payload":null},"inputs":[{"to":"/payload","source":{"kind":"node_result","node_id":"calc"},"required":true}]}`),
				},
			},
			Edges: []edge{
				{From: "fetch", To: "calc"},
				{From: "calc", To: "send"},
			},
		},
	}
	state := &runState{
		flowID: flow.FlowID,
		runID:  "123e4567-e89b-12d3-a456-426614174302",
		status: "queued",
		runtime: newRunContext(
			flow.FlowID,
			"123e4567-e89b-12d3-a456-426614174302",
			1,
			nil,
		),
	}

	h.executeFlow(ctx, flow, state)

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.status != "succeeded" {
		t.Fatalf("expected succeeded, got %s", state.status)
	}
	assertJSONEq(t, state.runtime.Nodes["calc"].Result, `42`)
	assertJSONEq(t, state.runtime.Nodes["send"].Result, `{"payload":42}`)
}

func TestExecuteFlow_TransformForeachBuildsNestedObjectArray(t *testing.T) {
	h := NewHandler(nil)
	srv := &testServer{nodeID: 1, cm: connmgr.New()}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)

	flow := setReq{
		FlowID: "123e4567-e89b-12d3-a456-426614174303",
		Graph: graph{
			Nodes: []node{
				{
					ID:   "loop",
					Kind: "foreach",
					Spec: json.RawMessage(`{"source":{"kind":"trigger","path":"/items"},"body":{"nodes":[{"id":"map","kind":"transform","spec":{"expr":{"object":{"item":{"source":{"kind":"loop_item"}},"index":{"source":{"kind":"loop_index"}},"summary":{"array":[{"op":"concat","args":[{"literal":"item-"},{"source":{"kind":"loop_index"}}]},{"op":"len","args":[{"source":{"kind":"loop_item","path":"/tags"}}]}]}}}}}],"edges":[]},"result_node_id":"map"}`),
				},
			},
		},
	}
	state := &runState{
		flowID: flow.FlowID,
		runID:  "123e4567-e89b-12d3-a456-426614174304",
		status: "queued",
		runtime: newRunContext(
			flow.FlowID,
			"123e4567-e89b-12d3-a456-426614174304",
			1,
			json.RawMessage(`{"items":[{"id":"a","tags":["x","y"]},{"id":"b","tags":[]}]}`),
		),
	}

	h.executeFlow(ctx, flow, state)

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.status != "succeeded" {
		t.Fatalf("expected succeeded, got %s", state.status)
	}
	assertJSONEq(t, state.runtime.Nodes["loop"].Result, `[{"index":0,"item":{"id":"a","tags":["x","y"]},"summary":["item-0",2]},{"index":1,"item":{"id":"b","tags":[]},"summary":["item-1",0]}]`)
}

func TestExecuteFlow_TransformCoalesceOptionalSource(t *testing.T) {
	h := NewHandler(nil)
	srv := &testServer{nodeID: 1, cm: connmgr.New()}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)

	flow := setReq{
		FlowID: "123e4567-e89b-12d3-a456-426614174305",
		Graph: graph{
			Nodes: []node{
				{
					ID:   "calc",
					Kind: "transform",
					Spec: json.RawMessage(`{"expr":{"op":"coalesce","args":[{"source":{"kind":"trigger","path":"/missing"},"required":false},{"literal":"fallback"}]}}`),
				},
			},
		},
	}
	state := &runState{
		flowID: flow.FlowID,
		runID:  "123e4567-e89b-12d3-a456-426614174306",
		status: "queued",
		runtime: newRunContext(
			flow.FlowID,
			"123e4567-e89b-12d3-a456-426614174306",
			1,
			json.RawMessage(`{}`),
		),
	}

	h.executeFlow(ctx, flow, state)

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.status != "succeeded" {
		t.Fatalf("expected succeeded, got %s", state.status)
	}
	assertJSONEq(t, state.runtime.Nodes["calc"].Result, `"fallback"`)
}

func TestExecuteFlow_TransformFailsOnRuntimeErrors(t *testing.T) {
	cases := []struct {
		name    string
		spec    string
		wantMsg string
	}{
		{
			name:    "divide_by_zero",
			spec:    `{"expr":{"op":"div","args":[{"literal":4},{"literal":0}]}}`,
			wantMsg: "divide by zero",
		},
		{
			name:    "type_mismatch",
			spec:    `{"expr":{"op":"add","args":[{"literal":"x"},{"literal":1}]}}`,
			wantMsg: "requires number",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(nil)
			srv := &testServer{nodeID: 1, cm: connmgr.New()}
			h.srv = srv
			ctx := core.WithServerContext(context.Background(), srv)

			flow := setReq{
				FlowID: "123e4567-e89b-12d3-a456-426614174307",
				Graph: graph{
					Nodes: []node{
						{ID: "calc", Kind: "transform", Spec: json.RawMessage(tc.spec)},
					},
				},
			}
			state := &runState{
				flowID: flow.FlowID,
				runID:  "123e4567-e89b-12d3-a456-426614174308",
				status: "queued",
				runtime: newRunContext(
					flow.FlowID,
					"123e4567-e89b-12d3-a456-426614174308",
					1,
					nil,
				),
			}

			h.executeFlow(ctx, flow, state)

			state.mu.Lock()
			defer state.mu.Unlock()
			if state.status != "failed" {
				t.Fatalf("expected failed, got %s", state.status)
			}
			nodeState := state.runtime.Nodes["calc"]
			if nodeState.Code != 400 {
				t.Fatalf("expected code 400, got %d", nodeState.Code)
			}
			if !strings.Contains(nodeState.Msg, tc.wantMsg) {
				t.Fatalf("unexpected msg=%q", nodeState.Msg)
			}
		})
	}
}
