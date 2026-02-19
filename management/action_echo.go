package management

import (
	"context"
	"encoding/json"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func registerEchoActions(h *ManagementHandler) core.SubProcessAction {
	return kit.NewAction(actionNodeEcho, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
		var req nodeEchoReq
		if err := json.Unmarshal(data, &req); err != nil || strings.TrimSpace(req.Message) == "" {
			h.sendActionResp(ctx, conn, hdr, actionNodeEchoResp, nodeEchoResp{Code: 400, Msg: "invalid echo data"})
			return
		}
		h.log.Info("management node_echo", "conn", conn.ID(), "message", req.Message)
		h.sendActionResp(ctx, conn, hdr, actionNodeEchoResp, nodeEchoResp{Code: 1, Msg: "ok", Echo: req.Message})
	})
}

