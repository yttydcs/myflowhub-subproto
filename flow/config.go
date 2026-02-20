package flow

import (
	"strings"

	core "github.com/yttydcs/myflowhub-core"
)

const (
	cfgBaseDir = "flow.base_dir"
)

type handlerConfig struct {
	BaseDir string
}

func loadConfig(cfg core.IConfig) handlerConfig {
	out := handlerConfig{
		BaseDir: "./flows",
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
	return out
}
