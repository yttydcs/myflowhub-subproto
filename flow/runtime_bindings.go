package flow

// 本文件承载 SubProto 中 `flow` 模块里与 `runtime_bindings` 相关的逻辑。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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
	Loop         *loopRuntimeData           `json:"loop,omitempty"`
	Nodes        map[string]nodeRuntimeData `json:"nodes,omitempty"`
	Vars         map[string]varRuntimeData  `json:"vars,omitempty"`
}

type loopRuntimeData struct {
	Item  json.RawMessage `json:"item,omitempty"`
	Index int             `json:"index,omitempty"`
}

type nodeRuntimeData struct {
	Status string          `json:"status,omitempty"`
	Code   int             `json:"code,omitempty"`
	Msg    string          `json:"msg,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

type varRuntimeData struct {
	Value        json.RawMessage `json:"value,omitempty"`
	WriterNodeID string          `json:"writer_node_id,omitempty"`
}

type bindingValidationOptions struct {
	allowLoop bool
}

type graphIndex struct {
	nodes         map[string]node
	ancestors     map[string]map[string]struct{}
	setVarWriters map[string][]string
	incoming      map[string][]edge
	outgoing      map[string][]edge
}

func normalizeNodeKind(kind nodeKind) nodeKind {
	switch nodeKind(strings.ToLower(strings.TrimSpace(string(kind)))) {
	case nodeKindCall:
		return nodeKindCall
	case nodeKindCompose:
		return nodeKindCompose
	case nodeKindTransform:
		return nodeKindTransform
	case nodeKindSetVar:
		return nodeKindSetVar
	case nodeKindBranch:
		return nodeKindBranch
	case nodeKindForeach:
		return nodeKindForeach
	case nodeKindSubflow:
		return nodeKindSubflow
	default:
		return nodeKind(strings.ToLower(strings.TrimSpace(string(kind))))
	}
}

func normalizeBindingSourceKind(kind bindingSourceKind) bindingSourceKind {
	switch bindingSourceKind(strings.ToLower(strings.TrimSpace(string(kind)))) {
	case bindingSourceNodeResult:
		return bindingSourceNodeResult
	case bindingSourceTrigger:
		return bindingSourceTrigger
	case bindingSourceFlowMeta:
		return bindingSourceFlowMeta
	case bindingSourceRunMeta:
		return bindingSourceRunMeta
	case bindingSourceLoopItem:
		return bindingSourceLoopItem
	case bindingSourceLoopIndex:
		return bindingSourceLoopIndex
	case bindingSourceFlowVar:
		return bindingSourceFlowVar
	default:
		return bindingSourceKind(strings.ToLower(strings.TrimSpace(string(kind))))
	}
}

func decodeJSONStrict(raw json.RawMessage, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if err := dec.Decode(new(struct{})); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing json content")
		}
		return err
	}
	return nil
}

func newRunContext(flowID, runID string, executorNode uint32, triggerCtx json.RawMessage) runContext {
	return runContext{
		FlowID:       strings.TrimSpace(flowID),
		RunID:        strings.TrimSpace(runID),
		ExecutorNode: executorNode,
		Trigger:      normalizeTriggerContext(triggerCtx),
		Nodes:        make(map[string]nodeRuntimeData),
		Vars:         make(map[string]varRuntimeData),
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

func buildCronTriggerContext(now time.Time, expr string) json.RawMessage {
	return mustJSON(map[string]any{
		"type":         triggerTypeCron,
		"cron":         strings.TrimSpace(expr),
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

func buildSubflowTriggerContext(parent *runState, nodeID string, input json.RawMessage) json.RawMessage {
	payload := map[string]any{
		"type":    triggerTypeSubflow,
		"node_id": strings.TrimSpace(nodeID),
	}
	if parent != nil {
		parent.mu.Lock()
		payload["parent_flow_id"] = parent.runtime.FlowID
		payload["parent_run_id"] = parent.runtime.RunID
		parent.mu.Unlock()
	}
	if len(bytes.TrimSpace(input)) != 0 {
		var value any
		if err := json.Unmarshal(input, &value); err == nil {
			payload["input"] = value
		}
	}
	return mustJSON(payload)
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

func (s *runState) setVarRuntimeLocked(name string, data varRuntimeData) {
	if s == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if s.runtime.Vars == nil {
		s.runtime.Vars = make(map[string]varRuntimeData)
	}
	data.Value = cloneRawJSON(data.Value)
	data.WriterNodeID = strings.TrimSpace(data.WriterNodeID)
	s.runtime.Vars[name] = data
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
	kind := normalizeBindingSourceKind(src.Kind)
	switch kind {
	case bindingSourceNodeResult:
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
	case bindingSourceTrigger:
		s.mu.Lock()
		raw := cloneRawJSON(s.runtime.Trigger)
		s.mu.Unlock()
		return readJSONSourceValue(raw, src.Path)
	case bindingSourceFlowMeta:
		field := strings.TrimSpace(src.Field)
		if field != "flow_id" {
			return nil, false, fmt.Errorf("unsupported flow_meta field: %s", field)
		}
		s.mu.Lock()
		flowID := s.runtime.FlowID
		s.mu.Unlock()
		return flowID, true, nil
	case bindingSourceRunMeta:
		field := strings.TrimSpace(src.Field)
		if field != "run_id" {
			return nil, false, fmt.Errorf("unsupported run_meta field: %s", field)
		}
		s.mu.Lock()
		runID := s.runtime.RunID
		s.mu.Unlock()
		return runID, true, nil
	case bindingSourceFlowVar:
		name := strings.TrimSpace(src.Name)
		if name == "" {
			return nil, false, errors.New("flow_var name required")
		}
		s.mu.Lock()
		varData, ok := s.runtime.Vars[name]
		raw := cloneRawJSON(varData.Value)
		s.mu.Unlock()
		if !ok || len(raw) == 0 {
			return nil, false, nil
		}
		return readJSONSourceValue(raw, src.Path)
	case bindingSourceLoopItem:
		s.mu.Lock()
		loop := s.runtime.Loop
		var raw json.RawMessage
		if loop != nil {
			raw = cloneRawJSON(loop.Item)
		}
		s.mu.Unlock()
		if len(raw) == 0 {
			return nil, false, nil
		}
		return readJSONSourceValue(raw, src.Path)
	case bindingSourceLoopIndex:
		s.mu.Lock()
		loop := s.runtime.Loop
		s.mu.Unlock()
		if loop == nil {
			return nil, false, nil
		}
		return loop.Index, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported binding source kind: %s", string(kind))
	}
}

func materializeCallArgs(nodeID string, spec callSpec, state *runState) (json.RawMessage, error) {
	return materializeBoundJSON(nodeID, "call args", spec.ArgsTemplate, spec.Inputs, state)
}

func materializeComposeResult(nodeID string, spec composeSpec, state *runState) (json.RawMessage, error) {
	return materializeBoundJSON(nodeID, "compose template", spec.Template, spec.Inputs, state)
}

func materializeSetVarValue(nodeID string, spec setVarSpec, state *runState) (json.RawMessage, error) {
	return materializeBoundJSONWithNormalizer(nodeID, "set_var template", spec.Template, spec.Inputs, state, normalizeSetVarTemplateJSON)
}

func materializeTransformResult(nodeID string, spec transformSpec, state *runState) (json.RawMessage, error) {
	value, err := evaluateTransformExpr("expr", spec.Expr, state)
	if err != nil {
		return nil, fmt.Errorf("node %s transform %w", nodeID, err)
	}
	out, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("node %s invalid transform result json", nodeID)
	}
	return out, nil
}

func materializeSubflowInput(nodeID string, spec subflowSpec, state *runState) (json.RawMessage, error) {
	return materializeBoundJSON(nodeID, "subflow input_template", spec.InputTemplate, spec.Inputs, state)
}

func materializeBoundJSON(nodeID, label string, base json.RawMessage, bindings []inputBinding, state *runState) (json.RawMessage, error) {
	return materializeBoundJSONWithNormalizer(nodeID, label, base, bindings, state, normalizeTemplateJSON)
}

func materializeBoundJSONWithNormalizer(nodeID, label string, base json.RawMessage, bindings []inputBinding, state *runState, normalize func(json.RawMessage) (json.RawMessage, error)) (json.RawMessage, error) {
	if normalize == nil {
		normalize = normalizeTemplateJSON
	}
	normalized, err := normalize(base)
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
	return normalizeJSONValue(trimmed)
}

func normalizeSetVarTemplateJSON(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return json.RawMessage(`null`), nil
	}
	return normalizeJSONValue(trimmed)
}

func normalizeJSONValue(raw json.RawMessage) (json.RawMessage, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out, _ := json.Marshal(doc)
	return out, nil
}

func validateTransformExprShape(nodeID, label string, expr transformExpr) error {
	literalSet, sourceSet, opSet, objectSet, arraySet := transformExprVariantFlags(expr)
	if expr.Required != nil && !sourceSet {
		return fmt.Errorf("node %s %s required only allowed with source", nodeID, label)
	}
	count := 0
	for _, set := range []bool{literalSet, sourceSet, opSet, objectSet, arraySet} {
		if set {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("node %s %s must define exactly one of literal, source, op, object or array", nodeID, label)
	}
	switch {
	case literalSet:
		if _, err := normalizeJSONValue(expr.Literal); err != nil {
			return fmt.Errorf("node %s %s invalid literal", nodeID, label)
		}
	case sourceSet:
		return nil
	case opSet:
		rawOp := strings.TrimSpace(expr.Op)
		op := normalizeTransformOp(rawOp)
		if op == "" {
			if rawOp == "" {
				return fmt.Errorf("node %s %s op required", nodeID, label)
			}
			return fmt.Errorf("node %s %s op unsupported", nodeID, label)
		}
		if err := validateTransformOpArity(op, len(expr.Args)); err != nil {
			return fmt.Errorf("node %s %s %w", nodeID, label, err)
		}
		for i := range expr.Args {
			if err := validateTransformExprShape(nodeID, fmt.Sprintf("%s.args[%d]", label, i), expr.Args[i]); err != nil {
				return err
			}
		}
	case objectSet:
		for _, key := range sortedTransformObjectKeys(expr.Object) {
			if err := validateTransformExprShape(nodeID, fmt.Sprintf("%s.object[%q]", label, key), expr.Object[key]); err != nil {
				return err
			}
		}
	case arraySet:
		for i := range expr.Array {
			if err := validateTransformExprShape(nodeID, fmt.Sprintf("%s.array[%d]", label, i), expr.Array[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateTransformExprSourcesForSet(nodeID, label string, expr transformExpr, idx *graphIndex, opts bindingValidationOptions) error {
	_, sourceSet, opSet, objectSet, arraySet := transformExprVariantFlags(expr)
	switch {
	case sourceSet:
		return validateBindingSourceForSetLabel(nodeID, label, *expr.Source, idx, opts)
	case opSet:
		for i := range expr.Args {
			if err := validateTransformExprSourcesForSet(nodeID, fmt.Sprintf("%s.args[%d]", label, i), expr.Args[i], idx, opts); err != nil {
				return err
			}
		}
	case objectSet:
		for _, key := range sortedTransformObjectKeys(expr.Object) {
			if err := validateTransformExprSourcesForSet(nodeID, fmt.Sprintf("%s.object[%q]", label, key), expr.Object[key], idx, opts); err != nil {
				return err
			}
		}
	case arraySet:
		for i := range expr.Array {
			if err := validateTransformExprSourcesForSet(nodeID, fmt.Sprintf("%s.array[%d]", label, i), expr.Array[i], idx, opts); err != nil {
				return err
			}
		}
	}
	return nil
}

func transformExprVariantFlags(expr transformExpr) (literalSet, sourceSet, opSet, objectSet, arraySet bool) {
	literalSet = len(bytes.TrimSpace(expr.Literal)) != 0
	sourceSet = expr.Source != nil
	opSet = strings.TrimSpace(expr.Op) != "" || expr.Args != nil
	objectSet = expr.Object != nil
	arraySet = expr.Array != nil
	return literalSet, sourceSet, opSet, objectSet, arraySet
}

func sortedTransformObjectKeys(values map[string]transformExpr) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func validateTransformOpArity(op string, argc int) error {
	switch op {
	case "add", "mul", "min", "max", "concat", "and", "or", "coalesce":
		if argc < 1 {
			return fmt.Errorf("%s requires at least 1 arg", op)
		}
	case "sub", "div", "mod":
		if argc < 2 {
			return fmt.Errorf("%s requires at least 2 args", op)
		}
	case "eq", "ne", "gt", "gte", "lt", "lte":
		if argc != 2 {
			return fmt.Errorf("%s requires exactly 2 args", op)
		}
	case "neg", "abs", "not", "lower", "upper", "trim", "len":
		if argc != 1 {
			return fmt.Errorf("%s requires exactly 1 arg", op)
		}
	case "if":
		if argc != 3 {
			return errors.New("if requires exactly 3 args")
		}
	default:
		return fmt.Errorf("op unsupported: %s", op)
	}
	return nil
}

func normalizeTransformOp(op string) string {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "add":
		return "add"
	case "sub":
		return "sub"
	case "mul":
		return "mul"
	case "div":
		return "div"
	case "mod":
		return "mod"
	case "neg":
		return "neg"
	case "abs":
		return "abs"
	case "min":
		return "min"
	case "max":
		return "max"
	case "eq":
		return "eq"
	case "ne":
		return "ne"
	case "gt":
		return "gt"
	case "gte":
		return "gte"
	case "lt":
		return "lt"
	case "lte":
		return "lte"
	case "and":
		return "and"
	case "or":
		return "or"
	case "not":
		return "not"
	case "coalesce":
		return "coalesce"
	case "if":
		return "if"
	case "concat":
		return "concat"
	case "lower":
		return "lower"
	case "upper":
		return "upper"
	case "trim":
		return "trim"
	case "len":
		return "len"
	default:
		return ""
	}
}

func evaluateTransformExpr(label string, expr transformExpr, state *runState) (any, error) {
	literalSet, sourceSet, opSet, objectSet, arraySet := transformExprVariantFlags(expr)
	if expr.Required != nil && !sourceSet {
		return nil, fmt.Errorf("%s required only allowed with source", label)
	}
	count := 0
	for _, set := range []bool{literalSet, sourceSet, opSet, objectSet, arraySet} {
		if set {
			count++
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("%s must define exactly one of literal, source, op, object or array", label)
	}
	switch {
	case literalSet:
		var value any
		if err := json.Unmarshal(expr.Literal, &value); err != nil {
			return nil, fmt.Errorf("%s invalid literal", label)
		}
		return value, nil
	case sourceSet:
		required := true
		if expr.Required != nil {
			required = *expr.Required
		}
		if state == nil {
			return nil, fmt.Errorf("%s run context required", label)
		}
		value, found, err := state.resolveBindingSource(*expr.Source)
		if err != nil {
			return nil, fmt.Errorf("%s %w", label, err)
		}
		if !found {
			if required {
				return nil, fmt.Errorf("%s required source missing", label)
			}
			return nil, nil
		}
		return value, nil
	case opSet:
		op := normalizeTransformOp(expr.Op)
		if op == "" {
			return nil, fmt.Errorf("%s op unsupported", label)
		}
		if err := validateTransformOpArity(op, len(expr.Args)); err != nil {
			return nil, fmt.Errorf("%s %w", label, err)
		}
		args := make([]any, 0, len(expr.Args))
		for i := range expr.Args {
			value, err := evaluateTransformExpr(fmt.Sprintf("%s.args[%d]", label, i), expr.Args[i], state)
			if err != nil {
				return nil, err
			}
			args = append(args, value)
		}
		return evaluateTransformOp(label, op, args)
	case objectSet:
		out := make(map[string]any, len(expr.Object))
		for _, key := range sortedTransformObjectKeys(expr.Object) {
			value, err := evaluateTransformExpr(fmt.Sprintf("%s.object[%q]", label, key), expr.Object[key], state)
			if err != nil {
				return nil, err
			}
			out[key] = value
		}
		return out, nil
	case arraySet:
		out := make([]any, 0, len(expr.Array))
		for i := range expr.Array {
			value, err := evaluateTransformExpr(fmt.Sprintf("%s.array[%d]", label, i), expr.Array[i], state)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s invalid transform expression", label)
	}
}

func evaluateTransformOp(label, op string, args []any) (any, error) {
	switch op {
	case "add":
		total := 0.0
		for i := range args {
			value, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, i), args[i])
			if err != nil {
				return nil, err
			}
			total += value
		}
		return total, nil
	case "sub":
		value, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, 0), args[0])
		if err != nil {
			return nil, err
		}
		for i := 1; i < len(args); i++ {
			next, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, i), args[i])
			if err != nil {
				return nil, err
			}
			value -= next
		}
		return value, nil
	case "mul":
		value := 1.0
		for i := range args {
			next, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, i), args[i])
			if err != nil {
				return nil, err
			}
			value *= next
		}
		return value, nil
	case "div":
		value, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, 0), args[0])
		if err != nil {
			return nil, err
		}
		for i := 1; i < len(args); i++ {
			next, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, i), args[i])
			if err != nil {
				return nil, err
			}
			if next == 0 {
				return nil, fmt.Errorf("%s divide by zero", label)
			}
			value /= next
		}
		return value, nil
	case "mod":
		value, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, 0), args[0])
		if err != nil {
			return nil, err
		}
		for i := 1; i < len(args); i++ {
			next, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, i), args[i])
			if err != nil {
				return nil, err
			}
			if next == 0 {
				return nil, fmt.Errorf("%s divide by zero", label)
			}
			value = math.Mod(value, next)
		}
		return value, nil
	case "neg":
		value, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, 0), args[0])
		if err != nil {
			return nil, err
		}
		return -value, nil
	case "abs":
		value, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, 0), args[0])
		if err != nil {
			return nil, err
		}
		return math.Abs(value), nil
	case "min":
		value, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, 0), args[0])
		if err != nil {
			return nil, err
		}
		for i := 1; i < len(args); i++ {
			next, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, i), args[i])
			if err != nil {
				return nil, err
			}
			if next < value {
				value = next
			}
		}
		return value, nil
	case "max":
		value, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, 0), args[0])
		if err != nil {
			return nil, err
		}
		for i := 1; i < len(args); i++ {
			next, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, i), args[i])
			if err != nil {
				return nil, err
			}
			if next > value {
				value = next
			}
		}
		return value, nil
	case "eq":
		return jsonValuesEqualAny(args[0], args[1])
	case "ne":
		ok, err := jsonValuesEqualAny(args[0], args[1])
		if err != nil {
			return nil, err
		}
		return !ok, nil
	case "gt", "gte", "lt", "lte":
		left, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, 0), args[0])
		if err != nil {
			return nil, err
		}
		right, err := transformNumberArg(fmt.Sprintf("%s.args[%d]", label, 1), args[1])
		if err != nil {
			return nil, err
		}
		switch op {
		case "gt":
			return left > right, nil
		case "gte":
			return left >= right, nil
		case "lt":
			return left < right, nil
		case "lte":
			return left <= right, nil
		}
	case "and":
		for i := range args {
			value, err := transformBoolArg(fmt.Sprintf("%s.args[%d]", label, i), args[i])
			if err != nil {
				return nil, err
			}
			if !value {
				return false, nil
			}
		}
		return true, nil
	case "or":
		for i := range args {
			value, err := transformBoolArg(fmt.Sprintf("%s.args[%d]", label, i), args[i])
			if err != nil {
				return nil, err
			}
			if value {
				return true, nil
			}
		}
		return false, nil
	case "not":
		value, err := transformBoolArg(fmt.Sprintf("%s.args[%d]", label, 0), args[0])
		if err != nil {
			return nil, err
		}
		return !value, nil
	case "coalesce":
		for _, value := range args {
			if value != nil {
				return value, nil
			}
		}
		return nil, nil
	case "if":
		cond, err := transformBoolArg(fmt.Sprintf("%s.args[%d]", label, 0), args[0])
		if err != nil {
			return nil, err
		}
		if cond {
			return args[1], nil
		}
		return args[2], nil
	case "concat":
		var b strings.Builder
		for _, value := range args {
			part, err := transformValueString(value)
			if err != nil {
				return nil, fmt.Errorf("%s concat %w", label, err)
			}
			b.WriteString(part)
		}
		return b.String(), nil
	case "lower":
		value, err := transformStringArg(fmt.Sprintf("%s.args[%d]", label, 0), args[0])
		if err != nil {
			return nil, err
		}
		return strings.ToLower(value), nil
	case "upper":
		value, err := transformStringArg(fmt.Sprintf("%s.args[%d]", label, 0), args[0])
		if err != nil {
			return nil, err
		}
		return strings.ToUpper(value), nil
	case "trim":
		value, err := transformStringArg(fmt.Sprintf("%s.args[%d]", label, 0), args[0])
		if err != nil {
			return nil, err
		}
		return strings.TrimSpace(value), nil
	case "len":
		switch typed := args[0].(type) {
		case string:
			return len(typed), nil
		case []any:
			return len(typed), nil
		case map[string]any:
			return len(typed), nil
		default:
			return nil, fmt.Errorf("%s.args[0] requires string, array or object", label)
		}
	}
	return nil, fmt.Errorf("%s op unsupported", label)
}

func transformNumberArg(label string, value any) (float64, error) {
	number, err := jsonNumberValue(value)
	if err != nil {
		return 0, fmt.Errorf("%s requires number", label)
	}
	return number, nil
}

func transformBoolArg(label string, value any) (bool, error) {
	typed, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s requires bool", label)
	}
	return typed, nil
}

func transformStringArg(label string, value any) (string, error) {
	typed, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s requires string", label)
	}
	return typed, nil
}

func transformValueString(value any) (string, error) {
	if value == nil {
		return "null", nil
	}
	if typed, ok := value.(string); ok {
		return typed, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func validateCallSpecForSet(nodeID string, spec callSpec, idx *graphIndex, opts bindingValidationOptions) error {
	if strings.TrimSpace(spec.Method) == "" {
		return fmt.Errorf("node %s call method required", nodeID)
	}
	if _, err := normalizeTemplateJSON(spec.ArgsTemplate); err != nil {
		return fmt.Errorf("node %s invalid call args template", nodeID)
	}
	return validateBindings(nodeID, spec.Inputs, idx, opts)
}

func validateComposeSpecForSet(nodeID string, spec composeSpec, idx *graphIndex, opts bindingValidationOptions) error {
	if len(bytes.TrimSpace(spec.Template)) == 0 {
		return fmt.Errorf("node %s compose template required", nodeID)
	}
	if _, err := normalizeTemplateJSON(spec.Template); err != nil {
		return fmt.Errorf("node %s invalid compose template", nodeID)
	}
	return validateBindings(nodeID, spec.Inputs, idx, opts)
}

func validateSetVarSpecForSet(nodeID string, spec setVarSpec, idx *graphIndex, opts bindingValidationOptions) error {
	spec.Name = strings.TrimSpace(spec.Name)
	if !isValidSetVarName(spec.Name) {
		return fmt.Errorf("node %s invalid set_var name", nodeID)
	}
	if _, err := normalizeSetVarTemplateJSON(spec.Template); err != nil {
		return fmt.Errorf("node %s invalid set_var template", nodeID)
	}
	return validateBindings(nodeID, spec.Inputs, idx, opts)
}

func validateTransformSpecForSet(nodeID string, spec transformSpec, idx *graphIndex, opts bindingValidationOptions) error {
	if err := validateTransformExprShape(nodeID, "expr", spec.Expr); err != nil {
		return err
	}
	return validateTransformExprSourcesForSet(nodeID, "expr", spec.Expr, idx, opts)
}

func validateBranchSpecForSet(nodeID string, spec branchSpec, idx *graphIndex, opts bindingValidationOptions) error {
	seen := make(map[string]struct{}, len(spec.Cases))
	for i, candidate := range spec.Cases {
		name := strings.TrimSpace(candidate.Name)
		if name == "" {
			return fmt.Errorf("node %s branch case %d name required", nodeID, i)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("node %s duplicate branch case %q", nodeID, name)
		}
		seen[name] = struct{}{}
		if err := validateBindingSourceForSet(nodeID, i, candidate.Match.Source, idx, opts); err != nil {
			return err
		}
		op := normalizeBranchMatchOp(candidate.Match.Op)
		if op == "" {
			return fmt.Errorf("node %s branch case %q invalid match op", nodeID, name)
		}
		if op != branchMatchExists {
			if len(bytes.TrimSpace(candidate.Match.Value)) == 0 {
				return fmt.Errorf("node %s branch case %q match value required", nodeID, name)
			}
			if _, err := normalizeJSONValue(candidate.Match.Value); err != nil {
				return fmt.Errorf("node %s branch case %q invalid match value", nodeID, name)
			}
		}
	}
	return nil
}

func validateForeachSpecForSet(flowID, nodeID string, spec foreachSpec, idx *graphIndex, opts bindingValidationOptions) error {
	if err := validateBindingSourceForSet(nodeID, 0, spec.Source, idx, opts); err != nil {
		return err
	}
	if strings.TrimSpace(spec.ResultNodeID) == "" {
		return fmt.Errorf("node %s foreach result_node_id required", nodeID)
	}
	if err := validateGraphScoped(flowID, spec.Body, bindingValidationOptions{allowLoop: true}); err != nil {
		return fmt.Errorf("node %s foreach body %w", nodeID, err)
	}
	bodyIdx, err := buildGraphIndex(spec.Body)
	if err != nil {
		return fmt.Errorf("node %s foreach body %w", nodeID, err)
	}
	if !bodyIdx.hasNode(spec.ResultNodeID) {
		return fmt.Errorf("node %s foreach result_node_id %q not found", nodeID, spec.ResultNodeID)
	}
	return nil
}

func validateSubflowSpecForSet(flowID, nodeID string, spec subflowSpec, idx *graphIndex, opts bindingValidationOptions) error {
	if flowID != "" && strings.EqualFold(flowID, spec.FlowID) {
		return fmt.Errorf("node %s subflow cannot call itself", nodeID)
	}
	if _, err := normalizeTemplateJSON(spec.InputTemplate); err != nil {
		return fmt.Errorf("node %s invalid subflow input_template", nodeID)
	}
	if err := validateBindings(nodeID, spec.Inputs, idx, opts); err != nil {
		return err
	}
	return nil
}

func validateBindings(nodeID string, bindings []inputBinding, idx *graphIndex, opts bindingValidationOptions) error {
	for i, binding := range bindings {
		if _, err := parseJSONPointer(binding.To); err != nil {
			return fmt.Errorf("node %s input %d invalid to pointer", nodeID, i)
		}
		if err := validateBindingSourceForSet(nodeID, i, binding.Source, idx, opts); err != nil {
			return err
		}
	}
	return nil
}

func validateBindingSourceForSet(nodeID string, inputIndex int, src bindingSource, idx *graphIndex, opts bindingValidationOptions) error {
	return validateBindingSourceForSetLabel(nodeID, fmt.Sprintf("input %d", inputIndex), src, idx, opts)
}

func validateBindingSourceForSetLabel(nodeID, label string, src bindingSource, idx *graphIndex, opts bindingValidationOptions) error {
	kind := normalizeBindingSourceKind(src.Kind)
	switch kind {
	case bindingSourceNodeResult:
		refNodeID := strings.TrimSpace(src.NodeID)
		if refNodeID == "" {
			return fmt.Errorf("node %s %s node_result node_id required", nodeID, label)
		}
		if idx == nil || !idx.hasNode(refNodeID) {
			return fmt.Errorf("node %s %s references unknown node %s", nodeID, label, refNodeID)
		}
		if !idx.isAncestor(refNodeID, nodeID) {
			return fmt.Errorf("node %s %s node_result must reference ancestor", nodeID, label)
		}
		if _, err := parseJSONPointer(src.Path); err != nil {
			return fmt.Errorf("node %s %s invalid node_result path", nodeID, label)
		}
	case bindingSourceTrigger:
		if _, err := parseJSONPointer(src.Path); err != nil {
			return fmt.Errorf("node %s %s invalid trigger path", nodeID, label)
		}
	case bindingSourceFlowMeta:
		if strings.TrimSpace(src.Field) != "flow_id" {
			return fmt.Errorf("node %s %s invalid flow_meta field", nodeID, label)
		}
	case bindingSourceRunMeta:
		if strings.TrimSpace(src.Field) != "run_id" {
			return fmt.Errorf("node %s %s invalid run_meta field", nodeID, label)
		}
	case bindingSourceFlowVar:
		name := strings.TrimSpace(src.Name)
		if !isValidSetVarName(name) {
			return fmt.Errorf("node %s %s invalid flow_var name", nodeID, label)
		}
		if idx == nil {
			return fmt.Errorf("node %s %s flow_var requires graph index", nodeID, label)
		}
		if _, err := idx.uniqueSetVarWriter(nodeID, name); err != nil {
			return fmt.Errorf("node %s %s %w", nodeID, label, err)
		}
		if _, err := parseJSONPointer(src.Path); err != nil {
			return fmt.Errorf("node %s %s invalid flow_var path", nodeID, label)
		}
	case bindingSourceLoopItem:
		if !opts.allowLoop {
			return fmt.Errorf("node %s %s loop_item only allowed in foreach body", nodeID, label)
		}
		if _, err := parseJSONPointer(src.Path); err != nil {
			return fmt.Errorf("node %s %s invalid loop_item path", nodeID, label)
		}
	case bindingSourceLoopIndex:
		if !opts.allowLoop {
			return fmt.Errorf("node %s %s loop_index only allowed in foreach body", nodeID, label)
		}
		if strings.TrimSpace(src.Path) != "" {
			return fmt.Errorf("node %s %s loop_index does not support path", nodeID, label)
		}
	default:
		return fmt.Errorf("node %s %s invalid source kind", nodeID, label)
	}
	return nil
}

func buildGraphIndex(g graph) (*graphIndex, error) {
	order, err := topoOrder(g)
	if err != nil {
		return nil, err
	}
	idx := &graphIndex{
		nodes:         make(map[string]node, len(g.Nodes)),
		ancestors:     make(map[string]map[string]struct{}, len(g.Nodes)),
		setVarWriters: make(map[string][]string),
		incoming:      make(map[string][]edge, len(g.Nodes)),
		outgoing:      make(map[string][]edge, len(g.Nodes)),
	}
	parents := make(map[string][]string, len(g.Nodes))
	for _, n := range g.Nodes {
		id := strings.TrimSpace(n.ID)
		idx.nodes[id] = n
		idx.ancestors[id] = make(map[string]struct{})
	}
	for _, e := range g.Edges {
		from := strings.TrimSpace(e.From)
		to := strings.TrimSpace(e.To)
		parents[to] = append(parents[to], from)
		idx.incoming[to] = append(idx.incoming[to], e)
		idx.outgoing[from] = append(idx.outgoing[from], e)
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

func (idx *graphIndex) node(nodeID string) (node, bool) {
	if idx == nil {
		return node{}, false
	}
	n, ok := idx.nodes[strings.TrimSpace(nodeID)]
	return n, ok
}

func (idx *graphIndex) incomingEdges(nodeID string) []edge {
	if idx == nil {
		return nil
	}
	return append([]edge(nil), idx.incoming[strings.TrimSpace(nodeID)]...)
}

func (idx *graphIndex) outgoingEdges(nodeID string) []edge {
	if idx == nil {
		return nil
	}
	return append([]edge(nil), idx.outgoing[strings.TrimSpace(nodeID)]...)
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

func (idx *graphIndex) addSetVarWriter(name, nodeID string) {
	if idx == nil {
		return
	}
	name = strings.TrimSpace(name)
	nodeID = strings.TrimSpace(nodeID)
	if name == "" || nodeID == "" {
		return
	}
	idx.setVarWriters[name] = append(idx.setVarWriters[name], nodeID)
}

func (idx *graphIndex) uniqueSetVarWriter(nodeID, name string) (string, error) {
	if idx == nil {
		return "", errors.New("graph index required")
	}
	nodeID = strings.TrimSpace(nodeID)
	name = strings.TrimSpace(name)
	if nodeID == "" || name == "" {
		return "", errors.New("flow_var name required")
	}
	candidates := make([]string, 0, len(idx.setVarWriters[name]))
	for _, writerNodeID := range idx.setVarWriters[name] {
		if idx.isAncestor(writerNodeID, nodeID) {
			candidates = append(candidates, writerNodeID)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("flow_var %q has no ancestor writer", name)
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		switch {
		case idx.isAncestor(best, candidate):
			best = candidate
		case idx.isAncestor(candidate, best):
		default:
			sort.Strings(candidates)
			return "", fmt.Errorf("flow_var %q has ambiguous ancestor writers: %s", name, strings.Join(candidates, ", "))
		}
	}
	return best, nil
}

func collectSetVarWriters(g graph, idx *graphIndex) error {
	if idx == nil {
		return errors.New("graph index required")
	}
	for _, n := range g.Nodes {
		if normalizeNodeKind(n.Kind) != nodeKindSetVar {
			continue
		}
		spec, err := decodeNodeSetVarSpec(n)
		if err != nil {
			return fmt.Errorf("node %s %w", strings.TrimSpace(n.ID), err)
		}
		idx.addSetVarWriter(spec.Name, strings.TrimSpace(n.ID))
	}
	return nil
}

func decodeNodeComposeSpec(n node) (composeSpec, error) {
	var spec composeSpec
	if err := decodeJSONStrict(n.Spec, &spec); err != nil {
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

func decodeNodeSetVarSpec(n node) (setVarSpec, error) {
	var spec setVarSpec
	if err := decodeJSONStrict(n.Spec, &spec); err != nil {
		return setVarSpec{}, errors.New("invalid set_var spec")
	}
	spec.Name = strings.TrimSpace(spec.Name)
	if !isValidSetVarName(spec.Name) {
		return setVarSpec{}, errors.New("invalid set_var name")
	}
	if _, err := normalizeSetVarTemplateJSON(spec.Template); err != nil {
		return setVarSpec{}, errors.New("invalid set_var template")
	}
	return spec, nil
}

func decodeNodeTransformSpec(n node) (transformSpec, error) {
	var spec transformSpec
	if err := decodeJSONStrict(n.Spec, &spec); err != nil {
		return transformSpec{}, errors.New("invalid transform spec")
	}
	if err := validateTransformExprShape(strings.TrimSpace(n.ID), "expr", spec.Expr); err != nil {
		return transformSpec{}, err
	}
	return spec, nil
}

func decodeNodeBranchSpec(n node) (branchSpec, error) {
	var spec branchSpec
	if err := decodeJSONStrict(n.Spec, &spec); err != nil {
		return branchSpec{}, errors.New("invalid branch spec")
	}
	if len(spec.Cases) == 0 {
		return branchSpec{}, errors.New("branch cases required")
	}
	seen := make(map[string]struct{}, len(spec.Cases))
	for i := range spec.Cases {
		spec.Cases[i].Name = strings.TrimSpace(spec.Cases[i].Name)
		if spec.Cases[i].Name == "" {
			return branchSpec{}, errors.New("branch case name required")
		}
		if _, ok := seen[spec.Cases[i].Name]; ok {
			return branchSpec{}, fmt.Errorf("duplicate branch case %q", spec.Cases[i].Name)
		}
		seen[spec.Cases[i].Name] = struct{}{}
		spec.Cases[i].Match.Op = normalizeBranchMatchOp(spec.Cases[i].Match.Op)
		if spec.Cases[i].Match.Op == "" {
			return branchSpec{}, fmt.Errorf("branch case %q match op unsupported", spec.Cases[i].Name)
		}
		if spec.Cases[i].Match.Op != branchMatchExists {
			if len(bytes.TrimSpace(spec.Cases[i].Match.Value)) == 0 {
				return branchSpec{}, fmt.Errorf("branch case %q match value required", spec.Cases[i].Name)
			}
			normalized, err := normalizeJSONValue(spec.Cases[i].Match.Value)
			if err != nil {
				return branchSpec{}, fmt.Errorf("branch case %q invalid match value", spec.Cases[i].Name)
			}
			spec.Cases[i].Match.Value = normalized
		}
	}
	spec.DefaultCase = strings.TrimSpace(spec.DefaultCase)
	return spec, nil
}

func decodeNodeForeachSpec(n node) (foreachSpec, error) {
	var spec foreachSpec
	if err := decodeJSONStrict(n.Spec, &spec); err != nil {
		return foreachSpec{}, errors.New("invalid foreach spec")
	}
	spec.ResultNodeID = strings.TrimSpace(spec.ResultNodeID)
	if spec.ResultNodeID == "" {
		return foreachSpec{}, errors.New("foreach result_node_id required")
	}
	if len(spec.Body.Nodes) == 0 {
		return foreachSpec{}, errors.New("foreach body required")
	}
	return spec, nil
}

func decodeNodeSubflowSpec(n node) (subflowSpec, error) {
	var spec subflowSpec
	if err := decodeJSONStrict(n.Spec, &spec); err != nil {
		return subflowSpec{}, errors.New("invalid subflow spec")
	}
	var err error
	spec.FlowID, err = validateFlowID(spec.FlowID)
	if err != nil {
		return subflowSpec{}, err
	}
	if _, err := normalizeTemplateJSON(spec.InputTemplate); err != nil {
		return subflowSpec{}, errors.New("invalid subflow input_template")
	}
	spec.ResultNodeID = strings.TrimSpace(spec.ResultNodeID)
	return spec, nil
}

func normalizeBranchMatchOp(op branchMatchOp) branchMatchOp {
	switch branchMatchOp(strings.ToLower(strings.TrimSpace(string(op)))) {
	case branchMatchEq:
		return branchMatchEq
	case branchMatchNe:
		return branchMatchNe
	case branchMatchGt:
		return branchMatchGt
	case branchMatchGte:
		return branchMatchGte
	case branchMatchLt:
		return branchMatchLt
	case branchMatchLte:
		return branchMatchLte
	case branchMatchExists:
		return branchMatchExists
	default:
		return ""
	}
}

func evaluateBranchCases(spec branchSpec, state *runState) (string, error) {
	for _, candidate := range spec.Cases {
		ok, err := evaluateBranchMatch(candidate.Match, state)
		if err != nil {
			return "", err
		}
		if ok {
			return candidate.Name, nil
		}
	}
	if spec.DefaultCase != "" {
		return spec.DefaultCase, nil
	}
	return "", errors.New("branch no case matched")
}

func readSelectedBranchCase(raw json.RawMessage) (string, bool, error) {
	value, found, err := readJSONSourceValue(raw, "/case")
	if err != nil || !found {
		return "", found, err
	}
	name, ok := value.(string)
	if !ok {
		return "", false, errors.New("branch result case must be string")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, errors.New("branch result case required")
	}
	return name, true, nil
}

func evaluateBranchMatch(match branchMatch, state *runState) (bool, error) {
	if state == nil {
		return false, errors.New("branch run context required")
	}
	match.Op = normalizeBranchMatchOp(match.Op)
	value, found, err := state.resolveBindingSource(match.Source)
	if err != nil {
		return false, err
	}
	if match.Op == branchMatchExists {
		return found, nil
	}
	if !found {
		return false, nil
	}
	switch match.Op {
	case branchMatchEq:
		ok, err := jsonValuesEqual(value, match.Value)
		return ok, err
	case branchMatchNe:
		ok, err := jsonValuesEqual(value, match.Value)
		return !ok, err
	case branchMatchGt, branchMatchGte, branchMatchLt, branchMatchLte:
		actual, err := jsonNumberValue(value)
		if err != nil {
			return false, err
		}
		var want float64
		if err := json.Unmarshal(match.Value, &want); err != nil {
			return false, errors.New("branch numeric match value required")
		}
		switch match.Op {
		case branchMatchGt:
			return actual > want, nil
		case branchMatchGte:
			return actual >= want, nil
		case branchMatchLt:
			return actual < want, nil
		case branchMatchLte:
			return actual <= want, nil
		}
	}
	return false, errors.New("branch match op unsupported")
}

func jsonValuesEqual(actual any, expected json.RawMessage) (bool, error) {
	actualRaw, err := json.Marshal(actual)
	if err != nil {
		return false, err
	}
	actualNorm, err := normalizeJSONValue(actualRaw)
	if err != nil {
		return false, err
	}
	expectedNorm, err := normalizeJSONValue(expected)
	if err != nil {
		return false, err
	}
	return bytes.Equal(actualNorm, expectedNorm), nil
}

func jsonValuesEqualAny(left, right any) (bool, error) {
	leftNorm, err := normalizeAnyJSONValue(left)
	if err != nil {
		return false, err
	}
	rightNorm, err := normalizeAnyJSONValue(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftNorm, rightNorm), nil
}

func normalizeAnyJSONValue(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return normalizeJSONValue(raw)
}

func jsonNumberValue(value any) (float64, error) {
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case float32:
		return float64(typed), nil
	case int:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case int32:
		return float64(typed), nil
	case json.Number:
		return typed.Float64()
	default:
		return 0, errors.New("branch numeric comparison requires number")
	}
}

func isValidSetVarName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if i == 0 {
			if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') && ch != '_' {
				return false
			}
			continue
		}
		if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return false
		}
	}
	return true
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
