package flow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
	protocolexec "github.com/yttydcs/myflowhub-proto/protocol/exec"
	"github.com/yttydcs/myflowhub-subproto/broker"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
)

func TestFlowCallNodeFallsBackToCapabilityRegistry(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	reg := execcap.SharedRegistry(cfg)
	err := reg.Register(execcap.Descriptor{
		Provider: "test",
		Method:   "test::cap",
	}, execcap.InvokeFunc(func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
		if string(args) != `{"x":1}` {
			return nil, errors.New("unexpected args")
		}
		return json.RawMessage(`{"ok":true}`), nil
	}))
	if err != nil {
		t.Fatalf("register capability err=%v", err)
	}

	h := NewHandlerWithConfig(cfg, nil)
	srv := &testServer{nodeID: 1, cm: connmgr.New()}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)
	n := node{
		ID:   "n1",
		Kind: "call",
		Spec: json.RawMessage(`{"method":"test::cap","args":{"x":1}}`),
	}

	code, _, runErr := h.executeNode(ctx, setReq{}, nil, n)
	if runErr != nil {
		t.Fatalf("execute local capability err=%v", runErr)
	}
	if code != 1 {
		t.Fatalf("execute local capability code=%d", code)
	}
}

func TestFlowCallNodeMethodTakesPrecedenceOverCapabilityRegistry(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	reg := execcap.SharedRegistry(cfg)
	called := false
	err := reg.Register(execcap.Descriptor{
		Provider: "test",
		Method:   "debug::echo",
	}, execcap.InvokeFunc(func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		called = true
		return nil, errors.New("should not be called")
	}))
	if err != nil {
		t.Fatalf("register override capability err=%v", err)
	}

	h := NewHandlerWithConfig(cfg, nil)
	srv := &testServer{nodeID: 1, cm: connmgr.New()}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)
	n := node{
		ID:   "n1",
		Kind: "call",
		Spec: json.RawMessage(`{"method":"debug::echo","args":{"hello":"world"}}`),
	}

	code, _, runErr := h.executeNode(ctx, setReq{}, nil, n)
	if runErr != nil {
		t.Fatalf("execute local debug err=%v", runErr)
	}
	if code != 1 {
		t.Fatalf("execute local debug code=%d", code)
	}
	if called {
		t.Fatalf("expected localMethods path before capability registry")
	}
}

func TestFlowLegacyLocalNodeRejectedAtRuntime(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	h := NewHandlerWithConfig(cfg, nil)
	srv := &testServer{nodeID: 1, cm: connmgr.New()}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)
	n := node{
		ID:   "n1",
		Kind: "local",
		Spec: json.RawMessage(`{"method":"debug::echo","args":{"hello":"legacy"}}`),
	}

	code, _, runErr := h.executeNode(ctx, setReq{}, nil, n)
	if runErr == nil {
		t.Fatal("expected legacy local node to be rejected")
	}
	if code != 400 {
		t.Fatalf("expected legacy local node to fail with 400, got=%d", code)
	}
}

func TestFlowCallNodeRemoteUsesExecCall(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	h := NewHandlerWithConfig(cfg, nil)
	cm := connmgr.New()
	targetConn := &mockConnection{id: "c-target"}
	targetConn.SetMeta("nodeID", uint32(2))
	if err := cm.Add(targetConn); err != nil {
		t.Fatalf("add target conn err=%v", err)
	}
	srv := &testServer{nodeID: 1, cm: cm}
	h.srv = srv
	ctx := core.WithServerContext(context.Background(), srv)

	resultCh := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(1500 * time.Millisecond)
		for time.Now().Before(deadline) {
			if len(srv.sends) == 0 {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			var env struct {
				Action string          `json:"action"`
				Data   json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(srv.sends[0].payload, &env); err != nil {
				resultCh <- err
				return
			}
			if env.Action != protocolexec.ActionCall {
				resultCh <- errors.New("unexpected action")
				return
			}
			var req protocolexec.CallReq
			if err := json.Unmarshal(env.Data, &req); err != nil {
				resultCh <- err
				return
			}
			if req.TargetNode != 2 || req.Method != "debug::echo" {
				resultCh <- errors.New("unexpected call request")
				return
			}
			broker.SharedExecCallBroker().Deliver(req.ReqID, protocolexec.CallResp{ReqID: req.ReqID, Code: 1})
			resultCh <- nil
			return
		}
		resultCh <- errors.New("exec call not sent")
	}()

	n := node{
		ID:   "n1",
		Kind: "call",
		Spec: json.RawMessage(`{"target":2,"method":"debug::echo","args":{"remote":true}}`),
	}
	code, _, runErr := h.executeNode(ctx, setReq{}, nil, n)
	if runErr != nil {
		t.Fatalf("execute remote call err=%v", runErr)
	}
	if code != 1 {
		t.Fatalf("execute remote call code=%d", code)
	}
	if err := <-resultCh; err != nil {
		t.Fatalf("remote call assertion err=%v", err)
	}
}
