package exec

// 本文件承载 SubProto 中 `exec` 模块里与 `actions` 相关的逻辑。

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
