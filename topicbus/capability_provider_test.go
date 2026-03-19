package topicbus

import (
	"context"
	"encoding/json"
	"testing"

	coreconfig "github.com/yttydcs/myflowhub-core/config"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
)

func TestTopicBusPublishCapability(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	NewTopicBusHandlerWithConfig(cfg, nil).Init()
	reg := execcap.SharedRegistry(cfg)

	_, invoke, ok := reg.Lookup(capabilityTopicPublish, "")
	if !ok || invoke == nil {
		t.Fatalf("expected %s capability registered", capabilityTopicPublish)
	}

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
