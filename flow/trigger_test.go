package flow

import (
	"encoding/json"
	"testing"
	"time"
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

func makeTestFlow(flowID string, tr trigger) setReq {
	return setReq{
		FlowID:  flowID,
		Trigger: tr,
		Graph: graph{
			Nodes: []node{
				{
					ID:   "n1",
					Kind: "call",
					Spec: []byte(`{"method":"debug::echo","args":{"ok":true}}`),
				},
			},
			Edges: nil,
		},
	}
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
