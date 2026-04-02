package flow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const runArchiveDirName = "_runs"

type archivedRunRef struct {
	flowID string
	runID  string
}

type archivedRunRecord struct {
	FlowID       string     `json:"flow_id"`
	RunID        string     `json:"run_id"`
	Status       string     `json:"status"`
	StartedAtMs  int64      `json:"started_at_ms,omitempty"`
	EndedAtMs    int64      `json:"ended_at_ms,omitempty"`
	CancelReason string     `json:"cancel_reason,omitempty"`
	Runtime      runContext `json:"runtime,omitempty"`
}

func cloneRunContext(rt runContext) runContext {
	out := runContext{
		FlowID:       strings.TrimSpace(rt.FlowID),
		RunID:        strings.TrimSpace(rt.RunID),
		ExecutorNode: rt.ExecutorNode,
		Trigger:      cloneRawJSON(rt.Trigger),
	}
	if len(rt.Nodes) > 0 {
		out.Nodes = make(map[string]nodeRuntimeData, len(rt.Nodes))
		for nodeID, nodeData := range rt.Nodes {
			nodeData.Result = cloneRawJSON(nodeData.Result)
			out.Nodes[nodeID] = nodeData
		}
	}
	if len(rt.Vars) > 0 {
		out.Vars = make(map[string]varRuntimeData, len(rt.Vars))
		for name, varData := range rt.Vars {
			varData.Value = cloneRawJSON(varData.Value)
			out.Vars[name] = varData
		}
	}
	return out
}

func (state *runState) snapshotArchivedRunRecordLocked() archivedRunRecord {
	record := archivedRunRecord{
		FlowID:       strings.TrimSpace(state.flowID),
		RunID:        strings.TrimSpace(state.runID),
		Status:       strings.TrimSpace(state.status),
		CancelReason: strings.TrimSpace(state.cancelReason),
		Runtime:      cloneRunContext(state.runtime),
	}
	if !state.start.IsZero() {
		record.StartedAtMs = state.start.UTC().UnixMilli()
	}
	if !state.end.IsZero() {
		record.EndedAtMs = state.end.UTC().UnixMilli()
	}
	return record
}

func archivedRunStateFromRecord(record archivedRunRecord) (*runState, error) {
	flowID, err := validateFlowID(record.FlowID)
	if err != nil {
		return nil, err
	}
	runID, err := validateRunID(record.RunID)
	if err != nil {
		return nil, err
	}
	state := &runState{
		flowID:       flowID,
		runID:        runID,
		status:       strings.TrimSpace(record.Status),
		cancelReason: strings.TrimSpace(record.CancelReason),
		runtime:      cloneRunContext(record.Runtime),
	}
	if record.StartedAtMs != 0 {
		state.start = time.UnixMilli(record.StartedAtMs).UTC()
	}
	if record.EndedAtMs != 0 {
		state.end = time.UnixMilli(record.EndedAtMs).UTC()
	}
	if state.runtime.FlowID == "" {
		state.runtime.FlowID = flowID
	}
	if state.runtime.RunID == "" {
		state.runtime.RunID = runID
	}
	if state.runtime.Nodes == nil {
		state.runtime.Nodes = make(map[string]nodeRuntimeData)
	}
	if state.runtime.Vars == nil {
		state.runtime.Vars = make(map[string]varRuntimeData)
	}
	return state, nil
}

func runArchiveRootDir(baseDir string) string {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return ""
	}
	return filepath.Join(baseDir, runArchiveDirName)
}

func archivedRunFilePath(baseDir, flowID, runID string) (string, error) {
	root := runArchiveRootDir(baseDir)
	if root == "" {
		return "", errors.New("flow base_dir required")
	}
	validFlowID, err := validateFlowID(flowID)
	if err != nil {
		return "", err
	}
	validRunID, err := validateRunID(runID)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, validFlowID, validRunID+".json"), nil
}

func (h *Handler) finalizeRun(flowID string, state *runState) {
	h.persistArchivedRun(state)
	h.pruneRuns(flowID)
}

func (h *Handler) persistArchivedRun(state *runState) {
	if h == nil || !h.runArchive || state == nil {
		return
	}
	state.mu.Lock()
	if !isTerminalRunStatus(state.status) {
		state.mu.Unlock()
		return
	}
	record := state.snapshotArchivedRunRecordLocked()
	state.mu.Unlock()

	path, err := archivedRunFilePath(h.baseDir, record.FlowID, record.RunID)
	if err != nil {
		h.log.Warn("flow archive path invalid", "flow_id", record.FlowID, "run_id", record.RunID, "err", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.log.Warn("flow archive mkdir failed", "path", filepath.Dir(path), "err", err)
		return
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		h.log.Warn("flow archive marshal failed", "flow_id", record.FlowID, "run_id", record.RunID, "err", err)
		return
	}
	if err := writeFileAtomic(path, raw, 0o644); err != nil {
		h.log.Warn("flow archive write failed", "path", path, "err", err)
	}
}

func (h *Handler) removeArchivedRuns(refs []archivedRunRef) {
	if h == nil || !h.runArchive || len(refs) == 0 {
		return
	}
	for _, ref := range refs {
		path, err := archivedRunFilePath(h.baseDir, ref.flowID, ref.runID)
		if err != nil {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			h.log.Warn("flow archive delete failed", "path", path, "err", err)
			continue
		}
		_ = os.Remove(filepath.Dir(path))
	}
}

func (h *Handler) loadArchivedRunsFromDisk() error {
	if h == nil || !h.runArchive {
		return nil
	}
	root := runArchiveRootDir(h.baseDir)
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	loaded := make(map[string][]*runState)
	for _, flowEntry := range entries {
		if flowEntry == nil || !flowEntry.IsDir() {
			continue
		}
		flowID := strings.TrimSpace(flowEntry.Name())
		if _, err := validateFlowID(flowID); err != nil {
			continue
		}
		runEntries, err := os.ReadDir(filepath.Join(root, flowID))
		if err != nil {
			h.log.Warn("flow archive list failed", "flow_id", flowID, "err", err)
			continue
		}
		for _, runEntry := range runEntries {
			if runEntry == nil || runEntry.IsDir() {
				continue
			}
			name := strings.TrimSpace(runEntry.Name())
			if !strings.HasSuffix(strings.ToLower(name), ".json") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(root, flowID, name))
			if err != nil {
				h.log.Warn("flow archive read failed", "flow_id", flowID, "file", name, "err", err)
				continue
			}
			var record archivedRunRecord
			if err := json.Unmarshal(raw, &record); err != nil {
				h.log.Warn("flow archive decode failed", "flow_id", flowID, "file", name, "err", err)
				continue
			}
			if record.FlowID == "" {
				record.FlowID = flowID
			}
			if record.RunID == "" {
				record.RunID = strings.TrimSuffix(name, filepath.Ext(name))
			}
			state, err := archivedRunStateFromRecord(record)
			if err != nil {
				h.log.Warn("flow archive record invalid", "flow_id", flowID, "file", name, "err", err)
				continue
			}
			loaded[flowID] = append(loaded[flowID], state)
		}
	}

	h.mu.Lock()
	var toDelete []archivedRunRef
	for flowID, states := range loaded {
		sort.Slice(states, func(i, j int) bool {
			if !states[i].start.Equal(states[j].start) {
				return states[i].start.Before(states[j].start)
			}
			if !states[i].end.Equal(states[j].end) {
				return states[i].end.Before(states[j].end)
			}
			return states[i].runID < states[j].runID
		})
		ids := make([]string, 0, len(states))
		for _, state := range states {
			if state == nil {
				continue
			}
			h.runs[state.runID] = state
			ids = append(ids, state.runID)
		}
		if len(ids) == 0 {
			continue
		}
		h.runOrderByFlow[flowID] = ids
		toDelete = append(toDelete, h.pruneRunsLocked(flowID)...)
	}
	h.mu.Unlock()
	h.removeArchivedRuns(toDelete)
	return nil
}
