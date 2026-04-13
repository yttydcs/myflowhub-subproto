package management

// Context: This file belongs to the SubProto implementation layer around action_node_info_test.

import (
	"testing"

	coreconfig "github.com/yttydcs/myflowhub-core/config"
)

func TestCollectNodeInfoItems_IncludesDisplayNameWhenConfigured(t *testing.T) {
	items := collectNodeInfoItems(7, coreconfig.NewMap(map[string]string{
		configKeyNodeDisplayName: "  Edge Node  ",
	}))

	if got := items["display_name"]; got != "Edge Node" {
		t.Fatalf("expected trimmed display_name, got %q", got)
	}
}

func TestCollectNodeInfoItems_OmitsBlankDisplayName(t *testing.T) {
	items := collectNodeInfoItems(7, coreconfig.NewMap(map[string]string{
		configKeyNodeDisplayName: "   ",
	}))

	if _, ok := items["display_name"]; ok {
		t.Fatalf("expected blank display_name to be omitted, got %v", items)
	}
}
