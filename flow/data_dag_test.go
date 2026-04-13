package flow

// Context: This file belongs to the SubProto implementation layer around data_dag_test.

import (
	"context"
	"encoding/json"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
)

func TestExecuteFlow_BindsAncestorResultsAndCompose(t *testing.T) {
	h := NewHandler(nil)
	srv := &testServer{nodeID: 1, cm: connmgr.New()}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)

	flow := setReq{
		FlowID: "123e4567-e89b-12d3-a456-426614174100",
		Graph: graph{
			Nodes: []node{
				{
					ID:   "fetch",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"user":{"id":"u-1","role":"admin"}}}`),
				},
				{
					ID:   "compose",
					Kind: "compose",
					Spec: json.RawMessage(`{"template":{"payload":{},"meta":{}},"inputs":[{"to":"/payload/user","source":{"kind":"node_result","node_id":"fetch","path":"/user"},"required":true},{"to":"/meta/type","source":{"kind":"trigger","path":"/type"},"required":true},{"to":"/meta/flow_id","source":{"kind":"flow_meta","field":"flow_id"},"required":true},{"to":"/meta/run_id","source":{"kind":"run_meta","field":"run_id"},"required":true}]}`),
				},
				{
					ID:   "send",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"body":{}},"inputs":[{"to":"/body","source":{"kind":"node_result","node_id":"compose"},"required":true}]}`),
				},
			},
			Edges: []edge{
				{From: "fetch", To: "compose"},
				{From: "compose", To: "send"},
			},
		},
	}
	state := &runState{
		flowID: flow.FlowID,
		runID:  "123e4567-e89b-12d3-a456-426614174101",
		status: "queued",
		runtime: newRunContext(
			flow.FlowID,
			"123e4567-e89b-12d3-a456-426614174101",
			1,
			json.RawMessage(`{"type":"event","name":"alarm"}`),
		),
	}

	h.executeFlow(ctx, flow, state)

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.status != "succeeded" {
		t.Fatalf("expected succeeded, got %s", state.status)
	}

	assertJSONEq(t, state.runtime.Nodes["fetch"].Result, `{"user":{"id":"u-1","role":"admin"}}`)
	assertJSONEq(t, state.runtime.Nodes["compose"].Result, `{"payload":{"user":{"id":"u-1","role":"admin"}},"meta":{"type":"event","flow_id":"123e4567-e89b-12d3-a456-426614174100","run_id":"123e4567-e89b-12d3-a456-426614174101"}}`)
	assertJSONEq(t, state.runtime.Nodes["send"].Result, `{"body":{"payload":{"user":{"id":"u-1","role":"admin"}},"meta":{"type":"event","flow_id":"123e4567-e89b-12d3-a456-426614174100","run_id":"123e4567-e89b-12d3-a456-426614174101"}}}`)
}

func TestExecuteFlow_FailsOnMissingRequiredBinding(t *testing.T) {
	h := NewHandler(nil)
	srv := &testServer{nodeID: 1, cm: connmgr.New()}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)

	flow := setReq{
		FlowID: "123e4567-e89b-12d3-a456-426614174102",
		Graph: graph{
			Nodes: []node{
				{
					ID:   "fetch",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"user":{}}}`),
				},
				{
					ID:   "send",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"payload":{}},"inputs":[{"to":"/payload/id","source":{"kind":"node_result","node_id":"fetch","path":"/user/id"},"required":true}]}`),
				},
			},
			Edges: []edge{{From: "fetch", To: "send"}},
		},
	}
	state := &runState{
		flowID: flow.FlowID,
		runID:  "123e4567-e89b-12d3-a456-426614174103",
		status: "queued",
		runtime: newRunContext(
			flow.FlowID,
			"123e4567-e89b-12d3-a456-426614174103",
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
	sendState := state.runtime.Nodes["send"]
	if sendState.Code != 400 {
		t.Fatalf("expected code 400, got %d", sendState.Code)
	}
	if sendState.Msg == "" {
		t.Fatalf("expected binding error message")
	}
}

func TestExecuteFlow_SetsAndReadsLocalVars(t *testing.T) {
	h := NewHandler(nil)
	srv := &testServer{nodeID: 1, cm: connmgr.New()}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)

	flow := setReq{
		FlowID: "123e4567-e89b-12d3-a456-426614174104",
		Graph: graph{
			Nodes: []node{
				{
					ID:   "seed",
					Kind: "set_var",
					Spec: json.RawMessage(`{"name":"session_payload","template":{"session":{"id":"s-1","version":1}}}`),
				},
				{
					ID:   "refresh",
					Kind: "set_var",
					Spec: json.RawMessage(`{"name":"session_payload","template":{"session":{"id":"s-2","version":2}}}`),
				},
				{
					ID:   "send",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"payload":null},"inputs":[{"to":"/payload","source":{"kind":"flow_var","name":"session_payload"},"required":true}]}`),
				},
			},
			Edges: []edge{
				{From: "seed", To: "refresh"},
				{From: "refresh", To: "send"},
			},
		},
	}
	state := &runState{
		flowID: flow.FlowID,
		runID:  "123e4567-e89b-12d3-a456-426614174105",
		status: "queued",
		runtime: newRunContext(
			flow.FlowID,
			"123e4567-e89b-12d3-a456-426614174105",
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
	assertJSONEq(t, state.runtime.Nodes["seed"].Result, `{"session":{"id":"s-1","version":1}}`)
	assertJSONEq(t, state.runtime.Nodes["refresh"].Result, `{"session":{"id":"s-2","version":2}}`)
	assertJSONEq(t, state.runtime.Nodes["send"].Result, `{"payload":{"session":{"id":"s-2","version":2}}}`)

	gotVar, ok := state.runtime.Vars["session_payload"]
	if !ok {
		t.Fatalf("expected session_payload var")
	}
	if gotVar.WriterNodeID != "refresh" {
		t.Fatalf("expected writer refresh, got %s", gotVar.WriterNodeID)
	}
	assertJSONEq(t, gotVar.Value, `{"session":{"id":"s-2","version":2}}`)
}

func TestExecuteFlow_FailsWhenRequiredFlowVarMissingAtRuntime(t *testing.T) {
	h := NewHandler(nil)
	srv := &testServer{nodeID: 1, cm: connmgr.New()}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)

	flow := setReq{
		FlowID: "123e4567-e89b-12d3-a456-426614174106",
		Graph: graph{
			Nodes: []node{
				{
					ID:        "seed",
					Kind:      "set_var",
					AllowFail: true,
					Spec:      json.RawMessage(`{"name":"session_payload","template":{},"inputs":[{"to":"/id","source":{"kind":"trigger","path":"/missing"},"required":true}]}`),
				},
				{
					ID:   "send",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args_template":{"payload":null},"inputs":[{"to":"/payload","source":{"kind":"flow_var","name":"session_payload"},"required":true}]}`),
				},
			},
			Edges: []edge{{From: "seed", To: "send"}},
		},
	}
	state := &runState{
		flowID: flow.FlowID,
		runID:  "123e4567-e89b-12d3-a456-426614174107",
		status: "queued",
		runtime: newRunContext(
			flow.FlowID,
			"123e4567-e89b-12d3-a456-426614174107",
			1,
			json.RawMessage(`{}`),
		),
	}

	h.executeFlow(ctx, flow, state)

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.status != "failed" {
		t.Fatalf("expected failed, got %s", state.status)
	}
	seedState := state.runtime.Nodes["seed"]
	if seedState.Status != "failed" || seedState.Code != 400 {
		t.Fatalf("expected seed failure, got %#v", seedState)
	}
	sendState := state.runtime.Nodes["send"]
	if sendState.Status != "failed" || sendState.Code != 400 {
		t.Fatalf("expected send failure, got %#v", sendState)
	}
	if sendState.Msg == "" {
		t.Fatalf("expected missing flow_var message")
	}
	if _, ok := state.runtime.Vars["session_payload"]; ok {
		t.Fatalf("unexpected session_payload var after failed writer")
	}
}

func assertJSONEq(t *testing.T, raw json.RawMessage, want string) {
	t.Helper()
	var gotDoc any
	if err := json.Unmarshal(raw, &gotDoc); err != nil {
		t.Fatalf("invalid got json: %v", err)
	}
	var wantDoc any
	if err := json.Unmarshal([]byte(want), &wantDoc); err != nil {
		t.Fatalf("invalid want json: %v", err)
	}
	gotNorm, _ := json.Marshal(gotDoc)
	wantNorm, _ := json.Marshal(wantDoc)
	if string(gotNorm) != string(wantNorm) {
		t.Fatalf("json mismatch want=%s got=%s", wantNorm, gotNorm)
	}
}
