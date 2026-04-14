package varstore

// 本文件覆盖 SubProto 中 `varstore` 模块里与 `capability_provider` 相关的行为。

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
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

func TestVarStoreCapabilitySetPropagatesSubscriberAndUpstream(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	h := NewVarStoreHandlerWithConfig(cfg, nil)
	h.Init()
	reg := execcap.SharedRegistry(cfg)

	_, setInvoke, ok := reg.Lookup(capabilityVarSetMethod, "")
	if !ok || setInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityVarSetMethod)
	}

	cm := connmgr.New()
	parent := newTestConn("parent")
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	parent.SetMeta("nodeID", uint32(9))
	if err := cm.Add(parent); err != nil {
		t.Fatalf("add parent conn err=%v", err)
	}

	child := newTestConn("child")
	child.SetMeta("nodeID", uint32(2))
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child conn err=%v", err)
	}

	srv := newTestServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h.addSubscription(1, "foo", 2, child.ID())

	if _, err := setInvoke(ctx, json.RawMessage(`{"owner":1,"name":"foo","value":"bar","visibility":"public"}`)); err != nil {
		t.Fatalf("set capability err=%v", err)
	}

	if len(child.sent) != 1 {
		t.Fatalf("expected one child notify, got %d", len(child.sent))
	}
	childMsg := decodeCapabilityMessage(t, child.sent[0].payload)
	if childMsg.Action != varActionVarChanged {
		t.Fatalf("unexpected child action=%s", childMsg.Action)
	}
	var childResp varResp
	if err := json.Unmarshal(childMsg.Data, &childResp); err != nil {
		t.Fatalf("decode child response err=%v", err)
	}
	if childResp.Owner != 1 || childResp.Name != "foo" || childResp.Value != "bar" {
		t.Fatalf("unexpected child response=%+v", childResp)
	}

	if len(parent.sent) != 1 {
		t.Fatalf("expected one upstream sync, got %d", len(parent.sent))
	}
	parentMsg := decodeCapabilityMessage(t, parent.sent[0].payload)
	if parentMsg.Action != varActionUpSet {
		t.Fatalf("unexpected parent action=%s", parentMsg.Action)
	}
	if parent.sent[0].hdr == nil {
		t.Fatalf("expected parent header")
	}
	if parent.sent[0].hdr.SourceID() != 1 {
		t.Fatalf("unexpected parent source=%d", parent.sent[0].hdr.SourceID())
	}
}

func TestVarStoreCapabilityRevokePropagatesSubscriberAndUpstream(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	h := NewVarStoreHandlerWithConfig(cfg, nil)
	h.Init()
	reg := execcap.SharedRegistry(cfg)

	_, revokeInvoke, ok := reg.Lookup(capabilityVarRevokeMethod, "")
	if !ok || revokeInvoke == nil {
		t.Fatalf("expected %s capability registered", capabilityVarRevokeMethod)
	}

	cm := connmgr.New()
	parent := newTestConn("parent")
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	parent.SetMeta("nodeID", uint32(9))
	if err := cm.Add(parent); err != nil {
		t.Fatalf("add parent conn err=%v", err)
	}

	child := newTestConn("child")
	child.SetMeta("nodeID", uint32(2))
	if err := cm.Add(child); err != nil {
		t.Fatalf("add child conn err=%v", err)
	}

	srv := newTestServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h.saveRecord("foo", varRecord{
		Owner:      1,
		Value:      "bar",
		Type:       "string",
		Visibility: visibilityPublic,
		IsPublic:   true,
	})
	h.addSubscription(1, "foo", 2, child.ID())

	if _, err := revokeInvoke(ctx, json.RawMessage(`{"owner":1,"name":"foo"}`)); err != nil {
		t.Fatalf("revoke capability err=%v", err)
	}

	if len(child.sent) != 1 {
		t.Fatalf("expected one child delete notify, got %d", len(child.sent))
	}
	childMsg := decodeCapabilityMessage(t, child.sent[0].payload)
	if childMsg.Action != varActionVarDeleted {
		t.Fatalf("unexpected child action=%s", childMsg.Action)
	}
	var childResp varResp
	if err := json.Unmarshal(childMsg.Data, &childResp); err != nil {
		t.Fatalf("decode child delete response err=%v", err)
	}
	if childResp.Owner != 1 || childResp.Name != "foo" {
		t.Fatalf("unexpected child delete response=%+v", childResp)
	}

	if len(parent.sent) != 1 {
		t.Fatalf("expected one upstream revoke, got %d", len(parent.sent))
	}
	parentMsg := decodeCapabilityMessage(t, parent.sent[0].payload)
	if parentMsg.Action != varActionUpRevoke {
		t.Fatalf("unexpected parent action=%s", parentMsg.Action)
	}
	if parent.sent[0].hdr == nil {
		t.Fatalf("expected parent header")
	}
	if parent.sent[0].hdr.SourceID() != 1 {
		t.Fatalf("unexpected parent source=%d", parent.sent[0].hdr.SourceID())
	}
}

func decodeCapabilityMessage(t *testing.T, payload []byte) varMessage {
	t.Helper()
	var msg varMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode message err=%v", err)
	}
	return msg
}
