package flow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
)

func TestFlowLocalNodeFallsBackToCapabilityRegistry(t *testing.T) {
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
		Kind: "local",
		Spec: json.RawMessage(`{"method":"test::cap","args":{"x":1}}`),
	}

	code, runErr := h.executeNode(ctx, setReq{}, n)
	if runErr != nil {
		t.Fatalf("execute local capability err=%v", runErr)
	}
	if code != 1 {
		t.Fatalf("execute local capability code=%d", code)
	}
}

func TestFlowLocalMethodTakesPrecedenceOverCapabilityRegistry(t *testing.T) {
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
		Kind: "local",
		Spec: json.RawMessage(`{"method":"debug::echo","args":{"hello":"world"}}`),
	}

	code, runErr := h.executeNode(ctx, setReq{}, n)
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
