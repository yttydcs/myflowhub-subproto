package flow

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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
