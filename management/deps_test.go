package management

// Context: This file belongs to the SubProto implementation layer around deps_test.

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
