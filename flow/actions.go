package flow

// Context: This file belongs to the SubProto implementation layer around actions.

import (
	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func registerActions(h *Handler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionSet, h.handleSet),
		kit.NewAction(actionDelete, h.handleDelete),
		kit.NewAction(actionRun, h.handleRun),
		kit.NewAction(actionCancelRun, h.handleCancelRun),
		kit.NewAction(actionStatus, h.handleStatus),
		kit.NewAction(actionDetail, h.handleDetail),
		kit.NewAction(actionListRuns, h.handleListRuns),
		kit.NewAction(actionList, h.handleList),
		kit.NewAction(actionGet, h.handleGet),
	}
}
