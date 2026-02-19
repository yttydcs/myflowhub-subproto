package management

import core "github.com/yttydcs/myflowhub-core"

func registerActions(h *ManagementHandler) []core.SubProcessAction {
	return []core.SubProcessAction{
		// echo
		registerEchoActions(h),
		// config
		registerConfigGetActions(h),
		registerConfigSetActions(h),
		registerConfigListActions(h),
		// nodes
		registerListNodesActions(h),
		registerListSubtreeActions(h),
	}
}

