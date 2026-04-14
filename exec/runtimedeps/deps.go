package runtimedeps

// 本文件承载 SubProto 中 `exec` 模块里与 `deps` 相关的逻辑。

import (
	core "github.com/yttydcs/myflowhub-core"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
)

// Deps carries shared runtime objects that should be wired explicitly by the host.
// Constructors may still derive safe defaults from cfg for backward compatibility.
type Deps struct {
	CapRegistry *execcap.Registry
	PermConfig  *permission.Config
}

func Resolve(cfg core.IConfig, deps Deps) Deps {
	if deps.CapRegistry == nil {
		deps.CapRegistry = execcap.SharedRegistry(cfg)
	}
	if deps.PermConfig == nil {
		if cfg != nil {
			deps.PermConfig = permission.SharedConfig(cfg)
		}
		if deps.PermConfig == nil {
			deps.PermConfig = permission.NewConfig(nil)
		}
	}
	return deps
}
