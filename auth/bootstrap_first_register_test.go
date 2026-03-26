package auth

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/connmgr"
)

func TestHandleRegister_FirstRegisterBootstrapBypassesPending(t *testing.T) {
	withAuthTempWorkDir(t)

	h, cfg := newFirstRegisterBootstrapHandler(t, nil, false)
	if !h.Init() {
		t.Fatalf("expected init success, err=%v", h.initErr)
	}

	cm := connmgr.New()
	conn := &mockConnection{id: "child-bootstrap"}
	_ = cm.Add(conn)
	srv := newRecordingAuthServerWithConfig(1, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)

	raw, err := json.Marshal(registerData{DeviceID: "dev-bootstrap"})
	if err != nil {
		t.Fatalf("marshal register data: %v", err)
	}
	h.handleRegister(ctx, conn, nil, raw, false)

	rec, ok := h.whitelist["dev-bootstrap"]
	if !ok {
		t.Fatalf("expected bootstrap whitelist entry")
	}
	if rec.Role != "superadmin" {
		t.Fatalf("unexpected bootstrap role: got %q want %q", rec.Role, "superadmin")
	}
	if h.firstRegisterBootstrapState.ConsumedEpoch != 1 {
		t.Fatalf("unexpected consumed epoch: got %d want 1", h.firstRegisterBootstrapState.ConsumedEpoch)
	}
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response, got %d", len(srv.sent))
	}
	_, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if resp.Status != admissionStatusApproved {
		t.Fatalf("unexpected bootstrap status: got %q want %q", resp.Status, admissionStatusApproved)
	}

	connOther := &mockConnection{id: "child-pending-after-bootstrap"}
	_ = cm.Add(connOther)
	srv.sent = nil
	rawOther, err := json.Marshal(registerData{DeviceID: "dev-other"})
	if err != nil {
		t.Fatalf("marshal register data: %v", err)
	}
	h.handleRegister(ctx, connOther, nil, rawOther, false)
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response for non-bootstrap device, got %d", len(srv.sent))
	}
	_, pendingResp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if pendingResp.Status != admissionStatusPending {
		t.Fatalf("unexpected follow-up status: got %q want %q", pendingResp.Status, admissionStatusPending)
	}
}

func TestHandleRegister_FirstRegisterBootstrapConsumedFallsBackToPending(t *testing.T) {
	withAuthTempWorkDir(t)

	h, cfg := newFirstRegisterBootstrapHandler(t, nil, false)
	if !h.Init() {
		t.Fatalf("expected init success, err=%v", h.initErr)
	}

	cm := connmgr.New()
	conn := &mockConnection{id: "child-bootstrap-once"}
	_ = cm.Add(conn)
	srv := newRecordingAuthServerWithConfig(1, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)

	raw, err := json.Marshal(registerData{DeviceID: "dev-bootstrap"})
	if err != nil {
		t.Fatalf("marshal register data: %v", err)
	}
	h.handleRegister(ctx, conn, nil, raw, false)
	if !h.removeBinding("dev-bootstrap") {
		t.Fatalf("expected bootstrap binding removal")
	}

	srv.sent = nil
	h.handleRegister(ctx, conn, nil, raw, false)
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response, got %d", len(srv.sent))
	}
	_, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if resp.Status != admissionStatusPending {
		t.Fatalf("unexpected status after bootstrap consumed: got %q want %q", resp.Status, admissionStatusPending)
	}
}

func TestHandleRegister_FirstRegisterBootstrapPubKeyMismatchRejected(t *testing.T) {
	withAuthTempWorkDir(t)

	_, _, expectedPubB64 := mustKeyPair(t)
	_, _, wrongPubB64 := mustKeyPair(t)
	h, cfg := newFirstRegisterBootstrapHandler(t, map[string]string{
		coreconfig.KeyAuthBootstrapFirstRegisterPubKey: expectedPubB64,
	}, false)
	if !h.Init() {
		t.Fatalf("expected init success, err=%v", h.initErr)
	}

	cm := connmgr.New()
	conn := &mockConnection{id: "child-bootstrap-mismatch"}
	_ = cm.Add(conn)
	srv := newRecordingAuthServerWithConfig(1, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)

	raw, err := json.Marshal(registerData{DeviceID: "dev-bootstrap", PubKey: wrongPubB64})
	if err != nil {
		t.Fatalf("marshal register data: %v", err)
	}
	h.handleRegister(ctx, conn, nil, raw, false)

	if len(h.whitelist) != 0 {
		t.Fatalf("pubkey mismatch must not create whitelist entry")
	}
	if len(h.pendingRegisters) != 0 {
		t.Fatalf("pubkey mismatch must not fall through to pending")
	}
	if h.firstRegisterBootstrapState.ConsumedEpoch != 0 {
		t.Fatalf("pubkey mismatch must not consume bootstrap state")
	}
	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response, got %d", len(srv.sent))
	}
	_, resp := decodeAuthFrame[respData](t, srv.sent[0].payload)
	if resp.Status != admissionStatusRejected {
		t.Fatalf("unexpected status: got %q want %q", resp.Status, admissionStatusRejected)
	}
	if !strings.Contains(resp.Reason, "pubkey mismatch") {
		t.Fatalf("unexpected reject reason: %q", resp.Reason)
	}
}

func TestHandleRegister_FirstRegisterBootstrapEpochReopen(t *testing.T) {
	withAuthTempWorkDir(t)

	h, cfg := newFirstRegisterBootstrapHandler(t, map[string]string{
		coreconfig.KeyAuthBootstrapFirstRegisterEpoch: "2",
	}, false)
	if !h.Init() {
		t.Fatalf("expected init success, err=%v", h.initErr)
	}
	h.firstRegisterBootstrapState = firstRegisterBootstrapState{
		ConsumedEpoch: 1,
		ConsumedAt:    h.now().Add(-time.Hour).Unix(),
		DeviceID:      "dev-bootstrap",
		NodeID:        9,
		Role:          "superadmin",
	}

	cm := connmgr.New()
	conn := &mockConnection{id: "child-bootstrap-epoch"}
	_ = cm.Add(conn)
	srv := newRecordingAuthServerWithConfig(1, cm, cfg)
	ctx := core.WithServerContext(context.Background(), srv)

	raw, err := json.Marshal(registerData{DeviceID: "dev-bootstrap"})
	if err != nil {
		t.Fatalf("marshal register data: %v", err)
	}
	h.handleRegister(ctx, conn, nil, raw, false)

	if h.firstRegisterBootstrapState.ConsumedEpoch != 2 {
		t.Fatalf("unexpected consumed epoch after reopen: got %d want 2", h.firstRegisterBootstrapState.ConsumedEpoch)
	}
}

func TestLoginHandlerInit_FirstRegisterBootstrapRejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name           string
		overrides      map[string]string
		disablePersist bool
		wantErr        string
	}{
		{
			name: "missing device id",
			overrides: map[string]string{
				coreconfig.KeyAuthBootstrapFirstRegisterDeviceID: "",
			},
			wantErr: "device_id required",
		},
		{
			name: "unknown role",
			overrides: map[string]string{
				coreconfig.KeyAuthBootstrapFirstRegisterRole: "guest",
			},
			wantErr: "unknown role",
		},
		{
			name: "parent configured",
			overrides: map[string]string{
				coreconfig.KeyParentAddr:   "tcp://127.0.0.1:9000",
				coreconfig.KeyParentEnable: "true",
			},
			wantErr: "requires local authority",
		},
		{
			name:           "persist disabled",
			disablePersist: true,
			wantErr:        "requires persist enabled",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newFirstRegisterBootstrapHandler(t, tc.overrides, tc.disablePersist)
			if h.Init() {
				t.Fatalf("expected init failure")
			}
			if h.initErr == nil || !strings.Contains(h.initErr.Error(), tc.wantErr) {
				t.Fatalf("unexpected init err: %v", h.initErr)
			}
		})
	}
}

func newFirstRegisterBootstrapHandler(t *testing.T, overrides map[string]string, disablePersist bool) (*LoginHandler, core.IConfig) {
	t.Helper()

	cfgData := map[string]string{
		coreconfig.KeyAuthDefaultRole:                    "node",
		coreconfig.KeyAuthRegisterRequireApproval:        "true",
		coreconfig.KeyAuthBootstrapFirstRegisterEnable:   "true",
		coreconfig.KeyAuthBootstrapFirstRegisterDeviceID: "dev-bootstrap",
		coreconfig.KeyAuthBootstrapFirstRegisterEpoch:    "1",
	}
	for key, value := range overrides {
		cfgData[key] = value
	}
	cfg := coreconfig.NewMap(cfgData)
	now := time.Unix(1_700_000_000, 0).UTC()
	h := &LoginHandler{
		whitelist:         make(map[string]bindingRecord),
		pendingConn:       make(map[string]pendingInfo),
		pendingRegisters:  make(map[string]pendingRegisterRecord),
		pendingByDevice:   make(map[string]string),
		approvedRegisters: make(map[string]approvedRegisterRecord),
		registerPermits:   make(map[string]registerPermitRecord),
		disablePersist:    disablePersist,
		now:               func() time.Time { return now },
	}
	h.nextID.Store(2)
	h.loadAuthConfig(cfg)
	h.loadAdmissionConfig(cfg)
	h.loadFirstRegisterBootstrapConfig(cfg)
	return h, cfg
}

func withAuthTempWorkDir(t *testing.T) {
	t.Helper()

	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevWD)
	})
}
