package flow

// Context: This file belongs to the SubProto implementation layer around config_test.

import (
	"testing"

	"github.com/yttydcs/myflowhub-core/config"
)

func TestLoadConfigRunArchiveDefaultsToOff(t *testing.T) {
	cfg := loadConfig(config.NewMap(nil))
	if cfg.ConfigErr != nil {
		t.Fatalf("unexpected config err: %v", cfg.ConfigErr)
	}
	if cfg.RunArchive {
		t.Fatalf("expected run archive disabled by default")
	}
	if cfg.RunArchiveBackend != runArchiveBackendOff {
		t.Fatalf("unexpected backend: got %q want %q", cfg.RunArchiveBackend, runArchiveBackendOff)
	}
}

func TestLoadConfigRunArchiveLegacyBoolEnablesFileBackend(t *testing.T) {
	cfg := loadConfig(config.NewMap(map[string]string{
		cfgRunArchive: "true",
	}))
	if cfg.ConfigErr != nil {
		t.Fatalf("unexpected config err: %v", cfg.ConfigErr)
	}
	if !cfg.RunArchive {
		t.Fatalf("expected run archive enabled")
	}
	if cfg.RunArchiveBackend != runArchiveBackendFile {
		t.Fatalf("unexpected backend: got %q want %q", cfg.RunArchiveBackend, runArchiveBackendFile)
	}
}

func TestLoadConfigRunArchiveBackendOverridesLegacyBool(t *testing.T) {
	cfg := loadConfig(config.NewMap(map[string]string{
		cfgRunArchive:        "true",
		cfgRunArchiveBackend: runArchiveBackendOff,
	}))
	if cfg.ConfigErr != nil {
		t.Fatalf("unexpected config err: %v", cfg.ConfigErr)
	}
	if cfg.RunArchive {
		t.Fatalf("expected backend override to disable archive")
	}
	if cfg.RunArchiveBackend != runArchiveBackendOff {
		t.Fatalf("unexpected backend: got %q want %q", cfg.RunArchiveBackend, runArchiveBackendOff)
	}
}

func TestLoadConfigRunArchiveBackendPG(t *testing.T) {
	cfg := loadConfig(config.NewMap(map[string]string{
		cfgRunArchiveBackend: runArchiveBackendPG,
	}))
	if cfg.ConfigErr != nil {
		t.Fatalf("unexpected config err: %v", cfg.ConfigErr)
	}
	if !cfg.RunArchive {
		t.Fatalf("expected pg backend to enable archive")
	}
	if cfg.RunArchiveBackend != runArchiveBackendPG {
		t.Fatalf("unexpected backend: got %q want %q", cfg.RunArchiveBackend, runArchiveBackendPG)
	}
}

func TestLoadConfigRunArchiveBackendRejectsUnsupportedValue(t *testing.T) {
	cfg := loadConfig(config.NewMap(map[string]string{
		cfgRunArchiveBackend: "sqlite",
	}))
	if cfg.ConfigErr == nil {
		t.Fatalf("expected config error")
	}
}
