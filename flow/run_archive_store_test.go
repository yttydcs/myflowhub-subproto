package flow

// 本文件覆盖 SubProto 中 `flow` 模块里与 `run_archive_store` 相关的行为。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yttydcs/myflowhub-core/config"
)

type fakeRunArchiveStore struct {
	loadRecords []ArchivedRunRecord
	saveRecords []ArchivedRunRecord
	deletes     []archivedRunRef
	loadErr     error
	saveErr     error
	deleteErr   error
}

func (s *fakeRunArchiveStore) LoadAll(context.Context) ([]ArchivedRunRecord, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	out := make([]ArchivedRunRecord, len(s.loadRecords))
	copy(out, s.loadRecords)
	return out, nil
}

func (s *fakeRunArchiveStore) Save(_ context.Context, record ArchivedRunRecord) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saveRecords = append(s.saveRecords, record)
	return nil
}

func (s *fakeRunArchiveStore) Delete(_ context.Context, flowID, runID string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deletes = append(s.deletes, archivedRunRef{flowID: flowID, runID: runID})
	return nil
}

func TestHandlerInitRejectsPGArchiveBackendWithoutInjectedStore(t *testing.T) {
	h := NewHandlerWithConfig(config.NewMap(map[string]string{
		cfgRunArchiveBackend: runArchiveBackendPG,
	}), nil)
	if h.Init() {
		t.Fatalf("expected init failure without injected pg archive store")
	}
}

func TestLoadArchivedRunsUsesInjectedArchiveStore(t *testing.T) {
	store := &fakeRunArchiveStore{
		loadRecords: []ArchivedRunRecord{
			{
				FlowID:      "123e4567-e89b-12d3-a456-426614174301",
				RunID:       "123e4567-e89b-12d3-a456-426614174302",
				Status:      "succeeded",
				StartedAtMs: time.Unix(10, 0).UTC().UnixMilli(),
				EndedAtMs:   time.Unix(11, 0).UTC().UnixMilli(),
				Runtime: runContext{
					FlowID: "123e4567-e89b-12d3-a456-426614174301",
					RunID:  "123e4567-e89b-12d3-a456-426614174302",
				},
			},
		},
	}
	h := NewHandlerWithOptions(nil, HandlerOptions{RunArchiveStore: store}, nil)
	h.runArchive = true
	h.runArchiveBackend = runArchiveBackendPG
	if err := h.loadArchivedRuns(); err != nil {
		t.Fatalf("loadArchivedRuns err=%v", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if got := len(h.runOrderByFlow["123e4567-e89b-12d3-a456-426614174301"]); got != 1 {
		t.Fatalf("unexpected run order length: %d", got)
	}
	if _, ok := h.runs["123e4567-e89b-12d3-a456-426614174302"]; !ok {
		t.Fatalf("expected archived run to be loaded")
	}
}

func TestPersistArchivedRunUsesInjectedArchiveStore(t *testing.T) {
	store := &fakeRunArchiveStore{}
	h := NewHandlerWithOptions(nil, HandlerOptions{RunArchiveStore: store}, nil)
	h.runArchive = true
	h.runArchiveBackend = runArchiveBackendPG

	state := &runState{
		flowID: "123e4567-e89b-12d3-a456-426614174303",
		runID:  "123e4567-e89b-12d3-a456-426614174304",
		status: "succeeded",
		start:  time.Unix(20, 0).UTC(),
		end:    time.Unix(21, 0).UTC(),
		runtime: runContext{
			FlowID: "123e4567-e89b-12d3-a456-426614174303",
			RunID:  "123e4567-e89b-12d3-a456-426614174304",
		},
	}

	h.persistArchivedRun(state)
	if got := len(store.saveRecords); got != 1 {
		t.Fatalf("unexpected save count: %d", got)
	}
	if store.saveRecords[0].FlowID != state.flowID || store.saveRecords[0].RunID != state.runID {
		t.Fatalf("unexpected saved record: %#v", store.saveRecords[0])
	}
}

func TestPruneRunsDeletesInjectedArchiveStoreEntries(t *testing.T) {
	store := &fakeRunArchiveStore{}
	h := NewHandlerWithOptions(nil, HandlerOptions{RunArchiveStore: store}, nil)
	h.runArchive = true
	h.runArchiveBackend = runArchiveBackendPG
	h.maxRetainedRuns = 1

	runIDs := []string{
		"123e4567-e89b-12d3-a456-426614174305",
		"123e4567-e89b-12d3-a456-426614174306",
	}
	flowID := "123e4567-e89b-12d3-a456-426614174307"
	for idx, runID := range runIDs {
		state := &runState{
			flowID: flowID,
			runID:  runID,
			status: "succeeded",
			start:  time.Unix(int64(30+idx), 0).UTC(),
			end:    time.Unix(int64(31+idx), 0).UTC(),
		}
		h.mu.Lock()
		h.recordRunLocked(state)
		h.mu.Unlock()
	}

	h.pruneRuns(flowID)
	if got := len(store.deletes); got != 1 {
		t.Fatalf("unexpected delete count: %d", got)
	}
	if store.deletes[0].runID != runIDs[0] {
		t.Fatalf("unexpected deleted run: %#v", store.deletes[0])
	}
}

func TestLoadArchivedRunsReturnsStoreError(t *testing.T) {
	store := &fakeRunArchiveStore{loadErr: errors.New("boom")}
	h := NewHandlerWithOptions(nil, HandlerOptions{RunArchiveStore: store}, nil)
	h.runArchive = true
	h.runArchiveBackend = runArchiveBackendPG
	if err := h.loadArchivedRuns(); err == nil {
		t.Fatalf("expected loadArchivedRuns error")
	}
}
