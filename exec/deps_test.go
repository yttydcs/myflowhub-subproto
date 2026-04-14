package exec

// 本文件覆盖 SubProto 中 `exec` 模块里与 `deps` 相关的行为。

import (
	"testing"

	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
	"github.com/yttydcs/myflowhub-subproto/exec/runtimedeps"
)

func TestNewHandlerWithDepsUsesExplicitSharedObjects(t *testing.T) {
	reg := execcap.NewRegistry()
	perms := permission.NewConfig(nil)

	h := NewHandlerWithDeps(nil, runtimedeps.Deps{
		CapRegistry: reg,
		PermConfig:  perms,
	}, nil)

	if h.capRegistry != reg {
		t.Fatalf("capRegistry mismatch")
	}
	if h.permCfg != perms {
		t.Fatalf("permCfg mismatch")
	}
}
