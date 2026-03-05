package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"testing"
)

func TestBuildUpLoginData_UsesSenderNodePub(t *testing.T) {
	senderPriv, senderPubRaw, senderPubB64 := mustKeyPair(t)
	_, loginPubRaw, _ := mustKeyPair(t)

	h := &LoginHandler{
		nodePriv:   senderPriv,
		nodePubB64: senderPubB64,
	}

	data, ok := h.buildUpLoginData(
		9,
		"dev-10",
		10,
		loginPubRaw,
		"device-sig",
		defaultAlgES256,
		123,
		"nonce-a",
	)
	if !ok {
		t.Fatalf("buildUpLoginData should succeed")
	}
	if data.PubKey != encodePubKey(loginPubRaw) {
		t.Fatalf("PubKey mismatch")
	}
	if data.SenderPub != senderPubB64 {
		t.Fatalf("SenderPub mismatch: got=%q want sender node pub", data.SenderPub)
	}
	if data.SenderPub == data.PubKey {
		t.Fatalf("SenderPub should not reuse login node pub")
	}
	if data.SenderID != 9 || data.HubID != 9 || data.NodeID != 10 {
		t.Fatalf("unexpected ids: sender=%d hub=%d node=%d", data.SenderID, data.HubID, data.NodeID)
	}

	senderPub, err := parseECPubKeyRaw(senderPubRaw)
	if err != nil {
		t.Fatalf("parse sender pub: %v", err)
	}
	if !verifyEcdsaSig(senderPub, upLoginSenderSignBytes(data), data.SenderSig) {
		t.Fatalf("sender signature must be verifiable by sender pub")
	}
}

func TestBuildUpLoginData_MissingSenderPubRejected(t *testing.T) {
	senderPriv, _, _ := mustKeyPair(t)
	_, loginPubRaw, _ := mustKeyPair(t)

	h := &LoginHandler{
		nodePriv:   senderPriv,
		nodePubB64: "",
	}
	if _, ok := h.buildUpLoginData(9, "dev-10", 10, loginPubRaw, "device-sig", defaultAlgES256, 123, "nonce-a"); ok {
		t.Fatalf("expected buildUpLoginData to fail when sender pub missing")
	}
}

func mustKeyPair(t *testing.T) (*ecdsa.PrivateKey, []byte, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubRaw, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal pub: %v", err)
	}
	return priv, pubRaw, base64.StdEncoding.EncodeToString(pubRaw)
}
