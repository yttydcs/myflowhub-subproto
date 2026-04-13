package topicbus

// Context: This file belongs to the SubProto implementation layer around capability_provider_test.

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	coreconfig "github.com/yttydcs/myflowhub-core/config"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
)

func TestTopicBusPublishCapability(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	NewTopicBusHandlerWithConfig(cfg, nil).Init()
	reg := execcap.SharedRegistry(cfg)

	desc, invoke, ok := reg.Lookup(capabilityTopicPublish, "")
	if !ok || invoke == nil {
		t.Fatalf("expected %s capability registered", capabilityTopicPublish)
	}
	assertTopicPublishSchema(t, desc.InputSchema)

	if _, err := invoke(context.Background(), json.RawMessage(`{"name":""}`)); err == nil {
		t.Fatalf("expected invalid args error")
	}

	raw, err := invoke(context.Background(), json.RawMessage(`{"topic":"demo","name":"ping","payload":{"k":"v"}}`))
	if err != nil {
		t.Fatalf("invoke publish capability err=%v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal publish result err=%v", err)
	}
	if resp["topic"] != "demo" || resp["name"] != "ping" {
		t.Fatalf("unexpected publish result=%v", resp)
	}
}

func assertTopicPublishSchema(t *testing.T, raw json.RawMessage) {
	t.Helper()
	if len(raw) == 0 {
		t.Fatalf("expected input schema")
	}
	var schema struct {
		Title      string   `json:"title"`
		Type       string   `json:"type"`
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schema err=%v", err)
	}
	if schema.Title != "Publish Event" || schema.Type != "object" {
		t.Fatalf("unexpected schema header: %+v", schema)
	}
	if !reflect.DeepEqual(schema.Required, []string{"name"}) {
		t.Fatalf("unexpected required fields: %v", schema.Required)
	}
	wantTypes := map[string]string{
		"topic":   "string",
		"name":    "string",
		"ts":      "integer",
		"payload": "object",
	}
	if len(schema.Properties) != len(wantTypes) {
		t.Fatalf("unexpected property count: got=%d want=%d", len(schema.Properties), len(wantTypes))
	}
	for key, wantType := range wantTypes {
		got, ok := schema.Properties[key]
		if !ok {
			t.Fatalf("missing schema property %s", key)
		}
		if got.Type != wantType {
			t.Fatalf("unexpected type for %s: got=%s want=%s", key, got.Type, wantType)
		}
	}
}
