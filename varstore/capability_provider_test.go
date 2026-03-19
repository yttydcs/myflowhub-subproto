package varstore

import (
	"context"
	"encoding/json"
	"testing"

	coreconfig "github.com/yttydcs/myflowhub-core/config"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
)

func TestVarStoreCapabilitySetGetRevoke(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	h := NewVarStoreHandlerWithConfig(cfg, nil)
	reg := execcap.SharedRegistry(cfg)

	_, setInvoke, ok := reg.Lookup(capabilityVarSetMethod, "")
	if !ok || setInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityVarSetMethod)
	}
	_, getInvoke, ok := reg.Lookup(capabilityVarGetMethod, "")
	if !ok || getInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityVarGetMethod)
	}
	_, revokeInvoke, ok := reg.Lookup(capabilityVarRevokeMethod, "")
	if !ok || revokeInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityVarRevokeMethod)
	}

	rawSet, err := setInvoke(context.Background(), json.RawMessage(`{"owner":2,"name":"foo","value":"bar","visibility":"public"}`))
	if err != nil {
		t.Fatalf("set capability err=%v", err)
	}
	var setResp map[string]any
	if err := json.Unmarshal(rawSet, &setResp); err != nil {
		t.Fatalf("unmarshal set result err=%v", err)
	}
	if setResp["name"] != "foo" || setResp["value"] != "bar" {
		t.Fatalf("unexpected set result=%v", setResp)
	}

	rawGet, err := getInvoke(context.Background(), json.RawMessage(`{"owner":2,"name":"foo"}`))
	if err != nil {
		t.Fatalf("get capability err=%v", err)
	}
	var getResp map[string]any
	if err := json.Unmarshal(rawGet, &getResp); err != nil {
		t.Fatalf("unmarshal get result err=%v", err)
	}
	if getResp["value"] != "bar" {
		t.Fatalf("unexpected get result=%v", getResp)
	}

	if _, err := revokeInvoke(context.Background(), json.RawMessage(`{"owner":2,"name":"foo"}`)); err != nil {
		t.Fatalf("revoke capability err=%v", err)
	}
	if _, err := h.invokeCapabilityGet(context.Background(), json.RawMessage(`{"owner":2,"name":"foo"}`)); err == nil {
		t.Fatalf("expected var deleted")
	}
}
