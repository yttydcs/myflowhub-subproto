package flow

// Context: This file belongs to the SubProto implementation layer around persistence_test.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeFlowPersistence struct {
	loadAll []FlowDocument
	loadErr error
	saveErr error
	saved   []FlowDocument
}

func (p *fakeFlowPersistence) LoadAll(context.Context) ([]FlowDocument, error) {
	if p.loadErr != nil {
		return nil, p.loadErr
	}
	out := make([]FlowDocument, len(p.loadAll))
	copy(out, p.loadAll)
	return out, nil
}

func (p *fakeFlowPersistence) Save(_ context.Context, doc FlowDocument) error {
	if p.saveErr != nil {
		return p.saveErr
	}
	p.saved = append(p.saved, doc)
	return nil
}

func (p *fakeFlowPersistence) Delete(context.Context, string) error { return nil }

func TestNewHandlerWithOptionsUsesExplicitPersistence(t *testing.T) {
	store := &fakeFlowPersistence{}
	h := NewHandlerWithOptions(nil, HandlerOptions{Persistence: store}, nil)

	h.mu.Lock()
	got := h.currentPersistenceLocked()
	h.mu.Unlock()

	if got != store {
		t.Fatalf("expected explicit persistence")
	}
}

func TestFlowInitLoadsFlowsFromPersistence(t *testing.T) {
	store := &fakeFlowPersistence{
		loadAll: []FlowDocument{
			{
				FlowID: "123e4567-e89b-12d3-a456-426614174300",
				Trigger: trigger{
					Type: triggerTypeVarChanged,
				},
				Graph: graph{
					Nodes: []node{{
						ID:   "n1",
						Kind: "call",
						Spec: json.RawMessage(`{"method":"debug::echo"}`),
					}},
				},
			},
			{
				FlowID: "bad-flow-id",
				Trigger: trigger{
					Type: triggerTypeVarChanged,
				},
				Graph: graph{
					Nodes: []node{{
						ID:   "n1",
						Kind: "call",
						Spec: json.RawMessage(`{"method":"debug::echo"}`),
					}},
				},
			},
		},
	}

	h := NewHandlerWithOptions(nil, HandlerOptions{Persistence: store}, nil)
	if !h.Init() {
		t.Fatalf("expected init success")
	}

	h.mu.Lock()
	_, ok := h.flows["123e4567-e89b-12d3-a456-426614174300"]
	count := len(h.flows)
	h.mu.Unlock()

	if !ok {
		t.Fatalf("expected valid flow loaded from persistence")
	}
	if count != 1 {
		t.Fatalf("expected only valid persisted flows loaded, got=%d", count)
	}
}

func TestFlowApplySetLocalStopsOnPersistenceFailure(t *testing.T) {
	store := &fakeFlowPersistence{saveErr: errors.New("boom")}
	h := NewHandlerWithOptions(nil, HandlerOptions{Persistence: store}, nil)

	req := setReq{
		FlowID: "123e4567-e89b-12d3-a456-426614174301",
		Trigger: trigger{
			Type: triggerTypeVarChanged,
		},
		Graph: graph{
			Nodes: []node{{
				ID:   "n1",
				Kind: "call",
				Spec: json.RawMessage(`{"method":"debug::echo"}`),
			}},
		},
	}

	h.applySetLocal(context.Background(), nil, req, 0)

	h.mu.Lock()
	_, ok := h.flows[req.FlowID]
	h.mu.Unlock()

	if ok {
		t.Fatalf("flow should not enter memory state when persistence save fails")
	}
	if len(store.saved) != 0 {
		t.Fatalf("failed save should not record successful writes")
	}
}
