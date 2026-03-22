package management

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
)

type runtimeOnlyTestConfig struct {
	base     *coreconfig.MapConfig
	setCalls int
}

func newRuntimeOnlyTestConfig() *runtimeOnlyTestConfig {
	return &runtimeOnlyTestConfig{base: coreconfig.NewMap(map[string]string{})}
}

func (c *runtimeOnlyTestConfig) Get(key string) (string, bool) { return c.base.Get(key) }
func (c *runtimeOnlyTestConfig) Merge(other core.IConfig) core.IConfig {
	return c.base.Merge(other)
}
func (c *runtimeOnlyTestConfig) Set(key, val string) {
	c.setCalls++
	c.base.Set(key, val)
}
func (c *runtimeOnlyTestConfig) Keys() []string { return c.base.Keys() }

type persistentTestConfig struct {
	*runtimeOnlyTestConfig
	persistentCalls int
	persistentErr   error
}

func newPersistentTestConfig(persistentErr error) *persistentTestConfig {
	return &persistentTestConfig{
		runtimeOnlyTestConfig: newRuntimeOnlyTestConfig(),
		persistentErr:         persistentErr,
	}
}

func (c *persistentTestConfig) SetPersistent(key, val string) error {
	c.persistentCalls++
	if c.persistentErr != nil {
		return c.persistentErr
	}
	c.base.Set(key, val)
	return nil
}

func invokeConfigSet(t *testing.T, cfg core.IConfig, key, value string) configResp {
	t.Helper()

	req, err := json.Marshal(configSetReq{Key: key, Value: value})
	if err != nil {
		t.Fatalf("marshal request err=%v", err)
	}
	srv := &recordServer{nodeID: 1, cfg: cfg, cm: &stubConnManager{}}
	act := registerConfigSetActions(NewHandler(nil))
	ctx := core.WithServerContext(context.Background(), srv)

	act.Handle(ctx, newStubConn("caller"), newRequestHeader(9, 1), req)

	if len(srv.sent) != 1 {
		t.Fatalf("expected 1 response frame, got %d", len(srv.sent))
	}
	return decodeMgmtResponse[configResp](t, srv.sent[0].payload)
}

func TestConfigSet_PrefersPersistentSetter(t *testing.T) {
	cfg := newPersistentTestConfig(nil)

	resp := invokeConfigSet(t, cfg, configKeyNodeDisplayName, "Hub Alpha")

	if resp.Code != 1 {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if cfg.persistentCalls != 1 {
		t.Fatalf("expected SetPersistent to be called once, got %d", cfg.persistentCalls)
	}
	if cfg.setCalls != 0 {
		t.Fatalf("expected runtime Set fallback to be skipped, got %d calls", cfg.setCalls)
	}
	if val, _ := cfg.Get(configKeyNodeDisplayName); val != "Hub Alpha" {
		t.Fatalf("expected persisted value written, got %q", val)
	}
}

func TestConfigSet_FallsBackToRuntimeSetWhenPersistentUnsupported(t *testing.T) {
	cfg := newRuntimeOnlyTestConfig()

	resp := invokeConfigSet(t, cfg, configKeyNodeDisplayName, "Hub Beta")

	if resp.Code != 1 {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if cfg.setCalls != 1 {
		t.Fatalf("expected runtime Set to be called once, got %d", cfg.setCalls)
	}
	if val, _ := cfg.Get(configKeyNodeDisplayName); val != "Hub Beta" {
		t.Fatalf("expected runtime value written, got %q", val)
	}
}

func TestConfigSet_PersistentErrorReturnsFailure(t *testing.T) {
	cfg := newPersistentTestConfig(errors.New("disk full"))

	resp := invokeConfigSet(t, cfg, configKeyNodeDisplayName, "Hub Gamma")

	if resp.Code != 500 {
		t.Fatalf("expected failure response, got %+v", resp)
	}
	if resp.Msg != "disk full" {
		t.Fatalf("expected persistent error in response, got %+v", resp)
	}
	if cfg.persistentCalls != 1 {
		t.Fatalf("expected SetPersistent to be called once, got %d", cfg.persistentCalls)
	}
	if cfg.setCalls != 0 {
		t.Fatalf("expected runtime Set fallback to be skipped after persistent failure, got %d", cfg.setCalls)
	}
	if _, ok := cfg.Get(configKeyNodeDisplayName); ok {
		t.Fatalf("expected failed persistent write to leave config unchanged")
	}
}
