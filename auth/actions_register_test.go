package auth

import (
	"context"
	"encoding/json"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
)

func TestHandleRegister_MissingPubKeyDoesNotPoisonTrusted(t *testing.T) {
	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	h := &LoginHandler{
		whitelist:      make(map[string]bindingRecord),
		pendingConn:    make(map[string]pendingInfo),
		disablePersist: true,
	}
	h.nextID.Store(2)

	conn := &mockConnection{id: "c1"}
	raw, _ := json.Marshal(registerData{DeviceID: "dev-1"})
	h.handleRegister(ctx, conn, nil, raw, true)

	rec, ok := h.whitelist["dev-1"]
	if !ok {
		t.Fatalf("binding should be created")
	}
	if rec.NodeID == 0 {
		t.Fatalf("node id should be allocated")
	}
	if len(rec.PubKey) != 0 {
		t.Fatalf("PubKey should stay empty when request pubkey missing")
	}
	if len(h.trustedNode) != 0 {
		t.Fatalf("trustedNode should not be updated when request pubkey missing")
	}
}
