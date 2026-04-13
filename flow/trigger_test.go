package flow

// Context: This file belongs to the SubProto implementation layer around trigger_test.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
)

func TestValidateTrigger(t *testing.T) {
	cases := []struct {
		name    string
		input   trigger
		wantErr bool
	}{
		{
			name:    "interval requires every",
			input:   trigger{Type: "interval"},
			wantErr: true,
		},
		{
			name:    "interval ok",
			input:   trigger{Type: "interval", EveryMs: 1000},
			wantErr: false,
		},
		{
			name:    "interval dedup unsupported",
			input:   trigger{Type: "interval", EveryMs: 1000, DedupWindowMs: intPtr(100)},
			wantErr: true,
		},
		{
			name:    "cron requires expression",
			input:   trigger{Type: "cron"},
			wantErr: true,
		},
		{
			name:    "cron ok",
			input:   trigger{Type: "cron", Cron: "0 9 * * 1-5"},
			wantErr: false,
		},
		{
			name:    "cron invalid",
			input:   trigger{Type: "cron", Cron: "bad expr"},
			wantErr: true,
		},
		{
			name:    "cron dedup unsupported",
			input:   trigger{Type: "cron", Cron: "0 9 * * 1-5", DedupWindowMs: intPtr(100)},
			wantErr: true,
		},
		{
			name:    "event requires name or topic",
			input:   trigger{Type: "event"},
			wantErr: true,
		},
		{
			name:    "event with name",
			input:   trigger{Type: "event", EventName: "alarm"},
			wantErr: false,
		},
		{
			name:    "event with topic",
			input:   trigger{Type: "event", EventTopic: "sensor/temp"},
			wantErr: false,
		},
		{
			name:    "event with received mode",
			input:   trigger{Type: "event", EventMode: "received", EventTopic: "sensor/temp"},
			wantErr: false,
		},
		{
			name:    "event with dedup window",
			input:   trigger{Type: "event", EventTopic: "sensor/temp", DedupWindowMs: intPtr(100)},
			wantErr: false,
		},
		{
			name:    "event mode unsupported",
			input:   trigger{Type: "event", EventMode: "invalid", EventTopic: "sensor/temp"},
			wantErr: true,
		},
		{
			name:    "var_changed no filter",
			input:   trigger{Type: "var_changed"},
			wantErr: false,
		},
		{
			name:    "negative dedup window",
			input:   trigger{Type: "var_changed", DedupWindowMs: intPtr(-1)},
			wantErr: true,
		},
		{
			name:    "unsupported",
			input:   trigger{Type: "once"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTrigger(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}

func TestCronScheduleNextAfter(t *testing.T) {
	schedule, err := parseCronExpr("15 9 * * 1-5")
	if err != nil {
		t.Fatalf("parse err=%v", err)
	}
	start := time.Date(2026, 4, 3, 9, 14, 30, 0, time.Local)
	next := schedule.NextAfter(start)
	want := time.Date(2026, 4, 3, 9, 15, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("unexpected next cron time want=%v got=%v", want, next)
	}
}

func TestHandleTopicPublishEvent_StartsMatchingFlows(t *testing.T) {
	h := NewHandler(nil)
	h.flows["event-name"] = makeTestFlow("event-name", trigger{Type: "event", EventName: "alarm"})
	h.flows["event-topic"] = makeTestFlow("event-topic", trigger{Type: "event", EventTopic: "sensor/temp"})
	h.flows["event-both"] = makeTestFlow("event-both", trigger{Type: "event", EventName: "alarm", EventTopic: "sensor/temp"})
	h.flows["interval"] = makeTestFlow("interval", trigger{Type: "interval", EveryMs: 1000})

	h.handleTopicPublishEvent(eventModePublish, topicPublishEvent{Topic: "sensor/temp", Name: "alarm"})

	waitRunCount(t, h, 3)
	got := runFlowSet(h)
	for _, flowID := range []string{"event-name", "event-topic", "event-both"} {
		if !got[flowID] {
			t.Fatalf("expected run for flow %s, got=%v", flowID, got)
		}
	}
	if got["interval"] {
		t.Fatalf("interval flow should not be started by topic event")
	}
	state := latestRunStateForTest(t, h, "event-name")
	assertJSONPointerValue(t, state.runtime.Trigger, "/type", triggerTypeEvent)
	assertJSONPointerValue(t, state.runtime.Trigger, "/mode", eventModePublish)
	assertJSONPointerValue(t, state.runtime.Trigger, "/name", "alarm")
}

func TestHandleTopicReceivedEvent_StartsReceivedModeFlows(t *testing.T) {
	h := NewHandler(nil)
	h.flows["publish-default"] = makeTestFlow("publish-default", trigger{Type: "event", EventName: "alarm"})
	h.flows["received-only"] = makeTestFlow("received-only", trigger{Type: "event", EventMode: "received", EventName: "alarm"})
	h.flows["any-mode"] = makeTestFlow("any-mode", trigger{Type: "event", EventMode: "any", EventName: "alarm"})

	h.handleTopicPublishEvent(eventModeReceived, topicPublishEvent{Topic: "sensor/temp", Name: "alarm"})

	waitRunCount(t, h, 2)
	got := runFlowSet(h)
	if got["publish-default"] {
		t.Fatalf("publish-default should not be started by received event")
	}
	for _, flowID := range []string{"received-only", "any-mode"} {
		if !got[flowID] {
			t.Fatalf("expected run for flow %s, got=%v", flowID, got)
		}
	}
}

func TestHandleVarChangedEvent_StartsMatchingFlows(t *testing.T) {
	h := NewHandler(nil)
	h.flows["any"] = makeTestFlow("any", trigger{Type: "var_changed"})
	h.flows["owner"] = makeTestFlow("owner", trigger{Type: "var_changed", VarOwner: 2})
	h.flows["name"] = makeTestFlow("name", trigger{Type: "var_changed", VarName: "k1"})
	h.flows["both"] = makeTestFlow("both", trigger{Type: "var_changed", VarOwner: 2, VarName: "k1"})
	h.flows["mismatch"] = makeTestFlow("mismatch", trigger{Type: "var_changed", VarOwner: 3})

	h.handleVarChangedEvent(varChangeOpChanged, varChangedEvent{Owner: 2, Name: "k1"})

	waitRunCount(t, h, 4)
	got := runFlowSet(h)
	for _, flowID := range []string{"any", "owner", "name", "both"} {
		if !got[flowID] {
			t.Fatalf("expected run for flow %s, got=%v", flowID, got)
		}
	}
	if got["mismatch"] {
		t.Fatalf("mismatch flow should not be started by var_changed event")
	}
	state := latestRunStateForTest(t, h, "any")
	assertJSONPointerValue(t, state.runtime.Trigger, "/type", triggerTypeVarChanged)
	assertJSONPointerValue(t, state.runtime.Trigger, "/op", varChangeOpChanged)
	assertJSONPointerValue(t, state.runtime.Trigger, "/owner", float64(2))
}

func TestTryStartScheduledRun_PopulatesIntervalTriggerContext(t *testing.T) {
	h := NewHandler(nil)
	h.flows["interval"] = makeTestFlow("interval", trigger{Type: "interval", EveryMs: 1000})

	h.tryStartScheduledRun("interval")

	waitRunCount(t, h, 1)
	state := latestRunStateForTest(t, h, "interval")
	assertJSONPointerValue(t, state.runtime.Trigger, "/type", triggerTypeInterval)
	value, found, err := readJSONSourceValue(state.runtime.Trigger, "/triggered_at")
	if err != nil || !found {
		t.Fatalf("expected interval trigger timestamp, found=%v err=%v", found, err)
	}
	if _, ok := value.(string); !ok {
		t.Fatalf("unexpected triggered_at value=%T %v", value, value)
	}
}

func TestTryStartRunWithTrigger_DefaultLegacySingleFlight(t *testing.T) {
	h, _, _, _, _ := newDeleteTestEnv(t, nil)
	release := make(chan struct{})
	method := "test::trigger-legacy-single-flight"
	h.RegisterLocalMethod(method, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		<-release
		return json.RawMessage(`{"ok":true}`), nil
	})

	flowID := "123e4567-e89b-12d3-a456-426614174126"
	flow := makeTestFlow(flowID, trigger{Type: "interval", EveryMs: 1000})
	flow.Graph = simpleGraph(method)
	h.flows[flowID] = flow

	h.tryStartRunWithTrigger(flowID, buildIntervalTriggerContext(time.Now()))
	waitRunCount(t, h, 1)
	h.tryStartRunWithTrigger(flowID, buildIntervalTriggerContext(time.Now()))

	time.Sleep(50 * time.Millisecond)

	h.mu.Lock()
	gotRuns := len(h.runOrderByFlow[flowID])
	h.mu.Unlock()
	if gotRuns != 1 {
		t.Fatalf("expected legacy trigger single-flight, got runs=%d", gotRuns)
	}

	close(release)
	waitLatestRunStatus(t, h, flowID, "succeeded")
}

func TestTryStartRunWithTrigger_MaxActiveRunsZeroAllowsOverlap(t *testing.T) {
	h, _, _, _, _ := newDeleteTestEnv(t, nil)
	release := make(chan struct{})
	method := "test::trigger-unlimited-overlap"
	h.RegisterLocalMethod(method, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		<-release
		return json.RawMessage(`{"ok":true}`), nil
	})

	zero := 0
	flowID := "123e4567-e89b-12d3-a456-426614174127"
	flow := makeTestFlow(flowID, trigger{Type: "interval", EveryMs: 1000})
	flow.MaxActiveRuns = &zero
	flow.Graph = simpleGraph(method)
	h.flows[flowID] = flow

	h.tryStartRunWithTrigger(flowID, buildIntervalTriggerContext(time.Now()))
	waitRunCount(t, h, 1)
	h.tryStartRunWithTrigger(flowID, buildIntervalTriggerContext(time.Now()))
	waitRunCount(t, h, 2)

	h.mu.Lock()
	gotRuns := len(h.runOrderByFlow[flowID])
	h.mu.Unlock()
	if gotRuns != 2 {
		t.Fatalf("expected overlapping trigger runs when max_active_runs=0, got runs=%d", gotRuns)
	}

	close(release)
	waitLatestRunStatus(t, h, flowID, "succeeded")
}

func TestHandleTopicPublishEvent_DedupWindowSkipsDuplicateStarts(t *testing.T) {
	h := NewHandler(nil)
	dedupWindowMs := 80
	method := "test::event-dedup"
	h.RegisterLocalMethod(method, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true}`), nil
	})
	flowID := "event-dedup"
	flow := makeTestFlow(flowID, trigger{
		Type:          "event",
		EventTopic:    "sensor/temp",
		DedupWindowMs: &dedupWindowMs,
	})
	flow.Graph = simpleGraph(method)
	h.flows[flowID] = flow

	ev := topicPublishEvent{
		Topic: "sensor/temp",
		Name:  "alarm",
		Data:  mustJSON(map[string]any{"value": 1}),
	}
	h.handleTopicPublishEvent(eventModePublish, ev)
	waitRunCount(t, h, 1)
	waitRunTerminal(t, h, latestRunStateForTest(t, h, flowID).runID)

	h.handleTopicPublishEvent(eventModePublish, ev)
	time.Sleep(20 * time.Millisecond)

	h.mu.Lock()
	gotRuns := len(h.runOrderByFlow[flowID])
	h.mu.Unlock()
	if gotRuns != 1 {
		t.Fatalf("expected duplicate event to be deduped, got runs=%d", gotRuns)
	}

	time.Sleep(90 * time.Millisecond)
	h.handleTopicPublishEvent(eventModePublish, ev)
	waitRunCount(t, h, 2)
}

func TestHandleTopicPublishEvent_DedupWindowAllowsDifferentPayload(t *testing.T) {
	h := NewHandler(nil)
	dedupWindowMs := 200
	method := "test::event-dedup-payload"
	h.RegisterLocalMethod(method, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true}`), nil
	})
	flowID := "event-dedup-payload"
	flow := makeTestFlow(flowID, trigger{
		Type:          "event",
		EventTopic:    "sensor/temp",
		DedupWindowMs: &dedupWindowMs,
	})
	flow.Graph = simpleGraph(method)
	h.flows[flowID] = flow

	h.handleTopicPublishEvent(eventModePublish, topicPublishEvent{
		Topic: "sensor/temp",
		Name:  "alarm",
		Data:  mustJSON(map[string]any{"value": 1}),
	})
	waitRunCount(t, h, 1)
	waitRunTerminal(t, h, latestRunStateForTest(t, h, flowID).runID)

	h.handleTopicPublishEvent(eventModePublish, topicPublishEvent{
		Topic: "sensor/temp",
		Name:  "alarm",
		Data:  mustJSON(map[string]any{"value": 2}),
	})
	waitRunCount(t, h, 2)
}

func TestHandleVarChangedEvent_DedupWindowKeysByOwnerNameOp(t *testing.T) {
	h := NewHandler(nil)
	dedupWindowMs := 200
	method := "test::var-dedup"
	h.RegisterLocalMethod(method, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true}`), nil
	})
	flowID := "var-dedup"
	flow := makeTestFlow(flowID, trigger{
		Type:          "var_changed",
		VarOwner:      2,
		VarName:       "k1",
		DedupWindowMs: &dedupWindowMs,
	})
	flow.Graph = simpleGraph(method)
	h.flows[flowID] = flow

	h.handleVarChangedEvent(varChangeOpChanged, varChangedEvent{Owner: 2, Name: "k1"})
	waitRunCount(t, h, 1)
	waitRunTerminal(t, h, latestRunStateForTest(t, h, flowID).runID)

	h.handleVarChangedEvent(varChangeOpChanged, varChangedEvent{Owner: 2, Name: "k1"})
	time.Sleep(20 * time.Millisecond)

	h.mu.Lock()
	gotRuns := len(h.runOrderByFlow[flowID])
	h.mu.Unlock()
	if gotRuns != 1 {
		t.Fatalf("expected duplicate var_changed trigger to be deduped, got runs=%d", gotRuns)
	}

	h.handleVarChangedEvent(varChangeOpDeleted, varChangedEvent{Owner: 2, Name: "k1"})
	waitRunCount(t, h, 2)
}

func TestTriggerStartedRunPreservesServerContextForLocalCapability(t *testing.T) {
	cfg := coreconfig.NewMap(map[string]string{})
	reg := execcap.SharedRegistry(cfg)
	method := "test::ctx-trigger"
	seenNode := make(chan uint32, 1)
	if err := reg.Register(execcap.Descriptor{
		Provider: "test",
		Method:   method,
	}, execcap.InvokeFunc(func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		srv := core.ServerFromContext(ctx)
		if srv == nil {
			return nil, errors.New("missing server context")
		}
		select {
		case seenNode <- srv.NodeID():
		default:
		}
		return json.RawMessage(`{"ok":true}`), nil
	})); err != nil {
		t.Fatalf("register capability err=%v", err)
	}

	h := NewHandlerWithConfig(cfg, nil)
	srv := &testServer{nodeID: 7, cm: connmgr.New()}
	h.BindServer(srv)
	h.flows["ctx-trigger"] = setReq{
		FlowID:  "ctx-trigger",
		Trigger: trigger{Type: "var_changed"},
		Graph: graph{
			Nodes: []node{
				{
					ID:   "n1",
					Kind: "call",
					Spec: json.RawMessage(fmt.Sprintf(`{"method":"%s","args_template":{"ok":true}}`, method)),
				},
			},
		},
	}

	h.handleVarChangedEvent(varChangeOpChanged, varChangedEvent{Owner: 1, Name: "foo"})

	state := waitLatestRunStatus(t, h, "ctx-trigger", "succeeded")
	assertJSONPointerValue(t, state.runtime.Trigger, "/type", triggerTypeVarChanged)

	select {
	case nodeID := <-seenNode:
		if nodeID != 7 {
			t.Fatalf("unexpected server node id=%d", nodeID)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("capability was not invoked with server context")
	}
}

func makeTestFlow(flowID string, tr trigger) setReq {
	return setReq{
		FlowID:  flowID,
		Trigger: tr,
		Graph: graph{
			Nodes: []node{
				{
					ID:   "n1",
					Kind: "call",
					Spec: []byte(`{"method":"debug::echo","args_template":{"ok":true}}`),
				},
			},
			Edges: nil,
		},
	}
}

func intPtr(v int) *int {
	return &v
}

func waitRunCount(t *testing.T, h *Handler, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.mu.Lock()
		got := len(h.runs)
		h.mu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run count mismatch, want=%d got=%d", want, got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func runFlowSet(h *Handler) map[string]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]bool, len(h.runs))
	for _, st := range h.runs {
		if st == nil {
			continue
		}
		out[st.flowID] = true
	}
	return out
}

func latestRunStateForTest(t *testing.T, h *Handler, flowID string) *runState {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.latestRunStateLocked(flowID)
	if state == nil {
		t.Fatalf("expected latest run state for flow %s", flowID)
	}
	return state
}

func waitLatestRunStatus(t *testing.T, h *Handler, flowID string, want string) *runState {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state := latestRunStateForTest(t, h, flowID)
		state.mu.Lock()
		status := state.status
		state.mu.Unlock()
		if status == want {
			return state
		}
		if status == "failed" || status == "cancelled" {
			t.Fatalf("unexpected run status=%s for flow %s", status, flowID)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run status mismatch for flow %s, want=%s", flowID, want)
	return nil
}

func assertJSONPointerValue(t *testing.T, raw json.RawMessage, pointer string, want any) {
	t.Helper()
	got, found, err := readJSONSourceValue(raw, pointer)
	if err != nil {
		t.Fatalf("pointer %s err=%v", pointer, err)
	}
	if !found {
		t.Fatalf("pointer %s not found", pointer)
	}
	if got != want {
		t.Fatalf("pointer %s want=%v got=%v", pointer, want, got)
	}
}
