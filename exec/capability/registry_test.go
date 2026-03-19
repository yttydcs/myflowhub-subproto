package capability

import (
	"context"
	"encoding/json"
	"testing"
)

type sharedScope struct{}

func TestRegistryRegisterConflict(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Descriptor{
		Provider: "exec",
		Method:   "debug::echo",
	}, func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
		return args, nil
	}); err != nil {
		t.Fatalf("register first err=%v", err)
	}
	err := reg.Register(Descriptor{
		Provider: "flow",
		Method:   "debug::echo",
	}, nil)
	if err != ErrConflict {
		t.Fatalf("expected ErrConflict, got=%v", err)
	}
}

func TestRegistryRegisterSameProviderIdempotent(t *testing.T) {
	reg := NewRegistry()
	original := func(_ context.Context, args json.RawMessage) (json.RawMessage, error) { return args, nil }
	if err := reg.Register(Descriptor{
		Provider: "exec",
		Method:   "debug::echo",
	}, original); err != nil {
		t.Fatalf("register first err=%v", err)
	}

	updated := func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":1}`), nil
	}
	if err := reg.Register(Descriptor{
		Provider: "exec",
		Method:   "debug::echo",
	}, updated); err != nil {
		t.Fatalf("register second err=%v", err)
	}

	_, invoke, ok := reg.Lookup("debug::echo", "")
	if !ok || invoke == nil {
		t.Fatalf("expected lookup success")
	}
	got, err := invoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("invoke err=%v", err)
	}
	if string(got) != `{"ok":1}` {
		t.Fatalf("unexpected invoke result=%s", string(got))
	}
}

func TestSharedRegistryUsesPointerScope(t *testing.T) {
	scope := &sharedScope{}
	a := SharedRegistry(scope)
	b := SharedRegistry(scope)
	if a == nil || b == nil || a != b {
		t.Fatalf("expected same shared registry instance")
	}
}
