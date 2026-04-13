package management

// Context: This file belongs to the SubProto implementation layer around action_node_info.

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

const configKeyNodeDisplayName = "node.display_name"

func registerNodeInfoActions(h *ManagementHandler) core.SubProcessAction {
	return kit.NewAction(actionNodeInfo, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, _ json.RawMessage) {
		srv := core.ServerFromContext(ctx)
		if srv == nil {
			h.sendActionResp(ctx, conn, hdr, actionNodeInfoResp, nodeInfoResp{Code: 500, Msg: "no server context"})
			return
		}

		items := collectNodeInfoItems(srv.NodeID(), srv.Config())
		h.sendActionResp(ctx, conn, hdr, actionNodeInfoResp, nodeInfoResp{Code: 1, Msg: "ok", Items: items})
	})
}

func collectNodeInfoItems(nodeID uint32, cfg core.IConfig) map[string]string {
	items := map[string]string{
		"node_id":    fmt.Sprintf("%d", nodeID),
		"app":        filepath.Base(os.Args[0]),
		"platform":   fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		"go_version": runtime.Version(),
	}
	if displayName := nodeDisplayNameFromConfig(cfg); displayName != "" {
		items["display_name"] = displayName
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

func buildLocalNodeInfo(nodeID uint32, hasChildren bool, cfg core.IConfig) nodeInfo {
	info := nodeInfo{NodeID: nodeID, HasChildren: hasChildren}
	if displayName := nodeDisplayNameFromConfig(cfg); displayName != "" {
		info.DisplayName = displayName
	}
	return info
}

func nodeDisplayNameFromConfig(cfg core.IConfig) string {
	if cfg == nil {
		return ""
	}
	val, ok := cfg.Get(configKeyNodeDisplayName)
	if !ok {
		return ""
	}
	return strings.TrimSpace(val)
}
