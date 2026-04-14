package varstore

// 本文件覆盖 SubProto 中 `varstore` 模块里与 `persistence` 相关的行为。

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/header"
)

type fakeVarPersistence struct {
	loadAll   []VarDocument
	loadErr   error
	saveErr   error
	deleteErr error
}

func (p *fakeVarPersistence) LoadAll(context.Context) ([]VarDocument, error) {
	if p.loadErr != nil {
		return nil, p.loadErr
	}
	out := make([]VarDocument, len(p.loadAll))
	copy(out, p.loadAll)
	return out, nil
}

func (p *fakeVarPersistence) Save(context.Context, VarDocument) error {
	return p.saveErr
}

func (p *fakeVarPersistence) Delete(context.Context, uint32, string) error {
	return p.deleteErr
}

func TestVarStoreInitLoadsPersistedRecords(t *testing.T) {
	store := &fakeVarPersistence{
		loadAll: []VarDocument{{
			Owner:      1,
			Name:       "sys_flashlight_enabled",
			Value:      "1",
			Type:       "string",
			Visibility: visibilityPublic,
		}},
	}

	h := NewVarStoreHandlerWithOptions(nil, HandlerOptions{Persistence: store}, nil)
	if !h.Init() {
		t.Fatalf("expected init success")
	}

	rec, ok := h.lookupOwned(1, "sys_flashlight_enabled")
	if !ok {
		t.Fatalf("expected persisted record loaded into cache")
	}
	if rec.Value != "1" || !rec.IsPublic {
		t.Fatalf("unexpected loaded record: %#v", rec)
	}
}

func TestVarStoreHandleSetPersistFailureKeepsCacheClean(t *testing.T) {
	store := &fakeVarPersistence{saveErr: errors.New("boom")}
	h := NewVarStoreHandlerWithOptions(nil, HandlerOptions{Persistence: store}, nil)
	if !h.Init() {
		t.Fatalf("expected init success")
	}

	cm := connmgr.New()
	conn := newTestConn("c-local")
	conn.SetMeta("nodeID", uint32(1))
	if err := cm.Add(conn); err != nil {
		t.Fatalf("add conn: %v", err)
	}
	srv := newTestServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	req := setReq{
		Owner: 1,
		Name:  "sys_flashlight_enabled",
		Value: "1",
	}
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(1).
		WithTargetID(1)

	h.handleSet(ctx, conn, reqHdr, mustJSON(req), false)

	if _, ok := h.lookupOwned(1, req.Name); ok {
		t.Fatalf("cache should remain empty when persistence save fails")
	}
	if len(conn.sent) != 1 {
		t.Fatalf("expected 1 response, got=%d", len(conn.sent))
	}
	resp := decodeVarResp(t, conn.sent[0].payload)
	if resp.Code != 5 {
		t.Fatalf("expected persist failure response, got=%#v", resp)
	}
}

func TestVarStoreHandleRevokePersistFailureKeepsRecord(t *testing.T) {
	store := &fakeVarPersistence{
		loadAll: []VarDocument{{
			Owner:      1,
			Name:       "sys_flashlight_enabled",
			Value:      "1",
			Type:       "string",
			Visibility: visibilityPrivate,
		}},
		deleteErr: errors.New("boom"),
	}
	h := NewVarStoreHandlerWithOptions(nil, HandlerOptions{Persistence: store}, nil)
	if !h.Init() {
		t.Fatalf("expected init success")
	}

	cm := connmgr.New()
	conn := newTestConn("c-local")
	conn.SetMeta("nodeID", uint32(1))
	if err := cm.Add(conn); err != nil {
		t.Fatalf("add conn: %v", err)
	}
	srv := newTestServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	req := getReq{
		Owner: 1,
		Name:  "sys_flashlight_enabled",
	}
	reqHdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(3).
		WithSourceID(1).
		WithTargetID(1)

	h.handleRevoke(ctx, conn, reqHdr, mustJSON(req), false)

	rec, ok := h.lookupOwned(1, req.Name)
	if !ok || rec.Value != "1" {
		t.Fatalf("record should remain when persistence delete fails")
	}
	if len(conn.sent) != 1 {
		t.Fatalf("expected 1 response, got=%d", len(conn.sent))
	}
	resp := decodeVarResp(t, conn.sent[0].payload)
	if resp.Code != 5 {
		t.Fatalf("expected persist failure response, got=%#v", resp)
	}
}

func decodeVarResp(t *testing.T, payload []byte) varResp {
	t.Helper()
	var msg varMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("decode response envelope: %v", err)
	}
	var resp varResp
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		t.Fatalf("decode response data: %v", err)
	}
	return resp
}
