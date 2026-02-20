package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"strings"
)

const defaultAlgES256 = "ES256"

func parseECPubKey(b64 string) (*ecdsa.PublicKey, []byte, error) {
	raw := strings.TrimSpace(b64)
	if raw == "" {
		return nil, nil, errors.New("pubkey empty")
	}
	der, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, nil, err
	}
	pubAny, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, nil, err
	}
	pub, ok := pubAny.(*ecdsa.PublicKey)
	if !ok || pub == nil || pub.Curve != elliptic.P256() {
		return nil, nil, errors.New("pubkey not p256")
	}
	return pub, der, nil
}

func parseECPubKeyRaw(raw []byte) (*ecdsa.PublicKey, error) {
	if len(raw) == 0 {
		return nil, errors.New("pubkey empty")
	}
	pubAny, err := x509.ParsePKIXPublicKey(raw)
	if err != nil {
		return nil, err
	}
	pub, ok := pubAny.(*ecdsa.PublicKey)
	if !ok || pub == nil || pub.Curve != elliptic.P256() {
		return nil, errors.New("pubkey not p256")
	}
	return pub, nil
}

func verifyEcdsaSig(pub *ecdsa.PublicKey, msg []byte, sigB64 string) bool {
	if pub == nil {
		return false
	}
	sigRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	if err != nil {
		return false
	}
	hashed := sha256.Sum256(msg)
	return ecdsa.VerifyASN1(pub, hashed[:], sigRaw)
}

func loginSignBytes(req loginData) []byte {
	sb := strings.Builder{}
	sb.WriteString("login\n")
	sb.WriteString(strings.TrimSpace(req.DeviceID))
	sb.WriteString("\n")
	sb.WriteString(uintToString(req.NodeID))
	sb.WriteString("\n")
	sb.WriteString(int64ToString(req.TS))
	sb.WriteString("\n")
	sb.WriteString(strings.TrimSpace(req.Nonce))
	return []byte(sb.String())
}

func upLoginSignBytes(req upLoginData) []byte {
	sb := strings.Builder{}
	sb.WriteString("up_login\n")
	sb.WriteString(uintToString(req.NodeID))
	sb.WriteString("\n")
	sb.WriteString(strings.TrimSpace(req.DeviceID))
	sb.WriteString("\n")
	sb.WriteString(uintToString(req.HubID))
	sb.WriteString("\n")
	sb.WriteString(strings.TrimSpace(req.PubKey))
	sb.WriteString("\n")
	sb.WriteString(int64ToString(req.TS))
	sb.WriteString("\n")
	sb.WriteString(strings.TrimSpace(req.Nonce))
	return []byte(sb.String())
}

func upLoginSenderSignBytes(req upLoginData) []byte {
	sb := strings.Builder{}
	sb.WriteString("up_login_sender\n")
	sb.WriteString(uintToString(req.NodeID))
	sb.WriteString("\n")
	sb.WriteString(strings.TrimSpace(req.DeviceID))
	sb.WriteString("\n")
	sb.WriteString(uintToString(req.HubID))
	sb.WriteString("\n")
	sb.WriteString(strings.TrimSpace(req.PubKey))
	sb.WriteString("\n")
	sb.WriteString(int64ToString(req.DeviceTS))
	sb.WriteString("\n")
	sb.WriteString(strings.TrimSpace(req.DeviceNonce))
	sb.WriteString("\n")
	sb.WriteString(strings.TrimSpace(req.DeviceSig))
	sb.WriteString("\n")
	sb.WriteString(int64ToString(req.TS))
	sb.WriteString("\n")
	sb.WriteString(strings.TrimSpace(req.Nonce))
	return []byte(sb.String())
}

func signWithNodeKey(priv *ecdsa.PrivateKey, msg []byte) string {
	if priv == nil || len(msg) == 0 {
		return ""
	}
	hashed := sha256.Sum256(msg)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, hashed[:])
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func encodePubKey(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(raw)
}
