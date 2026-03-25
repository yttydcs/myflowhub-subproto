package varstore

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	coreconfig "github.com/yttydcs/myflowhub-core/config"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
)

func decodeSchemaObject(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	if len(raw) == 0 {
		t.Fatalf("expected schema, got empty")
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode schema err=%v", err)
	}
	return out
}

func requireRequiredFields(t *testing.T, schema map[string]any, expected []string) {
	t.Helper()
	gotRaw, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("required missing or invalid: %v", schema["required"])
	}
	got := make([]string, 0, len(gotRaw))
	for _, item := range gotRaw {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("required entry invalid: %T", item)
		}
		got = append(got, text)
	}
	sort.Strings(got)
	sort.Strings(expected)
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("required mismatch got=%v want=%v", got, expected)
	}
}

func requireSchemaProperty(t *testing.T, schema map[string]any, key string) map[string]any {
	t.Helper()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or invalid: %v", schema["properties"])
	}
	value, ok := props[key].(map[string]any)
	if !ok {
		t.Fatalf("property %s missing", key)
	}
	return value
}

func TestVarStoreCapabilitySetGetRevoke(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	h := NewVarStoreHandlerWithConfig(cfg, nil)
	reg := execcap.SharedRegistry(cfg)

	setDesc, setInvoke, ok := reg.Lookup(capabilityVarSetMethod, "")
	if !ok || setInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityVarSetMethod)
	}
	getDesc, getInvoke, ok := reg.Lookup(capabilityVarGetMethod, "")
	if !ok || getInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityVarGetMethod)
	}
	revokeDesc, revokeInvoke, ok := reg.Lookup(capabilityVarRevokeMethod, "")
	if !ok || revokeInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityVarRevokeMethod)
	}

	setInputSchema := decodeSchemaObject(t, setDesc.InputSchema)
	requireRequiredFields(t, setInputSchema, []string{"name", "owner", "value"})
	setValueProp := requireSchemaProperty(t, setInputSchema, "value")
	if setValueProp["x-ui-control"] != "textarea" {
		t.Fatalf("expected textarea ui control, got %v", setValueProp["x-ui-control"])
	}
	setVisibilityProp := requireSchemaProperty(t, setInputSchema, "visibility")
	if setVisibilityProp["default"] != "private" {
		t.Fatalf("expected visibility default private, got %v", setVisibilityProp["default"])
	}
	if !reflect.DeepEqual(setVisibilityProp["enum"], []any{"private", "public"}) {
		t.Fatalf("unexpected visibility enum=%v", setVisibilityProp["enum"])
	}

	getInputSchema := decodeSchemaObject(t, getDesc.InputSchema)
	requireRequiredFields(t, getInputSchema, []string{"name", "owner"})

	revokeInputSchema := decodeSchemaObject(t, revokeDesc.InputSchema)
	requireRequiredFields(t, revokeInputSchema, []string{"name", "owner"})

	if string(setDesc.OutputSchema) != string(getDesc.OutputSchema) {
		t.Fatalf("expected set/get to share output schema")
	}
	recordOutputSchema := decodeSchemaObject(t, setDesc.OutputSchema)
	requireRequiredFields(t, recordOutputSchema, []string{"is_public", "name", "owner", "type", "value", "visibility"})
	revokeOutputSchema := decodeSchemaObject(t, revokeDesc.OutputSchema)
	requireRequiredFields(t, revokeOutputSchema, []string{"deleted", "name", "owner"})

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
