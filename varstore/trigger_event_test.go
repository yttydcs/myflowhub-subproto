package varstore

// Context: This file belongs to the SubProto implementation layer around trigger_event_test.

import (
	"context"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/eventbus"
)

func TestPropagateChangePublishesTriggerEventWithoutSubscribers(t *testing.T) {
	h := NewVarStoreHandlerWithConfig(nil, nil)
	h.Init()

	cm := connmgr.New()
	srv := newTestServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	recv := make(chan map[string]any, 1)
	token := srv.bus.Subscribe("varstore.changed", func(_ context.Context, evt eventbus.Event) {
		if m, ok := evt.Data.(map[string]any); ok {
			select {
			case recv <- m:
			default:
			}
		}
	})
	defer srv.bus.Unsubscribe("varstore.changed", token)

	h.propagateChange(ctx, 2, "sys_temp", varRecord{
		Value:      "42",
		Owner:      2,
		IsPublic:   true,
		Visibility: visibilityPublic,
		Type:       "number",
	})

	select {
	case ev := <-recv:
		if got := parseUint32Any(ev["owner"]); got != 2 {
			t.Fatalf("owner mismatch, got=%d", got)
		}
		if got := parseStringAny(ev["name"]); got != "sys_temp" {
			t.Fatalf("name mismatch, got=%q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected varstore.changed event")
	}
}

func TestPropagateDeletePublishesTriggerEventWithoutSubscribers(t *testing.T) {
	h := NewVarStoreHandlerWithConfig(nil, nil)
	h.Init()

	cm := connmgr.New()
	srv := newTestServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	recv := make(chan map[string]any, 1)
	token := srv.bus.Subscribe("varstore.deleted", func(_ context.Context, evt eventbus.Event) {
		if m, ok := evt.Data.(map[string]any); ok {
			select {
			case recv <- m:
			default:
			}
		}
	})
	defer srv.bus.Unsubscribe("varstore.deleted", token)

	h.propagateDelete(ctx, 3, "sys_flag", 0)

	select {
	case ev := <-recv:
		if got := parseUint32Any(ev["owner"]); got != 3 {
			t.Fatalf("owner mismatch, got=%d", got)
		}
		if got := parseStringAny(ev["name"]); got != "sys_flag" {
			t.Fatalf("name mismatch, got=%q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("expected varstore.deleted event")
	}
}

func parseUint32Any(v any) uint32 {
	switch vv := v.(type) {
	case uint32:
		return vv
	case uint64:
		return uint32(vv)
	case int:
		if vv >= 0 {
			return uint32(vv)
		}
	case int64:
		if vv >= 0 {
			return uint32(vv)
		}
	case float64:
		if vv >= 0 {
			return uint32(vv)
		}
	}
	return 0
}

func parseStringAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
