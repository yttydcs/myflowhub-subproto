package management

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func registerNodeInfoActions(h *ManagementHandler) core.SubProcessAction {
	return kit.NewAction(actionNodeInfo, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, _ json.RawMessage) {
		srv := core.ServerFromContext(ctx)
		if srv == nil {
			h.sendActionResp(ctx, conn, hdr, actionNodeInfoResp, nodeInfoResp{Code: 500, Msg: "no server context"})
			return
		}

		items := collectNodeInfoItems(srv.NodeID())
		h.sendActionResp(ctx, conn, hdr, actionNodeInfoResp, nodeInfoResp{Code: 1, Msg: "ok", Items: items})
	})
}

func collectNodeInfoItems(nodeID uint32) map[string]string {
	items := map[string]string{
		"node_id":    fmt.Sprintf("%d", nodeID),
		"app":        filepath.Base(os.Args[0]),
		"platform":   fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		"go_version": runtime.Version(),
	}

	if bi, ok := debug.ReadBuildInfo(); ok && bi != nil {
		items["module"] = strings.TrimSpace(bi.Main.Path)
		items["version"] = strings.TrimSpace(bi.Main.Version)

		for _, s := range bi.Settings {
			switch strings.TrimSpace(s.Key) {
			case "vcs.revision":
				items["commit"] = strings.TrimSpace(s.Value)
			case "vcs.time":
				items["vcs_time"] = strings.TrimSpace(s.Value)
			case "vcs.modified":
				items["vcs_modified"] = strings.TrimSpace(s.Value)
			}
		}
	}

	return items
}
