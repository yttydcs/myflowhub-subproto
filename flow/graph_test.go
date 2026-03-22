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
	if !strings.Contains(err.Error(), "kind must be call or compose") {
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
