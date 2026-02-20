package exec

import (
	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func registerActions(h *Handler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionCall, h.handleCall),
		kit.NewAction(actionCallResp, h.handleCallResp),
	}
}
