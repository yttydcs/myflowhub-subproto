package flow

import "testing"

func TestValidateGraphOK(t *testing.T) {
	g := graph{
		Nodes: []node{
			{ID: "A", Kind: "local", Spec: []byte(`{"method":"debug::echo","args":{"x":1}}`)},
			{ID: "B", Kind: "local", Spec: []byte(`{"method":"debug::echo","args":{"x":2}}`)},
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
			{ID: "A", Kind: "local", Spec: []byte(`{"method":"debug::echo"}`)},
			{ID: "B", Kind: "local", Spec: []byte(`{"method":"debug::echo"}`)},
		},
		Edges: []edge{{From: "A", To: "B"}, {From: "B", To: "A"}},
	}
	if err := validateGraph(g); err == nil {
		t.Fatalf("expected err, got nil")
	}
}
