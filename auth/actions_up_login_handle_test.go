package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/connmgr"
	"github.com/yttydcs/myflowhub-core/header"
)

func TestHandleUpLogin_TrustedPoisoned_CanHealWithSenderPub(t *testing.T) {
	// sender(hub) key
	senderPriv, senderPubRaw, senderPubB64 := mustKeyPair(t)
	// poisoned trusted key (valid pub but wrong)
	_, poisonedRaw, _ := mustKeyPair(t)
	// descendant(leaf) key
	leafPriv, leafPubRaw, _ := mustKeyPair(t)

	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	h := &LoginHandler{
		whitelist:      map[string]bindingRecord{"hub-9": {NodeID: 9, PubKey: cloneSlice(poisonedRaw)}},
		pendingConn:    make(map[string]pendingInfo),
		trustedNode:    map[uint32][]byte{9: cloneSlice(poisonedRaw)},
		disablePersist: true,
	}

	conn := &mockConnection{id: "c-hub"}
	conn.SetMeta("nodeID", uint32(9))
	_ = cm.Add(conn)

	req := upLoginData{
		NodeID:      11,
		DeviceID:    "leaf-11",
		HubID:       9,
		PubKey:      encodePubKey(leafPubRaw),
		TS:          123,
		Nonce:       "n1",
		DeviceTS:    456,
		DeviceNonce: "dn1",
		DeviceAlg:   defaultAlgES256,
		SenderID:    9,
		SenderAlg:   defaultAlgES256,
		SenderPub:   senderPubB64,
		Alg:         defaultAlgES256,
	}
	req.DeviceSig = signWithNodeKey(leafPriv, loginSignBytes(loginData{
		DeviceID: req.DeviceID,
		NodeID:   req.NodeID,
		TS:       req.DeviceTS,
		Nonce:    req.DeviceNonce,
	}))
	req.SenderSig = signWithNodeKey(senderPriv, upLoginSenderSignBytes(req))

	body, _ := json.Marshal(req)
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2).WithSourceID(9).WithTargetID(0)

	h.handleUpLogin(ctx, conn, hdr, body)

	// route index should be established: descendant -> hub conn
	if c, ok := cm.GetByNode(11); !ok || c == nil || c.ID() != conn.ID() {
		t.Fatalf("route index not established for descendant node 11")
	}

	// trusted should be healed
	if raw, ok := h.trustedNode[9]; !ok || !bytes.Equal(raw, senderPubRaw) {
		t.Fatalf("trusted sender pubkey not healed")
	}
	// binding should be healed
	if rec := h.whitelist["hub-9"]; !bytes.Equal(rec.PubKey, senderPubRaw) {
		t.Fatalf("binding pubkey not healed")
	}
	// conn meta cache should be updated
	if v, ok := conn.GetMeta("node_pubkey"); !ok {
		t.Fatalf("conn meta node_pubkey not set")
	} else if b, ok2 := v.([]byte); !ok2 || !bytes.Equal(b, senderPubRaw) {
		t.Fatalf("conn meta node_pubkey mismatch")
	}
}

func TestHandleUpLogin_SenderMismatchRejected(t *testing.T) {
	senderPriv, senderPubRaw, senderPubB64 := mustKeyPair(t)
	_, poisonedRaw, _ := mustKeyPair(t)
	leafPriv, leafPubRaw, _ := mustKeyPair(t)

	cm := connmgr.New()
	srv := &testServer{nodeID: 1, cm: cm}
	ctx := core.WithServerContext(context.Background(), srv)

	h := &LoginHandler{
		whitelist:      map[string]bindingRecord{"hub-9": {NodeID: 9, PubKey: cloneSlice(poisonedRaw)}},
		pendingConn:    make(map[string]pendingInfo),
		trustedNode:    map[uint32][]byte{9: cloneSlice(poisonedRaw)},
		disablePersist: true,
	}

	conn := &mockConnection{id: "c-hub"}
	conn.SetMeta("nodeID", uint32(9))
	_ = cm.Add(conn)

	req := upLoginData{
		NodeID:      11,
		DeviceID:    "leaf-11",
		HubID:       9,
		PubKey:      encodePubKey(leafPubRaw),
		TS:          123,
		Nonce:       "n1",
		DeviceTS:    456,
		DeviceNonce: "dn1",
		DeviceAlg:   defaultAlgES256,
		SenderID:    9,
		SenderAlg:   defaultAlgES256,
		SenderPub:   senderPubB64,
		Alg:         defaultAlgES256,
	}
	req.DeviceSig = signWithNodeKey(leafPriv, loginSignBytes(loginData{
		DeviceID: req.DeviceID,
		NodeID:   req.NodeID,
		TS:       req.DeviceTS,
		Nonce:    req.DeviceNonce,
	}))
	req.SenderSig = signWithNodeKey(senderPriv, upLoginSenderSignBytes(req))

	body, _ := json.Marshal(req)
	// hdr.SourceID mismatch
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2).WithSourceID(8).WithTargetID(0)

	h.handleUpLogin(ctx, conn, hdr, body)

	if _, ok := cm.GetByNode(11); ok {
		t.Fatalf("route index should not be established when sender mismatch")
	}
	if raw := h.trustedNode[9]; !bytes.Equal(raw, poisonedRaw) {
		t.Fatalf("trusted should not be updated when sender mismatch")
	}
	if rec := h.whitelist["hub-9"]; !bytes.Equal(rec.PubKey, poisonedRaw) {
		t.Fatalf("binding should not be updated when sender mismatch")
	}
	if _, ok := conn.GetMeta("node_pubkey"); ok {
		t.Fatalf("conn meta node_pubkey should not be updated when sender mismatch")
	}
	// also ensure senderPubRaw isn't accidentally equal to poisonedRaw (sanity)
	if bytes.Equal(senderPubRaw, poisonedRaw) {
		t.Fatalf("test setup invalid: sender pub equals poisoned pub")
	}
}
