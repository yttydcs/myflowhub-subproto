package exec

// Context: This file belongs to the SubProto implementation layer around actions.

import (
	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func registerActions(h *Handler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionCall, h.handleCall),
		kit.NewAction(actionCallResp, h.handleCallResp),
		kit.NewAction(actionCapSnapshot, h.handleCapSnapshot),
		kit.NewAction(actionCapUpsert, h.handleCapUpsert),
		kit.NewAction(actionCapWithdraw, h.handleCapWithdraw),
		kit.NewAction(actionCapHeartbeat, h.handleCapHeartbeat),
		kit.NewAction(actionCapSyncResp, h.handleCapSyncResp),
		kit.NewAction(actionCapQuery, h.handleCapQuery),
		kit.NewAction(actionCapQueryResp, h.handleCapQueryResp),
	}
}
