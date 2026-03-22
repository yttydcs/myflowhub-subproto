package flow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type runContext struct {
	FlowID       string                     `json:"flow_id"`
	RunID        string                     `json:"run_id"`
	ExecutorNode uint32                     `json:"executor_node,omitempty"`
	Trigger      json.RawMessage            `json:"trigger,omitempty"`
	Nodes        map[string]nodeRuntimeData `json:"nodes,omitempty"`
}

type nodeRuntimeData struct {
	Status string          `json:"status,omitempty"`
	Code   int             `json:"code,omitempty"`
	Msg    string          `json:"msg,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

type inputBinding struct {
	To       string        `json:"to"`
	Source   bindingSource `json:"source"`
	Required bool          `json:"required,omitempty"`
}

type bindingSource struct {
	Kind   string `json:"kind"`
	NodeID string `json:"node_id,omitempty"`
	Path   string `json:"path,omitempty"`
	Field  string `json:"field,omitempty"`
}

type composeSpec struct {
	Template json.RawMessage `json:"template"`
	Inputs   []inputBinding  `json:"inputs,omitempty"`
}

type graphIndex struct {
	nodes     map[string]struct{}
	ancestors map[string]map[string]struct{}
}

func newRunContext(flowID, runID string, executorNode uint32, triggerCtx json.RawMessage) runContext {
	return runContext{
		FlowID:       strings.TrimSpace(flowID),
		RunID:        strings.TrimSpace(runID),
		ExecutorNode: executorNode,
		Trigger:      normalizeTriggerContext(triggerCtx),
		Nodes:        make(map[string]nodeRuntimeData),
	}
}

func cloneRawJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func normalizeTriggerContext(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return json.RawMessage(`{}`)
	}
	return cloneRawJSON(trimmed)
}

func normalizeNodeResult(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return json.RawMessage(`null`), nil
	}
	var doc any
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return nil, errors.New("invalid node result json")
	}
	out, _ := json.Marshal(doc)
	return out, nil
}

func buildIntervalTriggerContext(now time.Time) json.RawMessage {
	return mustJSON(map[string]any{
		"type":         triggerTypeInterval,
		"triggered_at": now.UTC().Format(time.RFC3339Nano),
	})
}

func buildTopicTriggerContext(mode string, ev topicPublishEvent) json.RawMessage {
	payload := map[string]any{
		"type":  triggerTypeEvent,
		"mode":  normalizeEventMode(mode),
		"topic": strings.TrimSpace(ev.Topic),
		"name":  strings.TrimSpace(ev.Name),
	}
	if ev.TS != 0 {
		payload["ts"] = ev.TS
	}
	if len(bytes.TrimSpace(ev.Data)) != 0 {
		var value any
		if err := json.Unmarshal(ev.Data, &value); err == nil {
			payload["payload"] = value
		}
	}
	return mustJSON(payload)
}

func buildVarChangedTriggerContext(op string, ev varChangedEvent) json.RawMessage {
	return mustJSON(map[string]any{
		"type":  triggerTypeVarChanged,
		"owner": ev.Owner,
		"name":  strings.TrimSpace(ev.Name),
		"op":    strings.TrimSpace(op),
	})
}

func (s *runState) setNodeRuntimeLocked(nodeID string, data nodeRuntimeData) {
	if s == nil {
		return
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return
	}
	if s.runtime.Nodes == nil {
		s.runtime.Nodes = make(map[string]nodeRuntimeData)
	}
	data.Result = cloneRawJSON(data.Result)
	s.runtime.Nodes[nodeID] = data
}

func (s *runState) snapshotNodeStatusesLocked() []nodeStatus {
	if s == nil {
		return nil
	}
	out := make([]nodeStatus, 0, len(s.runtime.Nodes))
	for id, rt := range s.runtime.Nodes {
		out = append(out, nodeStatus{
			ID:     id,
			Status: rt.Status,
			Code:   rt.Code,
			Msg:    rt.Msg,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *runState) resolveBindingSource(src bindingSource) (any, bool, error) {
	if s == nil {
		return nil, false, errors.New("run context required")
	}
	kind := strings.ToLower(strings.TrimSpace(src.Kind))
	switch kind {
	case "node_result":
		nodeID := strings.TrimSpace(src.NodeID)
		if nodeID == "" {
			return nil, false, errors.New("node_result node_id required")
		}
		s.mu.Lock()
		nodeData, ok := s.runtime.Nodes[nodeID]
		raw := cloneRawJSON(nodeData.Result)
		s.mu.Unlock()
		if !ok || len(raw) == 0 {
			return nil, false, nil
		}
		return readJSONSourceValue(raw, src.Path)
	case "trigger":
		s.mu.Lock()
		raw := cloneRawJSON(s.runtime.Trigger)
		s.mu.Unlock()
		return readJSONSourceValue(raw, src.Path)
	case "flow_meta":
		field := strings.TrimSpace(src.Field)
		if field != "flow_id" {
			return nil, false, fmt.Errorf("unsupported flow_meta field: %s", field)
		}
		s.mu.Lock()
		flowID := s.runtime.FlowID
		s.mu.Unlock()
		return flowID, true, nil
	case "run_meta":
		field := strings.TrimSpace(src.Field)
		if field != "run_id" {
			return nil, false, fmt.Errorf("unsupported run_meta field: %s", field)
		}
		s.mu.Lock()
		runID := s.runtime.RunID
		s.mu.Unlock()
		return runID, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported binding source kind: %s", kind)
	}
}

func materializeCallArgs(nodeID string, spec callSpec, state *runState) (json.RawMessage, error) {
	base := spec.ArgsTemplate
	if len(bytes.TrimSpace(base)) == 0 {
		base = spec.Args
	}
	return materializeBoundJSON(nodeID, "call args", base, spec.Inputs, state)
}

func materializeComposeResult(nodeID string, spec composeSpec, state *runState) (json.RawMessage, error) {
	return materializeBoundJSON(nodeID, "compose template", spec.Template, spec.Inputs, state)
}

func materializeBoundJSON(nodeID, label string, base json.RawMessage, bindings []inputBinding, state *runState) (json.RawMessage, error) {
	normalized, err := normalizeTemplateJSON(base)
	if err != nil {
		return nil, fmt.Errorf("node %s invalid %s: %w", nodeID, label, err)
	}
	if len(bindings) == 0 {
		return normalized, nil
	}
	if state == nil {
		return nil, fmt.Errorf("node %s run context required for inputs", nodeID)
	}

	var doc any
	if err := json.Unmarshal(normalized, &doc); err != nil {
		return nil, fmt.Errorf("node %s invalid %s: %w", nodeID, label, err)
	}
	for i, binding := range bindings {
		value, found, err := state.resolveBindingSource(binding.Source)
		if err != nil {
			return nil, fmt.Errorf("node %s input %d: %w", nodeID, i, err)
		}
		if !found {
			if binding.Required {
				return nil, fmt.Errorf("node %s input %d required source missing", nodeID, i)
			}
			continue
		}
		doc, err = setJSONPointerValue(doc, binding.To, value)
		if err != nil {
			return nil, fmt.Errorf("node %s input %d apply %s: %w", nodeID, i, strings.TrimSpace(binding.To), err)
		}
	}
	out, _ := json.Marshal(doc)
	return out, nil
}

func normalizeTemplateJSON(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var doc any
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return nil, err
	}
	out, _ := json.Marshal(doc)
	return out, nil
}

func validateCallSpecForSet(nodeID string, spec callSpec, idx *graphIndex) error {
	if strings.TrimSpace(spec.Method) == "" {
		return fmt.Errorf("node %s call method required", nodeID)
	}
	base := spec.ArgsTemplate
	if len(bytes.TrimSpace(base)) == 0 {
		base = spec.Args
	}
	if _, err := normalizeTemplateJSON(base); err != nil {
		return fmt.Errorf("node %s invalid call args template", nodeID)
	}
	return validateBindings(nodeID, spec.Inputs, idx)
}

func validateComposeSpecForSet(nodeID string, spec composeSpec, idx *graphIndex) error {
	if len(bytes.TrimSpace(spec.Template)) == 0 {
		return fmt.Errorf("node %s compose template required", nodeID)
	}
	if _, err := normalizeTemplateJSON(spec.Template); err != nil {
		return fmt.Errorf("node %s invalid compose template", nodeID)
	}
	return validateBindings(nodeID, spec.Inputs, idx)
}

func validateBindings(nodeID string, bindings []inputBinding, idx *graphIndex) error {
	for i, binding := range bindings {
		if _, err := parseJSONPointer(binding.To); err != nil {
			return fmt.Errorf("node %s input %d invalid to pointer", nodeID, i)
		}
		kind := strings.ToLower(strings.TrimSpace(binding.Source.Kind))
		switch kind {
		case "node_result":
			refNodeID := strings.TrimSpace(binding.Source.NodeID)
			if refNodeID == "" {
				return fmt.Errorf("node %s input %d node_result node_id required", nodeID, i)
			}
			if idx == nil || !idx.hasNode(refNodeID) {
				return fmt.Errorf("node %s input %d references unknown node %s", nodeID, i, refNodeID)
			}
			if !idx.isAncestor(refNodeID, nodeID) {
				return fmt.Errorf("node %s input %d node_result must reference ancestor", nodeID, i)
			}
			if _, err := parseJSONPointer(binding.Source.Path); err != nil {
				return fmt.Errorf("node %s input %d invalid node_result path", nodeID, i)
			}
		case "trigger":
			if _, err := parseJSONPointer(binding.Source.Path); err != nil {
				return fmt.Errorf("node %s input %d invalid trigger path", nodeID, i)
			}
		case "flow_meta":
			if strings.TrimSpace(binding.Source.Field) != "flow_id" {
				return fmt.Errorf("node %s input %d invalid flow_meta field", nodeID, i)
			}
		case "run_meta":
			if strings.TrimSpace(binding.Source.Field) != "run_id" {
				return fmt.Errorf("node %s input %d invalid run_meta field", nodeID, i)
			}
		default:
			return fmt.Errorf("node %s input %d invalid source kind", nodeID, i)
		}
	}
	return nil
}

func buildGraphIndex(g graph) (*graphIndex, error) {
	order, err := topoOrder(g)
	if err != nil {
		return nil, err
	}
	idx := &graphIndex{
		nodes:     make(map[string]struct{}, len(g.Nodes)),
		ancestors: make(map[string]map[string]struct{}, len(g.Nodes)),
	}
	parents := make(map[string][]string, len(g.Nodes))
	for _, n := range g.Nodes {
		id := strings.TrimSpace(n.ID)
		idx.nodes[id] = struct{}{}
		idx.ancestors[id] = make(map[string]struct{})
	}
	for _, e := range g.Edges {
		from := strings.TrimSpace(e.From)
		to := strings.TrimSpace(e.To)
		parents[to] = append(parents[to], from)
	}
	for _, n := range order {
		if n == nil {
			continue
		}
		id := strings.TrimSpace(n.ID)
		ancestorSet := idx.ancestors[id]
		for _, parentID := range parents[id] {
			ancestorSet[parentID] = struct{}{}
			for ancestorID := range idx.ancestors[parentID] {
				ancestorSet[ancestorID] = struct{}{}
			}
		}
	}
	return idx, nil
}

func (idx *graphIndex) hasNode(nodeID string) bool {
	if idx == nil {
		return false
	}
	_, ok := idx.nodes[strings.TrimSpace(nodeID)]
	return ok
}

func (idx *graphIndex) isAncestor(ancestorNodeID, nodeID string) bool {
	if idx == nil {
		return false
	}
	ancestorNodeID = strings.TrimSpace(ancestorNodeID)
	nodeID = strings.TrimSpace(nodeID)
	if ancestorNodeID == "" || nodeID == "" || ancestorNodeID == nodeID {
		return false
	}
	ancestors := idx.ancestors[nodeID]
	_, ok := ancestors[ancestorNodeID]
	return ok
}

func decodeNodeComposeSpec(n node) (composeSpec, error) {
	var spec composeSpec
	if err := json.Unmarshal(n.Spec, &spec); err != nil {
		return composeSpec{}, errors.New("invalid compose spec")
	}
	if len(bytes.TrimSpace(spec.Template)) == 0 {
		return composeSpec{}, errors.New("compose template required")
	}
	if _, err := normalizeTemplateJSON(spec.Template); err != nil {
		return composeSpec{}, errors.New("invalid compose template")
	}
	return spec, nil
}

func readJSONSourceValue(raw json.RawMessage, path string) (any, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, false, nil
	}
	var doc any
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return nil, false, err
	}
	return getJSONPointerValue(doc, path)
}

func parseJSONPointer(pointer string) ([]string, error) {
	pointer = strings.TrimSpace(pointer)
	if pointer == "" {
		return nil, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, errors.New("json pointer must start with /")
	}
	rawParts := strings.Split(pointer[1:], "/")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		unescaped, err := decodeJSONPointerToken(part)
		if err != nil {
			return nil, err
		}
		parts = append(parts, unescaped)
	}
	return parts, nil
}

func decodeJSONPointerToken(token string) (string, error) {
	if strings.IndexByte(token, '~') < 0 {
		return token, nil
	}
	var b strings.Builder
	for i := 0; i < len(token); i++ {
		ch := token[i]
		if ch != '~' {
			b.WriteByte(ch)
			continue
		}
		if i+1 >= len(token) {
			return "", errors.New("invalid json pointer escape")
		}
		switch token[i+1] {
		case '0':
			b.WriteByte('~')
		case '1':
			b.WriteByte('/')
		default:
			return "", errors.New("invalid json pointer escape")
		}
		i++
	}
	return b.String(), nil
}

func getJSONPointerValue(doc any, pointer string) (any, bool, error) {
	tokens, err := parseJSONPointer(pointer)
	if err != nil {
		return nil, false, err
	}
	if len(tokens) == 0 {
		return doc, true, nil
	}
	current := doc
	for _, token := range tokens {
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[token]
			if !ok {
				return nil, false, nil
			}
			current = next
		case []any:
			index, err := parseJSONPointerIndex(token)
			if err != nil {
				return nil, false, err
			}
			if index < 0 || index >= len(typed) {
				return nil, false, nil
			}
			current = typed[index]
		default:
			return nil, false, nil
		}
	}
	return current, true, nil
}

func setJSONPointerValue(doc any, pointer string, value any) (any, error) {
	tokens, err := parseJSONPointer(pointer)
	if err != nil {
		return nil, err
	}
	return setJSONPointerTokens(doc, tokens, value)
}

func setJSONPointerTokens(current any, tokens []string, value any) (any, error) {
	if len(tokens) == 0 {
		return value, nil
	}
	token := tokens[0]
	if current == nil {
		if isArrayIndexToken(token) {
			current = []any{}
		} else {
			current = map[string]any{}
		}
	}
	switch typed := current.(type) {
	case map[string]any:
		next, _ := typed[token]
		resolved, err := setJSONPointerTokens(next, tokens[1:], value)
		if err != nil {
			return nil, err
		}
		typed[token] = resolved
		return typed, nil
	case []any:
		index, err := parseJSONPointerIndex(token)
		if err != nil {
			return nil, err
		}
		if index >= len(typed) {
			grown := make([]any, index+1)
			copy(grown, typed)
			typed = grown
		}
		resolved, err := setJSONPointerTokens(typed[index], tokens[1:], value)
		if err != nil {
			return nil, err
		}
		typed[index] = resolved
		return typed, nil
	default:
		return nil, fmt.Errorf("pointer segment %q targets scalar", token)
	}
}

func isArrayIndexToken(token string) bool {
	if token == "" {
		return false
	}
	for _, ch := range token {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func parseJSONPointerIndex(token string) (int, error) {
	if !isArrayIndexToken(token) {
		return 0, fmt.Errorf("invalid array index: %s", token)
	}
	index, err := strconv.Atoi(token)
	if err != nil || index < 0 {
		return 0, fmt.Errorf("invalid array index: %s", token)
	}
	return index, nil
}
