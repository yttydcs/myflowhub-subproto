package flow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
)

func TestFlowRegistersCapabilityRun(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	h := NewHandlerWithConfig(cfg, nil)
	h.srv = &testServer{nodeID: 1, cm: connmgr.New()}
	flowID := "123e4567-e89b-12d3-a456-426614174003"
	h.flows[flowID] = setReq{
		FlowID: flowID,
		Graph: graph{
			Nodes: []node{
				{
					ID:   "n1",
					Kind: "call",
					Spec: json.RawMessage(`{"method":"debug::echo","args":{"hello":"world"}}`),
				},
			},
		},
	}

	reg := execcap.SharedRegistry(cfg)
	_, invoke, ok := reg.Lookup(capabilityMethodRun, "")
	if !ok || invoke == nil {
		t.Fatalf("expected flow::run capability registered")
	}

	raw, err := invoke(context.Background(), json.RawMessage(`{"flow_id":"123e4567-e89b-12d3-a456-426614174003"}`))
	if err != nil {
		t.Fatalf("invoke flow::run err=%v", err)
	}
	var resp map[string]string
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal flow::run result err=%v", err)
	}
	if resp["flow_id"] != flowID || resp["run_id"] == "" {
		t.Fatalf("unexpected flow::run result=%v", resp)
	}

	runID := resp["run_id"]
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		state := h.runs[runID]
		h.mu.Unlock()
		if state != nil {
			state.mu.Lock()
			st := state.status
			state.mu.Unlock()
			if st == "succeeded" {
				return
			}
			if st == "failed" {
				t.Fatalf("expected succeeded run, got failed")
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run did not finish in time")
}

func TestFlowCapabilityRunValidatesArgs(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	NewHandlerWithConfig(cfg, nil)
	reg := execcap.SharedRegistry(cfg)
	_, invoke, ok := reg.Lookup(capabilityMethodRun, "")
	if !ok || invoke == nil {
		t.Fatalf("expected flow::run capability registered")
	}

	_, err := invoke(context.Background(), json.RawMessage(`{"flow_id":""}`))
	if err == nil {
		t.Fatalf("expected invalid flow_id error")
	}
}

func TestFlowCapabilityRunPreservesServerContext(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	reg := execcap.SharedRegistry(cfg)
	method := "test::ctx-cap-run"
	seenNode := make(chan uint32, 1)
	if err := reg.Register(execcap.Descriptor{
		Provider: "test",
		Method:   method,
	}, execcap.InvokeFunc(func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		srv := core.ServerFromContext(ctx)
		if srv == nil {
			return nil, errors.New("missing server context")
		}
		select {
		case seenNode <- srv.NodeID():
		default:
		}
		return json.RawMessage(`{"ok":true}`), nil
	})); err != nil {
		t.Fatalf("register capability err=%v", err)
	}

	h := NewHandlerWithConfig(cfg, nil)
	srv := &testServer{nodeID: 11, cm: connmgr.New()}
	h.srv = srv
	flowID := "123e4567-e89b-12d3-a456-4266141740aa"
	h.flows[flowID] = setReq{
		FlowID: flowID,
		Graph: graph{
			Nodes: []node{
				{
					ID:   "n1",
					Kind: "call",
					Spec: json.RawMessage(fmt.Sprintf(`{"method":"%s","args":{"hello":"world"}}`, method)),
				},
			},
		},
	}

	_, invoke, ok := reg.Lookup(capabilityMethodRun, "")
	if !ok || invoke == nil {
		t.Fatalf("expected flow::run capability registered")
	}

	raw, err := invoke(core.WithServerContext(context.Background(), srv), json.RawMessage(fmt.Sprintf(`{"flow_id":"%s"}`, flowID)))
	if err != nil {
		t.Fatalf("invoke flow::run err=%v", err)
	}
	var resp map[string]string
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal flow::run result err=%v", err)
	}

	runID := resp["run_id"]
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		state := h.runs[runID]
		h.mu.Unlock()
		if state == nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		state.mu.Lock()
		status := state.status
		state.mu.Unlock()
		if status == "succeeded" {
			select {
			case nodeID := <-seenNode:
				if nodeID != 11 {
					t.Fatalf("unexpected server node id=%d", nodeID)
				}
				return
			case <-time.After(2 * time.Second):
				t.Fatalf("capability was not invoked with server context")
			}
		}
		if status == "failed" || status == "cancelled" {
			t.Fatalf("unexpected run status=%s", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run did not finish in time")
}
