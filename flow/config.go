package flow

// 本文件承载 SubProto 中 `flow` 模块里与 `config` 相关的逻辑。

import (
	"fmt"
	"strconv"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
)

const (
	cfgBaseDir           = "flow.base_dir"
	cfgMaxRetainedRuns   = "flow.max_retained_runs"
	cfgRunArchive        = "flow.run_archive_enabled"
	cfgRunArchiveBackend = "flow.run_archive.backend"

	defaultMaxRetainedRuns = 32

	runArchiveBackendOff  = "off"
	runArchiveBackendFile = "file"
	runArchiveBackendPG   = "pg"
)

type handlerConfig struct {
	BaseDir           string
	MaxRetainedRuns   int
	RunArchive        bool
	RunArchiveBackend string
	ConfigErr         error
}

func loadConfig(cfg core.IConfig) handlerConfig {
	out := handlerConfig{
		BaseDir:           "./flows",
		MaxRetainedRuns:   defaultMaxRetainedRuns,
		RunArchiveBackend: runArchiveBackendOff,
	}
	if cfg == nil {
		return out
	}
	if raw, ok := cfg.Get(cfgBaseDir); ok {
		if s := strings.TrimSpace(raw); s != "" {
			out.BaseDir = s
		}
	}
	if out.BaseDir == "" {
		out.BaseDir = "./flows"
	}
	if raw, ok := cfg.Get(cfgMaxRetainedRuns); ok {
		if v, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && v > 0 {
			out.MaxRetainedRuns = v
		}
	}
	if out.MaxRetainedRuns <= 0 {
		out.MaxRetainedRuns = defaultMaxRetainedRuns
	}

	if raw, ok := cfg.Get(cfgRunArchiveBackend); ok {
		backend := strings.ToLower(strings.TrimSpace(raw))
		switch backend {
		case "":
			// fall through to legacy bool handling
		case runArchiveBackendOff:
			out.RunArchiveBackend = runArchiveBackendOff
			out.RunArchive = false
			return out
		case runArchiveBackendFile:
			out.RunArchiveBackend = runArchiveBackendFile
			out.RunArchive = true
			return out
		case runArchiveBackendPG:
			out.RunArchiveBackend = runArchiveBackendPG
			out.RunArchive = true
			return out
		default:
			out.ConfigErr = fmt.Errorf("unsupported %s", cfgRunArchiveBackend)
			return out
		}
	}
	if raw, ok := cfg.Get(cfgRunArchive); ok {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "1", "true", "yes", "on":
			out.RunArchive = true
			out.RunArchiveBackend = runArchiveBackendFile
		}
	}
	return out
}
