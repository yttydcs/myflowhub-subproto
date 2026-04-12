package auth

import (
	"context"
	"encoding/json"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
)

type authSentFrame struct {
	connID  string
	header  core.IHeader
	payload []byte
}

type recordingAuthServer struct {
	*testServer
	sent []authSentFrame
}

func newRecordingAuthServer(nodeID uint32, cm core.IConnectionManager) *recordingAuthServer {
	return newRecordingAuthServerWithConfig(nodeID, cm, nil)
}

func newRecordingAuthServerWithConfig(nodeID uint32, cm core.IConnectionManager, cfg core.IConfig) *recordingAuthServer {
	return &recordingAuthServer{
		testServer: &testServer{nodeID: nodeID, cm: cm, cfg: cfg},
	}
}

func (s *recordingAuthServer) Send(_ context.Context, connID string, hdr core.IHeader, payload []byte) error {
	s.sent = append(s.sent, authSentFrame{
		connID:  connID,
		header:  hdr,
		payload: append([]byte(nil), payload...),
	})
	return nil
}

func decodeAuthFrame[T any](t *testing.T, payload []byte) (message, T) {
	t.Helper()

	var frame message
	if err := json.Unmarshal(payload, &frame); err != nil {
		t.Fatalf("unmarshal auth frame err=%v", err)
	}
	var resp T
	if err := json.Unmarshal(frame.Data, &resp); err != nil {
		t.Fatalf("unmarshal auth payload err=%v", err)
	}
	return frame, resp
}

func assertDisplayNameMeta(t *testing.T, conn core.IConnection, want string) {
	t.Helper()

	for _, key := range []string{metaDisplayNameKey, metaNodeDisplayNameKey} {
		got, ok := conn.GetMeta(key)
		if !ok {
			t.Fatalf("expected %s in connection metadata", key)
		}
		if got != want {
			t.Fatalf("expected %s=%q, got %v", key, want, got)
		}
	}
}

func TestHandleRegister_DirectChildDisplayNameBootstrap(t *testing.T) {
	cm := connmgr.New()
	conn := &mockConnection{id: "child-register"}
	_ = cm.Add(conn)
	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h := &LoginHandler{
		whitelist:      make(map[string]bindingRecord),
		pendingConn:    make(map[string]pendingInfo),
		disablePersist: true,
	}
	h.nextID.Store(2)

	raw, err := json.Marshal(registerData{DeviceID: "dev-register", DisplayName: "  Edge Register  "})
	if err != nil {
		t.Fatalf("marshal register request err=%v", err)
	}

	h.handleRegister(ctx, conn, nil, raw, false)

	assertDisplayNameMeta(t, conn, "Edge Register")
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response frame, got %d", len(srv.sent))
	}
	frame, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if frame.Action != actionRegisterResp {
		t.Fatalf("expected action %q, got %q", actionRegisterResp, frame.Action)
	}
	if resp.DisplayName != "Edge Register" {
		t.Fatalf("expected display_name in register resp, got %+v", resp)
	}
}

func TestHandleRegisterResp_AssistPathStoresDisplayName(t *testing.T) {
	cm := connmgr.New()
	conn := &mockConnection{id: "child-register-resp"}
	_ = cm.Add(conn)
	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h := &LoginHandler{
		whitelist:      make(map[string]bindingRecord),
		pendingConn:    map[string]pendingInfo{"dev-register-resp": {connID: conn.ID(), msgID: 7, traceID: 9}},
		disablePersist: true,
	}

	raw, err := json.Marshal(respData{Code: 1, DeviceID: "dev-register-resp", NodeID: 6, DisplayName: "Edge Register Resp"})
	if err != nil {
		t.Fatalf("marshal register resp err=%v", err)
	}

	h.handleRegisterResp(ctx, raw)

	assertDisplayNameMeta(t, conn, "Edge Register Resp")
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 downstream response frame, got %d", len(srv.sent))
	}
	frame, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if frame.Action != actionRegisterResp {
		t.Fatalf("expected action %q, got %q", actionRegisterResp, frame.Action)
	}
	if resp.DisplayName != "Edge Register Resp" {
		t.Fatalf("expected display_name in forwarded register resp, got %+v", resp)
	}
	if srv.sent[0].header.GetMsgID() != 7 || srv.sent[0].header.GetTraceID() != 9 {
		t.Fatalf("expected pending header to be preserved, got msg=%d trace=%d", srv.sent[0].header.GetMsgID(), srv.sent[0].header.GetTraceID())
	}
}

func TestHandleLogin_DirectChildDisplayNameBootstrap(t *testing.T) {
	priv, pubRaw, _ := mustKeyPair(t)

	cm := connmgr.New()
	conn := &mockConnection{id: "child-login"}
	_ = cm.Add(conn)
	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h := &LoginHandler{
		whitelist: map[string]bindingRecord{
			"dev-login": {NodeID: 6, Role: "superadmin", Perms: []string{"*"}, PubKey: cloneSlice(pubRaw)},
		},
		pendingConn:    make(map[string]pendingInfo),
		disablePersist: true,
	}
	h.loadAuthConfig(coreconfig.NewMap(map[string]string{
		coreconfig.KeyAuthNodeRoles: "6:superadmin",
		coreconfig.KeyAuthRolePerms: "superadmin:*",
	}))

	req := loginData{
		DeviceID:    "dev-login",
		NodeID:      6,
		DisplayName: "  Edge Login  ",
		TS:          123,
		Nonce:       "nonce-1",
		Alg:         defaultAlgES256,
	}
	req.Sig = signWithNodeKey(priv, loginSignBytes(req))

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal login request err=%v", err)
	}

	h.handleLogin(ctx, conn, nil, raw, false)

	assertDisplayNameMeta(t, conn, "Edge Login")
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 login response frame, got %d", len(srv.sent))
	}
	frame, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if frame.Action != actionLoginResp {
		t.Fatalf("expected action %q, got %q", actionLoginResp, frame.Action)
	}
	if resp.DisplayName != "Edge Login" {
		t.Fatalf("expected display_name in login resp, got %+v", resp)
	}
	if resp.Role != "superadmin" {
		t.Fatalf("expected role in login resp, got %+v", resp)
	}
	if len(resp.Perms) != 1 || resp.Perms[0] != "*" {
		t.Fatalf("expected perms in login resp, got %+v", resp)
	}
}

func TestHandleLogin_AssistPathCachesDisplayNameForDirectChild(t *testing.T) {
	priv, pubRaw, _ := mustKeyPair(t)

	cm := connmgr.New()
	conn := &mockConnection{id: "child-login-assist"}
	conn.SetMeta("nodeID", uint32(6))
	_ = cm.Add(conn)
	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h := &LoginHandler{
		whitelist: map[string]bindingRecord{
			"dev-login-assist": {NodeID: 6, Role: "superadmin", Perms: []string{"*"}, PubKey: cloneSlice(pubRaw)},
		},
		pendingConn:    make(map[string]pendingInfo),
		disablePersist: true,
	}
	h.loadAuthConfig(coreconfig.NewMap(map[string]string{
		coreconfig.KeyAuthNodeRoles: "6:superadmin",
		coreconfig.KeyAuthRolePerms: "superadmin:*",
	}))

	req := loginData{
		DeviceID:    "dev-login-assist",
		NodeID:      6,
		DisplayName: "  Edge Assist  ",
		TS:          456,
		Nonce:       "nonce-2",
		Alg:         defaultAlgES256,
	}
	req.Sig = signWithNodeKey(priv, loginSignBytes(req))

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal assist login request err=%v", err)
	}

	h.handleLogin(ctx, conn, nil, raw, true)

	assertDisplayNameMeta(t, conn, "Edge Assist")
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 assist login response frame, got %d", len(srv.sent))
	}
	frame, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if frame.Action != actionAssistLoginResp {
		t.Fatalf("expected action %q, got %q", actionAssistLoginResp, frame.Action)
	}
	if resp.DisplayName != "Edge Assist" {
		t.Fatalf("expected display_name in assist login resp, got %+v", resp)
	}
	if resp.Role != "superadmin" {
		t.Fatalf("expected role in assist login resp, got %+v", resp)
	}
	if len(resp.Perms) != 1 || resp.Perms[0] != "*" {
		t.Fatalf("expected perms in assist login resp, got %+v", resp)
	}
}

func TestHandleLoginResp_AssistPathStoresDisplayName(t *testing.T) {
	cm := connmgr.New()
	conn := &mockConnection{id: "child-login-resp"}
	_ = cm.Add(conn)
	srv := newRecordingAuthServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	h := &LoginHandler{
		whitelist:      make(map[string]bindingRecord),
		pendingConn:    map[string]pendingInfo{"dev-login-resp": {connID: conn.ID(), msgID: 17, traceID: 19}},
		disablePersist: true,
	}

	raw, err := json.Marshal(respData{Code: 1, DeviceID: "dev-login-resp", NodeID: 8, DisplayName: "Edge Login Resp"})
	if err != nil {
		t.Fatalf("marshal login resp err=%v", err)
	}

	h.handleLoginResp(ctx, raw)

	assertDisplayNameMeta(t, conn, "Edge Login Resp")
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 downstream login response frame, got %d", len(srv.sent))
	}
	frame, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if frame.Action != actionLoginResp {
		t.Fatalf("expected action %q, got %q", actionLoginResp, frame.Action)
	}
	if resp.DisplayName != "Edge Login Resp" {
		t.Fatalf("expected display_name in forwarded login resp, got %+v", resp)
	}
	if srv.sent[0].header.GetMsgID() != 17 || srv.sent[0].header.GetTraceID() != 19 {
		t.Fatalf("expected pending header to be preserved, got msg=%d trace=%d", srv.sent[0].header.GetMsgID(), srv.sent[0].header.GetTraceID())
	}
}
