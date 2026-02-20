package flow

import (
	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func registerActions(h *Handler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionSet, h.handleSet),
		kit.NewAction(actionRun, h.handleRun),
		kit.NewAction(actionStatus, h.handleStatus),
		kit.NewAction(actionList, h.handleList),
		kit.NewAction(actionGet, h.handleGet),
	}
}
