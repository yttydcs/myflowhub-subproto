package flow

import (
	"strings"
	"testing"
)

func TestValidateGraphOK(t *testing.T) {
	g := graph{
		Nodes: []node{
			{ID: "A", Kind: "call", Spec: []byte(`{"method":"debug::echo","args_template":{"payload":{"x":1}}}`)},
			{ID: "B", Kind: "compose", Spec: []byte(`{"template":{"joined":{}},"inputs":[{"to":"/joined/value","source":{"kind":"node_result","node_id":"A","path":"/payload/x"},"required":true}]}`)},
		},
		Edges: []edge{{From: "A", To: "B"}},
	}
	if err := validateGraph(g); err != nil {
		t.Fatalf("expected ok, got err=%v", err)
	}
	order, err := topoOrder(g)
	if err != nil {
		t.Fatalf("topoOrder err=%v", err)
	}
	if len(order) != 2 || order[0].ID != "A" || order[1].ID != "B" {
		t.Fatalf("unexpected order: %#v", order)
	}
}

func TestValidateGraphCycle(t *testing.T) {
	g := graph{
		Nodes: []node{
			{ID: "A", Kind: "call", Spec: []byte(`{"method":"debug::echo"}`)},
			{ID: "B", Kind: "call", Spec: []byte(`{"method":"debug::echo"}`)},
		},
		Edges: []edge{{From: "A", To: "B"}, {From: "B", To: "A"}},
	}
	if err := validateGraph(g); err == nil {
		t.Fatalf("expected err, got nil")
	}
}

func TestValidateGraphRejectsLegacyKind(t *testing.T) {
	g := graph{
		Nodes: []node{
			{ID: "A", Kind: "local", Spec: []byte(`{"method":"debug::echo"}`)},
		},
	}
	err := validateGraph(g)
	if err == nil {
		t.Fatalf("expected err, got nil")
	}
	if !strings.Contains(err.Error(), "kind must be call, compose or set_var") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestValidateGraphRejectsUnknownBindingNode(t *testing.T) {
	g := graph{
		Nodes: []node{
			{ID: "A", Kind: "call", Spec: []byte(`{"method":"debug::echo"}`)},
			{ID: "B", Kind: "compose", Spec: []byte(`{"template":{},"inputs":[{"to":"/value","source":{"kind":"node_result","node_id":"C"}}]}`)},
		},
		Edges: []edge{{From: "A", To: "B"}},
	}
	err := validateGraph(g)
	if err == nil {
		t.Fatalf("expected err, got nil")
	}
	if !strings.Contains(err.Error(), "references unknown node C") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestValidateGraphRejectsNonAncestorBinding(t *testing.T) {
	g := graph{
		Nodes: []node{
			{ID: "A", Kind: "call", Spec: []byte(`{"method":"debug::echo"}`)},
			{ID: "B", Kind: "call", Spec: []byte(`{"method":"debug::echo"}`)},
			{ID: "C", Kind: "compose", Spec: []byte(`{"template":{},"inputs":[{"to":"/value","source":{"kind":"node_result","node_id":"B"}}]}`)},
		},
		Edges: []edge{{From: "A", To: "C"}},
	}
	err := validateGraph(g)
	if err == nil {
		t.Fatalf("expected err, got nil")
	}
	if !strings.Contains(err.Error(), "must reference ancestor") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestValidateGraphAllowsFlowVarWithUniqueAncestorWriter(t *testing.T) {
	g := graph{
		Nodes: []node{
			{ID: "A", Kind: "set_var", Spec: []byte(`{"name":"session_payload","template":{"session":{"id":"s-1"}}}`)},
			{ID: "B", Kind: "compose", Spec: []byte(`{"template":{"payload":{}},"inputs":[{"to":"/payload/id","source":{"kind":"flow_var","name":"session_payload","path":"/session/id"},"required":true}]}`)},
		},
		Edges: []edge{{From: "A", To: "B"}},
	}
	if err := validateGraph(g); err != nil {
		t.Fatalf("expected ok, got err=%v", err)
	}
}

func TestValidateGraphRejectsFlowVarWithoutAncestorWriter(t *testing.T) {
	g := graph{
		Nodes: []node{
			{ID: "B", Kind: "compose", Spec: []byte(`{"template":{"payload":{}},"inputs":[{"to":"/payload/id","source":{"kind":"flow_var","name":"session_payload","path":"/session/id"},"required":true}]}`)},
		},
	}
	err := validateGraph(g)
	if err == nil {
		t.Fatalf("expected err, got nil")
	}
	if !strings.Contains(err.Error(), `flow_var "session_payload" has no ancestor writer`) {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestValidateGraphRejectsAmbiguousFlowVarWriter(t *testing.T) {
	g := graph{
		Nodes: []node{
			{ID: "A", Kind: "set_var", Spec: []byte(`{"name":"session_payload","template":{"id":"left"}}`)},
			{ID: "B", Kind: "set_var", Spec: []byte(`{"name":"session_payload","template":{"id":"right"}}`)},
			{ID: "C", Kind: "compose", Spec: []byte(`{"template":{"payload":{}},"inputs":[{"to":"/payload","source":{"kind":"flow_var","name":"session_payload"},"required":true}]}`)},
		},
		Edges: []edge{
			{From: "A", To: "C"},
			{From: "B", To: "C"},
		},
	}
	err := validateGraph(g)
	if err == nil {
		t.Fatalf("expected err, got nil")
	}
	if !strings.Contains(err.Error(), `flow_var "session_payload" has ambiguous ancestor writers`) {
		t.Fatalf("unexpected err=%v", err)
	}
}
