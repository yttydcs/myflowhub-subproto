package management

// 本文件覆盖 SubProto 中 `management` 模块里与 `deps` 相关的行为。

import (
	"testing"

	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
	"github.com/yttydcs/myflowhub-subproto/exec/runtimedeps"
)

func TestNewHandlerWithDepsUsesExplicitRegistry(t *testing.T) {
	reg := execcap.NewRegistry()

	h := NewHandlerWithDeps(runtimedeps.Deps{
		CapRegistry: reg,
	}, nil)

	if h.capRegistry != reg {
		t.Fatalf("capRegistry mismatch")
	}
}
