package flow

import (
	"strconv"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
)

const (
	cfgBaseDir         = "flow.base_dir"
	cfgMaxRetainedRuns = "flow.max_retained_runs"

	defaultMaxRetainedRuns = 32
)

type handlerConfig struct {
	BaseDir         string
	MaxRetainedRuns int
}

func loadConfig(cfg core.IConfig) handlerConfig {
	out := handlerConfig{
		BaseDir:         "./flows",
		MaxRetainedRuns: defaultMaxRetainedRuns,
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
	return out
}
