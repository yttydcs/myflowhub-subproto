package flow

// 本文件承载 SubProto 中 `flow` 模块里与 `actions` 相关的逻辑。

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
