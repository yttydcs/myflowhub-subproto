package topicbus

import (
	"testing"

	execcap "github.com/yttydcs/myflowhub-subproto/exec/capability"
	"github.com/yttydcs/myflowhub-subproto/exec/runtimedeps"
)

func TestNewTopicBusHandlerWithDepsUsesExplicitRegistry(t *testing.T) {
	reg := execcap.NewRegistry()

	h := NewTopicBusHandlerWithDeps(nil, runtimedeps.Deps{
		CapRegistry: reg,
	}, nil)

	if h.capRegistry != reg {
		t.Fatalf("capRegistry mismatch")
	}
}
